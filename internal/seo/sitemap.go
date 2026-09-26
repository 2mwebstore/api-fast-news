// Package seo builds sitemaps and audits on-site SEO health (§50, §51, §60).
package seo

import (
	"context"
	"encoding/xml"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/repositories"
)

// Google caps a single sitemap at 50,000 URLs, and the News sitemap at 1,000.
const (
	maxSitemapURLs = 50000
	maxNewsURLs    = 1000
	// newsWindow is how far back the News sitemap reaches. Google only
	// considers articles from roughly the last two days, and §51 is explicit
	// that the whole archive must not go in here.
	newsWindow = 48 * time.Hour
)

type Service struct {
	db       *gorm.DB
	articles *repositories.ArticleRepository
	cfg      *config.Config
}

func NewService(db *gorm.DB, articles *repositories.ArticleRepository, cfg *config.Config) *Service {
	return &Service{db: db, articles: articles, cfg: cfg}
}

// URLSet is the standard sitemap document.
type URLSet struct {
	XMLName xml.Name `xml:"urlset"`
	Xmlns   string   `xml:"xmlns,attr"`
	XmlnsNews string `xml:"xmlns:news,attr,omitempty"`
	XmlnsImage string `xml:"xmlns:image,attr,omitempty"`
	URLs    []URL    `xml:"url"`
}

type URL struct {
	Loc        string    `xml:"loc"`
	LastMod    string    `xml:"lastmod,omitempty"`
	ChangeFreq string    `xml:"changefreq,omitempty"`
	Priority   string    `xml:"priority,omitempty"`
	News       *NewsInfo `xml:"news:news,omitempty"`
	Image      *ImageInfo `xml:"image:image,omitempty"`
}

type NewsInfo struct {
	Publication     Publication `xml:"news:publication"`
	PublicationDate string      `xml:"news:publication_date"`
	Title           string      `xml:"news:title"`
}

type Publication struct {
	Name     string `xml:"news:name"`
	Language string `xml:"news:language"`
}

type ImageInfo struct {
	Loc string `xml:"image:loc"`
}

const (
	nsSitemap = "http://www.sitemaps.org/schemas/sitemap/0.9"
	nsNews    = "http://www.google.com/schemas/sitemap-news/0.9"
	nsImage   = "http://www.google.com/schemas/sitemap-image/1.1"
)

// BuildSitemap returns the main XML sitemap: homepage, sections, authors,
// videos, published articles and the static pages. Admin, login, search and
// private routes are excluded (§50).
func (s *Service) BuildSitemap(ctx context.Context) ([]byte, error) {
	base := s.cfg.App.URL
	now := time.Now().UTC().Format(time.RFC3339)

	set := URLSet{Xmlns: nsSitemap}
	add := func(path, lastmod, freq, priority string) {
		set.URLs = append(set.URLs, URL{
			Loc: base + path, LastMod: lastmod, ChangeFreq: freq, Priority: priority,
		})
	}

	add("/", now, "hourly", "1.0")
	add("/live", now, "hourly", "0.9")
	add("/video", now, "daily", "0.8")

	var categories []models.Category
	if err := s.db.WithContext(ctx).
		Where("is_active = ?", true).Order("position ASC").Find(&categories).Error; err != nil {
		return nil, fmt.Errorf("sitemap categories: %w", err)
	}
	for _, c := range categories {
		add("/category/"+c.Slug, now, "hourly", "0.8")
	}

	var authors []models.Author
	if err := s.db.WithContext(ctx).
		Where("is_active = ? AND article_count > 0", true).Find(&authors).Error; err != nil {
		return nil, fmt.Errorf("sitemap authors: %w", err)
	}
	for _, a := range authors {
		add("/author/"+a.Slug, now, "weekly", "0.5")
	}

	var videos []models.Video
	if err := s.db.WithContext(ctx).
		Where("status = ?", models.StatusPublished).
		Order("published_at DESC").Limit(2000).Find(&videos).Error; err != nil {
		return nil, fmt.Errorf("sitemap videos: %w", err)
	}
	for _, v := range videos {
		lastmod := v.UpdatedAt.UTC().Format(time.RFC3339)
		add("/video/"+v.Slug, lastmod, "weekly", "0.6")
	}

	remaining := maxSitemapURLs - len(set.URLs) - 6 // leave room for static pages
	rows, err := s.articles.SitemapRows(ctx, time.Time{}, remaining)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		url := URL{
			Loc:        base + "/news/" + r.Slug,
			LastMod:    r.UpdatedAt.UTC().Format(time.RFC3339),
			ChangeFreq: "daily",
			Priority:   "0.7",
		}
		if r.ImageURL != "" {
			url.Image = &ImageInfo{Loc: r.ImageURL}
			set.XmlnsImage = nsImage
		}
		set.URLs = append(set.URLs, url)
	}

	for _, p := range []string{"/about", "/contact", "/privacy", "/terms", "/editorial-policy", "/correction-policy"} {
		add(p, now, "monthly", "0.3")
	}

	return marshal(set)
}

// BuildNewsSitemap returns the Google News sitemap. It deliberately contains
// only articles published inside the news window — putting the full archive
// here is the most common way to get a News sitemap rejected (§51).
func (s *Service) BuildNewsSitemap(ctx context.Context) ([]byte, error) {
	since := time.Now().UTC().Add(-newsWindow)
	rows, err := s.articles.SitemapRows(ctx, since, maxNewsURLs)
	if err != nil {
		return nil, err
	}

	set := URLSet{Xmlns: nsSitemap, XmlnsNews: nsNews}
	for _, r := range rows {
		// Prefer the Khmer headline: it is the canonical language of the page,
		// and news:title must match what the page actually shows.
		title := r.TitleKh
		language := "km"
		if title == "" {
			title, language = r.TitleEn, "en"
		}
		if title == "" {
			continue
		}
		set.URLs = append(set.URLs, URL{
			Loc: s.cfg.App.URL + "/news/" + r.Slug,
			News: &NewsInfo{
				Publication: Publication{
					Name:     s.cfg.App.SiteName,
					Language: language,
				},
				PublicationDate: r.PublishedAt.UTC().Format(time.RFC3339),
				Title:           title,
			},
		})
	}
	return marshal(set)
}

func marshal(set URLSet) ([]byte, error) {
	body, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal sitemap: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

// BuildRobotsTxt returns robots.txt (§52).
//
// A non-production deployment returns a blanket Disallow: a staging copy of the
// site indexed alongside production is a duplicate-content problem that is
// tedious to undo.
func (s *Service) BuildRobotsTxt() string {
	base := s.cfg.App.URL

	if !s.cfg.App.IsProduction() {
		return "# Non-production environment\nUser-agent: *\nDisallow: /\n"
	}

	return fmt.Sprintf(`User-agent: *
Allow: /

# Private and low-value surfaces
Disallow: /admin
Disallow: /admin/
Disallow: /login
Disallow: /api/
Disallow: /search
Disallow: /*?utm_
Disallow: /*?ref=

# Crawl the news feeds freely
Allow: /news/
Allow: /category/
Allow: /video/
Allow: /author/

Sitemap: %s/sitemap.xml
Sitemap: %s/news-sitemap.xml
`, base, base)
}
