package application

import (
	"context"
	"errors"
	"strings"

	"one-mail/backend/internal/credentials"
	"one-mail/backend/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TelegramClient interface {
	TestConnection(context.Context, string, string) error
	SendNewEmailWithConfig(context.Context, *models.Email, string, string, string) error
}

type TelegramService struct {
	db     *gorm.DB
	mail   *MailService
	client TelegramClient
}

func NewTelegramService(db *gorm.DB, mail *MailService, client TelegramClient) *TelegramService {
	return &TelegramService{db: db, mail: mail, client: client}
}

type TelegramConfig struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type UpdateTelegramConfig struct {
	Enabled  *bool  `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

func MaskToken(token string) string {
	if len(token) > 8 {
		return token[:4] + "****" + token[len(token)-4:]
	}
	if token != "" {
		return "****"
	}
	return ""
}

func configResponse(cfg *models.TelegramConfig) TelegramConfig {
	return TelegramConfig{Enabled: cfg.Enabled, BotToken: MaskToken(cfg.BotToken), ChatID: cfg.ChatID}
}

func (s *TelegramService) load(ctx context.Context) (*models.TelegramConfig, error) {
	db := s.db.WithContext(ctx)
	var cfg models.TelegramConfig
	err := db.First(&cfg, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		cfg.ID = 1
		if err = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&cfg).Error; err == nil {
			err = db.First(&cfg, 1).Error
		}
	}
	if err != nil {
		return nil, failure(Internal, "failed to load telegram configuration", err)
	}
	cfg.BotToken, err = credentials.Decrypt(cfg.BotToken)
	if err != nil {
		return nil, failure(Internal, "failed to unlock telegram configuration", err)
	}
	return &cfg, nil
}

func (s *TelegramService) Config(ctx context.Context) (TelegramConfig, error) {
	cfg, err := s.load(ctx)
	if err != nil {
		return TelegramConfig{}, err
	}
	return configResponse(cfg), nil
}

func (s *TelegramService) Update(ctx context.Context, req UpdateTelegramConfig) (TelegramConfig, error) {
	cfg, err := s.load(ctx)
	if err != nil {
		return TelegramConfig{}, err
	}
	updates := make(map[string]interface{})
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
		updates["enabled"] = cfg.Enabled
	}
	if req.BotToken != "" && !strings.Contains(req.BotToken, "****") {
		sealed, err := credentials.Encrypt(req.BotToken)
		if err != nil {
			return TelegramConfig{}, failure(Internal, "failed to secure telegram configuration", err)
		}
		cfg.BotToken = req.BotToken
		updates["bot_token"] = sealed
	}
	if req.ChatID != "" {
		cfg.ChatID = req.ChatID
		updates["chat_id"] = cfg.ChatID
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&models.TelegramConfig{}).Where("id = ?", 1).Updates(updates).Error; err != nil {
			return TelegramConfig{}, failure(Internal, "failed to save config", err)
		}
	}
	return configResponse(cfg), nil
}

func (s *TelegramService) Test(ctx context.Context, req UpdateTelegramConfig) error {
	cfg, err := s.load(ctx)
	if err != nil {
		return err
	}
	token, chat := req.BotToken, req.ChatID
	if token == "" || strings.Contains(token, "****") {
		token = cfg.BotToken
	}
	if chat == "" {
		chat = cfg.ChatID
	}
	if token == "" || chat == "" {
		return failure(Invalid, "bot_token and chat_id are required", nil)
	}
	if err := s.client.TestConnection(ctx, token, chat); err != nil {
		return failure(Invalid, "telegram connection test failed; check bot token and chat ID", err)
	}
	return nil
}

func (s *TelegramService) Forward(ctx context.Context, id uint) error {
	// Reject evicted and stale messages before attempting remote body retrieval.
	_, _, err := s.mail.Snapshot(ctx, id)
	if err != nil {
		return err
	}
	cfg, err := s.load(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || cfg.BotToken == "" || cfg.ChatID == "" {
		return failure(Invalid, "telegram notification is not configured", nil)
	}
	email, err := s.mail.Get(ctx, id)
	if err != nil {
		return err
	}
	// Recheck the identity after fetching; an account edit must not forward the
	// old message using the newly configured mailbox identity.
	current, account, err := s.mail.Snapshot(ctx, id)
	if err != nil {
		return err
	}
	if current.IdentityVersion != email.IdentityVersion || current.UIDValidity != email.UIDValidity {
		return failure(Conflict, "mailbox identity changed; refresh the email list before retrying", nil)
	}
	if err := s.client.SendNewEmailWithConfig(ctx, email, account.Email, cfg.BotToken, cfg.ChatID); err != nil {
		return failure(Upstream, "failed to send telegram notification", err)
	}
	return nil
}
