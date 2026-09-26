// Package routes wires every handler onto the Gin engine.
package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/ai"
	"github.com/cambodia-fast-news/backend/internal/auth"
	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/controllers"
	"github.com/cambodia-fast-news/backend/internal/media"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/repositories"
	"github.com/cambodia-fast-news/backend/internal/seo"
	"github.com/cambodia-fast-news/backend/internal/services"
	"github.com/cambodia-fast-news/backend/internal/telegram"
	ws "github.com/cambodia-fast-news/backend/internal/websocket"
)

// Dependencies is everything the router needs, assembled in main.
type Dependencies struct {
	Cfg      *config.Config
	DB       *gorm.DB
	Cache    *cache.Cache
	Hub      *ws.Hub
	Tokens   *auth.Service
	Media    *media.Service
	AI       *ai.Service
	Telegram *telegram.Service
	SEO      *seo.Service

	Articles   *repositories.ArticleRepository
	ArticleSvc *services.ArticleService
	Trending   *services.TrendingService
	Views      *services.ViewService
	Ads        *services.AdService
	Audit      *services.AuditService
	Settings   *services.SettingsService
}

// Register builds the engine with the full middleware chain and route table.
func Register(d *Dependencies) *gin.Engine {
	if d.Cfg.App.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// One resolver decides which IP every request is billed to. It only
	// believes forwarding headers from our own proxies (§75).
	ipResolver := middleware.NewIPResolver(d.Cfg.TrustedProxies)

	r := gin.New()

	// Gin's own proxy trust would independently parse X-Forwarded-For for
	// c.ClientIP(). Disable it so IPResolver is the single source of truth.
	_ = r.SetTrustedProxies(nil)

	r.Use(middleware.Recovery(), middleware.Logger(ipResolver), middleware.SecurityHeaders(),
		middleware.CORS(d.Cfg), middleware.Device())

	// Uploads are capped at the router level too, so an oversized body is
	// rejected before it is buffered.
	r.MaxMultipartMemory = 16 << 20

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false, "message": "Endpoint not found", "code": "NOT_FOUND",
		})
	})

	// Controllers.
	news := controllers.NewNewsController(d.Articles, d.Trending, d.Views, d.Cache, d.DB)
	taxonomy := controllers.NewTaxonomyController(d.DB, d.Articles, d.Cache)
	video := controllers.NewVideoController(d.DB, d.Views)
	search := controllers.NewSearchController(d.DB, d.Articles)
	ads := controllers.NewAdController(d.Ads, d.DB)
	tips := controllers.NewTipController(d.DB)
	traffic := controllers.NewTrafficController(d.DB)
	authCtl := controllers.NewAuthController(d.DB, d.Tokens, d.Audit)
	adminNews := controllers.NewAdminNewsController(d.ArticleSvc, d.Articles, d.DB)
	aiCtl := controllers.NewAIController(d.AI, d.DB, d.Audit)
	mediaCtl := controllers.NewMediaController(d.Media, d.DB)
	seoCtl := controllers.NewSEOController(d.SEO, d.DB)
	ops := controllers.NewAdminOpsController(d.DB, d.Articles, d.ArticleSvc, d.Telegram, d.Hub, d.Audit, d.Cfg)
	adminAds := controllers.NewAdminAdController(d.DB, d.Ads, d.Audit)
	adminCategory := controllers.NewAdminCategoryController(d.DB, d.Cache, d.Audit)
	push := controllers.NewPushController(d.DB, d.Cfg)
	settingsCtl := controllers.NewSettingsController(d.Settings, d.Telegram, d.Audit)
	site := controllers.NewSiteController(d.Settings, d.Audit)
	pagesCtl := controllers.NewPageController(d.DB, d.Audit)
	adminVideo := controllers.NewAdminVideoController(d.DB, d.Audit)
	meta := controllers.NewMetaController(d.DB, d.ArticleSvc)
	access := controllers.NewAdminAccessController(d.DB, d.Audit)

	// Each class of endpoint gets its own bucket, so the six ad requests a
	// homepage fires cannot exhaust the budget a reader needs for articles.
	limits := d.Cfg.Limits
	bucket := func(name string, perMinute int) gin.HandlerFunc {
		return middleware.RateLimit(d.Cache, ipResolver, name, perMinute)
	}
	publicLimit := bucket("public", limits.PublicPerMin)
	adsLimit := bucket("ads", limits.AdsPerMin)
	beaconLimit := bucket("beacon", limits.BeaconPerMin)
	authLimit := bucket("auth", limits.AuthPerMin)
	aiLimit := bucket("ai", limits.AIPerMin)
	uploadLimit := bucket("upload", limits.UploadPerMin)
	writeLimit := bucket("write", 60)

	requireAuth := middleware.RequireAuth(d.Tokens, d.DB)

	// ── SEO documents, served at the root, not under /api ──────────────────
	r.GET("/robots.txt", seoCtl.Robots)
	r.GET("/sitemap.xml", seoCtl.Sitemap)
	r.GET("/news-sitemap.xml", seoCtl.NewsSitemap)

	// ── WebSocket (§7) ─────────────────────────────────────────────────────
	upgrader := ws.Upgrader(d.Cfg.CORS.AllowedOrigins)
	r.GET("/ws", func(c *gin.Context) {
		ws.Serve(d.Hub, upgrader, c.Writer, c.Request)
	})

	api := r.Group("/api")
	api.GET("/health", ops.Health)

	// ── Public read endpoints (§65) ────────────────────────────────────────
	pub := api.Group("", publicLimit)
	{
		pub.GET("/news", news.List)
		pub.GET("/news/:slug", news.Get)
		pub.GET("/featured", news.Featured)
		pub.GET("/breaking", news.Breaking)
		pub.GET("/live", news.Live)
		pub.GET("/trending", news.Trending)
		pub.GET("/five-minute", news.FiveMinute)
		pub.GET("/pulse", news.Pulse)
		pub.GET("/archive", news.Archive)

		// Option lists for forms and filters, derived from the same constants
		// the API validates against.
		pub.GET("/meta", meta.Meta)

		// Footer chrome: tagline, contact details and social profiles.
		pub.GET("/site", site.Get)

		// Standalone pages: the policies, About and Contact.
		pub.GET("/pages", pagesCtl.List)
		pub.GET("/pages/:slug", pagesCtl.Get)

		pub.GET("/categories", taxonomy.List)
		pub.GET("/categories/:slug", taxonomy.Get)
		pub.GET("/authors", taxonomy.Authors)
		pub.GET("/authors/:slug", taxonomy.Author)

		pub.GET("/video", video.List)
		pub.GET("/video/:slug", video.Get)
		pub.GET("/live-video", video.Live)

		pub.GET("/search", search.Search)
		pub.GET("/traffic-status", traffic.Status)

		pub.GET("/push/key", push.PublicKey)
		pub.GET("/redirects/resolve", seoCtl.ResolveRedirect)
	}

	// ── Advertising ────────────────────────────────────────────────────────
	// Every ad slot on a page makes its own request, so these get a separate
	// budget from article reads.
	{
		adsGroup := api.Group("", adsLimit)
		adsGroup.GET("/ads", ads.Serve)
		adsGroup.GET("/ads/slots", ads.Slots)
	}

	// ── Public write endpoints — tighter limits, no auth ───────────────────
	{
		// Beacons are high-volume by nature: a reader scrolling an article
		// fires view, share and ad-impression pings.
		beacon := api.Group("", beaconLimit)
		beacon.POST("/news/:slug/view", news.TrackView)
		beacon.POST("/news/:slug/share", news.TrackShare)
		beacon.POST("/video/:slug/share", video.TrackShare)
		beacon.POST("/ads/:id/impression", ads.Impression)
		beacon.POST("/ads/:id/click", ads.Click)

		// A tip is a human action; 5 a minute is plenty and blunts spam.
		submit := api.Group("", bucket("tip", 5))
		submit.POST("/tip", tips.Submit)

		subscribe := api.Group("", bucket("push", 20))
		subscribe.POST("/push/subscribe", push.Subscribe)
		subscribe.POST("/push/unsubscribe", push.Unsubscribe)
	}

	// ── Authentication ─────────────────────────────────────────────────────
	authGroup := api.Group("/auth")
	{
		authGroup.POST("/login", authLimit, authCtl.Login)
		authGroup.POST("/refresh", authLimit, authCtl.Refresh)
		authGroup.GET("/me", requireAuth, authCtl.Me)
		authGroup.POST("/password", requireAuth, authLimit, authCtl.ChangePassword)
	}

	// ── Editorial: article CRUD ────────────────────────────────────────────
	// These are the paths documented in §65, each behind auth plus a specific
	// permission. The workflow sub-routes live under /api/admin/news/:id
	// instead: the public beacons already own POST /api/news/:slug/*, and one
	// router tree cannot bind two different parameter names at that position.
	editorial := api.Group("", requireAuth, writeLimit)
	{
		editorial.POST("/news", middleware.RequirePermission(auth.NewsCreate), adminNews.Create)
		editorial.PUT("/news/:id", middleware.RequirePermission(auth.NewsEdit), adminNews.Update)
		editorial.DELETE("/news/:id", middleware.RequirePermission(auth.NewsDelete), adminNews.Delete)

		editorial.POST("/telegram/publish", middleware.RequirePermission(auth.TelegramManage), ops.TelegramPublish)

		// Video (§26). Publishing is embedding a YouTube link, so the same
		// permission that covers video production covers the whole lifecycle.
		editorial.POST("/videos", middleware.RequirePermission(auth.VideoManage), adminVideo.Create)
		editorial.PUT("/videos/:id", middleware.RequirePermission(auth.VideoManage), adminVideo.Update)
		editorial.DELETE("/videos/:id", middleware.RequirePermission(auth.VideoManage), adminVideo.Delete)

		editorial.POST("/ads", middleware.RequirePermission(auth.AdsCreate), adminAds.CreateAd)
		editorial.PUT("/ads/:id", middleware.RequirePermission(auth.AdsEdit), adminAds.UpdateAd)
		editorial.DELETE("/ads/:id", middleware.RequirePermission(auth.AdsDelete), adminAds.DeleteAd)
		editorial.PUT("/ads/:id/status", middleware.RequirePermission(auth.AdsPublish), adminAds.UpdateAdStatus)
	}

	// ── AI assistants (§21–§25) ────────────────────────────────────────────
	aiGroup := api.Group("/ai", requireAuth, middleware.RequirePermission(auth.AIUse), aiLimit)
	{
		aiGroup.GET("/status", aiCtl.Status)
		aiGroup.GET("/image-prompts", aiCtl.ImagePrompts)
		aiGroup.POST("/generate-news", aiCtl.GenerateNews)
		aiGroup.POST("/generate-seo", aiCtl.GenerateSEO)
		aiGroup.POST("/generate-summary", aiCtl.GenerateSummary)
	}

	// ── Media library ──────────────────────────────────────────────────────
	mediaGroup := api.Group("/media", requireAuth)
	{
		mediaGroup.GET("", middleware.RequirePermission(auth.MediaUpload), mediaCtl.List)
		mediaGroup.POST("/upload", middleware.RequirePermission(auth.MediaUpload), uploadLimit, mediaCtl.Upload)
		mediaGroup.PATCH("/:id", middleware.RequirePermission(auth.MediaUpload), mediaCtl.Update)
		mediaGroup.DELETE("/:id", middleware.RequirePermission(auth.MediaDelete), mediaCtl.Delete)
	}

	// ── Admin surfaces ─────────────────────────────────────────────────────
	admin := api.Group("/admin", requireAuth)
	{
		admin.GET("/dashboard", middleware.RequireAnyPermission(auth.NewsView, auth.AnalyticsView), ops.Dashboard)

		admin.GET("/news", middleware.RequirePermission(auth.NewsView), adminNews.List)
		admin.GET("/news/:id", middleware.RequirePermission(auth.NewsView), adminNews.Get)
		admin.GET("/news/:id/revisions", middleware.RequirePermission(auth.NewsView), adminNews.Revisions)
		admin.PUT("/news/:id/seo", middleware.RequirePermission(auth.SEOEdit), seoCtl.SaveMetadata)

		// Workflow transitions (§16, §17, §19, §20).
		admin.POST("/news/:id/status", writeLimit, middleware.RequirePermission(auth.NewsEdit), adminNews.Transition)
		admin.POST("/news/:id/breaking", writeLimit, middleware.RequirePermission(auth.NewsPublish), adminNews.SetBreaking)
		admin.POST("/news/:id/correction", writeLimit, middleware.RequirePermission(auth.NewsPublish), adminNews.AddCorrection)
		admin.POST("/news/:id/sources", writeLimit, middleware.RequirePermission(auth.NewsEdit), adminNews.AddSource)

		admin.GET("/seo/health", middleware.RequirePermission(auth.SEOManage), seoCtl.Health)
		admin.GET("/redirects", middleware.RequirePermission(auth.SEOManage), seoCtl.Redirects)
		admin.POST("/redirects", middleware.RequirePermission(auth.SEOManage), seoCtl.CreateRedirect)

		admin.GET("/telegram", middleware.RequirePermission(auth.TelegramManage), ops.TelegramStatus)

		admin.GET("/tips", middleware.RequirePermission(auth.TipsReview), ops.Tips)
		admin.PATCH("/tips/:id", middleware.RequirePermission(auth.TipsReview), ops.ReviewTip)

		admin.PUT("/traffic/:id", middleware.RequirePermission(auth.TrafficManage), ops.UpdateTraffic)

		admin.GET("/analytics", middleware.RequirePermission(auth.AnalyticsView), ops.Analytics)
		admin.GET("/audit-logs", middleware.RequirePermission(auth.UsersManage), ops.AuditLog)
		admin.GET("/audit-logs/retention", middleware.RequirePermission(auth.UsersManage), ops.AuditRetention)
		// Trimming the log removes the record of other actions, so it needs
		// RolesManage — Super Admin only — not UsersManage.
		admin.DELETE("/audit-logs", writeLimit, middleware.RequirePermission(auth.RolesManage), ops.PurgeAuditLog)

		// Every active category, including the ones hidden from the public nav,
		// so a form can file a story anywhere it legitimately belongs.
		admin.GET("/categories", middleware.RequireAnyPermission(auth.NewsView, auth.VideoManage), taxonomy.AdminList)

		// The section structure itself. Reorder is registered before :id so
		// "order" is never parsed as an id.
		categories := admin.Group("/categories", middleware.RequirePermission(auth.CategoriesManage))
		{
			categories.POST("", writeLimit, adminCategory.Create)
			categories.PUT("/order", writeLimit, adminCategory.Reorder)
			categories.PUT("/:id", writeLimit, adminCategory.Update)
			categories.DELETE("/:id", writeLimit, adminCategory.Delete)
		}

		admin.GET("/videos", middleware.RequirePermission(auth.VideoManage), adminVideo.List)
		admin.GET("/videos/:id", middleware.RequirePermission(auth.VideoManage), adminVideo.Get)
		admin.POST("/videos/:id/status", writeLimit, middleware.RequirePermission(auth.VideoManage), adminVideo.SetStatus)

		// Runtime configuration. Secrets are write-only: the token is returned
		// masked and can be replaced, never read back.
		pages := admin.Group("/pages", middleware.RequirePermission(auth.PagesManage))
		{
			pages.GET("", pagesCtl.AdminList)
			pages.GET("/:id", pagesCtl.AdminGet)
			pages.POST("", writeLimit, pagesCtl.Create)
			pages.PUT("/:id", writeLimit, pagesCtl.Update)
			pages.DELETE("/:id", writeLimit, pagesCtl.Delete)
		}

		admin.GET("/settings/site", middleware.RequirePermission(auth.SettingsManage), site.AdminGet)
		admin.PUT("/settings/site", writeLimit, middleware.RequirePermission(auth.SettingsManage), site.AdminSave)

		admin.GET("/settings/telegram", middleware.RequirePermission(auth.SettingsManage), settingsCtl.GetTelegram)
		admin.PUT("/settings/telegram", writeLimit, middleware.RequirePermission(auth.SettingsManage), settingsCtl.SaveTelegram)
		admin.DELETE("/settings/telegram/token", middleware.RequirePermission(auth.SettingsManage), settingsCtl.ClearTelegramToken)
		admin.POST("/settings/telegram/test", writeLimit, middleware.RequirePermission(auth.SettingsManage), settingsCtl.TestTelegram)

		admin.GET("/ads/campaigns", middleware.RequirePermission(auth.AdsView), adminAds.Campaigns)
		admin.POST("/ads/campaigns", middleware.RequirePermission(auth.AdsCreate), adminAds.CreateCampaign)
		admin.POST("/ads/campaigns/:id/approve", middleware.RequirePermission(auth.AdsPublish), adminAds.ApproveCampaign)
		admin.GET("/ads", middleware.RequirePermission(auth.AdsView), adminAds.Ads)
		admin.GET("/ads/:id", middleware.RequirePermission(auth.AdsView), adminAds.Ad)
		admin.GET("/ads/campaigns/:id", middleware.RequirePermission(auth.AdsView), adminAds.Campaign)
		admin.PUT("/ads/campaigns/:id", writeLimit, middleware.RequirePermission(auth.AdsEdit), adminAds.UpdateCampaign)
		admin.DELETE("/ads/campaigns/:id", middleware.RequirePermission(auth.AdsDelete), adminAds.DeleteCampaign)
		admin.GET("/ads/performance", middleware.RequirePermission(auth.AdsView), adminAds.Performance)

		// Access control. Roles are seeded, not editable here: rewriting what a
		// role may do from an admin screen is a privilege-escalation surface.
		// Assigning an existing role to a person is the day-to-day operation.
		admin.GET("/roles", middleware.RequirePermission(auth.UsersManage), access.Roles)
		admin.GET("/permissions", middleware.RequirePermission(auth.UsersManage), access.Permissions)
		// RolesManage, not UsersManage: redefining a role can hand out every
		// other permission, so it stays with Super Admin.
		admin.PUT("/roles/:id/permissions", writeLimit,
			middleware.RequirePermission(auth.RolesManage), access.SetRolePermissions)
		admin.DELETE("/roles/:id/permissions", writeLimit,
			middleware.RequirePermission(auth.RolesManage), access.ResetRolePermissions)
		admin.GET("/users", middleware.RequirePermission(auth.UsersManage), access.Users)
		admin.PUT("/users/:id/role", writeLimit, middleware.RequirePermission(auth.UsersManage), access.SetUserRole)
		admin.PUT("/users/:id/active", writeLimit, middleware.RequirePermission(auth.UsersManage), access.SetUserActive)
	}

	return r
}
