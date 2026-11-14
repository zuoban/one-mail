package imap

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/mailwindow"
	"one-mail/backend/internal/models"
)

type Client struct {
	account       *models.EmailAccount
	client        *imapclient.Client
	username      string
	password      string
	transport     net.Conn
	credentialErr error
	lastActivity  time.Time
	pool          *ConnectionPool
	release       func(bool)
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
	"icloud": {
		IMAPHost: "imap.mail.me.com",
		IMAPPort: 993,
		SMTPHost: "smtp.mail.me.com",
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

func NewClient(account *models.EmailAccount, pool *ConnectionPool) *Client {
	password, err := credentials.Decrypt(account.Password)
	return &Client{account: account, pool: pool, username: account.Username, password: password, credentialErr: err}
}

const defaultConnectTimeout = 15 * time.Second

func IsAuthError(err error) bool {
	var status *imap.Error
	if !errors.As(err, &status) || status.Type != imap.StatusResponseTypeNo {
		return false
	}
	switch status.Code {
	case imap.ResponseCodeAuthenticationFailed, imap.ResponseCodeAuthorizationFailed, imap.ResponseCodeExpired:
		return true
	case "", imap.ResponseCodeAlert:
		if isTemporaryIMAPError(status) {
			return false
		}
		// Some providers omit AUTHENTICATIONFAILED, but still identify credential
		// problems explicitly. A generic NO or "failed to login" is not enough.
		s := strings.ToLower(status.Text)
		return strings.Contains(s, "authentication failed") ||
			strings.Contains(s, "invalid credentials") ||
			strings.Contains(s, "bad credentials") ||
			strings.Contains(s, "application-specific password") ||
			strings.Contains(s, "web login required")
	default:
		return false
	}
}

func (c *Client) Connect() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultConnectTimeout)
	defer cancel()
	return c.ConnectContext(ctx)
}

func (c *Client) ConnectContext(ctx context.Context) error {
	if c.pool == nil {
		return errors.New("IMAP connection pool is required")
	}
	if c.client != nil {
		return errors.New("IMAP client is already connected")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()
	_, release, err := c.pool.acquire(ctx, c.account, true, func(ctx context.Context, _ *models.EmailAccount) (*Client, error) {
		if err := c.connectContext(ctx); err != nil {
			c.closeTransport()
			return nil, err
		}
		return c, nil
	})
	if err != nil {
		return err
	}
	c.release = release
	return nil
}

// connectContext is called only after the pool has reserved admission.
func (c *Client) connectContext(ctx context.Context) error {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultConnectTimeout)
		defer cancel()
	}

	if c.credentialErr != nil {
		return errors.New("unable to decrypt IMAP credentials")
	}
	if err := ctx.Err(); err != nil {
		return err
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
	c.transport = tlsConn

	if err := loginContext(ctx, conn, c.username, c.password); err != nil {
		return err
	}
	c.client = conn
	c.lastActivity = time.Now()
	return nil
}

func loginContext(ctx context.Context, conn *imapclient.Client, username, password string) error {
	loginDone := make(chan error, 1)
	go func() {
		// QQ ignores LOGIN sent before its greeting, leaving the command pending.
		// Keep the greeting wait inside the cancellable login operation.
		if err := conn.WaitGreeting(); err != nil {
			loginDone <- err
			return
		}
		loginDone <- conn.Login(username, password).Wait()
	}()

	select {
	case err := <-loginDone:
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("failed to login: %w", err)
		}
		return nil
	case <-ctx.Done():
		_ = conn.Close()
		return fmt.Errorf("failed to login: %w", ctx.Err())
	}
}

func (c *Client) Disconnect() error {
	defer c.ForceClose()
	if c.client == nil {
		return nil
	}

	client := c.client

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
	c.closeTransport()
	if c.release != nil {
		c.release(false)
	}
}

func (c *Client) closeTransport() {
	// imapclient.Close waits for its decoder, which can be waiting for the current
	// literal consumer. Closing just the transport lets that consumer unwind first.
	if c.transport != nil {
		_ = c.transport.Close()
	}
}

func (c *Client) IsConnected() bool {
	if c.client == nil {
		return false
	}
	if !c.lastActivity.IsZero() && time.Since(c.lastActivity) < 30*time.Second {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.noopContext(ctx) == nil
}

func (c *Client) noopContext(ctx context.Context) error {
	finish, err := c.commandContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	err = c.client.Noop().Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		c.ForceClose()
		return err
	}
	c.lastActivity = time.Now()
	return nil
}

type EmailSummary struct {
	MessageID     string
	From          string
	FromName      string
	To            string
	Subject       string
	Date          time.Time
	HasAttachment bool
	UID           uint
}

var ErrUIDValidityChanged = errors.New("mailbox UIDVALIDITY changed; refresh required")
var ErrBodyTooLarge = errors.New("message body exceeds 8 MiB limit")

const maxBodyBytes = 8 << 20

// commandContext closes the transport on cancellation and joins its callback before reuse.
func (c *Client) commandContext(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn := c.client
	if conn == nil {
		return nil, errors.New("not connected")
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { c.ForceClose(); close(done) })
	return func() {
		if !stop() {
			<-done
		}
	}, nil
}

func retryTemporary(ctx context.Context, operation func() error) error {
	var lastErr error
	for attempt := range 3 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 0 {
			timer := time.NewTimer(time.Second * time.Duration(1<<uint(attempt-1)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		lastErr = operation()
		if lastErr == nil || !isTemporaryIMAPError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func computeLatestSeqRange(total uint32, limit int) (uint32, uint32, bool) {
	if total == 0 {
		return 0, 0, false
	}
	limit, err := mailwindow.Resolve(limit)
	if err != nil {
		return 0, 0, false
	}
	start := uint32(1)
	if total > uint32(limit) {
		start = total - uint32(limit) + 1
	}
	return start, total, true
}

func (c *Client) FetchLatestEmails(ctx context.Context, folder string, limit int) ([]*EmailSummary, uint32, error) {
	return c.FetchLatestEmailsCached(ctx, folder, limit, 0, nil)
}

func (c *Client) FetchLatestEmailsCached(ctx context.Context, folder string, limit int, cachedEpoch uint32, cached map[uint]EmailSummary) ([]*EmailSummary, uint32, error) {
	limit, err := mailwindow.Resolve(limit)
	if err != nil {
		return nil, 0, err
	}
	finish, err := c.commandContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer finish()
	if folder == "" {
		folder = "INBOX"
	}

	var messages []*imapclient.FetchMessageBuffer
	var reused []EmailSummary
	var requested map[imap.UID]bool
	var uidValidity uint32
	err = retryTemporary(ctx, func() error {
		messages, reused, requested = nil, nil, nil
		selectedMbox, err := c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			return fmt.Errorf("failed to select mailbox: %w", err)
		}
		uidValidity = selectedMbox.UIDValidity
		if uidValidity == 0 {
			return ErrUIDValidityChanged
		}

		start, end, ok := computeLatestSeqRange(selectedMbox.NumMessages, limit)
		if !ok {
			messages = nil
			return nil
		}

		var seqSet imap.SeqSet
		seqSet.AddRange(start, end)

		fetchOptions := &imap.FetchOptions{
			Envelope:      true,
			UID:           true,
			BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		}

		if uidValidity == cachedEpoch && len(cached) > 0 {
			window, err := c.client.Fetch(seqSet, &imap.FetchOptions{UID: true}).Collect()
			if err != nil {
				return fmt.Errorf("failed to fetch message UIDs: %w", err)
			}
			var missing imap.UIDSet
			requested = make(map[imap.UID]bool)
			seen := make(map[imap.UID]bool, len(window))
			for _, message := range window {
				if message == nil || message.UID == 0 || seen[message.UID] {
					return errors.New("invalid message UID window")
				}
				seen[message.UID] = true
				if summary, exists := cached[uint(message.UID)]; exists {
					summary.UID = uint(message.UID)
					reused = append(reused, summary)
				} else {
					missing.AddNum(message.UID)
					requested[message.UID] = true
				}
			}
			if len(missing) > 0 {
				messages, err = c.client.Fetch(missing, fetchOptions).Collect()
			}
		} else {
			messages, err = c.client.Fetch(seqSet, fetchOptions).Collect()
		}
		if err != nil {
			return fmt.Errorf("failed to fetch messages: %w", err)
		}
		return nil
	})
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	if err != nil {
		return nil, 0, err
	}

	results := make([]*EmailSummary, 0, len(messages)+len(reused))
	for _, summary := range reused {
		results = append(results, &summary)
	}
	received := make(map[imap.UID]bool, len(messages))
	for _, msg := range messages {
		if msg == nil || msg.UID == 0 {
			return nil, 0, errors.New("message has no UID")
		}
		if requested != nil && !requested[msg.UID] {
			return nil, 0, errors.New("message UID is outside the requested window")
		}
		if received[msg.UID] {
			return nil, 0, errors.New("duplicate message UID")
		}
		received[msg.UID] = true
		email := &EmailSummary{
			UID:           uint(msg.UID),
			HasAttachment: hasAttachment(msg.BodyStructure),
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

		results = append(results, email)
	}
	if requested != nil && len(received) != len(requested) {
		return nil, 0, errors.New("message metadata changed during refresh; retry required")
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
	return results, uidValidity, nil
}

func parseMessageBody(raw []byte) (bodyText string, bodyHTML string, err error) {
	if len(raw) == 0 {
		return "", "", errors.New("missing message MIME data")
	}
	entity, parseErr := message.Read(bytes.NewReader(raw))
	return readMIMEEntity(entity, parseErr)
}

func readMIMEEntity(entity *message.Entity, parseErr error) (bodyText string, bodyHTML string, err error) {
	if parseErr != nil && !message.IsUnknownCharset(parseErr) {
		return "", "", fmt.Errorf("invalid MIME entity: %w", parseErr)
	}
	if entity == nil {
		return "", "", errors.New("missing MIME entity")
	}
	contentType, params, err := entity.Header.ContentType()
	if err != nil {
		return "", "", fmt.Errorf("invalid MIME content type: %w", err)
	}
	if entity.Header.Get("Content-Disposition") != "" {
		disposition, dispositionParams, err := entity.Header.ContentDisposition()
		if err != nil {
			return "", "", fmt.Errorf("invalid MIME disposition: %w", err)
		}
		if strings.EqualFold(disposition, "attachment") || hasFilename(dispositionParams, "filename") {
			return "", "", nil
		}
	}
	if hasFilename(params, "name") {
		return "", "", nil
	}
	if multipart := entity.MultipartReader(); multipart != nil {
		if params["boundary"] == "" {
			return "", "", errors.New("multipart MIME entity has no boundary")
		}
		defer multipart.Close()
		for {
			part, partErr := multipart.NextPart()
			if partErr == io.EOF {
				return bodyText, bodyHTML, nil
			}
			text, html, err := readMIMEEntity(part, partErr)
			if err != nil {
				return "", "", err
			}
			if bodyText == "" {
				bodyText = text
			}
			if bodyHTML == "" {
				bodyHTML = html
			}
		}
	}
	// Unknown encodings must never return the original encoded bytes, including
	// when go-message reports an unknown charset after an encoding error.
	switch strings.ToLower(strings.TrimSpace(entity.Header.Get("Content-Transfer-Encoding"))) {
	case "", "7bit", "8bit", "binary", "base64", "quoted-printable":
	default:
		return "", "", errors.New("unsupported MIME transfer encoding")
	}
	if contentType != "text/plain" && contentType != "text/html" {
		return "", "", nil
	}
	decoded, err := io.ReadAll(entity.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode MIME body: %w", err)
	}
	// go-message leaves the transfer-decoded body readable for unknown charsets.
	// It is safe to accept that fallback only when its bytes already form UTF-8.
	if message.IsUnknownCharset(parseErr) && !utf8.Valid(decoded) {
		return "", "", fmt.Errorf("cannot decode MIME charset: %w", parseErr)
	}
	if contentType == "text/html" {
		return "", string(decoded), nil
	}
	return string(decoded), "", nil
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

func hasFilename(params map[string]string, name string) bool {
	for key, value := range params {
		key = strings.ToLower(key)
		if value != "" && (key == name || strings.HasPrefix(key, name+"*")) {
			return true
		}
	}
	return false
}

func isAttachment(part imap.BodyStructure) bool {
	if disposition := part.Disposition(); disposition != nil {
		if strings.EqualFold(disposition.Value, "attachment") || hasFilename(disposition.Params, "filename") {
			return true
		}
	}
	switch part := part.(type) {
	case *imap.BodyStructureSinglePart:
		return hasFilename(part.Params, "name")
	case *imap.BodyStructureMultiPart:
		return part.Extended != nil && hasFilename(part.Extended.Params, "name")
	}
	return false
}

func hasAttachment(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	var found bool
	bs.Walk(func(_ []int, part imap.BodyStructure) bool {
		if isAttachment(part) {
			found = true
		}
		return !found
	})
	return found
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
		if isAttachment(part) {
			return false
		}
		sp, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
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

func decodeBodyPart(mimeHeaderBytes, bodyBytes []byte) (string, error) {
	if len(mimeHeaderBytes) == 0 || bodyBytes == nil {
		return "", errors.New("missing MIME header or body section")
	}
	separator := ""
	if !bytes.HasSuffix(mimeHeaderBytes, []byte("\r\n\r\n")) && !bytes.HasSuffix(mimeHeaderBytes, []byte("\n\n")) {
		if bytes.HasSuffix(mimeHeaderBytes, []byte("\n")) {
			separator = "\r\n"
		} else {
			separator = "\r\n\r\n"
		}
	}
	entity, parseErr := message.Read(io.MultiReader(bytes.NewReader(mimeHeaderBytes), strings.NewReader(separator), bytes.NewReader(bodyBytes)))
	if entity != nil {
		contentType, _, err := entity.Header.ContentType()
		if err != nil || (contentType != "text/plain" && contentType != "text/html") {
			return "", errors.New("body section has invalid text MIME content type")
		}
	}
	text, html, err := readMIMEEntity(entity, parseErr)
	if err != nil {
		return "", err
	}
	if entity != nil {
		if contentType, _, _ := entity.Header.ContentType(); contentType == "text/html" {
			return html, nil
		}
	}
	return text, nil
}

func (c *Client) FetchEmailBody(ctx context.Context, folder string, uid uint, uidValidity uint32) (bodyText string, bodyHTML string, err error) {
	finish, err := c.commandContext(ctx)
	if err != nil {
		return "", "", err
	}
	defer finish()
	selected, err := c.client.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return "", "", fmt.Errorf("failed to select mailbox: %w", err)
	}
	if uidValidity == 0 || selected.UIDValidity != uidValidity {
		return "", "", ErrUIDValidityChanged
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
		msgs, fetchErr := c.fetchBodySections(uidSet, []*imap.FetchItemBodySection{fullSection})
		if fetchErr != nil {
			return "", "", fmt.Errorf("failed to fetch message body: %w", fetchErr)
		}
		if len(msgs) == 0 {
			return "", "", fmt.Errorf("message not found")
		}
		bodyText, bodyHTML, err = parseMessageBody(msgs[0].FindBodySection(fullSection))
		if err != nil {
			return "", "", fmt.Errorf("failed to parse message body: %w", err)
		}
		c.lastActivity = time.Now()
		return bodyText, bodyHTML, nil
	}

	if len(sel.textPath) == 0 && len(sel.htmlPath) == 0 {
		c.lastActivity = time.Now()
		return "", "", nil
	}

	if !sel.isMultipart {
		fullSection := &imap.FetchItemBodySection{Peek: true}
		msgs, fetchErr := c.fetchBodySections(uidSet, []*imap.FetchItemBodySection{fullSection})
		if fetchErr != nil {
			return "", "", fmt.Errorf("failed to fetch message body: %w", fetchErr)
		}
		if len(msgs) == 0 {
			return "", "", fmt.Errorf("message not found")
		}
		bodyText, bodyHTML, err = parseMessageBody(msgs[0].FindBodySection(fullSection))
		if err != nil {
			return "", "", fmt.Errorf("failed to parse message body: %w", err)
		}
		c.lastActivity = time.Now()
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

	messages, err := c.fetchBodySections(uidSet, sections)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch message body parts: %w", err)
	}
	if len(messages) == 0 {
		return "", "", fmt.Errorf("message not found")
	}

	if textBodySec != nil {
		bodyText, err = decodeBodyPart(messages[0].FindBodySection(textMimeSec), messages[0].FindBodySection(textBodySec))
		if err != nil {
			return "", "", fmt.Errorf("failed to decode text body: %w", err)
		}
	}
	if htmlBodySec != nil {
		bodyHTML, err = decodeBodyPart(messages[0].FindBodySection(htmlMimeSec), messages[0].FindBodySection(htmlBodySec))
		if err != nil {
			return "", "", fmt.Errorf("failed to decode HTML body: %w", err)
		}
	}
	c.lastActivity = time.Now()
	return bodyText, bodyHTML, nil
}

// fetchBodySections bounds every literal even when the server ignores BODY partial ranges.
func (c *Client) fetchBodySections(uids imap.UIDSet, sections []*imap.FetchItemBodySection) ([]*imapclient.FetchMessageBuffer, error) {
	for _, section := range sections {
		section.Partial = &imap.SectionPartial{Size: maxBodyBytes + 1}
	}
	cmd := c.client.Fetch(uids, &imap.FetchOptions{BodySection: sections})
	defer cmd.Close()
	var result []*imapclient.FetchMessageBuffer
	remaining := int64(maxBodyBytes)
	for message := cmd.Next(); message != nil; message = cmd.Next() {
		buffer := &imapclient.FetchMessageBuffer{}
		for item := message.Next(); item != nil; item = message.Next() {
			section, ok := item.(imapclient.FetchItemDataBodySection)
			if !ok || section.Literal == nil {
				continue
			}
			body, err := io.ReadAll(io.LimitReader(section.Literal, remaining+1))
			if err != nil {
				c.ForceClose()
				return nil, err
			}
			remaining -= int64(len(body))
			if remaining < 0 {
				c.ForceClose()
				return nil, ErrBodyTooLarge
			}
			buffer.BodySection = append(buffer.BodySection, imapclient.FetchBodySectionBuffer{Section: section.Section, Bytes: body})
		}
		result = append(result, buffer)
	}
	if err := cmd.Close(); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) TestConnectionContext(ctx context.Context) error {
	if c.pool == nil {
		return errors.New("IMAP connection pool is required")
	}
	return c.pool.TestConnectionContext(ctx, c.account)
}
