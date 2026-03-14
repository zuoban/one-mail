package imap

import (
	"bytes"
	"fmt"
"math"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"

	"one-mail/backend/internal/models"
)

type Client struct {
	account  *models.EmailAccount
	client   *imapclient.Client
	username string
	password string
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
		account:  account,
		username: account.Username,
		password: account.Password,
	}
}

func (c *Client) Connect() error {
	host := fmt.Sprintf("%s:%d", c.account.IMAPHost, c.account.IMAPPort)

	conn, err := imapclient.DialTLS(host, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to IMAP server: %w", err)
	}

	if err := conn.Login(c.username, c.password).Wait(); err != nil {
		conn.Close()
		return fmt.Errorf("failed to login: %w", err)
	}

	c.client = conn
	return nil
}

func (c *Client) Disconnect() error {
	if c.client == nil {
		return nil
	}

	if err := c.client.Logout().Wait(); err != nil {
		c.client.Close()
		return err
	}

	c.client.Close()
	c.client = nil
	return nil
}

func (c *Client) IsConnected() bool {
	return c.client != nil
}

func (c *Client) ListMailboxes() ([]string, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	mailboxes, err := c.client.List("", "%", nil).Collect()
	if err != nil {
		return nil, err
	}

	var result []string
	for _, mbox := range mailboxes {
		result = append(result, mbox.Mailbox)
	}

	return result, nil
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
	IsRead        bool
	IsStarred     bool
	UID           uint
}

func parseMessageBody(raw []byte) (bodyText string, bodyHTML string) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if mr == nil {
		return "", ""
	}
	_ = err // CreateReader may return an error but still provide a usable Reader

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			break
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			contentType, _, _ := h.ContentType()
			b, readErr := io.ReadAll(p.Body)
			if readErr != nil {
				continue
			}

			if strings.HasPrefix(strings.ToLower(contentType), "text/html") {
				if bodyHTML == "" {
					bodyHTML = string(b)
				}
			} else if strings.HasPrefix(strings.ToLower(contentType), "text/plain") {
				if bodyText == "" {
					bodyText = string(b)
				}
			} else {
				if bodyText == "" {
					bodyText = string(b)
				}
			}
		case *mail.AttachmentHeader:
			// ignore attachments
		default:
			// ignore unknown parts
		}
	}

	return bodyText, bodyHTML
}

type FolderInfo struct {
	Name string
}

func (c *Client) ListFolders() ([]FolderInfo, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	mailboxes, err := c.client.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}

	var result []FolderInfo
	for _, mbox := range mailboxes {
		info := FolderInfo{
			Name: mbox.Mailbox,
		}
		result = append(result, info)
	}

	return result, nil
}

func (c *Client) FetchEmails(folder string, since time.Time, limit int) ([]*EmailSummary, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	selectedMbox, err := c.client.Select(folder, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to select mailbox: %w", err)
	}

	if selectedMbox.NumMessages == 0 {
		return []*EmailSummary{}, nil
	}

	total := int(selectedMbox.NumMessages)
	start := uint32(total) - uint32(limit) + 1
	if start < 1 {
		start = 1
	}

	seqSet := imap.SeqSet{}
	seqSet.AddRange(start, uint32(total))

	criteria := imap.SearchCriteria{}
	criteria.SeqNum = []imap.SeqSet{seqSet}

	data, err := c.client.Search(&criteria, nil).Wait()
	if err != nil {
		return nil, err
	}

	seqNums := data.AllSeqNums()
	if len(seqNums) == 0 {
		return []*EmailSummary{}, nil
	}

	if len(seqNums) > limit {
		seqNums = seqNums[len(seqNums)-limit:]
	}

	numSet := imap.SeqSet{}
	for _, num := range seqNums {
		numSet.AddNum(num)
	}

	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetchOptions := &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		UID:      true,
		BodySection: []*imap.FetchItemBodySection{
			bodySection,
		},
	}

	messages, err := c.client.Fetch(numSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	var results []*EmailSummary
	for _, msg := range messages {
		email := &EmailSummary{
			UID: uint(msg.UID),
		}

		if msg.Envelope != nil {
			email.Subject = msg.Envelope.Subject
			email.Date = msg.Envelope.Date

			if len(msg.Envelope.From) > 0 {
				from := msg.Envelope.From[0]
				if from.Mailbox != "" && from.Host != "" {
					email.From = fmt.Sprintf("%s@%s", from.Mailbox, from.Host)
				}
				if from.Name != "" {
					email.FromName = from.Name
				}
			}

			if len(msg.Envelope.To) > 0 {
				to := msg.Envelope.To[0]
				if to.Mailbox != "" && to.Host != "" {
					email.To = fmt.Sprintf("%s@%s", to.Mailbox, to.Host)
				}
			}

			if msg.Envelope.MessageID != "" {
				email.MessageID = msg.Envelope.MessageID
			} else {
				email.MessageID = fmt.Sprintf("%d", msg.UID)
			}
		}

		raw := msg.FindBodySection(bodySection)
		if len(raw) > 0 {
			email.BodyText, email.BodyHTML = parseMessageBody(raw)
		}

		results = append(results, email)
	}

	return results, nil
}

func (c *Client) FetchEmailsIncremental(folder string, lastUID uint, limit int) ([]*EmailSummary, uint, error) {
	if c.client == nil {
		return nil, 0, fmt.Errorf("not connected")
	}

	selectedMbox, err := c.client.Select(folder, nil).Wait()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to select mailbox: %w", err)
	}

	if selectedMbox.NumMessages == 0 {
		return []*EmailSummary{}, 0, nil
	}

	uidSet := imap.UIDSet{}
	if lastUID > 0 {
		uidSet.AddRange(imap.UID(lastUID)+1, math.MaxUint32)
	} else {
		uidSet.AddRange(1, math.MaxUint32)
	}

	criteria := imap.SearchCriteria{}
	criteria.UID = []imap.UIDSet{uidSet}

	data, err := c.client.Search(&criteria, nil).Wait()
	if err != nil {
		return nil, 0, err
	}

	uids := data.AllUIDs()
	if len(uids) == 0 {
		return []*EmailSummary{}, lastUID, nil
	}

	if limit > 0 && len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}

	fetchUIDSet := imap.UIDSet{}
	for _, uid := range uids {
		fetchUIDSet.AddNum(uid)
	}

	fetchOptions := &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		UID:      true,
	}

	messages, err := c.client.Fetch(fetchUIDSet, fetchOptions).Collect()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch messages: %w", err)
	}

	var results []*EmailSummary
	var maxUID uint

	for _, msg := range messages {
		email := &EmailSummary{
			UID: uint(msg.UID),
		}

		if uint(msg.UID) > maxUID {
			maxUID = uint(msg.UID)
		}

		if msg.Envelope != nil {
			email.Subject = msg.Envelope.Subject
			email.Date = msg.Envelope.Date

			if len(msg.Envelope.From) > 0 {
				from := msg.Envelope.From[0]
				if from.Mailbox != "" && from.Host != "" {
					email.From = fmt.Sprintf("%s@%s", from.Mailbox, from.Host)
				}
				if from.Name != "" {
					email.FromName = from.Name
				}
			}

			if len(msg.Envelope.To) > 0 {
				to := msg.Envelope.To[0]
				if to.Mailbox != "" && to.Host != "" {
					email.To = fmt.Sprintf("%s@%s", to.Mailbox, to.Host)
				}
			}

			if msg.Envelope.MessageID != "" {
				email.MessageID = msg.Envelope.MessageID
			} else {
				email.MessageID = fmt.Sprintf("%d", msg.UID)
			}
		}

		if msg.Flags != nil {
			for _, flag := range msg.Flags {
				if flag == imap.FlagSeen {
					email.IsRead = true
				}
				if flag == imap.FlagFlagged {
					email.IsStarred = true
				}
			}
		}

		results = append(results, email)
	}

	if maxUID == 0 {
		maxUID = lastUID
	}

	return results, maxUID, nil
}

func (c *Client) FetchEmailBody(folder string, uid uint) (bodyText string, bodyHTML string, err error) {
	if c.client == nil {
		return "", "", fmt.Errorf("not connected")
	}

	_, err = c.client.Select(folder, nil).Wait()
	if err != nil {
		return "", "", fmt.Errorf("failed to select mailbox: %w", err)
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))
	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetchOptions := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}

	messages, err := c.client.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch message body: %w", err)
	}

	if len(messages) == 0 {
		return "", "", fmt.Errorf("message not found")
	}

	raw := messages[0].FindBodySection(bodySection)
	if len(raw) > 0 {
		bodyText, bodyHTML = parseMessageBody(raw)
	}

	return bodyText, bodyHTML, nil
}

func (c *Client) MarkAsRead(uid uint, folder string) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))
	storeCmd := c.client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagSeen},
	}, nil)

	_ = storeCmd.Close()
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

	conn, err := imapclient.DialTLS(fmt.Sprintf("%s:%d", host, port), nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	defer conn.Close()

	if err := conn.Login(username, password).Wait(); err != nil {
		return fmt.Errorf("failed to login: %v", err)
	}

	if err := conn.Logout().Wait(); err != nil {
		return fmt.Errorf("failed to logout: %v", err)
	}

	return nil
}
