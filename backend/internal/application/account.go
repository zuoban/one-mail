package application

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"time"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"
	"one-mail/backend/internal/services/imap"

	"gorm.io/gorm"
)

type Provider struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Custom   bool   `json:"custom"`
	IMAPHost string `json:"imap_host"`
	IMAPPort int    `json:"imap_port"`
}

func Providers() []Provider {
	labels := map[string]string{
		"gmail":   "Gmail",
		"qq":      "QQ 邮箱",
		"outlook": "Outlook",
		"qq-work": "腾讯企业邮箱",
		"163":     "163 邮箱",
		"custom":  "自定义邮箱",
	}
	result := make([]Provider, 0, len(imap.ProviderConfigs))
	for id, cfg := range imap.ProviderConfigs {
		label := labels[id]
		if label == "" {
			label = id
		}
		result = append(result, Provider{
			ID:       id,
			Label:    label,
			Custom:   id == "custom",
			IMAPHost: cfg.IMAPHost,
			IMAPPort: cfg.IMAPPort,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Custom != result[j].Custom {
			return !result[i].Custom
		}
		return result[i].ID < result[j].ID
	})
	return result
}

type CreateAccount struct {
	Email    string `json:"email" binding:"required"`
	Provider string `json:"provider" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	IMAPHost string `json:"imap_host"`
	IMAPPort int    `json:"imap_port"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
}

type UpdateAccount struct {
	Email    *string `json:"email"`
	Provider *string `json:"provider"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	IMAPHost *string `json:"imap_host"`
	IMAPPort *int    `json:"imap_port"`
}

type AccountService struct {
	db             *gorm.DB
	pool           *imap.ConnectionPool
	mail           *MailService
	testConnection func(context.Context, *models.EmailAccount) error
}

func NewAccountService(db *gorm.DB, pool *imap.ConnectionPool, mail *MailService) *AccountService {
	return &AccountService{
		db:             db,
		pool:           pool,
		mail:           mail,
		testConnection: pool.TestConnectionContext,
	}
}

func (s *AccountService) List(ctx context.Context) ([]models.EmailAccount, error) {
	accounts := make([]models.EmailAccount, 0)
	if err := s.db.WithContext(ctx).Find(&accounts).Error; err != nil {
		return nil, failure(Internal, "failed to load accounts", err)
	}
	return accounts, nil
}

func validHost(host string) bool {
	if host == "" || len(host) > 253 || strings.TrimSpace(host) != host {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}

func configureAccount(account *models.EmailAccount) error {
	cfg, ok := imap.ProviderConfigs[account.Provider]
	if !ok {
		return failure(Invalid, "unsupported provider", nil)
	}
	if account.Provider == "custom" {
		if !validHost(account.IMAPHost) {
			return failure(Invalid, "valid imap_host is required", nil)
		}
		if account.IMAPPort < 1 || account.IMAPPort > 65535 {
			return failure(Invalid, "imap_port must be between 1 and 65535", nil)
		}
	} else {
		account.IMAPHost, account.IMAPPort = cfg.IMAPHost, cfg.IMAPPort
		account.SMTPHost, account.SMTPPort = cfg.SMTPHost, cfg.SMTPPort
	}
	return nil
}

// Create returns a committed account even when the initial refresh fails. The
// separate refresh error lets the transport report that partial success safely.
func (s *AccountService) Create(ctx context.Context, req CreateAccount) (*models.EmailAccount, error, error) {
	if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.Username) == "" || req.Password == "" {
		return nil, nil, failure(Invalid, "email, username and password are required", nil)
	}
	account := models.EmailAccount{
		Email:    req.Email,
		Provider: req.Provider,
		Username: req.Username,
		Password: req.Password,
		IMAPHost: req.IMAPHost,
		IMAPPort: req.IMAPPort,
		SMTPHost: req.SMTPHost,
		SMTPPort: req.SMTPPort,
		Status:   "active",
	}
	if err := configureAccount(&account); err != nil {
		return nil, nil, err
	}
	if err := s.testConnection(ctx, &account); err != nil {
		return nil, nil, failure(Invalid, "failed to connect to mailbox; check account settings", err)
	}
	sealed, err := credentials.Encrypt(account.Password)
	if err != nil {
		return nil, nil, failure(Internal, "failed to protect account credentials", err)
	}
	account.Password = sealed
	if err := s.db.WithContext(ctx).Create(&account).Error; err != nil {
		return nil, nil, failure(Internal, "failed to create account", err)
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, refreshErr := s.mail.RefreshAccount(refreshCtx, account.ID)
	return &account, refreshErr, nil
}

func (s *AccountService) Delete(ctx context.Context, id uint) error {
	var account models.EmailAccount
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&account, id).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", account.ID).Delete(&models.Email{}).Error; err != nil {
			return err
		}
		if err := tx.Where("account_id = ?", account.ID).Delete(&models.NotificationTask{}).Error; err != nil {
			return err
		}
		return tx.Delete(&account).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return failure(NotFound, "account not found", err)
	}
	if err != nil {
		return failure(Internal, "failed to delete account", err)
	}
	s.pool.RemoveConnection(account.ID, account.ConfigVersion+1)
	return nil
}

func (s *AccountService) Update(ctx context.Context, id uint, req UpdateAccount) (*models.EmailAccount, error) {
	for _, field := range []*string{req.Email, req.Provider, req.Username} {
		if field != nil && strings.TrimSpace(*field) == "" {
			return nil, failure(Invalid, "email, provider and username cannot be empty", nil)
		}
	}
	var sealed string
	if req.Password != nil && *req.Password != "" {
		var err error
		sealed, err = credentials.Encrypt(*req.Password)
		if err != nil {
			return nil, failure(Internal, "failed to protect account credentials", err)
		}
	}
	var account models.EmailAccount
	var changed bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&account, id).Error; err != nil {
			return err
		}
		old := account
		if req.Email != nil {
			account.Email = *req.Email
		}
		if req.Provider != nil {
			account.Provider = *req.Provider
		}
		if req.Username != nil {
			account.Username = *req.Username
		}
		if req.Provider != nil && *req.Provider == "custom" && old.Provider != "custom" && (req.IMAPHost == nil || req.IMAPPort == nil) {
			return failure(Invalid, "imap_host and imap_port are required for custom accounts", nil)
		}
		if req.IMAPHost != nil {
			account.IMAPHost = *req.IMAPHost
		}
		if req.IMAPPort != nil {
			account.IMAPPort = *req.IMAPPort
		}
		// Canonicalize known providers on every edit, while permitting unrelated
		// credential updates to legacy rows whose provider has not been assigned.
		if account.Provider != "" || req.Provider != nil || req.IMAPHost != nil || req.IMAPPort != nil {
			if err := configureAccount(&account); err != nil {
				return err
			}
		}
		if sealed != "" {
			account.Password = sealed
		}
		identityChanged := account.Provider != old.Provider || account.Username != old.Username || account.IMAPHost != old.IMAPHost || account.IMAPPort != old.IMAPPort
		changed = identityChanged || sealed != ""
		if changed {
			account.ConfigVersion++
		}
		if identityChanged {
			account.IdentityVersion++
			account.UIDValidity = 0
			account.LastSyncTime = time.Time{}
			if err := tx.Where("account_id = ?", account.ID).Delete(&models.Email{}).Error; err != nil {
				return err
			}
			if err := tx.Where("account_id = ?", account.ID).Delete(&models.NotificationTask{}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&account).Select(
			"Email", "Provider", "Username", "Password", "IMAPHost", "IMAPPort",
			"SMTPHost", "SMTPPort", "ConfigVersion", "IdentityVersion", "UIDValidity", "LastSyncTime",
		).Updates(&account).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, failure(NotFound, "account not found", err)
	}
	if err != nil {
		var appErr *Error
		if errors.As(err, &appErr) {
			return nil, err
		}
		return nil, failure(Internal, "failed to update account", err)
	}
	if changed {
		s.pool.RemoveConnection(account.ID, account.ConfigVersion)
	}
	return &account, nil
}

func (s *AccountService) Test(ctx context.Context, id uint) error {
	var account models.EmailAccount
	if err := s.db.WithContext(ctx).First(&account, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return failure(NotFound, "account not found", err)
		}
		return failure(Internal, "failed to load account", err)
	}
	if err := s.testConnection(ctx, &account); err != nil {
		return failure(Invalid, "failed to connect to mailbox; check account settings", err)
	}
	return nil
}
