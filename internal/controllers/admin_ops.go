package controllers

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/repositories"
	"github.com/cambodia-fast-news/backend/internal/services"
	"github.com/cambodia-fast-news/backend/internal/telegram"
	"github.com/cambodia-fast-news/backend/internal/websocket"
)

// AdminOpsController backs the dashboard and the operational panels: Telegram,
// push, tips, traffic, analytics and the audit log (§68–§71).
type AdminOpsController struct {
	db       *gorm.DB
	repo     *repositories.ArticleRepository
	articles *services.ArticleService
	telegram *telegram.Service
	hub      *websocket.Hub
	audit    *services.AuditService
	cfg      *config.Config
}

func NewAdminOpsController(
	db *gorm.DB,
	repo *repositories.ArticleRepository,
	articles *services.ArticleService,
	tg *telegram.Service,
	hub *websocket.Hub,
	audit *services.AuditService,
	cfg *config.Config,
) *AdminOpsController {
	return &AdminOpsController{db: db, repo: repo, articles: articles, telegram: tg, hub: hub, audit: audit, cfg: cfg}
}

// Dashboard handles GET /api/admin/dashboard (§68).
func (ctl *AdminOpsController) Dashboard(c *gin.Context) {
	ctx := c.Request.Context()
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	today := todayStart.Format("2006-01-02")

	count := func(model any, query string, args ...any) int64 {
		var n int64
		ctl.db.WithContext(ctx).Model(model).Where(query, args...).Count(&n)
		return n
	}

	article := &models.Article{}
	news := gin.H{
		"todayPublished": count(article, "status = ? AND published_at >= ?", models.StatusPublished, todayStart),
		"breaking":       count(article, "is_breaking = ? AND status = ?", true, models.StatusPublished),
		"drafts":         count(article, "status = ?", models.StatusDraft),
		"reviewQueue":    count(article, "status = ?", models.StatusReview),
		"scheduled":      count(article, "status = ?", models.StatusScheduled),
		"totalPublished": count(article, "status = ?", models.StatusPublished),
	}

	var todayViews, todayUniques int64
	ctl.db.WithContext(ctx).Model(&models.ArticleView{}).
		Where("day = ?", today).
		Select("COALESCE(SUM(views),0)").Scan(&todayViews)
	ctl.db.WithContext(ctx).Model(&models.ArticleView{}).
		Where("day = ?", today).
		Select("COALESCE(SUM(unique_views),0)").Scan(&todayUniques)

	var videoViews int64
	ctl.db.WithContext(ctx).Model(&models.Video{}).
		Select("COALESCE(SUM(view_count),0)").Scan(&videoViews)

	var adImpressions, adClicks int64
	ctl.db.WithContext(ctx).Model(&models.AdvertisementImpression{}).
		Where("day = ?", today).Select("COALESCE(SUM(count),0)").Scan(&adImpressions)
	ctl.db.WithContext(ctx).Model(&models.AdvertisementClick{}).
		Where("day = ?", today).Select("COALESCE(SUM(count),0)").Scan(&adClicks)

	ctr := 0.0
	if adImpressions > 0 {
		ctr = float64(adClicks) / float64(adImpressions) * 100
	}

	ads := gin.H{
		"activeAds":        count(&models.Advertisement{}, "status = ?", models.AdActive),
		"activeCampaigns":  count(&models.AdvertisementCampaign{}, "status = ?", models.AdActive),
		"expiredCampaigns": count(&models.AdvertisementCampaign{}, "status = ?", models.AdExpired),
		"pendingApproval":  count(&models.AdvertisementCampaign{}, "approved_at IS NULL AND status <> ?", models.AdDraft),
		"impressionsToday": adImpressions,
		"clicksToday":      adClicks,
		"ctrToday":         ctr,
	}

	var lastTelegram models.TelegramPost
	hasTelegramPost := ctl.db.WithContext(ctx).
		Where("status = ?", "sent").Order("sent_at DESC").First(&lastTelegram).Error == nil

	distribution := gin.H{
		"telegramConfigured":  ctl.telegram.Configured(ctx),
		"telegramAutoPublish": ctl.telegram.AutoPublishEnabled(ctx),
		"telegramPostsToday":  count(&models.TelegramPost{}, "status = ? AND sent_at >= ?", "sent", todayStart),
		"telegramFailed":      count(&models.TelegramPost{}, "status = ?", "failed"),
		"pushSubscribers":     count(&models.PushSubscription{}, "is_active = ?", true),
		"websocketClients":    ctl.hub.Count(),
	}
	if hasTelegramPost {
		distribution["telegramLastPostAt"] = lastTelegram.SentAt
	}

	moderation := gin.H{
		"pendingTips":     count(&models.NewsTip{}, "status = ?", models.TipPending),
		"pendingComments": count(&models.Comment{}, "status = ?", "pending"),
		"openReports":     count(&models.CommentReport{}, "status = ?", "open"),
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, gin.H{
		"news":         news,
		"traffic":      gin.H{"viewsToday": todayViews, "uniqueVisitorsToday": todayUniques, "videoViews": videoViews},
		"ads":          ads,
		"distribution": distribution,
		"moderation":   moderation,
		"system":       gin.H{"environment": ctl.cfg.App.Env, "serverTime": now},
	})
}

// TelegramStatus handles GET /api/admin/telegram (§70).
func (ctl *AdminOpsController) TelegramStatus(c *gin.Context) {
	ctx := c.Request.Context()

	connected := false
	var connectionError string
	if ctl.telegram.Configured(ctx) {
		if err := ctl.telegram.VerifyConnection(ctx); err != nil {
			connectionError = err.Error()
		} else {
			connected = true
		}
	}

	var recent []models.TelegramPost
	ctl.db.WithContext(ctx).Order("created_at DESC").Limit(20).Find(&recent)

	httpx.OK(c, gin.H{
		"configured":  ctl.telegram.Configured(ctx),
		"connected":   connected,
		"error":       connectionError,
		"autoPublish": ctl.telegram.AutoPublishEnabled(ctx),
		"recentPosts": recent,
	})
}

// TelegramPublish handles POST /api/telegram/publish — the manual send (§70).
func (ctl *AdminOpsController) TelegramPublish(c *gin.Context) {
	var req struct {
		ArticleID uint `json:"articleId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "An article id is required")
		return
	}

	ctx := c.Request.Context()
	if !ctl.telegram.Configured(ctx) {
		httpx.Fail(c, 503, httpx.CodeNotConfigured, "Telegram is not configured")
		return
	}
	article, err := ctl.repo.GetByID(ctx, req.ArticleID)
	if err != nil {
		httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
		return
	}
	// Posting an unpublished article would send readers to a 404.
	if article.Status != models.StatusPublished {
		httpx.BadRequest(c, httpx.CodeValidation, "Only published articles can be sent to Telegram")
		return
	}

	ctl.articles.PublishToTelegram(ctx, article, middleware.CurrentUserID(c))
	httpx.OK(c, gin.H{"message": "Sent to Telegram"})
}

// Tips handles GET /api/admin/tips (§34).
func (ctl *AdminOpsController) Tips(c *gin.Context) {
	page := queryInt(c, "page", 1, 1, 200)
	limit := queryInt(c, "limit", 25, 1, 100)

	q := ctl.db.WithContext(c.Request.Context()).Model(&models.NewsTip{})
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}

	var total int64
	q.Session(&gorm.Session{}).Count(&total)

	var tips []models.NewsTip
	if err := q.Order("created_at DESC").Limit(limit).Offset((page - 1) * limit).Find(&tips).Error; err != nil {
		httpx.Internal(c, "Could not load tips")
		return
	}
	httpx.OKList(c, tips, httpx.NewMeta(page, limit, total))
}

// ReviewTip handles PATCH /api/admin/tips/:id.
func (ctl *AdminOpsController) ReviewTip(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid tip id")
		return
	}

	var req struct {
		Status models.TipStatus `json:"status" binding:"required"`
		Note   string           `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A status is required")
		return
	}

	switch req.Status {
	case models.TipPending, models.TipReviewing, models.TipVerified, models.TipRejected, models.TipPublished:
	default:
		httpx.BadRequest(c, httpx.CodeValidation, "Unknown tip status")
		return
	}

	ctx := c.Request.Context()
	now := time.Now().UTC()
	err := ctl.db.WithContext(ctx).Model(&models.NewsTip{}).Where("id = ?", id).
		Updates(map[string]any{
			"status": req.Status, "review_note": req.Note,
			"reviewed_by_id": middleware.CurrentUserID(c), "reviewed_at": now,
		}).Error
	if err != nil {
		httpx.Internal(c, "Could not update the tip")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "tip."+string(req.Status), "news_tip", &id, req.Note, nil)
	httpx.NoData(c)
}

// UpdateTraffic handles PUT /api/admin/traffic/:id (§71).
func (ctl *AdminOpsController) UpdateTraffic(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid route id")
		return
	}

	var req struct {
		Level       models.TrafficLevel `json:"level" binding:"required"`
		NoteKh      string              `json:"noteKh"`
		SourceLabel string              `json:"sourceLabel"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A traffic level is required")
		return
	}
	switch req.Level {
	case models.TrafficNormal, models.TrafficModerate, models.TrafficHeavy:
	default:
		httpx.BadRequest(c, httpx.CodeValidation, "Unknown traffic level")
		return
	}
	if req.SourceLabel == "" {
		req.SourceLabel = "CFN newsroom"
	}

	ctx := c.Request.Context()
	now := time.Now().UTC()
	// ObservedAt is set to now because a human is asserting the condition now.
	// It is what the public endpoint reports, so the board can never look more
	// current than the last human assessment (§71).
	err := ctl.db.WithContext(ctx).Model(&models.TrafficStatus{}).Where("id = ?", id).
		Updates(map[string]any{
			"level": req.Level, "note_kh": req.NoteKh,
			"source_label": req.SourceLabel, "observed_at": now,
			"updated_by_id": middleware.CurrentUserID(c),
		}).Error
	if err != nil {
		httpx.Internal(c, "Could not update the traffic board")
		return
	}

	ctl.hub.Publish(websocket.EventTraffic, gin.H{"routeId": id, "level": req.Level, "observedAt": now})
	httpx.NoData(c)
}

// Analytics handles GET /api/admin/analytics (§43).
func (ctl *AdminOpsController) Analytics(c *gin.Context) {
	ctx := c.Request.Context()
	days := queryInt(c, "days", 7, 1, 90)
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	type dayRow struct {
		Day         string `json:"day"`
		Views       int64  `json:"views"`
		UniqueViews int64  `json:"uniqueViews"`
	}
	// Declared with a length, not as a nil slice: encoding/json turns a nil
	// slice into `null`, and a client doing `data.daily.length` then throws and
	// blanks the whole page. An empty list is the honest answer to "no activity".
	daily := []dayRow{}
	ctl.db.WithContext(ctx).Model(&models.ArticleView{}).
		Select("day, COALESCE(SUM(views),0) AS views, COALESCE(SUM(unique_views),0) AS unique_views").
		Where("day >= ?", since).Group("day").Order("day ASC").Scan(&daily)

	type topRow struct {
		Slug    string `json:"slug"`
		TitleKh string `json:"titleKh"`
		Views   int64  `json:"views"`
	}
	top := []topRow{}
	ctl.db.WithContext(ctx).Raw(`
		SELECT a.slug, a.title_kh, COALESCE(SUM(v.views), 0) AS views
		FROM article_views v JOIN articles a ON a.id = v.article_id
		WHERE v.day >= ? AND a.deleted_at IS NULL
		GROUP BY a.id, a.slug, a.title_kh
		ORDER BY views DESC LIMIT 20`, since).Scan(&top)

	type shareRow struct {
		Network string `json:"network"`
		Count   int64  `json:"count"`
	}
	// Articles and videos live in separate tables (the daily upsert needs a
	// unique index that cannot span nullable columns), so the breakdown is the
	// union of both. A reader sharing a video is a share either way.
	shares := []shareRow{}
	ctl.db.WithContext(ctx).Raw(`
		SELECT network, COALESCE(SUM(count), 0) AS count FROM (
			SELECT network, count FROM article_shares WHERE day >= ?
			UNION ALL
			SELECT network, count FROM video_shares  WHERE day >= ?
		) AS combined
		GROUP BY network ORDER BY count DESC`, since, since).Scan(&shares)

	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, gin.H{
		"days": days, "daily": daily, "topArticles": top, "shares": shares,
		"note": "Aggregated page and article activity. No per-visitor records are stored.",
	})
}

// AuditLog handles GET /api/admin/audit-logs.
func (ctl *AdminOpsController) AuditLog(c *gin.Context) {
	page := queryInt(c, "page", 1, 1, 500)
	limit := queryInt(c, "limit", 50, 1, 100)

	entries, total, err := ctl.audit.List(c.Request.Context(), page, limit)
	if err != nil {
		httpx.Internal(c, "Could not load the audit log")
		return
	}
	httpx.OKList(c, entries, httpx.NewMeta(page, limit, total))
}

// AuditRetention handles GET /api/admin/audit-logs/retention — the windows on
// offer and what each one would remove.
func (ctl *AdminOpsController) AuditRetention(c *gin.Context) {
	stats, err := ctl.audit.Stats(c.Request.Context())
	if err != nil {
		httpx.Internal(c, "Could not read the audit log size")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, stats)
}

// PurgeAuditLog handles DELETE /api/admin/audit-logs?months=3.
//
// Gated on RolesManage rather than UsersManage: trimming the audit log is the
// one action that removes the record of other actions, so it stays with Super
// Admin for the same reason editing a role's permissions does.
func (ctl *AdminOpsController) PurgeAuditLog(c *gin.Context) {
	// Parsed strictly, not with queryInt: queryInt clamps out-of-range values,
	// so ?months=12 would have silently become 3 and deleted nine months more
	// than the caller asked to remove. A retention window has to be exact.
	months, err := strconv.Atoi(c.Query("months"))
	if err != nil || months < 1 || months > 3 {
		httpx.BadRequest(c, httpx.CodeValidation,
			"Choose how many months of history to keep: 1, 2 or 3.")
		return
	}

	removed, purgeErr := ctl.audit.Purge(c.Request.Context(), months, middleware.CurrentUserID(c))
	if purgeErr != nil {
		httpx.BadRequest(c, httpx.CodeValidation, purgeErr.Error())
		return
	}
	httpx.OK(c, gin.H{"removed": removed, "months": months})
}

// Health handles GET /api/health — the liveness probe used by Docker and
// Cloudflare.
func (ctl *AdminOpsController) Health(c *gin.Context) {
	dbOK := true
	if sqlDB, err := ctl.db.DB(); err != nil || sqlDB.PingContext(c.Request.Context()) != nil {
		dbOK = false
	}

	status := 200
	if !dbOK {
		status = 503
	}
	c.JSON(status, gin.H{
		"success":   dbOK,
		"database":  dbOK,
		"websocket": ctl.hub.Count(),
		"time":      time.Now().UTC(),
	})
}
