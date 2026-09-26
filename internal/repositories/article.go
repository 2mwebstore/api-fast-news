// Package repositories holds data access. Query construction lives here;
// business rules live in services.
package repositories

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/models"
)

// ArticleFilter mirrors the query parameters documented in §10.
type ArticleFilter struct {
	Category    string
	Subcategory string
	Tag         string
	AuthorSlug  string
	Search      string
	Status      models.ArticleStatus
	ContentType models.ContentType
	Breaking    *bool
	Featured    *bool
	From        *time.Time
	To          *time.Time
	Sort        string // latest | oldest | popular | trending
	Page        int
	Limit       int

	// IncludeNonPublic is set by admin endpoints. Public handlers leave it
	// false, which pins the query to published articles regardless of what
	// the client asked for.
	IncludeNonPublic bool
}

// Normalize clamps pagination and defaults the sort, so a hostile or sloppy
// client cannot ask for 100k rows.
func (f *ArticleFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 {
		f.Limit = 20
	}
	if f.Limit > 60 {
		f.Limit = 60
	}
	switch f.Sort {
	case "latest", "oldest", "popular", "trending":
	default:
		f.Sort = "latest"
	}
}

func (f ArticleFilter) offset() int { return (f.Page - 1) * f.Limit }

type ArticleRepository struct{ db *gorm.DB }

func NewArticleRepository(db *gorm.DB) *ArticleRepository { return &ArticleRepository{db: db} }

// listPreloads are the associations every card and list row needs. They are
// kept deliberately small — article bodies are never loaded for a list.
func (r *ArticleRepository) listQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&models.Article{}).
		Preload("Category").
		Preload("Author")
}

// apply translates a filter into SQL.
func (r *ArticleRepository) apply(q *gorm.DB, f ArticleFilter) *gorm.DB {
	if f.IncludeNonPublic {
		if f.Status != "" {
			q = q.Where("articles.status = ?", f.Status)
		}
	} else {
		// Public traffic only ever sees published, already-due articles.
		q = q.Where("articles.status = ?", models.StatusPublished).
			Where("articles.published_at IS NOT NULL AND articles.published_at <= ?", time.Now().UTC())
	}

	if f.ContentType != "" {
		q = q.Where("articles.content_type = ?", f.ContentType)
	}
	if f.Category != "" {
		// Match the canonical category or any child of it, so /category/cambodia
		// also surfaces Phnom Penh stories.
		q = q.Where(`articles.category_id IN (
			SELECT c.id FROM categories c
			LEFT JOIN categories p ON c.parent_id = p.id
			WHERE c.slug = ? OR p.slug = ?
		)`, f.Category, f.Category)
	}
	if f.Subcategory != "" {
		q = q.Where("articles.category_id IN (SELECT id FROM categories WHERE slug = ?)", f.Subcategory)
	}
	if f.Tag != "" {
		q = q.Where(`articles.id IN (
			SELECT at.article_id FROM article_tags at
			JOIN tags t ON t.id = at.tag_id WHERE t.slug = ?
		)`, f.Tag)
	}
	if f.AuthorSlug != "" {
		q = q.Where("articles.author_id IN (SELECT id FROM authors WHERE slug = ?)", f.AuthorSlug)
	}
	if f.Breaking != nil {
		q = q.Where("articles.is_breaking = ?", *f.Breaking)
	}
	if f.Featured != nil {
		q = q.Where("articles.is_featured = ?", *f.Featured)
	}
	if f.From != nil {
		q = q.Where("articles.published_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("articles.published_at <= ?", *f.To)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		q = applySearch(q, s)
	}
	return q
}

// applySearch prefers the ngram full-text index and falls back to LIKE.
// Khmer has no word delimiters, so a plain word-boundary match would return
// nothing for most Khmer queries — ngram is what makes Khmer search work.
func applySearch(q *gorm.DB, term string) *gorm.DB {
	like := "%" + term + "%"
	return q.Where(
		`(MATCH(articles.title_kh, articles.title_en, articles.summary_kh, articles.summary_en)
		   AGAINST (? IN NATURAL LANGUAGE MODE)
		 OR articles.title_kh LIKE ? OR articles.title_en LIKE ?
		 OR articles.summary_kh LIKE ? OR articles.summary_en LIKE ?)`,
		term, like, like, like, like,
	)
}

func applySort(q *gorm.DB, sort string) *gorm.DB {
	switch sort {
	case "oldest":
		return q.Order("articles.published_at ASC")
	case "popular":
		return q.Order("articles.view_count DESC, articles.published_at DESC")
	case "trending":
		return q.Order("articles.trending_score DESC, articles.published_at DESC")
	default:
		// Pinned stories lead the feed, then reverse chronological.
		return q.Order("articles.is_pinned DESC, articles.published_at DESC, articles.id DESC")
	}
}

// List returns one page of articles plus the total matching count.
func (r *ArticleRepository) List(ctx context.Context, f ArticleFilter) ([]models.Article, int64, error) {
	f.Normalize()

	var total int64
	countQ := r.apply(r.db.WithContext(ctx).Model(&models.Article{}), f)
	if err := countQ.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count articles: %w", err)
	}
	if total == 0 {
		return []models.Article{}, 0, nil
	}

	var out []models.Article
	q := applySort(r.apply(r.listQuery(ctx), f), f.Sort).
		Limit(f.Limit).Offset(f.offset())
	if err := q.Find(&out).Error; err != nil {
		return nil, 0, fmt.Errorf("list articles: %w", err)
	}
	return out, total, nil
}

// GetBySlug loads a single published article with everything the article page
// renders. includeNonPublic is set for admin preview.
func (r *ArticleRepository) GetBySlug(ctx context.Context, slug string, includeNonPublic bool) (*models.Article, error) {
	q := r.db.WithContext(ctx).
		Preload("Category").Preload("Category.Parent").
		Preload("Author").Preload("Tags").Preload("SEO").
		Preload("Corrections", func(db *gorm.DB) *gorm.DB {
			return db.Order("corrected_at DESC")
		}).
		Where("slug = ?", slug)

	if !includeNonPublic {
		q = q.Where("status = ?", models.StatusPublished).
			Where("published_at IS NOT NULL AND published_at <= ?", time.Now().UTC())
	}

	var a models.Article
	if err := q.First(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// GetByID loads an article for editing, including internal-only associations.
func (r *ArticleRepository) GetByID(ctx context.Context, id uint) (*models.Article, error) {
	var a models.Article
	err := r.db.WithContext(ctx).
		Preload("Category").Preload("Author").Preload("Tags").Preload("SEO").
		Preload("Sources").Preload("Corrections").
		First(&a, id).Error
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Related returns stories to show beneath an article (§62): same category
// first, newest, excluding the article itself.
func (r *ArticleRepository) Related(ctx context.Context, a *models.Article, limit int) ([]models.Article, error) {
	var out []models.Article
	err := r.listQuery(ctx).
		Where("articles.id <> ?", a.ID).
		Where("articles.category_id = ?", a.CategoryID).
		Where("articles.status = ?", models.StatusPublished).
		Where("articles.published_at <= ?", time.Now().UTC()).
		Order("articles.published_at DESC").
		Limit(limit).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("related articles: %w", err)
	}

	// Backfill from anywhere on the site when the category is too thin to fill
	// the rail, so the slot is never half-empty.
	if len(out) < limit {
		exclude := []uint{a.ID}
		for _, x := range out {
			exclude = append(exclude, x.ID)
		}
		var extra []models.Article
		err := r.listQuery(ctx).
			Where("articles.id NOT IN ?", exclude).
			Where("articles.status = ?", models.StatusPublished).
			Where("articles.published_at <= ?", time.Now().UTC()).
			Order("articles.published_at DESC").
			Limit(limit - len(out)).Find(&extra).Error
		if err == nil {
			out = append(out, extra...)
		}
	}
	return out, nil
}

// Breaking returns the live breaking stories for the red bar and /live (§7, §8).
func (r *ArticleRepository) Breaking(ctx context.Context, limit int) ([]models.Article, error) {
	now := time.Now().UTC()
	var out []models.Article
	err := r.listQuery(ctx).
		Where("articles.is_breaking = ?", true).
		Where("articles.status = ?", models.StatusPublished).
		Where("articles.published_at <= ?", now).
		Where("articles.breaking_started_at IS NOT NULL AND articles.breaking_started_at <= ?", now).
		Where("articles.breaking_ended_at IS NULL OR articles.breaking_ended_at > ?", now).
		Order("articles.breaking_started_at DESC").
		Limit(limit).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("breaking articles: %w", err)
	}
	return out, nil
}

// Featured returns the hero story plus its companions (§9).
func (r *ArticleRepository) Featured(ctx context.Context, limit int) ([]models.Article, error) {
	var out []models.Article
	now := time.Now().UTC()
	err := r.listQuery(ctx).
		Where("articles.status = ?", models.StatusPublished).
		Where("articles.published_at <= ?", now).
		Order("articles.is_featured DESC, articles.is_pinned DESC, articles.published_at DESC").
		Limit(limit).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("featured articles: %w", err)
	}
	return out, nil
}

// SlugExists reports whether a slug is taken, optionally ignoring one article
// (so re-saving an article does not collide with itself).
func (r *ArticleRepository) SlugExists(ctx context.Context, slug string, exceptID uint) (bool, error) {
	q := r.db.WithContext(ctx).Unscoped().Model(&models.Article{}).Where("slug = ?", slug)
	if exceptID > 0 {
		q = q.Where("id <> ?", exceptID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *ArticleRepository) Create(ctx context.Context, a *models.Article) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *ArticleRepository) Save(ctx context.Context, a *models.Article) error {
	return r.db.WithContext(ctx).Save(a).Error
}

func (r *ArticleRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&models.Article{}, id).Error
}

// DueForPublish returns scheduled articles whose time has arrived (§16).
func (r *ArticleRepository) DueForPublish(ctx context.Context, now time.Time) ([]models.Article, error) {
	var out []models.Article
	err := r.db.WithContext(ctx).
		Where("status = ?", models.StatusScheduled).
		Where("scheduled_at IS NOT NULL AND scheduled_at <= ?", now).
		Limit(50).Find(&out).Error
	return out, err
}

// SitemapRows is the trimmed projection the sitemap builders need — loading
// full articles to emit URLs would be wasteful at scale (§50, §51).
type SitemapRow struct {
	Slug        string
	TitleKh     string
	TitleEn     string
	PublishedAt time.Time
	UpdatedAt   time.Time
	ImageURL    string
	CategorySlug string
}

// SitemapRows returns published articles for the XML sitemap. A `since` of the
// zero time returns everything; the news sitemap passes 48 hours ago (§51).
func (r *ArticleRepository) SitemapRows(ctx context.Context, since time.Time, limit int) ([]SitemapRow, error) {
	q := r.db.WithContext(ctx).Model(&models.Article{}).
		Select(`articles.slug, articles.title_kh, articles.title_en,
		        articles.published_at, articles.updated_at, articles.image_url,
		        categories.slug AS category_slug`).
		Joins("LEFT JOIN categories ON categories.id = articles.category_id").
		Where("articles.status = ?", models.StatusPublished).
		Where("articles.published_at IS NOT NULL AND articles.published_at <= ?", time.Now().UTC()).
		Where("articles.deleted_at IS NULL").
		Order("articles.published_at DESC").
		Limit(limit)

	if !since.IsZero() {
		q = q.Where("articles.published_at >= ?", since)
	}

	var rows []SitemapRow
	if err := q.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("sitemap rows: %w", err)
	}
	return rows, nil
}
