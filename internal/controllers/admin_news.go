package controllers

import (
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/repositories"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// AdminNewsController is the editorial CRUD and workflow surface.
type AdminNewsController struct {
	svc  *services.ArticleService
	repo *repositories.ArticleRepository
	db   *gorm.DB
}

func NewAdminNewsController(svc *services.ArticleService, repo *repositories.ArticleRepository, db *gorm.DB) *AdminNewsController {
	return &AdminNewsController{svc: svc, repo: repo, db: db}
}

// List handles GET /api/admin/news — includes drafts and every other status.
func (ctl *AdminNewsController) List(c *gin.Context) {
	filter := buildArticleFilter(c)
	filter.IncludeNonPublic = true
	filter.Status = models.ArticleStatus(c.Query("status"))

	articles, total, err := ctl.repo.List(c.Request.Context(), filter)
	if err != nil {
		httpx.Internal(c, "Could not load articles")
		return
	}

	// Admin lists are per-user and include unpublished work.
	c.Header("Cache-Control", "private, no-store")
	httpx.OKList(c, toAdminCards(articles), httpx.NewMeta(filter.Page, filter.Limit, total))
}

// Get handles GET /api/admin/news/:id.
func (ctl *AdminNewsController) Get(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}

	article, err := ctl.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
			return
		}
		httpx.Internal(c, "Could not load article")
		return
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, gin.H{
		"article":  toDetail(article),
		"status":   article.Status,
		"sources":  article.Sources,
		"checklist": buildChecklist(article),
	})
}

// Create handles POST /api/news.
func (ctl *AdminNewsController) Create(c *gin.Context) {
	var in services.ArticleInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A Khmer headline and a category are required")
		return
	}

	article, err := ctl.svc.Create(c.Request.Context(), in, middleware.CurrentUserID(c))
	if err != nil {
		if errors.Is(err, services.ErrSlugTaken) {
			httpx.Conflict(c, httpx.CodeDuplicateSlug, "Could not generate a unique slug for this article")
			return
		}
		httpx.Internal(c, "Could not create the article")
		return
	}
	httpx.Created(c, toDetail(article))
}

// Update handles PUT /api/news/:id.
func (ctl *AdminNewsController) Update(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}

	var in services.ArticleInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A Khmer headline and a category are required")
		return
	}

	user := middleware.CurrentUser(c)
	editorName := ""
	if user != nil {
		editorName = user.Name
	}

	article, err := ctl.svc.Update(c.Request.Context(), id, in, middleware.CurrentUserID(c), editorName)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
		case errors.Is(err, services.ErrSlugTaken):
			httpx.Conflict(c, httpx.CodeDuplicateSlug, "That slug is already in use")
		default:
			httpx.Internal(c, "Could not update the article")
		}
		return
	}
	httpx.OK(c, toDetail(article))
}

// Delete handles DELETE /api/news/:id. This is a soft delete — editorial
// records are never destroyed outright.
func (ctl *AdminNewsController) Delete(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}
	if err := ctl.repo.Delete(c.Request.Context(), id); err != nil {
		httpx.Internal(c, "Could not delete the article")
		return
	}
	httpx.NoData(c)
}

type transitionRequest struct {
	Status models.ArticleStatus `json:"status" binding:"required"`
}

// Transition handles POST /api/news/:id/status — the §16 workflow.
func (ctl *AdminNewsController) Transition(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}

	var req transitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A target status is required")
		return
	}

	user := middleware.CurrentUser(c)
	// Publishing and scheduling are gated separately from editing: a
	// journalist may edit their own draft but must not be able to push it live.
	if req.Status == models.StatusPublished || req.Status == models.StatusScheduled {
		if user == nil || !user.Can("news.publish") {
			httpx.Forbidden(c, "You do not have permission to publish")
			return
		}
	}
	if req.Status == models.StatusApproved || req.Status == models.StatusRejected {
		if user == nil || !user.Can("news.review") {
			httpx.Forbidden(c, "You do not have permission to review articles")
			return
		}
	}

	editorName := ""
	if user != nil {
		editorName = user.Name
	}

	article, err := ctl.svc.Transition(c.Request.Context(), id, req.Status, middleware.CurrentUserID(c), editorName)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
		case errors.Is(err, services.ErrInvalidStatus), errors.Is(err, services.ErrMissingForPublish):
			httpx.BadRequest(c, httpx.CodeValidation, err.Error())
		default:
			httpx.Internal(c, "Could not change the article status")
		}
		return
	}
	httpx.OK(c, toDetail(article))
}

type breakingRequest struct {
	IsBreaking bool `json:"isBreaking"`
}

// SetBreaking handles POST /api/news/:id/breaking (§7).
func (ctl *AdminNewsController) SetBreaking(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}

	var req breakingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "isBreaking is required")
		return
	}

	article, err := ctl.svc.SetBreaking(c.Request.Context(), id, req.IsBreaking, middleware.CurrentUserID(c))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
			return
		}
		httpx.Internal(c, "Could not update breaking status")
		return
	}
	httpx.OK(c, gin.H{"id": article.ID, "isBreaking": article.IsBreaking})
}

type correctionRequest struct {
	NoteKh string `json:"noteKh" binding:"required,min=5"`
	NoteEn string `json:"noteEn"`
	Reason string `json:"reason" binding:"required"`
}

// AddCorrection handles POST /api/news/:id/correction (§20).
func (ctl *AdminNewsController) AddCorrection(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}

	var req correctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A correction note and a reason are required")
		return
	}

	user := middleware.CurrentUser(c)
	editorName := ""
	if user != nil {
		editorName = user.Name
	}

	correction, err := ctl.svc.AddCorrection(c.Request.Context(), id,
		req.NoteKh, req.NoteEn, req.Reason, middleware.CurrentUserID(c), editorName)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, httpx.CodeArticleNotFound, "Article not found")
			return
		}
		httpx.Internal(c, "Could not publish the correction")
		return
	}
	httpx.Created(c, correction)
}

// Revisions handles GET /api/admin/news/:id/revisions (§20).
func (ctl *AdminNewsController) Revisions(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}

	var revisions []models.ArticleRevision
	err := ctl.db.WithContext(c.Request.Context()).
		Where("article_id = ?", id).Order("version DESC").Limit(50).Find(&revisions).Error
	if err != nil {
		httpx.Internal(c, "Could not load revisions")
		return
	}
	httpx.OK(c, revisions)
}

// Sources handles POST /api/admin/news/:id/sources (§19).
func (ctl *AdminNewsController) AddSource(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid article id")
		return
	}

	var req struct {
		NameKh       string                    `json:"nameKh" binding:"required"`
		URL          string                    `json:"url"`
		Type         models.SourceType         `json:"type" binding:"required"`
		Notes        string                    `json:"notes"`
		Verification models.VerificationStatus `json:"verification"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A source name and type are required")
		return
	}

	source := models.ArticleSource{
		ArticleID: id, NameKh: req.NameKh, URL: req.URL,
		Type: req.Type, Notes: req.Notes,
		ReporterID:   middleware.CurrentUserID(c),
		Verification: req.Verification,
	}
	if source.Verification == "" {
		source.Verification = models.VerifyUnverified
	}
	// A social-media post is unverified until a human says otherwise. Accepting
	// "verified" on submission for this type would defeat the point of §19.
	if source.Type == models.SourceSocialMedia && source.Verification == models.VerifyVerified {
		source.Verification = models.VerifyPending
	}

	if err := ctl.db.WithContext(c.Request.Context()).Create(&source).Error; err != nil {
		httpx.Internal(c, "Could not add the source")
		return
	}
	httpx.Created(c, source)
}

// AdminCard adds workflow fields the newsroom list needs.
type AdminCard struct {
	ArticleCard
	Status      models.ArticleStatus `json:"status"`
	ScheduledAt any                  `json:"scheduledAt,omitempty"`
	UpdatedAt   any                  `json:"updatedAt"`
	WordCount   int                  `json:"wordCount"`
	AIAssisted  bool                 `json:"aiAssisted"`
}

func toAdminCards(articles []models.Article) []AdminCard {
	out := make([]AdminCard, 0, len(articles))
	for i := range articles {
		a := &articles[i]
		out = append(out, AdminCard{
			ArticleCard: toCard(a),
			Status:      a.Status,
			ScheduledAt: a.ScheduledAt,
			UpdatedAt:   a.UpdatedAt,
			WordCount:   a.WordCount,
			AIAssisted:  a.AIAssisted,
		})
	}
	return out
}

// ChecklistItem is one line of the §59 pre-publication checklist.
type ChecklistItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Done  bool   `json:"done"`
	// Required marks the items publishing actually enforces; the rest are
	// editorial judgement the UI surfaces but does not block on.
	Required bool `json:"required"`
}

// buildChecklist renders the §59 list.
//
// This is an editorial checklist, not a ranking score — the response says so,
// and the UI repeats it.
func buildChecklist(a *models.Article) gin.H {
	items := []ChecklistItem{
		{"title", "Headline", a.TitleKh != "", true},
		{"summary", "Summary", a.SummaryKh != "", true},
		{"image", "Main image", a.ImageURL != "", false},
		{"image-alt", "Image ALT text", a.ImageURL == "" || a.ImageAltKh != "" || a.ImageAltEn != "", true},
		{"category", "Category", a.CategoryID != 0, true},
		{"author", "Author", a.AuthorID != nil, false},
		{"source", "Source recorded", len(a.Sources) > 0, false},
		{"body", "Article body", a.ContentKh != "", true},
		{"seo-title", "SEO title", a.SEO != nil && a.SEO.SEOTitle != "", false},
		{"seo-description", "SEO description", a.SEO != nil && a.SEO.SEODescription != "", false},
		{"og-image", "Open Graph image", (a.SEO != nil && a.SEO.OGImage != "") || a.ImageURL != "", false},
		{"disclosure", "Sponsor disclosed", !a.ContentType.RequiresDisclosure() || a.SponsorName != "", true},
	}

	requiredDone := true
	doneCount := 0
	for _, item := range items {
		if item.Done {
			doneCount++
		}
		if item.Required && !item.Done {
			requiredDone = false
		}
	}

	return gin.H{
		"items":        items,
		"completed":    doneCount,
		"total":        len(items),
		"readyToPublish": requiredDone,
		"note":         "An editorial checklist for our own standards. It is not a Google ranking score.",
	}
}
