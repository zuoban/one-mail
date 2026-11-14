package application

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"one-mail/backend/internal/mailwindow"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"
	"one-mail/backend/internal/services/mail"

	"gorm.io/gorm"
)

type MailService struct {
	db             *gorm.DB
	pool           *imap.ConnectionPool
	ftsEnabled     bool
	fetchBody      func(context.Context, *models.EmailAccount, *models.Email) (string, string, error)
	refreshAccount func(context.Context, *gorm.DB, *imap.ConnectionPool, *models.EmailAccount, int) (int, error)
}

func NewMailService(db *gorm.DB, pool *imap.ConnectionPool, ftsEnabled bool) *MailService {
	s := &MailService{db: db, pool: pool, ftsEnabled: ftsEnabled, refreshAccount: mail.RefreshAccount}
	s.fetchBody = func(ctx context.Context, account *models.EmailAccount, email *models.Email) (string, string, error) {
		client, release, err := pool.AcquireConnectionContext(ctx, account)
		if err != nil {
			return "", "", err
		}
		text, html, err := client.FetchEmailBody(ctx, email.Folder, email.UID, email.UIDValidity)
		release(err == nil)
		return text, html, err
	}
	return s
}

type ListEmails struct {
	AccountID uint   `form:"account_id"`
	Folder    string `form:"folder"`
	Search    string `form:"search"`
	Limit     int    `form:"limit"`
}

type EmailListItem struct {
	ID            uint      `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	AccountID     uint      `json:"account_id"`
	From          string    `json:"from"`
	FromName      string    `json:"from_name"`
	To            string    `json:"to"`
	Subject       string    `json:"subject"`
	Date          time.Time `json:"date"`
	HasAttachment bool      `json:"has_attachment"`
	Folder        string    `json:"folder"`
}

func (s *MailService) List(ctx context.Context, query ListEmails) ([]EmailListItem, error) {
	limit, err := mailwindow.Resolve(query.Limit)
	if err != nil {
		return nil, failure(Invalid, err.Error(), err)
	}
	db := s.db.WithContext(ctx)
	accounts := db.Model(&models.EmailAccount{}).Select("id")
	if query.AccountID > 0 {
		accounts = accounts.Where("id = ?", query.AccountID)
	}
	q := db.Model(&models.Email{}).Where("account_id IN (?)", accounts)
	folder := query.Folder
	if folder == "" {
		folder = mail.DefaultFolder
	}
	q = q.Where("folder = ?", folder)
	search := strings.TrimSpace(query.Search)
	if search != "" {
		if s.ftsEnabled && utf8.RuneCountInString(search) >= 3 {
			expr := `"` + strings.ReplaceAll(search, `"`, `""`) + `"`
			q = q.Where("emails.id IN (SELECT rowid FROM emails_fts WHERE emails_fts MATCH ?)", expr)
		} else {
			pattern := "%" + search + "%"
			q = q.Where("(subject LIKE ? OR `from` LIKE ? OR from_name LIKE ?)", pattern, pattern, pattern)
		}
	}
	emails := make([]EmailListItem, 0)
	if err := q.Select("id, created_at, account_id, `from`, from_name, `to`, subject, date, has_attachment, folder").
		Order("date DESC, id DESC").
		Limit(limit).
		Find(&emails).Error; err != nil {
		return nil, failure(Internal, "failed to load emails", err)
	}
	return emails, nil
}

// Snapshot checks identity even when the body is already cached.
func (s *MailService) Snapshot(ctx context.Context, id uint) (*models.Email, *models.EmailAccount, error) {
	var email models.Email
	if err := s.db.WithContext(ctx).
		Joins("JOIN email_accounts ON email_accounts.id = emails.account_id AND email_accounts.deleted_at IS NULL").
		Where("emails.id = ?", id).
		First(&email).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, failure(NotFound, "email not found", err)
		}
		return nil, nil, failure(Internal, "failed to load email", err)
	}
	var account models.EmailAccount
	if err := s.db.WithContext(ctx).First(&account, email.AccountID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, failure(NotFound, "email not found", err)
		}
		return nil, nil, failure(Internal, "failed to load account", err)
	}
	if email.IdentityVersion != account.IdentityVersion || email.UIDValidity != account.UIDValidity {
		return nil, nil, failure(Conflict, "mailbox identity changed; refresh the email list before retrying", mail.ErrAccountChanged)
	}
	return &email, &account, nil
}

func (s *MailService) Get(ctx context.Context, id uint) (*models.Email, error) {
	email, account, err := s.Snapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	if email.BodyFetched {
		return email, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	text, html, err := s.fetchBody(ctx, account, email)
	if err != nil {
		if errors.Is(err, mail.ErrAccountChanged) || errors.Is(err, imap.ErrUIDValidityChanged) || errors.Is(err, imap.ErrConnectionInvalidated) {
			return nil, failure(Conflict, "mailbox identity changed; refresh the email list before retrying", err)
		}
		return nil, failure(Upstream, "failed to load email body; please retry", err)
	}
	if err := s.cacheEmailBody(ctx, account, email, text, html); err != nil {
		if errors.Is(err, mail.ErrAccountChanged) {
			return nil, failure(Conflict, "mailbox identity changed; refresh the email list before retrying", err)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, failure(NotFound, "email not found", err)
		}
		return nil, failure(Internal, "failed to cache email body", err)
	}
	email.BodyText, email.BodyHTML, email.BodyFetched = text, html, true
	return email, nil
}

func (s *MailService) cacheEmailBody(ctx context.Context, account *models.EmailAccount, email *models.Email, text, html string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.EmailAccount
		if err := tx.First(&current, account.ID).Error; err != nil {
			return err
		}
		if current.ConfigVersion != account.ConfigVersion || current.IdentityVersion != email.IdentityVersion || current.UIDValidity != email.UIDValidity {
			return mail.ErrAccountChanged
		}
		result := tx.Model(&models.Email{}).
			Where("id = ? AND account_id = ? AND uid = ? AND uid_validity = ? AND identity_version = ?", email.ID, account.ID, email.UID, email.UIDValidity, email.IdentityVersion).
			Updates(map[string]interface{}{
				"body_text":    text,
				"body_html":    html,
				"body_fetched": true,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (s *MailService) RefreshAccount(ctx context.Context, id uint, limit int) (int, error) {
	limit, err := mailwindow.Resolve(limit)
	if err != nil {
		return 0, failure(Invalid, err.Error(), err)
	}
	var account models.EmailAccount
	if err := s.db.WithContext(ctx).First(&account, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, failure(NotFound, "account not found", err)
		}
		return 0, failure(Internal, "failed to load account", err)
	}
	count, err := s.refreshAccount(ctx, s.db, s.pool, &account, limit)
	if err != nil {
		return 0, failure(Upstream, "failed to refresh mailbox; please retry", err)
	}
	return count, nil
}

func (s *MailService) Refresh(ctx context.Context, accountID uint, limit int) (mail.RefreshSummary, error) {
	limit, err := mailwindow.Resolve(limit)
	if err != nil {
		return mail.RefreshSummary{}, failure(Invalid, err.Error(), err)
	}
	var accounts []models.EmailAccount
	q := s.db.WithContext(ctx).Where("status = ?", "active")
	if accountID > 0 {
		q = q.Where("id = ?", accountID)
	}
	if err := q.Find(&accounts).Error; err != nil {
		return mail.RefreshSummary{}, failure(Internal, "failed to load accounts", err)
	}
	if len(accounts) == 0 {
		if accountID > 0 {
			return mail.RefreshSummary{}, failure(NotFound, "account not found", nil)
		}
		return mail.RefreshSummary{}, nil
	}
	summary := mail.RefreshAccounts(ctx, s.db, s.pool, accounts, limit)
	if summary.Refreshed == 0 && len(summary.Errors) > 0 {
		return summary, failure(Upstream, "failed to refresh mailboxes; please retry", nil)
	}
	return summary, nil
}
