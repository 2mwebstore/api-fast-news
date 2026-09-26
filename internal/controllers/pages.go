package controllers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// PageController serves and manages the standalone pages: the policies, About
// and Contact (§93).
type PageController struct {
	db    *gorm.DB
	audit *services.AuditService
}

func NewPageController(db *gorm.DB, audit *services.AuditService) *PageController {
	return &PageController{db: db, audit: audit}
}

// ── Public ──────────────────────────────────────────────────────────────

type pageSummary struct {
	Slug    string `json:"slug"`
	TitleKh string `json:"titleKh"`
	TitleEn string `json:"titleEn,omitempty"`
}

// List handles GET /api/pages — the footer's policy column.
func (ctl *PageController) List(c *gin.Context) {
	var pages []models.Page
	err := ctl.db.WithContext(c.Request.Context()).
		Where("is_published = ? AND show_in_footer = ?", true, true).
		Order("position ASC, id ASC").Find(&pages).Error
	if err != nil {
		httpx.Internal(c, "Could not load pages")
		return
	}

	// Initialised, not nil: a nil slice marshals as `null` and a client reading
	// `.length` on that throws.
	out := make([]pageSummary, 0, len(pages))
	for _, p := range pages {
		out = append(out, pageSummary{Slug: p.Slug, TitleKh: p.TitleKh, TitleEn: p.TitleEn})
	}

	c.Header("Cache-Control", "public, max-age=300")
	httpx.OK(c, out)
}

// Get handles GET /api/pages/:slug.
func (ctl *PageController) Get(c *gin.Context) {
	var page models.Page
	err := ctl.db.WithContext(c.Request.Context()).
		Where("slug = ? AND is_published = ?", c.Param("slug"), true).
		First(&page).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, "PAGE_NOT_FOUND", "Page not found")
			return
		}
		httpx.Internal(c, "Could not load the page")
		return
	}

	c.Header("Cache-Control", "public, max-age=300")
	httpx.OK(c, gin.H{
		"slug":    page.Slug,
		"titleKh": page.TitleKh, "titleEn": page.TitleEn,
		"bodyKh": page.BodyKh, "bodyEn": page.BodyEn,
		"metaDescKh": page.MetaDescKh, "metaDescEn": page.MetaDescEn,
		// The renderer needs to know whether to show the "not translated yet"
		// notice, and that is a question about the body, not the title.
		"hasEnglish": strings.TrimSpace(page.BodyEn) != "",
		"updatedAt":  page.UpdatedAt,
	})
}

// ── Admin ───────────────────────────────────────────────────────────────

// AdminList handles GET /api/admin/pages.
func (ctl *PageController) AdminList(c *gin.Context) {
	var pages []models.Page
	err := ctl.db.WithContext(c.Request.Context()).
		Order("position ASC, id ASC").Find(&pages).Error
	if err != nil {
		httpx.Internal(c, "Could not load pages")
		return
	}

	type row struct {
		ID           uint      `json:"id"`
		Slug         string    `json:"slug"`
		TitleKh      string    `json:"titleKh"`
		TitleEn      string    `json:"titleEn,omitempty"`
		IsPublished  bool      `json:"isPublished"`
		ShowInFooter bool      `json:"showInFooter"`
		IsSystem     bool      `json:"isSystem"`
		Position     int       `json:"position"`
		HasEnglish   bool      `json:"hasEnglish"`
		WordsKh      int       `json:"wordsKh"`
		UpdatedAt    time.Time `json:"updatedAt"`
	}

	out := make([]row, 0, len(pages))
	for i := range pages {
		p := pages[i]
		out = append(out, row{
			ID: p.ID, Slug: p.Slug, TitleKh: p.TitleKh, TitleEn: p.TitleEn,
			IsPublished: p.IsPublished, ShowInFooter: p.ShowInFooter,
			IsSystem: p.IsSystem, Position: p.Position,
			HasEnglish: strings.TrimSpace(p.BodyEn) != "",
			WordsKh:    len(strings.Fields(utils.StripHTML(p.BodyKh))),
			UpdatedAt:  p.UpdatedAt,
		})
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, out)
}

// AdminGet handles GET /api/admin/pages/:id — the full record for the editor.
func (ctl *PageController) AdminGet(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid page id")
		return
	}

	var page models.Page
	if err := ctl.db.WithContext(c.Request.Context()).First(&page, id).Error; err != nil {
		httpx.NotFound(c, "PAGE_NOT_FOUND", "Page not found")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, page)
}

type pageRequest struct {
	TitleKh string `json:"titleKh" binding:"required,min=1"`
	TitleEn string `json:"titleEn"`
	Slug    string `json:"slug"`
	BodyKh  string `json:"bodyKh"`
	BodyEn  string `json:"bodyEn"`

	MetaDescKh string `json:"metaDescKh"`
	MetaDescEn string `json:"metaDescEn"`

	// Pointers so an explicit false is distinguishable from an omitted field.
	IsPublished  *bool `json:"isPublished"`
	ShowInFooter *bool `json:"showInFooter"`
	Position     int   `json:"position"`
}

// Create handles POST /api/admin/pages.
func (ctl *PageController) Create(c *gin.Context) {
	var req pageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A Khmer title is required")
		return
	}

	ctx := c.Request.Context()
	slug, err := ctl.uniqueSlug(ctx, req.Slug, req.TitleEn, req.TitleKh, 0)
	if err != nil {
		httpx.Conflict(c, httpx.CodeDuplicateSlug, "Could not generate a unique address for this page")
		return
	}
	if reason := reservedSlug(slug); reason != "" {
		httpx.BadRequest(c, httpx.CodeValidation, reason)
		return
	}

	page := models.Page{
		Slug: slug, TitleKh: req.TitleKh, TitleEn: req.TitleEn,
		// Sanitised on the way in, so a stored body can never carry a script
		// even if it was written by a trusted editor pasting from elsewhere.
		BodyKh:     utils.SanitizeHTML(req.BodyKh),
		BodyEn:     utils.SanitizeHTML(req.BodyEn),
		MetaDescKh: req.MetaDescKh, MetaDescEn: req.MetaDescEn,
		Position:     req.Position,
		IsPublished:  req.IsPublished != nil && *req.IsPublished,
		ShowInFooter: req.ShowInFooter == nil || *req.ShowInFooter,
		UpdatedByID:  middleware.CurrentUserID(c),
	}
	if page.IsPublished {
		now := time.Now().UTC()
		page.PublishedAt = &now
	}

	if err := ctl.db.WithContext(ctx).Create(&page).Error; err != nil {
		httpx.Internal(c, "Could not create the page")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "page.create", "page", &page.ID, page.Slug, nil)
	httpx.Created(c, page)
}

// Update handles PUT /api/admin/pages/:id.
func (ctl *PageController) Update(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid page id")
		return
	}

	var req pageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A Khmer title is required")
		return
	}

	ctx := c.Request.Context()
	var page models.Page
	if err := ctl.db.WithContext(ctx).First(&page, id).Error; err != nil {
		httpx.NotFound(c, "PAGE_NOT_FOUND", "Page not found")
		return
	}

	// A slug change moves a public, indexed URL. System pages keep theirs: the
	// footer, robots.txt and outside links all assume /privacy is /privacy.
	if requested := utils.Slugify(req.Slug); requested != "" && requested != page.Slug {
		if page.IsSystem {
			httpx.BadRequest(c, httpx.CodeValidation,
				"This page's address is fixed. Other sites and search engines link to it directly.")
			return
		}
		if reason := reservedSlug(requested); reason != "" {
			httpx.BadRequest(c, httpx.CodeValidation, reason)
			return
		}
		slug, err := ctl.uniqueSlug(ctx, requested, req.TitleEn, req.TitleKh, id)
		if err != nil {
			httpx.Conflict(c, httpx.CodeDuplicateSlug, "That address is already in use")
			return
		}
		ctl.createRedirect(ctx, "/"+page.Slug, "/"+slug)
		page.Slug = slug
	}

	page.TitleKh, page.TitleEn = req.TitleKh, req.TitleEn
	page.BodyKh = utils.SanitizeHTML(req.BodyKh)
	page.BodyEn = utils.SanitizeHTML(req.BodyEn)
	page.MetaDescKh, page.MetaDescEn = req.MetaDescKh, req.MetaDescEn
	page.Position = req.Position
	page.UpdatedByID = middleware.CurrentUserID(c)

	wasPublished := page.IsPublished
	if req.IsPublished != nil {
		page.IsPublished = *req.IsPublished
	}
	if req.ShowInFooter != nil {
		page.ShowInFooter = *req.ShowInFooter
	}
	if page.IsPublished && !wasPublished {
		now := time.Now().UTC()
		page.PublishedAt = &now
	}

	if err := ctl.db.WithContext(ctx).Save(&page).Error; err != nil {
		httpx.Internal(c, "Could not save the page")
		return
	}
	// UpdateColumns for the flags: Save() skips a Go false as a zero value,
	// which is how a "hide this page" edit would silently do nothing.
	err := ctl.db.WithContext(ctx).Model(&models.Page{}).Where("id = ?", page.ID).
		UpdateColumns(map[string]any{
			"is_published":   page.IsPublished,
			"show_in_footer": page.ShowInFooter,
		}).Error
	if err != nil {
		httpx.Internal(c, "Could not save the page's visibility")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "page.update", "page", &page.ID, page.Slug, nil)
	httpx.OK(c, page)
}

// Delete handles DELETE /api/admin/pages/:id.
//
// A required page can be deleted, but only with ?confirm=permanent. Deleting the
// privacy policy leaves a 404 where an indexed legal document used to be, and
// unpublishing achieves the same visible result reversibly — so the extra step
// is there to make sure the operator meant this one rather than that one. The UI
// pairs it with a type-the-name confirmation.
func (ctl *PageController) Delete(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid page id")
		return
	}

	ctx := c.Request.Context()
	var page models.Page
	if err := ctl.db.WithContext(ctx).First(&page, id).Error; err != nil {
		httpx.NotFound(c, "PAGE_NOT_FOUND", "Page not found")
		return
	}
	if page.IsSystem && c.Query("confirm") != "permanent" {
		httpx.Conflict(c, "PAGE_IS_SYSTEM",
			"This page is one a news site is expected to have. Deleting it leaves a 404 at an address search engines already know. Untick Published to take it off the site reversibly, or confirm to delete it permanently.")
		return
	}

	// Unscoped for the same reason as sections: a soft-deleted row keeps its
	// slug against the unique index, so /offers could never be re-created.
	if err := ctl.db.WithContext(ctx).Unscoped().Delete(&page).Error; err != nil {
		httpx.Internal(c, "Could not delete the page")
		return
	}

	// The slug goes in the summary because after a delete it is the only way to
	// tell from the log which page this was.
	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "page.delete", "page", &id,
		page.Slug, models.JSONMap{"slug": page.Slug, "wasRequired": page.IsSystem})
	httpx.NoData(c)
}

// ── Helpers ─────────────────────────────────────────────────────────────

// reservedSlug rejects addresses that belong to the application. A page at
// /admin or /video would be unreachable — the frontend's own route wins — so
// the operator would be editing something nobody can see.
func reservedSlug(slug string) string {
	reserved := map[string]bool{
		"admin": true, "api": true, "news": true, "video": true, "category": true,
		"author": true, "search": true, "live": true, "live-video": true,
		"archive": true, "traffic": true, "tip": true, "login": true,
		"sitemap.xml": true, "robots.txt": true, "news-sitemap.xml": true, "ws": true,
	}
	if reserved[slug] {
		return "/" + slug + " is used by the site itself. Choose a different address."
	}
	return ""
}

func (ctl *PageController) uniqueSlug(ctx context.Context, requested, titleEn, titleKh string, exceptID uint) (string, error) {
	base := utils.Slugify(requested)
	if base == "" {
		base = utils.SlugifyWithFallback(titleEn, titleKh, "page")
	}

	candidate := base
	for attempt := 0; attempt < 12; attempt++ {
		q := ctl.db.WithContext(ctx).Unscoped().Model(&models.Page{}).Where("slug = ?", candidate)
		if exceptID > 0 {
			q = q.Where("id <> ?", exceptID)
		}
		var count int64
		if err := q.Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
		candidate = base + "-" + utils.RandomHex(3)
	}
	return "", errors.New("could not generate a unique slug")
}

func (ctl *PageController) createRedirect(ctx context.Context, from, to string) {
	redirect := models.Redirect{
		FromPath: from, ToPath: to,
		StatusCode: 301, IsActive: true,
		Note: "automatic: page address changed",
	}
	ctl.db.WithContext(ctx).Where("from_path = ?", from).FirstOrCreate(&redirect)
}
