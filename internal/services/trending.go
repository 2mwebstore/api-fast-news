package services

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/models"
)

// TrendingWeights are the configurable factors from §44. They are exposed as a
// struct rather than hard-coded constants so the newsroom can retune what
// "trending" means without a code change.
type TrendingWeights struct {
	Views       float64 `json:"views"`
	UniqueViews float64 `json:"uniqueViews"`
	Shares      float64 `json:"shares"`
	ReadSeconds float64 `json:"readSeconds"`
	// HalfLifeHours controls how fast an older story decays out of the list.
	HalfLifeHours float64 `json:"halfLifeHours"`
}

func DefaultTrendingWeights() TrendingWeights {
	return TrendingWeights{
		Views:         1.0,
		UniqueViews:   2.0,
		Shares:        8.0,
		ReadSeconds:   0.02,
		HalfLifeHours: 12,
	}
}

type TrendingService struct {
	db      *gorm.DB
	cache   *cache.Cache
	weights TrendingWeights
}

func NewTrendingService(db *gorm.DB, c *cache.Cache) *TrendingService {
	return &TrendingService{db: db, cache: c, weights: DefaultTrendingWeights()}
}

// scoreRow is the aggregate pulled per article for scoring.
type scoreRow struct {
	ArticleID   uint
	CategoryID  uint
	Views       int64
	UniqueViews int64
	ReadSeconds int64
	Shares      int64
	PublishedAt time.Time
}

// Recompute rebuilds the trending table from the last 48 hours of activity.
// It runs on a ticker rather than per request so the homepage stays cacheable.
func (s *TrendingService) Recompute(ctx context.Context) error {
	since := time.Now().UTC().Add(-48 * time.Hour)
	day := since.Format("2006-01-02")

	var rows []scoreRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT a.id AS article_id,
		       a.category_id,
		       a.published_at,
		       COALESCE(v.views, 0)        AS views,
		       COALESCE(v.unique_views, 0) AS unique_views,
		       COALESCE(v.read_seconds, 0) AS read_seconds,
		       COALESCE(sh.shares, 0)      AS shares
		FROM articles a
		LEFT JOIN (
		    SELECT article_id,
		           SUM(views) AS views,
		           SUM(unique_views) AS unique_views,
		           SUM(read_seconds) AS read_seconds
		    FROM article_views WHERE day >= ? GROUP BY article_id
		) v ON v.article_id = a.id
		LEFT JOIN (
		    SELECT article_id, SUM(count) AS shares
		    FROM article_shares WHERE day >= ? GROUP BY article_id
		) sh ON sh.article_id = a.id
		WHERE a.status = ? AND a.deleted_at IS NULL
		  AND a.published_at IS NOT NULL AND a.published_at >= ?
	`, day, day, models.StatusPublished, since).Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("load trending aggregates: %w", err)
	}

	now := time.Now().UTC()
	type scored struct {
		row   scoreRow
		score float64
	}
	results := make([]scored, 0, len(rows))
	for _, r := range rows {
		score := s.score(r, now)
		if score > 0 {
			results = append(results, scored{row: r, score: score})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })

	const keep = 30
	if len(results) > keep {
		results = results[:keep]
	}

	// Replace the table in one transaction so readers never see a half-built
	// trending list.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("time_window = ?", "24h").Delete(&models.TrendingNews{}).Error; err != nil {
			return err
		}
		for i, r := range results {
			entry := models.TrendingNews{
				ArticleID:  r.row.ArticleID,
				Score:      r.score,
				Position:   i + 1,
				Window:     "24h",
				ComputedAt: now,
			}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.Article{}).Where("id = ?", r.row.ArticleID).
				Update("trending_score", r.score).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("store trending: %w", err)
	}

	s.cache.Delete(ctx, cache.KeyTrending, cache.KeyPulse, cache.KeyFiveMin)
	slog.Info("recomputed trending", "articles", len(results))
	return nil
}

// score combines engagement with exponential time decay, so a story with
// moderate traffic in the last hour can outrank yesterday's biggest piece.
func (s *TrendingService) score(r scoreRow, now time.Time) float64 {
	engagement := float64(r.Views)*s.weights.Views +
		float64(r.UniqueViews)*s.weights.UniqueViews +
		float64(r.Shares)*s.weights.Shares +
		float64(r.ReadSeconds)*s.weights.ReadSeconds

	if engagement <= 0 {
		return 0
	}
	ageHours := now.Sub(r.PublishedAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	decay := math.Pow(0.5, ageHours/s.weights.HalfLifeHours)
	return engagement * decay
}

// Top returns the current trending articles.
func (s *TrendingService) Top(ctx context.Context, limit int) ([]models.Article, error) {
	if limit < 1 || limit > 30 {
		limit = 10
	}

	var entries []models.TrendingNews
	err := s.db.WithContext(ctx).
		Preload("Article").Preload("Article.Category").Preload("Article.Author").
		Where("time_window = ?", "24h").
		Order("position ASC").Limit(limit).Find(&entries).Error
	if err != nil {
		return nil, fmt.Errorf("load trending: %w", err)
	}

	out := make([]models.Article, 0, len(entries))
	for _, e := range entries {
		if e.Article != nil {
			out = append(out, *e.Article)
		}
	}

	// Before the first recompute — or on a quiet night — fall back to recent
	// popular stories so the module is never empty.
	if len(out) == 0 {
		err := s.db.WithContext(ctx).
			Preload("Category").Preload("Author").
			Where("status = ?", models.StatusPublished).
			Where("published_at IS NOT NULL AND published_at <= ?", time.Now().UTC()).
			Order("view_count DESC, published_at DESC").
			Limit(limit).Find(&out).Error
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PulseEntry is one bar in the News Pulse module (§12).
type PulseEntry struct {
	CategorySlug string  `json:"categorySlug"`
	CategoryKh   string  `json:"categoryKh"`
	CategoryEn   string  `json:"categoryEn"`
	Color        string  `json:"color"`
	Score        float64 `json:"score"`
	// Percent is the bar length relative to the busiest category, 0–100.
	Percent int `json:"percent"`
}

// PulseResult wraps the bars with the disclosure the module must carry.
type PulseResult struct {
	Entries  []PulseEntry `json:"entries"`
	Window   string       `json:"window"`
	Computed time.Time    `json:"computedAt"`
	// Disclaimer is rendered with the module. News Pulse measures activity on
	// this site only — it is not a measure of public opinion (§12).
	Disclaimer   string `json:"disclaimer"`
	DisclaimerKh string `json:"disclaimerKh"`
}

const (
	pulseDisclaimerEn = "Based on reading activity on Cambodia Fast News over the last 24 hours. Not a measure of public opinion."
	pulseDisclaimerKh = "ផ្អែកលើសកម្មភាពអានលើ Cambodia Fast News ក្នុងរយៈពេល ២៤ ម៉ោងចុងក្រោយ។ មិនមែនជារង្វាស់មតិសាធារណៈទេ។"
)

// Pulse aggregates trending scores per category (§12).
func (s *TrendingService) Pulse(ctx context.Context) (*PulseResult, error) {
	type row struct {
		Slug   string
		NameKh string
		NameEn string
		Color  string
		Score  float64
	}

	var rows []row
	err := s.db.WithContext(ctx).Raw(`
		SELECT c.slug, c.name_kh, c.name_en, c.color, COALESCE(SUM(a.trending_score), 0) AS score
		FROM categories c
		LEFT JOIN articles a ON a.category_id = c.id
		     AND a.status = ? AND a.deleted_at IS NULL
		     AND a.published_at >= ?
		WHERE c.is_active = 1 AND c.deleted_at IS NULL
		GROUP BY c.id, c.slug, c.name_kh, c.name_en, c.color
		HAVING score > 0
		ORDER BY score DESC
		LIMIT 8
	`, models.StatusPublished, time.Now().UTC().Add(-24*time.Hour)).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load pulse: %w", err)
	}

	result := &PulseResult{
		Entries:      make([]PulseEntry, 0, len(rows)),
		Window:       "24h",
		Computed:     time.Now().UTC(),
		Disclaimer:   pulseDisclaimerEn,
		DisclaimerKh: pulseDisclaimerKh,
	}
	if len(rows) == 0 {
		return result, nil
	}

	top := rows[0].Score
	for _, r := range rows {
		percent := 0
		if top > 0 {
			percent = int(math.Round(r.Score / top * 100))
		}
		result.Entries = append(result.Entries, PulseEntry{
			CategorySlug: r.Slug,
			CategoryKh:   r.NameKh,
			CategoryEn:   r.NameEn,
			Color:        r.Color,
			Score:        r.Score,
			Percent:      percent,
		})
	}
	return result, nil
}

// FiveMinute returns the numbered fast-read list from §11: the most important
// recent stories, one per category where possible so the list spans the day's
// news rather than five takes on one story.
func (s *TrendingService) FiveMinute(ctx context.Context, limit int) ([]models.Article, error) {
	if limit < 1 || limit > 10 {
		limit = 5
	}
	since := time.Now().UTC().Add(-24 * time.Hour)

	var candidates []models.Article
	err := s.db.WithContext(ctx).
		Preload("Category").Preload("Author").
		Where("status = ?", models.StatusPublished).
		Where("published_at IS NOT NULL AND published_at <= ?", time.Now().UTC()).
		Where("published_at >= ?", since).
		Order("is_breaking DESC, trending_score DESC, published_at DESC").
		Limit(limit * 6).Find(&candidates).Error
	if err != nil {
		return nil, fmt.Errorf("load five-minute candidates: %w", err)
	}

	out := make([]models.Article, 0, limit)
	seen := make(map[uint]bool, limit)
	for _, a := range candidates {
		if seen[a.CategoryID] {
			continue
		}
		seen[a.CategoryID] = true
		out = append(out, a)
		if len(out) == limit {
			return out, nil
		}
	}
	// Top up with the best remaining stories if there were not enough distinct
	// categories to fill the list.
	for _, a := range candidates {
		if len(out) == limit {
			break
		}
		duplicate := false
		for _, picked := range out {
			if picked.ID == a.ID {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, a)
		}
	}
	return out, nil
}
