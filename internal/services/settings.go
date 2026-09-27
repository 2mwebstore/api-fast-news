package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/secrets"
)

// SettingsService reads and writes runtime configuration.
//
// Values live in the database so an operator can change them from the admin
// without a redeploy. The environment still supplies the defaults, so a fresh
// install works before anyone opens the settings screen.
//
// Reads are cached in memory: the Telegram credentials are needed on every
// publish, and a database round trip per publish would be waste.
type SettingsService struct {
	db     *gorm.DB
	cipher *secrets.Cipher
	cfg    *config.Config

	mu       sync.RWMutex
	cache    map[string]string
	cachedAt time.Time
}

const settingsCacheTTL = 30 * time.Second

func NewSettingsService(db *gorm.DB, cfg *config.Config) (*SettingsService, error) {
	key := cfg.SettingsKey
	if key == "" {
		// Falling back keeps a fresh install working, but couples the two:
		// rotating JWT_SECRET makes stored secrets unreadable.
		key = cfg.JWT.Secret
		slog.Warn("SETTINGS_KEY is not set; deriving the settings encryption key from JWT_SECRET. " +
			"Rotating JWT_SECRET will make saved secrets unreadable and they will need re-entering.")
	}
	cipher, err := secrets.New(key)
	if err != nil {
		return nil, fmt.Errorf("settings encryption: %w", err)
	}
	return &SettingsService{db: db, cipher: cipher, cfg: cfg, cache: map[string]string{}}, nil
}

// load returns every setting, decrypting secrets, using a short-lived cache.
func (s *SettingsService) load(ctx context.Context) map[string]string {
	s.mu.RLock()
	fresh := time.Since(s.cachedAt) < settingsCacheTTL && s.cachedAt.After(time.Time{})
	if fresh {
		snapshot := make(map[string]string, len(s.cache))
		for k, v := range s.cache {
			snapshot[k] = v
		}
		s.mu.RUnlock()
		return snapshot
	}
	s.mu.RUnlock()

	var rows []models.Setting
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		slog.Error("could not load settings", "error", err)
		return map[string]string{}
	}

	values := make(map[string]string, len(rows))
	for _, row := range rows {
		if !row.Secret {
			values[row.Key] = row.Value
			continue
		}
		plain, err := s.cipher.Decrypt(row.Value)
		if err != nil {
			// Do not fall back to the ciphertext — a bot token that is really
			// a base64 blob would fail confusingly at the API instead of here.
			slog.Error("could not decrypt setting", "key", row.Key, "error", err)
			continue
		}
		values[row.Key] = plain
	}

	s.mu.Lock()
	s.cache = values
	s.cachedAt = time.Now()
	s.mu.Unlock()

	snapshot := make(map[string]string, len(values))
	for k, v := range values {
		snapshot[k] = v
	}
	return snapshot
}

// Get returns a setting, falling back to the supplied default.
func (s *SettingsService) Get(ctx context.Context, key, fallback string) string {
	if value, ok := s.load(ctx)[key]; ok && value != "" {
		return value
	}
	return fallback
}

// Set writes one value, encrypting it when the key is a declared secret.
//
// An empty value for a secret is treated as "leave it alone": the admin UI
// shows a mask rather than the real token, so a blank field means the editor
// did not change it, not that they want it cleared. Clearing is a separate,
// explicit action (Clear).
func (s *SettingsService) Set(ctx context.Context, key, value string, userID *uint) error {
	isSecret := models.SecretSettingKeys()[key]

	if isSecret && value == "" {
		return nil
	}

	stored := value
	if isSecret {
		encrypted, err := s.cipher.Encrypt(value)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", key, err)
		}
		stored = encrypted
	}

	record := models.Setting{
		Key: key, Value: stored, Secret: isSecret,
		Group: groupFor(key), UpdatedByID: userID,
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "setting_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "secret", "setting_group", "updated_by_id", "updated_at"}),
	}).Create(&record).Error
	if err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}

	s.invalidate()
	return nil
}

// Clear removes a setting, so the environment default applies again.
func (s *SettingsService) Clear(ctx context.Context, key string) error {
	if err := s.db.WithContext(ctx).Where("setting_key = ?", key).Delete(&models.Setting{}).Error; err != nil {
		return fmt.Errorf("clear setting %s: %w", key, err)
	}
	s.invalidate()
	return nil
}

func (s *SettingsService) invalidate() {
	s.mu.Lock()
	s.cachedAt = time.Time{}
	s.mu.Unlock()
}

func groupFor(key string) string {
	switch key {
	case models.SettingTelegramBotToken, models.SettingTelegramChannelID, models.SettingTelegramAutoPublish:
		return "telegram"
	default:
		return "site"
	}
}

// TelegramCredentials resolves the effective Telegram configuration:
// database first, environment as the fallback.
func (s *SettingsService) TelegramCredentials(ctx context.Context) (token, channel string, autoPublish bool) {
	values := s.load(ctx)

	token = values[models.SettingTelegramBotToken]
	if token == "" {
		token = s.cfg.Telegram.BotToken
	}
	channel = values[models.SettingTelegramChannelID]
	if channel == "" {
		channel = s.cfg.Telegram.ChannelID
	}

	autoPublish = s.cfg.Telegram.AutoPublish
	if raw, ok := values[models.SettingTelegramAutoPublish]; ok && raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			autoPublish = parsed
		}
	}
	return token, channel, autoPublish
}

// SiteNames resolves the site's name in both languages: database first,
// SITE_NAME / SITE_NAME_KH from the environment as the fallback. A name
// cleared in the admin therefore falls back to the deployment's default rather
// than leaving the site nameless.
func (s *SettingsService) SiteNames(ctx context.Context) (en, kh string) {
	values := s.load(ctx)
	en = strings.TrimSpace(values[models.SettingSiteName])
	if en == "" {
		en = s.cfg.App.SiteName
	}
	kh = strings.TrimSpace(values[models.SettingSiteNameKh])
	if kh == "" {
		kh = s.cfg.App.SiteNameKH
	}
	return en, kh
}

// TelegramView is the admin-safe projection: the token is masked, never sent.
type TelegramView struct {
	BotTokenMasked string `json:"botTokenMasked"`
	BotTokenSet    bool   `json:"botTokenSet"`
	// Source says where the effective value came from, so an operator can tell
	// a database override from an environment default.
	BotTokenSource string `json:"botTokenSource"`
	ChannelID      string `json:"channelId"`
	AutoPublish    bool   `json:"autoPublish"`
}

func (s *SettingsService) TelegramView(ctx context.Context) TelegramView {
	values := s.load(ctx)
	token, channel, auto := s.TelegramCredentials(ctx)

	source := "unset"
	switch {
	case values[models.SettingTelegramBotToken] != "":
		source = "database"
	case s.cfg.Telegram.BotToken != "":
		source = "environment"
	}

	return TelegramView{
		BotTokenMasked: secrets.Mask(token),
		BotTokenSet:    token != "",
		BotTokenSource: source,
		ChannelID:      channel,
		AutoPublish:    auto,
	}
}

var ErrSettingNotFound = errors.New("setting not found")
