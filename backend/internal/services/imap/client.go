package imap

import (
	"fmt"
	"one-mail/backend/internal/models"
	"time"
)

type Client struct {
	account *models.EmailAccount
}

var ProviderConfigs = map[string]struct {
	IMAPHost string
	IMAPPort int
	SMTPHost string
	SMTPPort int
}{
	"gmail": {
		IMAPHost: "imap.gmail.com",
		IMAPPort: 993,
		SMTPHost: "smtp.gmail.com",
		SMTPPort: 587,
	},
	"qq": {
		IMAPHost: "imap.qq.com",
		IMAPPort: 993,
		SMTPHost: "smtp.qq.com",
		SMTPPort: 587,
	},
	"outlook": {
		IMAPHost: "outlook.office365.com",
		IMAPPort: 993,
		SMTPHost: "smtp.office365.com",
		SMTPPort: 587,
	},
	"qq-work": {
		IMAPHost: "exmail.qq.com",
		IMAPPort: 993,
		SMTPHost: "smtp.exmail.qq.com",
		SMTPPort: 587,
	},
	"163": {
		IMAPHost: "imap.163.com",
		IMAPPort: 993,
		SMTPHost: "smtp.163.com",
		SMTPPort: 587,
	},
	"custom": {
		IMAPHost: "",
		IMAPPort: 993,
		SMTPHost: "",
		SMTPPort: 587,
	},
}

func NewClient(account *models.EmailAccount) *Client {
	return &Client{
		account: account,
	}
}

func (c *Client) Connect() error {
	return nil
}

func (c *Client) Disconnect() error {
	return nil
}

func (c *Client) ListMailboxes() ([]string, error) {
	return []string{"INBOX", "Sent", "Drafts", "Trash"}, nil
}

type EmailSummary struct {
	MessageID     string
	From          string
	FromName      string
	To            string
	Subject       string
	Date          time.Time
	BodyText      string
	BodyHTML      string
	HasAttachment bool
	UID           uint
}

func (c *Client) FetchEmails(folder string, since time.Time, limit int) ([]*EmailSummary, error) {
	return []*EmailSummary{}, nil
}

func (c *Client) MarkAsRead(uid uint, folder string) error {
	return nil
}

func (c *Client) TestConnection() error {
	host := c.account.IMAPHost
	port := c.account.IMAPPort
	username := c.account.Username
	password := c.account.Password

	if host == "" || username == "" || password == "" {
		return fmt.Errorf("invalid configuration")
	}

	fmt.Printf("Testing connection to %s:%d with username %s\n", host, port, username)

	return nil
}
