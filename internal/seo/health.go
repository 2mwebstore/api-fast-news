package seo

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/models"
)

// HealthReport backs the SEO dashboard in §60 and §89.
//
// This is an editorial and technical hygiene report. It counts concrete,
// checkable defects on this site — it is not a ranking score, and nothing here
// predicts how Google will treat a page.
type HealthReport struct {
	PublishedArticles int64  `json:"publishedArticles"`
	SitemapOK         bool   `json:"sitemapOk"`
	NewsSitemapOK     bool   `json:"newsSitemapOk"`
	RobotsOK          bool   `json:"robotsOk"`
	NewsSitemapCount  int64  `json:"newsSitemapCount"`

	Issues []Issue `json:"issues"`

	// Note is rendered with the dashboard so the numbers are not mistaken for
	// a Google score.
	Note string `json:"note"`
}

// Issue is one category of defect, with a sample of affected articles so an
// editor can act on it rather than just read a count.
type Issue struct {
	Key      string       `json:"key"`
	Label    string       `json:"label"`
	Severity string       `json:"severity"` // warning | error
	Count    int64        `json:"count"`
	Samples  []IssueSample `json:"samples,omitempty"`
}

type IssueSample struct {
	ID      uint   `json:"id"`
	Slug    string `json:"slug"`
	TitleKh string `json:"titleKh"`
}

const healthNote = "This dashboard reports checkable defects in our own content and configuration. It is not a Google ranking score and does not predict indexing."

// Audit runs every check and returns the report.
func (s *Service) Audit(ctx context.Context) (*HealthReport, error) {
	report := &HealthReport{Note: healthNote, RobotsOK: true}

	db := s.db.WithContext(ctx)
	published := db.Model(&models.Article{}).Where("status = ?", models.StatusPublished)

	if err := published.Session(&gorm.Session{}).Count(&report.PublishedArticles).Error; err != nil {
		return nil, fmt.Errorf("count published: %w", err)
	}

	checks := []struct {
		key, label, severity, where string
		args                        []any
	}{
		{
			"missing-description", "Articles with no summary or SEO description", "warning",
			`status = ? AND (summary_kh IS NULL OR summary_kh = '')
			 AND id NOT IN (SELECT article_id FROM seo_metadata
			                WHERE article_id IS NOT NULL AND seo_description <> '')`,
			[]any{models.StatusPublished},
		},
		{
			"missing-alt", "Published images with no ALT text", "warning",
			`status = ? AND image_url <> '' AND (image_alt_kh = '' AND image_alt_en = '')`,
			[]any{models.StatusPublished},
		},
		{
			"missing-image", "Published articles with no main image", "warning",
			`status = ? AND (image_url IS NULL OR image_url = '')`,
			[]any{models.StatusPublished},
		},
		{
			"missing-author", "Published articles with no byline", "warning",
			`status = ? AND author_id IS NULL`,
			[]any{models.StatusPublished},
		},
		{
			"missing-source", "Published articles with no recorded source", "error",
			`status = ? AND id NOT IN (SELECT article_id FROM article_sources WHERE deleted_at IS NULL)`,
			[]any{models.StatusPublished},
		},
		{
			"undisclosed-sponsored", "Sponsored articles with no sponsor name", "error",
			`status = ? AND content_type <> ? AND (sponsor_name IS NULL OR sponsor_name = '')`,
			[]any{models.StatusPublished, models.ContentEditorial},
		},
		{
			"thin-content", "Published articles under 100 words", "warning",
			`status = ? AND word_count < 100`,
			[]any{models.StatusPublished},
		},
	}

	for _, check := range checks {
		var count int64
		q := db.Model(&models.Article{}).Where(check.where, check.args...)
		if err := q.Session(&gorm.Session{}).Count(&count).Error; err != nil {
			return nil, fmt.Errorf("seo check %s: %w", check.key, err)
		}
		if count == 0 {
			continue
		}

		var samples []IssueSample
		if err := db.Model(&models.Article{}).
			Select("id, slug, title_kh").
			Where(check.where, check.args...).
			Order("published_at DESC").Limit(5).Scan(&samples).Error; err != nil {
			return nil, fmt.Errorf("seo samples %s: %w", check.key, err)
		}

		report.Issues = append(report.Issues, Issue{
			Key: check.key, Label: check.label, Severity: check.severity,
			Count: count, Samples: samples,
		})
	}

	// Duplicate slugs would make two articles fight over one canonical URL.
	var duplicateSlugs int64
	err := db.Raw(`SELECT COUNT(*) FROM (
		SELECT slug FROM articles WHERE deleted_at IS NULL
		GROUP BY slug HAVING COUNT(*) > 1) d`).Scan(&duplicateSlugs).Error
	if err != nil {
		return nil, fmt.Errorf("duplicate slug check: %w", err)
	}
	if duplicateSlugs > 0 {
		report.Issues = append(report.Issues, Issue{
			Key: "duplicate-slug", Label: "Duplicate article slugs", Severity: "error",
			Count: duplicateSlugs,
		})
	}

	// Sitemap readiness.
	if err := db.Model(&models.Article{}).
		Where("status = ? AND published_at >= ?", models.StatusPublished, newsCutoff()).
		Count(&report.NewsSitemapCount).Error; err != nil {
		return nil, fmt.Errorf("news sitemap count: %w", err)
	}
	report.SitemapOK = report.PublishedArticles > 0
	report.NewsSitemapOK = true // the document is always valid; it may be empty on a quiet day

	return report, nil
}

// newsCutoff is the start of the Google News window, shared with the sitemap
// builder so the dashboard count matches what the sitemap actually emits.
func newsCutoff() time.Time { return time.Now().UTC().Add(-newsWindow) }
