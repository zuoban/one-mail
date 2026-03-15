package imap

import (
	"bytes"
	"fmt"
	"io"
	"log"
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

type FolderStatus struct {
	Name     string `json:"name"`
	Messages uint32 `json:"messages"`
	Unseen   uint32 `json:"unseen"`
}

func (c *Client) ListFoldersWithStatus() ([]FolderStatus, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	mailboxes, err := c.client.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}

	result := make([]FolderStatus, 0, len(mailboxes))
	for _, mbox := range mailboxes {
		if mbox.Mailbox == "" {
			continue
		}

		status, err := c.client.Status(mbox.Mailbox, &imap.StatusOptions{
			NumMessages: true,
			NumUnseen:   true,
		}).Wait()
		if err != nil {
			result = append(result, FolderStatus{
				Name:     mbox.Mailbox,
				Messages: 0,
				Unseen:   0,
			})
			continue
		}

		var messages uint32
		var unseen uint32
		if status.NumMessages != nil {
			messages = *status.NumMessages
		}
		if status.NumUnseen != nil {
			unseen = *status.NumUnseen
		}

		result = append(result, FolderStatus{
			Name:     mbox.Mailbox,
			Messages: messages,
			Unseen:   unseen,
		})
	}

	return result, nil
}

func (c *Client) Status(folder string) (*imap.StatusData, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	return c.client.Status(folder, &imap.StatusOptions{
		NumMessages: true,
		NumUnseen:   true,
		UIDNext:     true,
	}).Wait()
}

func (c *Client) FetchEmails(folder string, since time.Time, limit int) ([]*EmailSummary, error) {
	if c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	bodySection := &imap.FetchItemBodySection{Peek: true}
	var messages []*imapclient.FetchMessageBuffer
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second * time.Duration(1<<uint(attempt-1)))
		}

		selectedMbox, err := c.client.Select(folder, nil).Wait()
		if err != nil {
			if isTemporaryIMAPError(err) {
				continue
			}
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
			if isTemporaryIMAPError(err) {
				continue
			}
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

		fetchOptions := &imap.FetchOptions{
			Envelope: true,
			Flags:    true,
			UID:      true,
			BodySection: []*imap.FetchItemBodySection{
				bodySection,
			},
		}

		messages, err = c.client.Fetch(numSet, fetchOptions).Collect()
		if err != nil {
			if isTemporaryIMAPError(err) {
				continue
			}
			return nil, fmt.Errorf("failed to fetch messages: %w", err)
		}
		break
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

	var messages []*imapclient.FetchMessageBuffer
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second * time.Duration(1<<uint(attempt-1)))
		}

		selectedMbox, err := c.client.Select(folder, nil).Wait()
		if err != nil {
			if isTemporaryIMAPError(err) {
				continue
			}
			return nil, 0, fmt.Errorf("failed to select mailbox: %w", err)
		}
		mailboxMessages := selectedMbox.NumMessages
		var uidNext uint32
		if mailboxMessages == 0 {
			status, err := c.client.Status(folder, &imap.StatusOptions{
				NumMessages: true,
				UIDNext:     true,
			}).Wait()
			if err == nil {
				if status.NumMessages != nil {
					mailboxMessages = *status.NumMessages
				}
				if status.UIDNext > 0 {
					uidNext = uint32(status.UIDNext)
				}
			}
		}

		uidSet := imap.UIDSet{}
		if lastUID > 0 {
			uidSet.AddRange(imap.UID(lastUID)+1, 0)
		} else {
			uidSet.AddRange(1, 0)
		}

		criteria := imap.SearchCriteria{}
		criteria.UID = []imap.UIDSet{uidSet}

		data, err := c.client.Search(&criteria, nil).Wait()
		if err != nil {
			if isTemporaryIMAPError(err) {
				continue
			}
			return nil, 0, err
		}

		uids := data.AllUIDs()
		log.Printf("Fetch incremental search: folder=%s last_uid=%d uids=%d limit=%d mailbox_messages=%d uid_next=%d", folder, lastUID, len(uids), limit, mailboxMessages, uidNext)
		if len(uids) == 0 {
			if lastUID == 0 && mailboxMessages > 0 {
				log.Printf("Fetch incremental fallback for initial sync: folder=%s messages=%d limit=%d", folder, mailboxMessages, limit)
				// Some servers return empty UID SEARCH; fallback to SEARCH ALL via header-less criteria.
				allCriteria := imap.SearchCriteria{}
				allData, allErr := c.client.Search(&allCriteria, nil).Wait()
				if allErr == nil {
					seqNums := allData.AllSeqNums()
					if len(seqNums) > 0 {
						if limit > 0 && len(seqNums) > limit {
							seqNums = seqNums[len(seqNums)-limit:]
						}
						seqSet := imap.SeqSet{}
						for _, num := range seqNums {
							seqSet.AddNum(num)
						}
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
							return nil, 0, fmt.Errorf("failed to fetch messages: %w", err)
						}
						break
					}
				}

				total := int(mailboxMessages)
				start := uint32(total) - uint32(limit) + 1
				if start < 1 {
					start = 1
				}

				seqSet := imap.SeqSet{}
				seqSet.AddRange(start, uint32(total))

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
					return nil, 0, fmt.Errorf("failed to fetch messages: %w", err)
				}
				break
			}

			if lastUID > 0 && limit > 0 {
				log.Printf("Fetch incremental fallback to UID range: folder=%s last_uid=%d limit=%d", folder, lastUID, limit)
				startUID := lastUID + 1
				endUID := startUID + uint(limit) - 1
				fallbackSet := imap.UIDSet{}
				fallbackSet.AddRange(imap.UID(startUID), imap.UID(endUID))
				fetchOptions := &imap.FetchOptions{
					Envelope: true,
					Flags:    true,
					UID:      true,
				}
				messages, err = c.client.Fetch(fallbackSet, fetchOptions).Collect()
				if err != nil {
					if isTemporaryIMAPError(err) {
						continue
					}
					return nil, 0, fmt.Errorf("failed to fetch messages: %w", err)
				}
				break
			}

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

		messages, err = c.client.Fetch(fetchUIDSet, fetchOptions).Collect()
		if err != nil {
			if isTemporaryIMAPError(err) {
				continue
			}
			return nil, 0, fmt.Errorf("failed to fetch messages: %w", err)
		}
		break
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

	log.Printf("Fetch incremental done: folder=%s last_uid=%d result=%d max_uid=%d", folder, lastUID, len(results), maxUID)

	return results, maxUID, nil
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
