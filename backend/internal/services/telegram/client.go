package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"one-mail/backend/internal/models"
)

type Client struct {
	httpClient *http.Client
}

type DeliveryError struct {
	Code       int
	RetryAfter time.Duration
	Permanent  bool
	Reason     string
}

func (e *DeliveryError) Error() string { return e.Reason }

var (
	styleTagRegex     = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	multiNewlineRegex = regexp.MustCompile(`\n\s*\n\s*\n+`)
	entityRegex       = regexp.MustCompile(`&#(\d+);`)
	hexEntityRegex    = regexp.MustCompile(`&#x([0-9a-fA-F]+);`)
)

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) SendNewEmailWithConfig(ctx context.Context, email *models.Email, accountEmail, botToken, chatID string) error {
	text := c.formatMessage(email, accountEmail)
	return c.sendMessage(ctx, botToken, chatID, text)
}

func (c *Client) formatMessage(email *models.Email, accountEmail string) string {
	var sb strings.Builder
	sb.WriteString("📧 *新邮件通知*\n\n")
	from := email.From
	if email.FromName != "" {
		from = fmt.Sprintf("%s (%s)", email.FromName, email.From)
	}
	writeField := func(label, value string, limit int) {
		sb.WriteString("*" + label + ":* ")
		sb.WriteString(escapeMarkdownLimit(value, limit))
		sb.WriteByte('\n')
	}
	writeField("账户", accountEmail, 256)
	writeField("发件人", from, 512)
	writeField("收件人", email.To, 256)
	if email.Subject != "" {
		writeField("主题", email.Subject, 512)
	}
	writeField("时间", email.Date.Format("2006-01-02 15:04:05"), 64)
	if email.HasAttachment {
		sb.WriteString("*附件:* 有\n")
	}
	sb.WriteString("\n")
	body := email.BodyText
	if body == "" && email.BodyHTML != "" {
		body = stripHTML(email.BodyHTML)
	}
	if body != "" {
		sb.WriteString("*正文:*\n")
		units := 0
		for _, character := range sb.String() {
			units++
			if character > 0xffff {
				units++
			}
		}
		sb.WriteString(escapeMarkdownLimit(body, min(3500, 4096-units)))
	}
	return sb.String()
}

func (c *Client) sendMessage(ctx context.Context, botToken, chatID, text string) error {
	return c.send(ctx, botToken, chatID, text, "MarkdownV2")
}

func (c *Client) send(ctx context.Context, botToken, chatID, text, parseMode string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to encode telegram request")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return &DeliveryError{Permanent: true, Reason: "invalid telegram request"}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &DeliveryError{Reason: "telegram request failed"}
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		ErrorCode   int    `json:"error_code"`
		Parameters  struct {
			RetryAfter int64 `json:"retry_after"`
		} `json:"parameters"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil && resp.StatusCode == http.StatusOK {
		return &DeliveryError{Reason: "invalid telegram response"}
	}

	if resp.StatusCode != http.StatusOK || !result.OK {
		code := result.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		reason := "telegram request failed"
		switch code {
		case 400:
			reason = "telegram message rejected"
		case 401:
			reason = "telegram bot token rejected"
		case 403:
			reason = "telegram chat access denied"
		case 429:
			reason = "telegram rate limited"
		}
		retryAfter := time.Duration(0)
		if result.Parameters.RetryAfter > 0 {
			retryAfter = time.Duration(min(result.Parameters.RetryAfter, math.MaxInt64/int64(time.Second))) * time.Second
		}
		return &DeliveryError{
			Code: code, RetryAfter: retryAfter, Reason: reason,
			Permanent: code >= 400 && code < 500 && code != 408 && code != 429,
		}
	}

	return nil
}

func (c *Client) TestConnection(ctx context.Context, botToken, chatID string) error {
	return c.send(ctx, botToken, chatID, "✅ One-Mail Telegram 通知测试成功！", "")
}

func escapeMarkdown(text string) string {
	return escapeMarkdownLimit(text, math.MaxInt32)
}

func escapeMarkdownLimit(text string, limit int) string {
	var result strings.Builder
	units := 0
	for offset, character := range text {
		if character < ' ' && character != '\n' && character != '\t' {
			continue
		}
		escaped := strings.ContainsRune("\\_*[]()~`>#+-=|{}.!", character)
		width := 1
		if character > 0xffff {
			width++
		}
		if escaped {
			width++
		}
		last := offset+utf8.RuneLen(character) >= len(text)
		if units+width > limit || (!last && units+width == limit) {
			if units < limit {
				result.WriteRune('…')
			}
			break
		}
		if escaped {
			result.WriteByte('\\')
		}
		result.WriteRune(character)
		units += width
	}
	return result.String()
}

func stripHTML(html string) string {
	html = styleTagRegex.ReplaceAllString(html, "")

	html = decodeHTMLEntities(html)

	html = strings.ReplaceAll(html, "<br>", "\n")
	html = strings.ReplaceAll(html, "<br/>", "\n")
	html = strings.ReplaceAll(html, "<br />", "\n")
	html = strings.ReplaceAll(html, "</p>", "\n")
	html = strings.ReplaceAll(html, "</div>", "\n")

	var result strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}

	text := result.String()

	text = multiNewlineRegex.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)

	return text
}

func decodeHTMLEntities(s string) string {
	replacements := map[string]string{
		"&nbsp;":   " ",
		"&middot;": "·",
		"&copy;":   "©",
		"&amp;":    "&",
		"&lt;":     "<",
		"&gt;":     ">",
		"&quot;":   "\"",
		"&apos;":   "'",
		"&reg;":    "®",
		"&trade;":  "™",
		"&hellip;": "…",
		"&ndash;":  "–",
		"&mdash;":  "—",
		"&lsquo;":  "\u2018",
		"&rsquo;":  "\u2019",
		"&ldquo;":  "\u201c",
		"&rdquo;":  "\u201d",
		"&bull;":   "•",
		"&deg;":    "°",
		"&divide;": "÷",
		"&times;":  "×",
		"&euro;":   "€",
		"&pound;":  "£",
		"&yen;":    "¥",
	}

	result := s
	for entity, replacement := range replacements {
		result = strings.ReplaceAll(result, entity, replacement)
	}

	result = entityRegex.ReplaceAllStringFunc(result, func(match string) string {
		matches := entityRegex.FindStringSubmatch(match)
		if len(matches) > 1 {
			code := matches[1]
			var num int
			fmt.Sscanf(code, "%d", &num)
			if num > 0 && num < 0x10FFFF {
				return string(rune(num))
			}
		}
		return match
	})

	result = hexEntityRegex.ReplaceAllStringFunc(result, func(match string) string {
		matches := hexEntityRegex.FindStringSubmatch(match)
		if len(matches) > 1 {
			code := matches[1]
			var num int
			fmt.Sscanf(code, "%x", &num)
			if num > 0 && num < 0x10FFFF {
				return string(rune(num))
			}
		}
		return match
	})

	return result
}
