package controllers

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// AdminCategoryController manages the section structure (§13).
//
// Sections are part of the site's URL space: /category/<slug> is a public,
// indexable address. So a slug change leaves a redirect behind, and a delete is
// refused while anything still points at the section — an editor should not be
// able to 404 a set of published articles by tidying up a menu.
type AdminCategoryController struct {
	db    *gorm.DB
	cache *cache.Cache
	audit *services.AuditService
}

func NewAdminCategoryController(db *gorm.DB, c *cache.Cache, audit *services.AuditService) *AdminCategoryController {
	return &AdminCategoryController{db: db, cache: c, audit: audit}
}

type categoryRequest struct {
	NameKh   string `json:"nameKh" binding:"required,min=1"`
	NameEn   string `json:"nameEn" binding:"required,min=1"`
	Slug     string `json:"slug"`
	DescKh   string `json:"descKh"`
	DescEn   string `json:"descEn"`
	Icon     string `json:"icon"`
	Color    string `json:"color"`
	Position int    `json:"position"`
	// Pointers so an explicit false is distinguishable from an omitted field.
	InNav    *bool `json:"inNav"`
	IsActive *bool `json:"isActive"`
	ParentID *uint `json:"parentId"`

	SEOTitleKh string `json:"seoTitleKh"`
	SEODescKh  string `json:"seoDescKh"`
	SEOTitleEn string `json:"seoTitleEn"`
	SEODescEn  string `json:"seoDescEn"`
}

// Create handles POST /api/admin/categories.
func (ctl *AdminCategoryController) Create(c *gin.Context) {
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A Khmer and an English name are required")
		return
	}

	ctx := c.Request.Context()
	slug, err := ctl.uniqueSlug(ctx, req.Slug, req.NameEn, req.NameKh, 0)
	if err != nil {
		httpx.Conflict(c, httpx.CodeDuplicateSlug, "Could not generate a unique slug for this section")
		return
	}
	if err := ctl.checkParent(ctx, req.ParentID, 0); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, err.Error())
		return
	}

	category := models.Category{
		Slug: slug, NameKh: req.NameKh, NameEn: req.NameEn,
		DescKh: req.DescKh, DescEn: req.DescEn,
		Icon: req.Icon, Color: req.Color, Position: req.Position,
		ParentID:   req.ParentID,
		SEOTitleKh: req.SEOTitleKh, SEODescKh: req.SEODescKh,
		SEOTitleEn: req.SEOTitleEn, SEODescEn: req.SEODescEn,
		InNav:    req.InNav == nil || *req.InNav,
		IsActive: req.IsActive == nil || *req.IsActive,
	}

	if err := ctl.db.WithContext(ctx).Create(&category).Error; err != nil {
		httpx.Internal(c, "Could not create the section")
		return
	}

	ctl.cache.Delete(ctx, cache.KeyNav)
	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "category.create", "category", &category.ID, category.NameKh, nil)
	httpx.Created(c, category)
}

// Update handles PUT /api/admin/categories/:id.
func (ctl *AdminCategoryController) Update(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid section id")
		return
	}

	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A Khmer and an English name are required")
		return
	}

	ctx := c.Request.Context()
	var category models.Category
	if err := ctl.db.WithContext(ctx).First(&category, id).Error; err != nil {
		httpx.NotFound(c, httpx.CodeCategoryNotFound, "Section not found")
		return
	}
	if err := ctl.checkParent(ctx, req.ParentID, id); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, err.Error())
		return
	}

	// A slug change moves a public URL, so leave a 301 behind rather than
	// breaking every existing link to the section (§61).
	if requested := utils.Slugify(req.Slug); requested != "" && requested != category.Slug {
		slug, err := ctl.uniqueSlug(ctx, requested, req.NameEn, req.NameKh, id)
		if err != nil {
			httpx.Conflict(c, httpx.CodeDuplicateSlug, "That slug is already in use")
			return
		}
		ctl.createSlugRedirect(ctx, category.Slug, slug)
		category.Slug = slug
	}

	category.NameKh, category.NameEn = req.NameKh, req.NameEn
	category.DescKh, category.DescEn = req.DescKh, req.DescEn
	category.Icon, category.Color, category.Position = req.Icon, req.Color, req.Position
	category.ParentID = req.ParentID
	category.SEOTitleKh, category.SEODescKh = req.SEOTitleKh, req.SEODescKh
	category.SEOTitleEn, category.SEODescEn = req.SEOTitleEn, req.SEODescEn
	if req.InNav != nil {
		category.InNav = *req.InNav
	}
	if req.IsActive != nil {
		category.IsActive = *req.IsActive
	}

	// UpdateColumns for the booleans: Save() on a struct skips a false as a
	// zero value, which is how InNav: false silently failed to persist before.
	if err := ctl.db.WithContext(ctx).Save(&category).Error; err != nil {
		httpx.Internal(c, "Could not update the section")
		return
	}
	err := ctl.db.WithContext(ctx).Model(&models.Category{}).Where("id = ?", category.ID).
		UpdateColumns(map[string]any{"in_nav": category.InNav, "is_active": category.IsActive}).Error
	if err != nil {
		httpx.Internal(c, "Could not update the section's visibility")
		return
	}

	ctl.cache.Delete(ctx, cache.KeyNav)
	ctl.cache.InvalidateContent(ctx)
	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "category.update", "category", &id, category.NameKh, nil)
	httpx.OK(c, category)
}

// Delete handles DELETE /api/admin/categories/:id.
//
// Refused while the section still has children or articles. Deleting it anyway
// would orphan published stories — their category link would 404 and they would
// vanish from section feeds and sitemaps. Reassign first, or hide the section
// with isActive instead.
func (ctl *AdminCategoryController) Delete(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid section id")
		return
	}

	ctx := c.Request.Context()
	var category models.Category
	if err := ctl.db.WithContext(ctx).First(&category, id).Error; err != nil {
		httpx.NotFound(c, httpx.CodeCategoryNotFound, "Section not found")
		return
	}

	var children int64
	ctl.db.WithContext(ctx).Model(&models.Category{}).Where("parent_id = ?", id).Count(&children)
	if children > 0 {
		httpx.Conflict(c, "CATEGORY_HAS_CHILDREN",
			"This section has subsections. Move or remove them first.")
		return
	}

	var articles int64
	ctl.db.WithContext(ctx).Model(&models.Article{}).Where("category_id = ?", id).Count(&articles)
	if articles > 0 {
		httpx.Conflict(c, "CATEGORY_HAS_ARTICLES",
			"This section still holds articles. Move them to another section first, or hide the section instead of deleting it.")
		return
	}

	var videos int64
	ctl.db.WithContext(ctx).Model(&models.Video{}).Where("category_id = ?", id).Count(&videos)
	if videos > 0 {
		httpx.Conflict(c, "CATEGORY_HAS_VIDEOS",
			"This section still holds videos. Move them to another section first.")
		return
	}

	// Unscoped, so the row is really gone. A soft delete would keep the slug
	// occupied against the unique index forever: the section would be invisible
	// yet un-recreatable, and the seeder would hit a duplicate-key error on a
	// slug it owns. Safe here because the guards above proved nothing points at
	// this row.
	if err := ctl.db.WithContext(ctx).Unscoped().Delete(&category).Error; err != nil {
		httpx.Internal(c, "Could not delete the section")
		return
	}

	ctl.cache.Delete(ctx, cache.KeyNav)
	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "category.delete", "category", &id, category.NameKh, nil)
	httpx.NoData(c)
}

// Reorder handles PUT /api/admin/categories/order.
func (ctl *AdminCategoryController) Reorder(c *gin.Context) {
	var req struct {
		Order []uint `json:"order" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "An ordered list of section ids is required")
		return
	}

	ctx := c.Request.Context()
	err := ctl.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index, id := range req.Order {
			if err := tx.Model(&models.Category{}).Where("id = ?", id).
				Update("position", index+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpx.Internal(c, "Could not save the new order")
		return
	}

	ctl.cache.Delete(ctx, cache.KeyNav)
	httpx.NoData(c)
}

// checkParent rejects a parent that would create a cycle or a third level.
// The public nav renders one level of nesting, so a grandchild would simply
// never appear.
func (ctl *AdminCategoryController) checkParent(ctx context.Context, parentID *uint, selfID uint) error {
	if parentID == nil {
		return nil
	}
	if selfID != 0 && *parentID == selfID {
		return errors.New("a section cannot be its own parent")
	}

	var parent models.Category
	if err := ctl.db.WithContext(ctx).First(&parent, *parentID).Error; err != nil {
		return errors.New("the chosen parent section does not exist")
	}
	if parent.ParentID != nil {
		return errors.New("sections nest one level only — the chosen parent is already a subsection")
	}
	return nil
}

func (ctl *AdminCategoryController) uniqueSlug(ctx context.Context, requested, nameEn, nameKh string, exceptID uint) (string, error) {
	base := utils.Slugify(requested)
	if base == "" {
		base = utils.SlugifyWithFallback(nameEn, nameKh, "section")
	}

	candidate := base
	for attempt := 0; attempt < 12; attempt++ {
		q := ctl.db.WithContext(ctx).Unscoped().Model(&models.Category{}).Where("slug = ?", candidate)
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

func (ctl *AdminCategoryController) createSlugRedirect(ctx context.Context, oldSlug, newSlug string) {
	redirect := models.Redirect{
		FromPath:   "/category/" + oldSlug,
		ToPath:     "/category/" + newSlug,
		StatusCode: 301, IsActive: true,
		Note: "automatic: section slug changed",
	}
	ctl.db.WithContext(ctx).Where("from_path = ?", redirect.FromPath).
		FirstOrCreate(&redirect)
}
