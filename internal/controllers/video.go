package controllers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// VideoController serves /video, /video/[slug] and the live
// stream endpoint (§26–§28).
type VideoController struct {
	db    *gorm.DB
	views *services.ViewService
}

func NewVideoController(db *gorm.DB, views *services.ViewService) *VideoController {
	return &VideoController{db: db, views: views}
}

// TrackShare handles POST /api/video/:slug/share.
//
// Same contract as the article beacon: the network is validated against a fixed
// list so an arbitrary string cannot be written into the analytics table, and
// every failure path returns 204. A share count is never worth failing a
// reader's click over.
func (ctl *VideoController) TrackShare(c *gin.Context) {
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

	var video models.Video
	if err := ctl.db.WithContext(c.Request.Context()).
		Select("id").Where("slug = ? AND status = ?", c.Param("slug"), models.StatusPublished).
		First(&video).Error; err != nil {
		c.Status(http.StatusNoContent)
		return
	}

	ctl.views.RecordVideoShare(c.Request.Context(), video.ID, body.Network)
	c.Status(http.StatusNoContent)
}

type VideoCard struct {
	ID           uint         `json:"id"`
	Slug         string       `json:"slug"`
	TitleKh      string       `json:"titleKh"`
	TitleEn      string       `json:"titleEn,omitempty"`
	DescKh       string       `json:"descKh,omitempty"`
	ThumbnailURL string       `json:"thumbnailUrl,omitempty"`
	ThumbnailAlt string       `json:"thumbnailAlt,omitempty"`
	// YouTube is the primary delivery path. EmbedURL is built server-side from
	// the stored id, so the player we render is never a URL an editor pasted.
	YouTubeID string `json:"youtubeId,omitempty"`
	EmbedURL  string `json:"embedUrl,omitempty"`
	WatchURL  string `json:"watchUrl,omitempty"`

	// Self-hosted fallbacks, used only when there is no YouTube id.
	SourceURL string `json:"sourceUrl,omitempty"`
	HLSURL    string `json:"hlsUrl,omitempty"`
	// SEO overrides, on the detail endpoint only. A list has no use for them.
	SEO *VideoSEO `json:"seo,omitempty"`

	DurationSec  int          `json:"durationSec"`
	Width        int          `json:"width,omitempty"`
	Height       int          `json:"height,omitempty"`
	IsLive       bool         `json:"isLive"`
	ViewCount    int64        `json:"viewCount"`
	PublishedAt  *time.Time   `json:"publishedAt"`
	Category     *CategoryRef `json:"category,omitempty"`
}

// VideoSEO is the subset of SEOMetadata a public page can act on. The review
// and AI-provenance columns stay internal.
type VideoSEO struct {
	SEOTitle           string   `json:"seoTitle,omitempty"`
	SEODescription     string   `json:"seoDescription,omitempty"`
	SEOKeywords        []string `json:"seoKeywords,omitempty"`
	CanonicalURL       string   `json:"canonicalUrl,omitempty"`
	OGTitle            string   `json:"ogTitle,omitempty"`
	OGDescription      string   `json:"ogDescription,omitempty"`
	OGImage            string   `json:"ogImage,omitempty"`
	TwitterTitle       string   `json:"twitterTitle,omitempty"`
	TwitterDescription string   `json:"twitterDescription,omitempty"`
	TwitterImage       string   `json:"twitterImage,omitempty"`
	Robots             string   `json:"robots,omitempty"`
}

func toVideoCard(v *models.Video, includeSource bool) VideoCard {
	card := VideoCard{
		ID: v.ID, Slug: v.Slug, TitleKh: v.TitleKh, TitleEn: v.TitleEn,
		DescKh: v.DescKh, ThumbnailURL: v.ThumbnailURL, ThumbnailAlt: v.ThumbnailAlt,
		DurationSec: v.DurationSec, Width: v.Width, Height: v.Height,
		IsLive: v.IsLive,
		ViewCount: v.ViewCount, PublishedAt: v.PublishedAt,
	}
	// The embed id is safe to expose in a list: it is public on YouTube anyway,
	// and a card needs it for its thumbnail. Self-hosted asset URLs stay on the
	// detail endpoint.
	card.YouTubeID = v.YouTubeID
	card.EmbedURL = utils.YouTubeEmbedURL(v.YouTubeID)
	card.WatchURL = utils.YouTubeWatchURL(v.YouTubeID)

	if includeSource {
		card.SourceURL = v.SourceURL
		card.HLSURL = v.HLSURL
		// includeSource marks the detail endpoint, which is the only place SEO
		// overrides can be applied.
		if v.SEO != nil {
			card.SEO = &VideoSEO{
				SEOTitle: v.SEO.SEOTitle, SEODescription: v.SEO.SEODescription,
				SEOKeywords: v.SEO.SEOKeywords, CanonicalURL: v.SEO.CanonicalURL,
				OGTitle: v.SEO.OGTitle, OGDescription: v.SEO.OGDescription,
				OGImage: v.SEO.OGImage, TwitterTitle: v.SEO.TwitterTitle,
				TwitterDescription: v.SEO.TwitterDescription,
				TwitterImage:       v.SEO.TwitterImage, Robots: v.SEO.Robots,
			}
		}
	}
	if v.Category != nil {
		card.Category = &CategoryRef{
			ID: v.Category.ID, Slug: v.Category.Slug,
			NameKh: v.Category.NameKh, NameEn: v.Category.NameEn,
			Color: v.Category.Color, Icon: v.Category.Icon,
		}
	}
	return card
}

// List handles GET /api/video.
func (ctl *VideoController) List(c *gin.Context) {
	page := queryInt(c, "page", 1, 1, 200)
	limit := queryInt(c, "limit", 20, 1, 50)

	q := ctl.db.WithContext(c.Request.Context()).Model(&models.Video{}).
		Where("status = ?", models.StatusPublished).
		Where("published_at IS NOT NULL AND published_at <= ?", time.Now().UTC())

	if category := c.Query("category"); category != "" {
		q = q.Where("category_id IN (SELECT id FROM categories WHERE slug = ?)", category)
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		httpx.Internal(c, "Could not load videos")
		return
	}

	var videos []models.Video
	err := q.Preload("Category").
		Order("published_at DESC").
		Limit(limit).Offset((page - 1) * limit).Find(&videos).Error
	if err != nil {
		httpx.Internal(c, "Could not load videos")
		return
	}

	// Lists never carry self-hosted asset URLs; the detail endpoint does.
	out := make([]VideoCard, 0, len(videos))
	for i := range videos {
		out = append(out, toVideoCard(&videos[i], false))
	}

	c.Header("Cache-Control", "public, max-age=120")
	httpx.OKList(c, out, httpx.NewMeta(page, limit, total))
}

// Get handles GET /api/video/:slug.
func (ctl *VideoController) Get(c *gin.Context) {
	var video models.Video
	err := ctl.db.WithContext(c.Request.Context()).Preload("Category").Preload("SEO").
		Where("slug = ? AND status = ?", c.Param("slug"), models.StatusPublished).
		First(&video).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, httpx.CodeVideoNotFound, "Video not found")
			return
		}
		httpx.Internal(c, "Could not load video")
		return
	}

	// Video view counts are low-volume next to article views, so a single
	// atomic increment is fine here without a Redis buffer.
	ctl.db.WithContext(c.Request.Context()).Model(&models.Video{}).
		Where("id = ?", video.ID).
		UpdateColumn("view_count", gorm.Expr("view_count + 1"))

	httpx.OK(c, toVideoCard(&video, true))
}

// Live handles GET /api/live-video (§28). It returns the active stream, or
// null when nothing is on air — the page renders a schedule in that case
// rather than a broken player.
func (ctl *VideoController) Live(c *gin.Context) {
	now := time.Now().UTC()

	var video models.Video
	err := ctl.db.WithContext(c.Request.Context()).Preload("Category").
		Where("is_live = ? AND status = ?", true, models.StatusPublished).
		Where("live_start_at IS NULL OR live_start_at <= ?", now).
		Where("live_end_at IS NULL OR live_end_at > ?", now).
		Order("live_start_at DESC").First(&video).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Header("Cache-Control", "public, max-age=30")
			httpx.OK(c, gin.H{"live": nil})
			return
		}
		httpx.Internal(c, "Could not load the live stream")
		return
	}

	c.Header("Cache-Control", "public, max-age=15")
	httpx.OK(c, gin.H{"live": toVideoCard(&video, true)})
}
