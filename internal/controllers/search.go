package controllers

import (
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/repositories"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// SearchController backs /search (§32).
type SearchController struct {
	db   *gorm.DB
	repo *repositories.ArticleRepository
}

func NewSearchController(db *gorm.DB, repo *repositories.ArticleRepository) *SearchController {
	return &SearchController{db: db, repo: repo}
}

// Search handles GET /api/search across articles, videos, authors and
// categories.
func (ctl *SearchController) Search(c *gin.Context) {
	ctx := c.Request.Context()
	term := utils.Truncate(strings.TrimSpace(c.Query("q")), 120)

	// Search result pages are noindex,follow (§32). The API says so explicitly
	// so the frontend does not have to remember.
	c.Header("X-Robots-Tag", "noindex, follow")

	if len([]rune(term)) < 2 {
		httpx.OK(c, gin.H{
			"query": term, "articles": []ArticleCard{},
			"videos": []VideoCard{}, "authors": []AuthorRef{}, "categories": []CategoryRef{},
		})
		return
	}

	filter := buildArticleFilter(c)
	filter.Search = term
	articles, total, err := ctl.repo.List(ctx, filter)
	if err != nil {
		httpx.Internal(c, "Search failed")
		return
	}

	like := "%" + term + "%"

	var videos []models.Video
	ctl.db.WithContext(ctx).Preload("Category").
		Where("status = ?", models.StatusPublished).
		Where("title_kh LIKE ? OR title_en LIKE ?", like, like).
		Order("published_at DESC").Limit(8).Find(&videos)

	var authors []models.Author
	ctl.db.WithContext(ctx).
		Where("is_active = ?", true).
		Where("name_kh LIKE ? OR name_en LIKE ?", like, like).
		Limit(5).Find(&authors)

	var categories []models.Category
	ctl.db.WithContext(ctx).
		Where("is_active = ?", true).
		Where("name_kh LIKE ? OR name_en LIKE ?", like, like).
		Limit(5).Find(&categories)

	videoCards := make([]VideoCard, 0, len(videos))
	for i := range videos {
		videoCards = append(videoCards, toVideoCard(&videos[i], false))
	}
	authorRefs := make([]AuthorRef, 0, len(authors))
	for _, a := range authors {
		authorRefs = append(authorRefs, AuthorRef{
			ID: a.ID, Slug: a.Slug, NameKh: a.NameKh, NameEn: a.NameEn,
			Title: a.Title, PhotoURL: a.PhotoURL,
		})
	}
	categoryRefs := make([]CategoryRef, 0, len(categories))
	for _, cat := range categories {
		categoryRefs = append(categoryRefs, CategoryRef{
			ID: cat.ID, Slug: cat.Slug, NameKh: cat.NameKh,
			NameEn: cat.NameEn, Color: cat.Color, Icon: cat.Icon,
		})
	}

	httpx.OKList(c, gin.H{
		"query":      term,
		"articles":   toCards(articles),
		"videos":     videoCards,
		"authors":    authorRefs,
		"categories": categoryRefs,
	}, httpx.NewMeta(filter.Page, filter.Limit, total))
}
