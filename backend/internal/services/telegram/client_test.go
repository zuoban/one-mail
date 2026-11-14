package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"one-mail/backend/internal/models"
)

func TestStripHTMLAndDecodeEntities(t *testing.T) {
	rawHTML := `<style>.a { color: red; }</style><div>Hello&nbsp;World&#33;<br/>Line&#x20;2</div><p>End</p>`
	got := stripHTML(rawHTML)
	want := "Hello World!\nLine 2\nEnd"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	escaped := escapeMarkdown("v1.0_test!")
	if escaped != `v1\.0\_test\!` {
		t.Fatalf("unexpected markdown escape: %q", escaped)
	}
}

func TestMessageBudgetIncludesHeadersAndEscapedUnicode(t *testing.T) {
	client := NewClient()
	email := &models.Email{
		From: strings.Repeat("sender", 1000), FromName: strings.Repeat("😀\\_*", 1000),
		To: strings.Repeat("recipient", 1000), Subject: strings.Repeat("主题😀\\_*", 2000),
		BodyText: strings.Repeat("😀\\_*", 10000), HasAttachment: true,
	}
	message := client.formatMessage(email, strings.Repeat("account", 1000))
	if units := len(utf16.Encode([]rune(message))); units > 4096 {
		t.Fatalf("complete message exceeded Telegram's budget: %d", units)
	}
	for _, field := range []string{"*账户:*", "*发件人:*", "*收件人:*", "*主题:*", "*时间:*", "*附件:*", "*正文:*"} {
		if !strings.Contains(message, field) {
			t.Fatalf("truncation discarded field %q", field)
		}
	}
	if got := escapeMarkdown(`\_*`); got != `\\\_\*` {
		t.Fatalf("backslash was not escaped safely: %q", got)
	}
	for limit := 1; limit <= 10; limit++ {
		value := escapeMarkdownLimit(strings.Repeat(`\😀*`, 20), limit)
		if len(utf16.Encode([]rune(value))) > limit || strings.HasSuffix(value, `\`) {
			t.Fatalf("invalid truncated Markdown for budget %d: %q", limit, value)
		}
	}
}

func TestTelegramErrorsClassifyPermanentFailuresAndRateLimits(t *testing.T) {
	for _, code := range []int{400, 401, 403, 408, 429, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: code, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"private-token payload","parameters":{"retry_after":45}}`)),
				}, nil
			})}}
			err := client.sendMessage(context.Background(), "private-token", "chat", "message")
			var deliveryErr *DeliveryError
			if !errors.As(err, &deliveryErr) || deliveryErr.Code != code || strings.Contains(err.Error(), "private") {
				t.Fatalf("missing safe structured response: %v", err)
			}
			permanent := code >= 400 && code < 500 && code != 408 && code != 429
			if deliveryErr.Permanent != permanent || deliveryErr.RetryAfter.Seconds() != 45 {
				t.Fatalf("wrong failure policy: %+v", deliveryErr)
			}
		})
	}
}

func TestNonJSONTelegramRejectionsKeepHTTPFailurePolicy(t *testing.T) {
	for _, code := range []int{401, 429, 500} {
		client := &Client{httpClient: &http.Client{Transport: telegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("upstream unavailable")), Header: make(http.Header)}, nil
		})}}
		var deliveryErr *DeliveryError
		if err := client.sendMessage(context.Background(), "token", "chat", "message"); !errors.As(err, &deliveryErr) || deliveryErr.Code != code || deliveryErr.Permanent != (code == 401) {
			t.Fatalf("HTTP failure classification was lost for %d: %v", code, err)
		}
	}
}
