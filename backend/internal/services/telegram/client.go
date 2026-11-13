package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"one-mail/backend/database"
	"one-mail/backend/internal/models"
)

type Client struct {
	httpClient *http.Client
}

var (
	globalClient      *Client
	clientOnce        sync.Once
	configCacheMu     sync.RWMutex
	cachedConfig      *models.TelegramConfig
	configCached      bool
	styleTagRegex     = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	multiNewlineRegex = regexp.MustCompile(`\n\s*\n\s*\n+`)
	entityRegex       = regexp.MustCompile(`&#(\d+);`)
	hexEntityRegex    = regexp.MustCompile(`&#x([0-9a-fA-F]+);`)
)

func GetClient() *Client {
	clientOnce.Do(func() {
		globalClient = &Client{
			httpClient: &http.Client{Timeout: 10 * time.Second},
		}
	})
	return globalClient
}

// InvalidateConfigCache clears the in-memory Telegram config cache so the next send reloads from DB.
func InvalidateConfigCache() {
	configCacheMu.Lock()
	cachedConfig = nil
	configCached = false
	configCacheMu.Unlock()
}

func getConfig() *models.TelegramConfig {
	configCacheMu.RLock()
	if configCached {
		cfg := cachedConfig
		configCacheMu.RUnlock()
		return cfg
	}
	configCacheMu.RUnlock()

	db := database.GetDB()
	var cfg models.TelegramConfig
	if err := db.First(&cfg).Error; err != nil {
		return nil
	}

	configCacheMu.Lock()
	cfgCopy := cfg
	cachedConfig = &cfgCopy
	configCached = true
	configCacheMu.Unlock()
	return &cfgCopy
}

func (c *Client) IsEnabled() bool {
	cfg := getConfig()
	return cfg != nil && cfg.Enabled && cfg.BotToken != "" && cfg.ChatID != ""
}

func (c *Client) SendNewEmail(email *models.Email, accountEmail string) error {
	cfg := getConfig()
	if cfg == nil || !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == "" {
		return nil
	}

	text := c.formatMessage(email, accountEmail)
	return c.sendMessage(cfg.BotToken, cfg.ChatID, text)
}

func (c *Client) SendNewEmailWithConfig(email *models.Email, accountEmail, botToken, chatID string) error {
	text := c.formatMessage(email, accountEmail)
	return c.sendMessage(botToken, chatID, text)
}

func (c *Client) formatMessage(email *models.Email, accountEmail string) string {
	var sb strings.Builder

	sb.WriteString("📧 *新邮件通知*\n\n")

	from := email.From
	if email.FromName != "" {
		from = fmt.Sprintf("%s (%s)", email.FromName, email.From)
	}
	sb.WriteString(fmt.Sprintf("*账户:* %s\n", escapeMarkdown(accountEmail)))
	sb.WriteString(fmt.Sprintf("*发件人:* %s\n", escapeMarkdown(from)))
	sb.WriteString(fmt.Sprintf("*收件人:* %s\n", escapeMarkdown(email.To)))

	if email.Subject != "" {
		sb.WriteString(fmt.Sprintf("*主题:* %s\n", escapeMarkdown(email.Subject)))
	}

	sb.WriteString(fmt.Sprintf("*时间:* %s\n", escapeMarkdown(email.Date.Format("2006-01-02 15:04:05"))))

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
		maxLen := 3500
		if utf8.RuneCountInString(body) > maxLen {
			body = string([]rune(body)[:maxLen]) + "..."
		}
		sb.WriteString(escapeMarkdown(body))
	}

	return sb.String()
}

func (c *Client) sendMessage(botToken, chatID, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("telegram api error: %s", result.Description)
	}

	return nil
}

func (c *Client) TestConnection(botToken, chatID string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]interface{}{
		"chat_id": chatID,
		"text":    "✅ One-Mail Telegram 通知测试成功！",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("telegram api error: %s", result.Description)
	}

	return nil
}

func escapeMarkdown(text string) string {
	var result strings.Builder
	for _, r := range text {
		switch r {
		case '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!':
			result.WriteRune('\\')
			result.WriteRune(r)
		default:
			result.WriteRune(r)
		}
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

type notifyJob struct {
	email        models.Email
	accountEmail string
}

var (
	notifyQueue     chan notifyJob
	notifyQueueOnce sync.Once
)

func initNotifyQueue() {
	notifyQueueOnce.Do(func() {
		notifyQueue = make(chan notifyJob, 128)
		// 2 个限流 worker，防止首次同步大量邮件时并发打爆 Telegram API
		for range 2 {
			go func() {
				client := GetClient()
				for job := range notifyQueue {
					emailCopy := job.email
					if err := client.SendNewEmail(&emailCopy, job.accountEmail); err != nil {
						log.Printf("Failed to send telegram notification: %v", err)
					}
				}
			}()
		}
	})
}

func SendNewEmailAsync(email *models.Email, accountEmail string) {
	if email == nil {
		return
	}
	initNotifyQueue()
	select {
	case notifyQueue <- notifyJob{email: *email, accountEmail: accountEmail}:
	default:
		log.Printf("Telegram notify queue full, dropping notification for subject=%q", email.Subject)
	}
}
