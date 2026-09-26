package controllers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/repositories"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// NewsController serves the public reading endpoints.
type NewsController struct {
	repo     *repositories.ArticleRepository
	trending *services.TrendingService
	views    *services.ViewService
	cache    *cache.Cache
	db       *gorm.DB
}

func NewNewsController(
	repo *repositories.ArticleRepository,
	trending *services.TrendingService,
	views *services.ViewService,
	c *cache.Cache,
	db *gorm.DB,
) *NewsController {
	return &NewsController{repo: repo, trending: trending, views: views, cache: c, db: db}
}

// List handles GET /api/news (§10).
func (ctl *NewsController) List(c *gin.Context) {
	filter := buildArticleFilter(c)

	articles, total, err := ctl.repo.List(c.Request.Context(), filter)
	if err != nil {
		httpx.Internal(c, "Could not load news")
		return
	}

	// Lists are cacheable at the edge briefly; a newsroom publishes often
	// enough that anything longer would make the feed feel stale.
	c.Header("Cache-Control", "public, max-age=30, stale-while-revalidate=120")
	httpx.OKList(c, toCards(articles), httpx.NewMeta(filter.Page, filter.Limit, total))
}

// Get handles GET /api/news/:slug.
func (ctl *NewsController) Get(c *gin.Context) {
	slug := c.Param("slug")
	ctx := c.Request.Context()

	article, err := ctl.repo.GetBySlug(ctx, slug, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
			return
		}
		httpx.Internal(c, "Could not load article")
		return
	}

	related, err := ctl.repo.Related(ctx, article, 6)
	if err != nil {
		related = nil // a missing related rail is not worth failing the page
	}

	// Count the read. This is buffered in Redis, so it adds no write to the
	// request path (§76).
	ctl.views.RecordView(ctx, article.ID, visitorHash(c), 0)

	c.Header("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
	httpx.OK(c, gin.H{
		"article": toDetail(article),
		"related": toCards(related),
	})
}

// Breaking handles GET /api/breaking (§7).
func (ctl *NewsController) Breaking(c *gin.Context) {
	ctx := c.Request.Context()
	limit := queryInt(c, "limit", 10, 1, 30)

	// Initialised, not nil: a cache hit that holds an empty list would leave
	// this nil, and encoding/json writes nil as `null`. A client reading
	// `.length` on that throws.
	cards := []ArticleCard{}
	if ctl.cache.GetJSON(ctx, cache.KeyBreaking, &cards) {
		httpx.OK(c, cards)
		return
	}

	articles, err := ctl.repo.Breaking(ctx, limit)
	if err != nil {
		httpx.Internal(c, "Could not load breaking news")
		return
	}
	cards = toCards(articles)
	ctl.cache.SetJSON(ctx, cache.KeyBreaking, cards, cache.TTLShort)

	c.Header("Cache-Control", "public, max-age=15")
	httpx.OK(c, cards)
}

// Live handles GET /api/live — the timeline behind /live (§8).
func (ctl *NewsController) Live(c *gin.Context) {
	ctx := c.Request.Context()
	limit := queryInt(c, "limit", 20, 1, 50)

	breaking, err := ctl.repo.Breaking(ctx, limit)
	if err != nil {
		httpx.Internal(c, "Could not load live news")
		return
	}

	// Fill the timeline with the newest stories when there is no active
	// breaking event, so /live is a live newsroom feed rather than a dead page.
	if len(breaking) < limit {
		filter := repositories.ArticleFilter{Page: 1, Limit: limit - len(breaking), Sort: "latest"}
		filter.Normalize()
		latest, _, err := ctl.repo.List(ctx, filter)
		if err == nil {
			seen := make(map[uint]bool, len(breaking))
			for _, a := range breaking {
				seen[a.ID] = true
			}
			for _, a := range latest {
				if !seen[a.ID] {
					breaking = append(breaking, a)
				}
			}
		}
	}

	c.Header("Cache-Control", "public, max-age=15")
	httpx.OK(c, toCards(breaking))
}

// Trending handles GET /api/trending (§44).
func (ctl *NewsController) Trending(c *gin.Context) {
	ctx := c.Request.Context()
	limit := queryInt(c, "limit", 10, 1, 30)

	articles, err := ctl.trending.Top(ctx, limit)
	if err != nil {
		httpx.Internal(c, "Could not load trending news")
		return
	}
	c.Header("Cache-Control", "public, max-age=120")
	httpx.OK(c, toCards(articles))
}

// FiveMinute handles GET /api/five-minute (§11).
func (ctl *NewsController) FiveMinute(c *gin.Context) {
	ctx := c.Request.Context()

	cards := []ArticleCard{}
	if ctl.cache.GetJSON(ctx, cache.KeyFiveMin, &cards) {
		httpx.OK(c, cards)
		return
	}

	articles, err := ctl.trending.FiveMinute(ctx, queryInt(c, "limit", 5, 3, 10))
	if err != nil {
		httpx.Internal(c, "Could not load the five-minute briefing")
		return
	}
	cards = toCards(articles)
	ctl.cache.SetJSON(ctx, cache.KeyFiveMin, cards, cache.TTLMedium)

	c.Header("Cache-Control", "public, max-age=120")
	httpx.OK(c, cards)
}

// Pulse handles GET /api/pulse (§12).
func (ctl *NewsController) Pulse(c *gin.Context) {
	ctx := c.Request.Context()

	var result services.PulseResult
	if ctl.cache.GetJSON(ctx, cache.KeyPulse, &result) {
		httpx.OK(c, result)
		return
	}

	pulse, err := ctl.trending.Pulse(ctx)
	if err != nil {
		httpx.Internal(c, "Could not load news pulse")
		return
	}
	ctl.cache.SetJSON(ctx, cache.KeyPulse, pulse, cache.TTLMedium)

	c.Header("Cache-Control", "public, max-age=300")
	httpx.OK(c, pulse)
}

// Featured handles GET /api/featured — the hero block (§9).
func (ctl *NewsController) Featured(c *gin.Context) {
	articles, err := ctl.repo.Featured(c.Request.Context(), queryInt(c, "limit", 5, 1, 10))
	if err != nil {
		httpx.Internal(c, "Could not load featured news")
		return
	}
	c.Header("Cache-Control", "public, max-age=60")
	httpx.OK(c, toCards(articles))
}

// TrackView handles POST /api/news/:slug/view — the reading-time beacon.
func (ctl *NewsController) TrackView(c *gin.Context) {
	var body struct {
		ReadSeconds int `json:"readSeconds"`
	}
	_ = c.ShouldBindJSON(&body)

	var article models.Article
	err := ctl.db.WithContext(c.Request.Context()).
		Select("id").Where("slug = ?", c.Param("slug")).First(&article).Error
	if err != nil {
		// Silently accept: a beacon for a deleted article is not the client's
		// problem, and returning 404 to a background fetch is noise.
		c.Status(http.StatusNoContent)
		return
	}

	ctl.views.RecordView(c.Request.Context(), article.ID, visitorHash(c), body.ReadSeconds)
	c.Status(http.StatusNoContent)
}

// TrackShare handles POST /api/news/:slug/share (§43).
// shareNetworks is the allow-list for share beacons, shared by articles and
// videos. Anything outside it is dropped rather than stored: the value ends up
// grouped in analytics, so an open field would let anyone invent rows.
var shareNetworks = map[string]bool{
	"facebook": true, "messenger": true, "telegram": true,
	"whatsapp": true, "x": true, "copy": true,
}

func (ctl *NewsController) TrackShare(c *gin.Context) {
	var body struct {
		Network string `json:"network" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Status(http.StatusNoContent)
		return
	}

	if !shareNetworks[body.Network] {
		c.Status(http.StatusNoContent)
		return
	}

	var article models.Article
	if err := ctl.db.WithContext(c.Request.Context()).
		Select("id").Where("slug = ?", c.Param("slug")).First(&article).Error; err != nil {
		c.Status(http.StatusNoContent)
		return
	}

	ctl.views.RecordShare(c.Request.Context(), article.ID, body.Network)
	c.Status(http.StatusNoContent)
}

// Archive handles GET /api/archive (§33).
func (ctl *NewsController) Archive(c *gin.Context) {
	year := queryInt(c, "year", 0, 2000, 2100)
	month := queryInt(c, "month", 0, 1, 12)

	filter := buildArticleFilter(c)
	if year > 0 {
		startMonth := time.Month(1)
		if month > 0 {
			startMonth = time.Month(month)
		}
		from := time.Date(year, startMonth, 1, 0, 0, 0, 0, time.UTC)
		var to time.Time
		if month > 0 {
			to = from.AddDate(0, 1, 0)
		} else {
			to = from.AddDate(1, 0, 0)
		}
		filter.From, filter.To = &from, &to
	}

	articles, total, err := ctl.repo.List(c.Request.Context(), filter)
	if err != nil {
		httpx.Internal(c, "Could not load the archive")
		return
	}
	httpx.OKList(c, toCards(articles), httpx.NewMeta(filter.Page, filter.Limit, total))
}
