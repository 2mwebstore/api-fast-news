package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/seo"
)

// SEOController serves sitemaps, robots.txt, redirects and the health report.
type SEOController struct {
	seo *seo.Service
	db  *gorm.DB
}

func NewSEOController(svc *seo.Service, db *gorm.DB) *SEOController {
	return &SEOController{seo: svc, db: db}
}

// Sitemap handles GET /sitemap.xml (§50).
func (ctl *SEOController) Sitemap(c *gin.Context) {
	body, err := ctl.seo.BuildSitemap(c.Request.Context())
	if err != nil {
		httpx.Internal(c, "Could not build the sitemap")
		return
	}
	c.Header("Cache-Control", "public, max-age=1800")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", body)
}

// NewsSitemap handles GET /news-sitemap.xml (§51).
func (ctl *SEOController) NewsSitemap(c *gin.Context) {
	body, err := ctl.seo.BuildNewsSitemap(c.Request.Context())
	if err != nil {
		httpx.Internal(c, "Could not build the news sitemap")
		return
	}
	// Short TTL: Google re-crawls this frequently and it must reflect the last
	// couple of hours of publishing.
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, "application/xml; charset=utf-8", body)
}

// Robots handles GET /robots.txt (§52).
func (ctl *SEOController) Robots(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=3600")
	c.String(http.StatusOK, ctl.seo.BuildRobotsTxt())
}

// Health handles GET /api/admin/seo/health (§60, §89).
func (ctl *SEOController) Health(c *gin.Context) {
	report, err := ctl.seo.Audit(c.Request.Context())
	if err != nil {
		httpx.Internal(c, "Could not run the SEO audit")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, report)
}

// SaveMetadata handles PUT /api/admin/news/:id/seo (§58).
func (ctl *SEOController) SaveMetadata(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}

	var req struct {
		SEOTitle       string   `json:"seoTitle"`
		SEODescription string   `json:"seoDescription"`
		SEOKeywords    []string `json:"seoKeywords"`
		CanonicalURL   string   `json:"canonicalUrl"`
		OGTitle        string   `json:"ogTitle"`
		OGDescription  string   `json:"ogDescription"`
		OGImage        string   `json:"ogImage"`
		TwitterTitle   string   `json:"twitterTitle"`
		TwitterDescription string `json:"twitterDescription"`
		TwitterImage   string   `json:"twitterImage"`
		Robots         string   `json:"robots"`
		FromAI         bool     `json:"fromAi"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid request")
		return
	}

	ctx := c.Request.Context()
	var metadata models.SEOMetadata
	ctl.db.WithContext(ctx).Where("article_id = ?", id).FirstOrInit(&metadata)

	metadata.ArticleID = &id
	metadata.SEOTitle = req.SEOTitle
	metadata.SEODescription = req.SEODescription
	metadata.SEOKeywords = models.StringSlice(req.SEOKeywords)
	metadata.CanonicalURL = req.CanonicalURL
	metadata.OGTitle = req.OGTitle
	metadata.OGDescription = req.OGDescription
	metadata.OGImage = req.OGImage
	metadata.TwitterTitle = req.TwitterTitle
	metadata.TwitterDescription = req.TwitterDescription
	metadata.TwitterImage = req.TwitterImage
	if req.Robots != "" {
		metadata.Robots = req.Robots
	}

	// Metadata saved straight from the assistant is marked unreviewed. Saving
	// it by hand is the human review, so the flag clears.
	metadata.AIGenerated = req.FromAI
	if !req.FromAI {
		now := timeNow()
		metadata.ReviewedAt = &now
		metadata.ReviewedByID = middleware.CurrentUserID(c)
	}

	if err := ctl.db.WithContext(ctx).Save(&metadata).Error; err != nil {
		httpx.Internal(c, "Could not save the SEO metadata")
		return
	}
	httpx.OK(c, metadata)
}

// Redirects handles GET /api/admin/redirects (§61).
func (ctl *SEOController) Redirects(c *gin.Context) {
	var redirects []models.Redirect
	err := ctl.db.WithContext(c.Request.Context()).
		Order("created_at DESC").Limit(500).Find(&redirects).Error
	if err != nil {
		httpx.Internal(c, "Could not load redirects")
		return
	}
	httpx.OK(c, redirects)
}

// CreateRedirect handles POST /api/admin/redirects.
func (ctl *SEOController) CreateRedirect(c *gin.Context) {
	var req struct {
		FromPath   string `json:"fromPath" binding:"required"`
		ToPath     string `json:"toPath" binding:"required"`
		StatusCode int    `json:"statusCode"`
		Note       string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A source and destination path are required")
		return
	}
	if req.StatusCode != 301 && req.StatusCode != 302 {
		req.StatusCode = 301
	}
	// A redirect pointing at itself is an infinite loop at the edge.
	if req.FromPath == req.ToPath {
		httpx.BadRequest(c, httpx.CodeValidation, "A redirect cannot point at itself")
		return
	}

	redirect := models.Redirect{
		FromPath: req.FromPath, ToPath: req.ToPath,
		StatusCode: req.StatusCode, IsActive: true, Note: req.Note,
		CreatedByID: middleware.CurrentUserID(c),
	}
	if err := ctl.db.WithContext(c.Request.Context()).Create(&redirect).Error; err != nil {
		httpx.Conflict(c, "REDIRECT_EXISTS", "A redirect already exists for that path")
		return
	}
	httpx.Created(c, redirect)
}

// ResolveRedirect handles GET /api/redirects/resolve?path=/news/old-slug.
// The frontend calls this from its 404 path before rendering an error page.
func (ctl *SEOController) ResolveRedirect(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		httpx.OK(c, gin.H{"redirect": nil})
		return
	}

	var redirect models.Redirect
	err := ctl.db.WithContext(c.Request.Context()).
		Where("from_path = ? AND is_active = ?", path, true).First(&redirect).Error
	if err != nil {
		httpx.OK(c, gin.H{"redirect": nil})
		return
	}

	now := timeNow()
	ctl.db.WithContext(c.Request.Context()).Model(&redirect).UpdateColumns(map[string]any{
		"hit_count":   gorm.Expr("hit_count + 1"),
		"last_hit_at": now,
	})

	httpx.OK(c, gin.H{"redirect": gin.H{
		"toPath":     redirect.ToPath,
		"statusCode": redirect.StatusCode,
	}})
}
