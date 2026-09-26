package controllers

import (
	"github.com/gin-gonic/gin"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
	"github.com/cambodia-fast-news/backend/internal/telegram"
)

// SettingsController exposes runtime configuration to the admin.
//
// Secrets are write-only over this API: the token is returned masked and can
// be replaced, never read back. An admin screen that can display a live
// credential is one screenshot or one over-the-shoulder glance from leaking it.
type SettingsController struct {
	settings *services.SettingsService
	telegram *telegram.Service
	audit    *services.AuditService
}

func NewSettingsController(s *services.SettingsService, t *telegram.Service, audit *services.AuditService) *SettingsController {
	return &SettingsController{settings: s, telegram: t, audit: audit}
}

// GetTelegram handles GET /api/admin/settings/telegram.
func (ctl *SettingsController) GetTelegram(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, ctl.settings.TelegramView(c.Request.Context()))
}

type telegramSettingsRequest struct {
	// Empty means "leave the saved token alone" — the UI shows a mask, so a
	// blank field is an unchanged field, not a request to clear it.
	BotToken    string `json:"botToken"`
	ChannelID   string `json:"channelId"`
	AutoPublish *bool  `json:"autoPublish"`
}

// SaveTelegram handles PUT /api/admin/settings/telegram.
func (ctl *SettingsController) SaveTelegram(c *gin.Context) {
	var req telegramSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid request")
		return
	}

	ctx := c.Request.Context()
	userID := middleware.CurrentUserID(c)

	// Reject a token that Telegram will not accept, rather than saving a typo
	// and leaving publishing quietly broken until someone notices.
	if req.BotToken != "" {
		if err := ctl.telegram.VerifyToken(ctx, req.BotToken); err != nil {
			httpx.BadRequest(c, "TELEGRAM_TOKEN_REJECTED",
				"Telegram rejected that bot token. Check it and try again.")
			return
		}
		if err := ctl.settings.Set(ctx, models.SettingTelegramBotToken, req.BotToken, userID); err != nil {
			httpx.Internal(c, "Could not save the bot token")
			return
		}
	}

	if req.ChannelID != "" {
		if err := ctl.settings.Set(ctx, models.SettingTelegramChannelID, req.ChannelID, userID); err != nil {
			httpx.Internal(c, "Could not save the channel")
			return
		}
	}
	if req.AutoPublish != nil {
		value := "false"
		if *req.AutoPublish {
			value = "true"
		}
		if err := ctl.settings.Set(ctx, models.SettingTelegramAutoPublish, value, userID); err != nil {
			httpx.Internal(c, "Could not save the auto-publish setting")
			return
		}
	}

	// The token itself is never written to the audit log.
	ctl.audit.Record(ctx, userID, "settings.telegram", "setting", nil,
		"updated Telegram settings", nil)

	httpx.OK(c, ctl.settings.TelegramView(ctx))
}

// ClearTelegramToken handles DELETE /api/admin/settings/telegram/token.
func (ctl *SettingsController) ClearTelegramToken(c *gin.Context) {
	ctx := c.Request.Context()
	if err := ctl.settings.Clear(ctx, models.SettingTelegramBotToken); err != nil {
		httpx.Internal(c, "Could not clear the bot token")
		return
	}
	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "settings.telegram_token_cleared",
		"setting", nil, "cleared the stored Telegram bot token", nil)
	httpx.OK(c, ctl.settings.TelegramView(ctx))
}

// TestTelegram handles POST /api/admin/settings/telegram/test — checks the
// saved credentials against Telegram without changing anything.
func (ctl *SettingsController) TestTelegram(c *gin.Context) {
	ctx := c.Request.Context()

	if !ctl.telegram.Configured(ctx) {
		httpx.Fail(c, 503, httpx.CodeNotConfigured, "No bot token and channel are configured yet")
		return
	}
	if err := ctl.telegram.VerifyConnection(ctx); err != nil {
		httpx.Fail(c, 502, "TELEGRAM_UNREACHABLE", err.Error())
		return
	}
	httpx.OK(c, gin.H{"connected": true, "message": "Telegram accepted the credentials"})
}
