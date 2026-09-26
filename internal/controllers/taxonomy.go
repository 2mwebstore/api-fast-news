package controllers

import (
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/repositories"
)

// TaxonomyController serves categories and authors.
type TaxonomyController struct {
	db    *gorm.DB
	repo  *repositories.ArticleRepository
	cache *cache.Cache
}

func NewTaxonomyController(db *gorm.DB, repo *repositories.ArticleRepository, c *cache.Cache) *TaxonomyController {
	return &TaxonomyController{db: db, repo: repo, cache: c}
}

// CategoryDetail carries the fields a category page needs for its H1 and its
// metadata (§13).
type CategoryDetail struct {
	CategoryRef
	DescKh     string       `json:"descKh,omitempty"`
	DescEn     string       `json:"descEn,omitempty"`
	SEOTitleKh string       `json:"seoTitleKh,omitempty"`
	SEODescKh  string       `json:"seoDescKh,omitempty"`
	SEOTitleEn string       `json:"seoTitleEn,omitempty"`
	SEODescEn  string       `json:"seoDescEn,omitempty"`
	Parent     *CategoryRef `json:"parent,omitempty"`
	Children   []CategoryRef `json:"children,omitempty"`
	ArticleCount int         `json:"articleCount"`
}

// List handles GET /api/categories — the navigation source of truth.
func (ctl *TaxonomyController) List(c *gin.Context) {
	ctx := c.Request.Context()

	out := []CategoryDetail{}
	if ctl.cache.GetJSON(ctx, cache.KeyNav, &out) {
		httpx.OK(c, out)
		return
	}

	var categories []models.Category
	err := ctl.db.WithContext(ctx).
		Where("is_active = ?", true).
		Order("position ASC, id ASC").Find(&categories).Error
	if err != nil {
		httpx.Internal(c, "Could not load categories")
		return
	}

	// Build the parent/child tree in one pass rather than querying per parent.
	byID := make(map[uint]*CategoryDetail, len(categories))
	for i := range categories {
		cat := categories[i]
		byID[cat.ID] = &CategoryDetail{
			CategoryRef: CategoryRef{
				ID: cat.ID, Slug: cat.Slug, NameKh: cat.NameKh,
				NameEn: cat.NameEn, Color: cat.Color, Icon: cat.Icon,
			},
			DescKh: cat.DescKh, DescEn: cat.DescEn,
			SEOTitleKh: cat.SEOTitleKh, SEODescKh: cat.SEODescKh,
			SEOTitleEn: cat.SEOTitleEn, SEODescEn: cat.SEODescEn,
			ArticleCount: cat.ArticleCount,
		}
	}
	for i := range categories {
		cat := categories[i]
		if cat.ParentID == nil {
			continue
		}
		parent, ok := byID[*cat.ParentID]
		if !ok {
			continue
		}
		// InNav has to be honoured for subsections too. Filtering it only on
		// top-level rows let a hidden child (Economy, under Business) appear in
		// the public nav anyway.
		if cat.InNav {
			parent.Children = append(parent.Children, byID[cat.ID].CategoryRef)
		}
		byID[cat.ID].Parent = &parent.CategoryRef
	}

	for i := range categories {
		if categories[i].ParentID == nil && categories[i].InNav {
			out = append(out, *byID[categories[i].ID])
		}
	}

	ctl.cache.SetJSON(ctx, cache.KeyNav, out, cache.TTLLong)
	c.Header("Cache-Control", "public, max-age=600")
	httpx.OK(c, out)
}

// Get handles GET /api/categories/:slug, returning the section plus its feed.
func (ctl *TaxonomyController) Get(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	var category models.Category
	err := ctl.db.WithContext(ctx).Preload("Parent").
		Where("slug = ? AND is_active = ?", slug, true).First(&category).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, httpx.CodeCategoryNotFound, "Category not found")
			return
		}
		httpx.Internal(c, "Could not load category")
		return
	}

	filter := buildArticleFilter(c)
	filter.Category = slug
	articles, total, err := ctl.repo.List(ctx, filter)
	if err != nil {
		httpx.Internal(c, "Could not load category news")
		return
	}

	detail := CategoryDetail{
		CategoryRef: CategoryRef{
			ID: category.ID, Slug: category.Slug, NameKh: category.NameKh,
			NameEn: category.NameEn, Color: category.Color, Icon: category.Icon,
		},
		DescKh: category.DescKh, DescEn: category.DescEn,
		SEOTitleKh: category.SEOTitleKh, SEODescKh: category.SEODescKh,
		SEOTitleEn: category.SEOTitleEn, SEODescEn: category.SEODescEn,
		ArticleCount: category.ArticleCount,
	}
	if category.Parent != nil {
		detail.Parent = &CategoryRef{
			ID: category.Parent.ID, Slug: category.Parent.Slug,
			NameKh: category.Parent.NameKh, NameEn: category.Parent.NameEn,
		}
	}

	c.Header("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
	httpx.OKList(c, gin.H{
		"category": detail,
		"articles": toCards(articles),
	}, httpx.NewMeta(filter.Page, filter.Limit, total))
}

// AuthorDetail is the /author/[slug] payload (§18).
type AuthorDetail struct {
	AuthorRef
	BioKh    string `json:"bioKh,omitempty"`
	BioEn    string `json:"bioEn,omitempty"`
	Facebook string `json:"facebook,omitempty"`
	Telegram string `json:"telegram,omitempty"`
	X        string `json:"x,omitempty"`
	ArticleCount int `json:"articleCount"`
}

// Author handles GET /api/authors/:slug.
func (ctl *TaxonomyController) Author(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	var author models.Author
	err := ctl.db.WithContext(ctx).
		Where("slug = ? AND is_active = ?", slug, true).First(&author).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, httpx.CodeAuthorNotFound, "Author not found")
			return
		}
		httpx.Internal(c, "Could not load author")
		return
	}

	filter := buildArticleFilter(c)
	filter.AuthorSlug = slug
	articles, total, err := ctl.repo.List(ctx, filter)
	if err != nil {
		httpx.Internal(c, "Could not load author's articles")
		return
	}

	detail := AuthorDetail{
		AuthorRef: AuthorRef{
			ID: author.ID, Slug: author.Slug, NameKh: author.NameKh,
			NameEn: author.NameEn, Title: author.Title, PhotoURL: author.PhotoURL,
		},
		BioKh: author.BioKh, BioEn: author.BioEn,
		Facebook: author.Facebook, Telegram: author.Telegram, X: author.X,
		ArticleCount: author.ArticleCount,
	}

	c.Header("Cache-Control", "public, max-age=300")
	httpx.OKList(c, gin.H{"author": detail, "articles": toCards(articles)},
		httpx.NewMeta(filter.Page, filter.Limit, total))
}

// Authors handles GET /api/authors.
func (ctl *TaxonomyController) Authors(c *gin.Context) {
	var authors []models.Author
	err := ctl.db.WithContext(c.Request.Context()).
		Where("is_active = ?", true).
		Order("article_count DESC, name_kh ASC").Limit(100).Find(&authors).Error
	if err != nil {
		httpx.Internal(c, "Could not load authors")
		return
	}

	out := make([]AuthorDetail, 0, len(authors))
	for _, a := range authors {
		out = append(out, AuthorDetail{
			AuthorRef: AuthorRef{
				ID: a.ID, Slug: a.Slug, NameKh: a.NameKh,
				NameEn: a.NameEn, Title: a.Title, PhotoURL: a.PhotoURL,
			},
			BioKh: a.BioKh, ArticleCount: a.ArticleCount,
		})
	}
	httpx.OK(c, out)
}

// AdminList handles GET /api/admin/categories.
//
// Distinct from the public /api/categories, which returns only the navigation
// tree. A form has to be able to file a story under a section that is
// deliberately absent from the nav — Traffic and Economy are both hidden from
// readers but are real sections an editor must be able to choose. Filtering by
// InNav there meant those two were unassignable from the admin.
func (ctl *TaxonomyController) AdminList(c *gin.Context) {
	// Forms want only the sections a story can actually be filed in; the
	// management screen wants the hidden ones too, so it can bring one back.
	includeInactive := c.Query("includeInactive") == "true"

	q := ctl.db.WithContext(c.Request.Context()).Preload("Parent")
	if !includeInactive {
		q = q.Where("is_active = ?", true)
	}

	var categories []models.Category
	if err := q.Order("position ASC, id ASC").Find(&categories).Error; err != nil {
		httpx.Internal(c, "Could not load categories")
		return
	}

	// One grouped query for video counts rather than a count per row. Articles
	// already carry a denormalised counter on the category.
	videoCounts := map[uint]int{}
	if len(categories) > 0 {
		var rows []struct {
			CategoryID uint
			Total      int
		}
		ctl.db.WithContext(c.Request.Context()).Model(&models.Video{}).
			Select("category_id, COUNT(*) AS total").
			Where("category_id IS NOT NULL").
			Group("category_id").Scan(&rows)
		for _, row := range rows {
			videoCounts[row.CategoryID] = row.Total
		}
	}

	type adminCategory struct {
		ID     uint   `json:"id"`
		Slug   string `json:"slug"`
		NameKh string `json:"nameKh"`
		NameEn string `json:"nameEn"`
		InNav  bool   `json:"inNav"`
		// ParentNameKh lets the form show "Business › Economy" rather than a
		// flat list where a subsection looks like a top-level one.
		ParentNameKh string `json:"parentNameKh,omitempty"`
		ParentNameEn string `json:"parentNameEn,omitempty"`
		ArticleCount int    `json:"articleCount"`

		// Editable fields, for the management screen.
		ParentID   *uint  `json:"parentId"`
		IsActive   bool   `json:"isActive"`
		Position   int    `json:"position"`
		Icon       string `json:"icon,omitempty"`
		Color      string `json:"color,omitempty"`
		DescKh     string `json:"descKh,omitempty"`
		DescEn     string `json:"descEn,omitempty"`
		SEOTitleKh string `json:"seoTitleKh,omitempty"`
		SEODescKh  string `json:"seoDescKh,omitempty"`
		SEOTitleEn string `json:"seoTitleEn,omitempty"`
		SEODescEn  string `json:"seoDescEn,omitempty"`
		VideoCount int    `json:"videoCount"`
	}

	out := make([]adminCategory, 0, len(categories))
	for i := range categories {
		cat := categories[i]
		row := adminCategory{
			ID: cat.ID, Slug: cat.Slug,
			NameKh: cat.NameKh, NameEn: cat.NameEn,
			InNav: cat.InNav, ArticleCount: cat.ArticleCount,
			ParentID: cat.ParentID, IsActive: cat.IsActive,
			Position: cat.Position, Icon: cat.Icon, Color: cat.Color,
			DescKh: cat.DescKh, DescEn: cat.DescEn,
			SEOTitleKh: cat.SEOTitleKh, SEODescKh: cat.SEODescKh,
			SEOTitleEn: cat.SEOTitleEn, SEODescEn: cat.SEODescEn,
			VideoCount: videoCounts[cat.ID],
		}
		if cat.Parent != nil {
			row.ParentNameKh, row.ParentNameEn = cat.Parent.NameKh, cat.Parent.NameEn
		}
		out = append(out, row)
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, out)
}
