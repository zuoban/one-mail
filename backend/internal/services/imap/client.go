package imap

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"

	"one-mail/backend/internal/models"
)

type Client struct {
	account      *models.EmailAccount
	client       *imapclient.Client
	username     string
	password     string
	lastActivity time.Time
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

const defaultConnectTimeout = 15 * time.Second

func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "authenticationfailed") ||
		strings.Contains(s, "authentication failed") ||
		strings.Contains(s, "invalid credentials") ||
		strings.Contains(s, "failed to login") ||
		strings.Contains(s, "login fail") ||
		strings.Contains(s, "bad credentials") ||
		strings.Contains(s, "application-specific password") ||
		strings.Contains(s, "web login required")
}

func (c *Client) Connect() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()
	return c.ConnectContext(ctx)
}

func (c *Client) ConnectContext(ctx context.Context) error {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultConnectTimeout)
		defer cancel()
	}

	host := net.JoinHostPort(c.account.IMAPHost, strconv.Itoa(c.account.IMAPPort))
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		},
		Config: &tls.Config{
			ServerName: c.account.IMAPHost,
			NextProtos: []string{"imap"},
		},
	}

	tlsConn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return fmt.Errorf("failed to connect to IMAP server: %w", err)
	}

	conn := imapclient.New(tlsConn, nil)

	loginDone := make(chan error, 1)
	go func() {
		loginDone <- conn.Login(c.username, c.password).Wait()
	}()

	select {
	case err = <-loginDone:
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("failed to login: %w", err)
		}
	case <-ctx.Done():
		_ = conn.Close()
		return fmt.Errorf("failed to login: %w", ctx.Err())
	}

	c.client = conn
	c.lastActivity = time.Now()
	return nil
}

func (c *Client) Disconnect() error {
	if c.client == nil {
		return nil
	}

	client := c.client
	c.client = nil

	done := make(chan error, 1)
	go func() {
		done <- client.Logout().Wait()
	}()

	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
	}

	_ = client.Close()
	return err
}

// ForceClose immediately closes the underlying TCP/TLS connection to unblock any stalled IMAP command.
func (c *Client) ForceClose() {
	if c.client == nil {
		return
	}
	_ = c.client.Close()
	c.client = nil
}

func (c *Client) IsConnected() bool {
	if c.client == nil {
		return false
	}

	// 刚活跃过的连接跳过无意义的 NOOP RTT
	if !c.lastActivity.IsZero() && time.Since(c.lastActivity) < 30*time.Second {
		return true
	}

	done := make(chan error, 1)
	go func() {
		done <- c.client.Noop().Wait()
	}()

	select {
	case err := <-done:
		if err == nil {
			c.lastActivity = time.Now()
			return true
		}
		c.ForceClose()
		return false
	case <-time.After(5 * time.Second):
		c.ForceClose()
		return false
	}
}

type EmailSummary struct {
	MessageID     string
	From          string
	FromName      string
	To            string
	Subject       string
	Date          time.Time
	HasAttachment bool
	IsRead        bool
	IsStarred     bool
	UID           uint
}

func computeLatestSeqRange(total uint32, limit int) (uint32, uint32, bool) {
	if total == 0 {
		return 0, 0, false
	}
	if limit <= 0 {
		limit = 50
	}
	start := uint32(1)
	if total > uint32(limit) {
		start = total - uint32(limit) + 1
	}
	return start, total, true
}

func (c *Client) FetchLatestEmails(folder string, limit int) ([]*EmailSummary, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	if folder == "" {
		folder = "INBOX"
	}
	if limit <= 0 {
		limit = 50
	}

	var messages []*imapclient.FetchMessageBuffer
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second * time.Duration(1<<uint(attempt-1)))
		}

		selectedMbox, err := c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			if isTemporaryIMAPError(err) {
				continue
			}
			return nil, fmt.Errorf("failed to select mailbox: %w", err)
		}
		c.lastActivity = time.Now()

		start, end, ok := computeLatestSeqRange(selectedMbox.NumMessages, limit)
		if !ok {
			return []*EmailSummary{}, nil
		}

		var seqSet imap.SeqSet
		seqSet.AddRange(start, end)

		fetchOptions := &imap.FetchOptions{
			Envelope: true,
			Flags:    true,
			UID:      true,
		}

		messages, err = c.client.Fetch(seqSet, fetchOptions).Collect()
		if err != nil {
			if isTemporaryIMAPError(err) {
				continue
			}
			return nil, fmt.Errorf("failed to fetch messages: %w", err)
		}
		break
	}

	results := make([]*EmailSummary, 0, len(messages))
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
				email.MessageID = fmt.Sprintf("uid-%d", msg.UID)
			}
		} else {
			email.MessageID = fmt.Sprintf("uid-%d", msg.UID)
		}

		for _, flag := range msg.Flags {
			if flag == imap.FlagSeen {
				email.IsRead = true
			} else if flag == imap.FlagFlagged {
				email.IsStarred = true
			}
		}

		results = append(results, email)
	}

	slices.SortFunc(results, func(a, b *EmailSummary) int {
		if !a.Date.Equal(b.Date) {
			if b.Date.After(a.Date) {
				return 1
			}
			return -1
		}
		if b.UID > a.UID {
			return 1
		}
		if b.UID < a.UID {
			return -1
		}
		return 0
	})

	c.lastActivity = time.Now()
	return results, nil
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
func isTemporaryIMAPError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "system busy") ||
		strings.Contains(message, "temporarily unavailable") ||
		strings.Contains(message, "try again")
}

type bodyPartSelection struct {
	isMultipart bool
	textPath    []int
	htmlPath    []int
}

func selectBodyParts(bs imap.BodyStructure) bodyPartSelection {
	var sel bodyPartSelection
	if bs == nil {
		return sel
	}
	if _, ok := bs.(*imap.BodyStructureMultiPart); ok {
		sel.isMultipart = true
	}
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		sp, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		if disp := sp.Disposition(); disp != nil && strings.EqualFold(disp.Value, "attachment") {
			return true
		}
		mt := sp.MediaType()
		if mt == "text/plain" && len(sel.textPath) == 0 {
			sel.textPath = append([]int(nil), path...)
		} else if mt == "text/html" && len(sel.htmlPath) == 0 {
			sel.htmlPath = append([]int(nil), path...)
		}
		return true
	})
	return sel
}

func decodeBodyPart(mimeHeaderBytes, bodyBytes []byte) string {
	if len(bodyBytes) == 0 {
		return ""
	}
	if len(mimeHeaderBytes) == 0 {
		return string(bodyBytes)
	}

	var buf bytes.Buffer
	buf.Write(mimeHeaderBytes)
	if !bytes.HasSuffix(mimeHeaderBytes, []byte("\r\n\r\n")) && !bytes.HasSuffix(mimeHeaderBytes, []byte("\n\n")) {
		if bytes.HasSuffix(mimeHeaderBytes, []byte("\r\n")) || bytes.HasSuffix(mimeHeaderBytes, []byte("\n")) {
			buf.WriteString("\r\n")
		} else {
			buf.WriteString("\r\n\r\n")
		}
	}
	buf.Write(bodyBytes)

	mr, err := mail.CreateReader(&buf)
	if mr != nil {
		p, nextErr := mr.NextPart()
		if nextErr == nil && p != nil {
			decoded, readErr := io.ReadAll(p.Body)
			if readErr == nil {
				return string(decoded)
			}
		}
	}
	_ = err
	return string(bodyBytes)
}

func (c *Client) FetchEmailBody(folder string, uid uint) (bodyText string, bodyHTML string, err error) {
	if c.client == nil {
		return "", "", fmt.Errorf("not connected")
	}

	_, err = c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return "", "", fmt.Errorf("failed to select mailbox: %w", err)
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))

	// 先拉取 BODYSTRUCTURE，仅按需抓取 text/plain 与 text/html part，避免下载大附件
	bsMessages, err := c.client.Fetch(uidSet, &imap.FetchOptions{
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}).Collect()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch message structure: %w", err)
	}
	if len(bsMessages) == 0 {
		return "", "", fmt.Errorf("message not found")
	}

	sel := selectBodyParts(bsMessages[0].BodyStructure)

	// BODYSTRUCTURE 不可用时降级到整封抓取
	if bsMessages[0].BodyStructure == nil {
		fullSection := &imap.FetchItemBodySection{Peek: true}
		msgs, fetchErr := c.client.Fetch(uidSet, &imap.FetchOptions{
			BodySection: []*imap.FetchItemBodySection{fullSection},
		}).Collect()
		if fetchErr != nil {
			return "", "", fmt.Errorf("failed to fetch message body: %w", fetchErr)
		}
		if len(msgs) == 0 {
			return "", "", fmt.Errorf("message not found")
		}
		c.lastActivity = time.Now()
		raw := msgs[0].FindBodySection(fullSection)
		if len(raw) > 0 {
			bodyText, bodyHTML = parseMessageBody(raw)
		}
		return bodyText, bodyHTML, nil
	}

	if len(sel.textPath) == 0 && len(sel.htmlPath) == 0 {
		c.lastActivity = time.Now()
		return "", "", nil
	}

	if !sel.isMultipart {
		fullSection := &imap.FetchItemBodySection{Peek: true}
		msgs, fetchErr := c.client.Fetch(uidSet, &imap.FetchOptions{
			BodySection: []*imap.FetchItemBodySection{fullSection},
		}).Collect()
		if fetchErr != nil {
			return "", "", fmt.Errorf("failed to fetch message body: %w", fetchErr)
		}
		if len(msgs) == 0 {
			return "", "", fmt.Errorf("message not found")
		}
		c.lastActivity = time.Now()
		raw := msgs[0].FindBodySection(fullSection)
		if len(raw) > 0 {
			bodyText, bodyHTML = parseMessageBody(raw)
		}
		return bodyText, bodyHTML, nil
	}

	var sections []*imap.FetchItemBodySection
	var textMimeSec, textBodySec, htmlMimeSec, htmlBodySec *imap.FetchItemBodySection
	if len(sel.textPath) > 0 {
		textMimeSec = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierMIME, Part: sel.textPath, Peek: true}
		textBodySec = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierNone, Part: sel.textPath, Peek: true}
		sections = append(sections, textMimeSec, textBodySec)
	}
	if len(sel.htmlPath) > 0 {
		htmlMimeSec = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierMIME, Part: sel.htmlPath, Peek: true}
		htmlBodySec = &imap.FetchItemBodySection{Specifier: imap.PartSpecifierNone, Part: sel.htmlPath, Peek: true}
		sections = append(sections, htmlMimeSec, htmlBodySec)
	}

	messages, err := c.client.Fetch(uidSet, &imap.FetchOptions{
		BodySection: sections,
	}).Collect()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch message body parts: %w", err)
	}
	if len(messages) == 0 {
		return "", "", fmt.Errorf("message not found")
	}

	c.lastActivity = time.Now()
	if textBodySec != nil {
		bodyText = decodeBodyPart(messages[0].FindBodySection(textMimeSec), messages[0].FindBodySection(textBodySec))
	}
	if htmlBodySec != nil {
		bodyHTML = decodeBodyPart(messages[0].FindBodySection(htmlMimeSec), messages[0].FindBodySection(htmlBodySec))
	}
	return bodyText, bodyHTML, nil
}

func (c *Client) MarkAsRead(uid uint, folder string) error {
	if c.client == nil {
		return fmt.Errorf("not connected")
	}

	if _, err := c.client.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("failed to select mailbox: %w", err)
	}

	uidSet := imap.UIDSetNum(imap.UID(uid))
	storeCmd := c.client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagSeen},
	}, nil)

	err := storeCmd.Close()
	c.lastActivity = time.Now()
	return err
}

func (c *Client) TestConnection() error {
	if c.account.IMAPHost == "" || c.account.Username == "" || c.account.Password == "" {
		return fmt.Errorf("invalid configuration")
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()

	testClient := NewClient(c.account)
	if err := testClient.ConnectContext(ctx); err != nil {
		return err
	}
	return testClient.Disconnect()
}
