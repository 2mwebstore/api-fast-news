package controllers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// PushController manages Web Push subscriptions (§30).
type PushController struct {
	db  *gorm.DB
	cfg *config.Config
}

func NewPushController(db *gorm.DB, cfg *config.Config) *PushController {
	return &PushController{db: db, cfg: cfg}
}

// validTopics is the allowlist from §30. Anything else is dropped rather than
// stored, so a client cannot invent topics.
var validTopics = map[string]bool{
	models.TopicBreaking: true, models.TopicCambodia: true, models.TopicSports: true,
	models.TopicKunKhmer: true, models.TopicBusiness: true,
	models.TopicTechnology: true, models.TopicEntertainment: true,
}

// PublicKey handles GET /api/push/key — the VAPID key the browser needs.
func (ctl *PushController) PublicKey(c *gin.Context) {
	httpx.OK(c, gin.H{
		"publicKey":  ctl.cfg.Push.PublicKey,
		"configured": ctl.cfg.Push.Configured(),
		"topics":     []string{
			models.TopicBreaking, models.TopicCambodia, models.TopicSports,
			models.TopicKunKhmer, models.TopicBusiness,
			models.TopicTechnology, models.TopicEntertainment,
		},
	})
}

// Subscribe handles POST /api/push/subscribe.
func (ctl *PushController) Subscribe(c *gin.Context) {
	var req struct {
		Endpoint string   `json:"endpoint" binding:"required"`
		Keys     struct {
			P256dh string `json:"p256dh" binding:"required"`
			Auth   string `json:"auth" binding:"required"`
		} `json:"keys" binding:"required"`
		Topics []string `json:"topics"`
		Lang   string   `json:"lang"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A valid push subscription is required")
		return
	}

	topics := make([]string, 0, len(req.Topics))
	for _, t := range req.Topics {
		if validTopics[t] {
			topics = append(topics, t)
		}
	}
	// Someone who subscribes without choosing topics gets breaking news only.
	// Defaulting to everything would be the "push every article" behaviour §30
	// rules out.
	if len(topics) == 0 {
		topics = []string{models.TopicBreaking}
	}

	lang := req.Lang
	if lang != "en" {
		lang = "km"
	}

	subscription := models.PushSubscription{
		Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth,
		Topics: models.StringSlice(topics), Lang: lang, IsActive: true,
		UserAgent: utils.Truncate(c.GetHeader("User-Agent"), 250),
	}

	// Re-subscribing from the same browser updates the existing row rather than
	// creating a duplicate that would double every notification.
	err := ctl.db.WithContext(c.Request.Context()).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "endpoint"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"p256dh", "auth", "topics", "lang", "is_active", "user_agent", "updated_at",
		}),
	}).Create(&subscription).Error
	if err != nil {
		httpx.Internal(c, "Could not save your notification settings")
		return
	}

	httpx.Created(c, gin.H{"topics": topics, "message": "Notification preferences saved"})
}

// Unsubscribe handles POST /api/push/unsubscribe.
func (ctl *PushController) Unsubscribe(c *gin.Context) {
	var req struct {
		Endpoint string `json:"endpoint" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "An endpoint is required")
		return
	}

	err := ctl.db.WithContext(c.Request.Context()).Model(&models.PushSubscription{}).
		Where("endpoint = ?", req.Endpoint).Update("is_active", false).Error
	if err != nil {
		httpx.Internal(c, "Could not update your notification settings")
		return
	}
	httpx.NoData(c)
}
