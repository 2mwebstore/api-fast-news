package seo

import (
	"strings"
	"testing"

	"github.com/cambodia-fast-news/backend/internal/config"
)

func TestRobotsTxtBlocksNonProduction(t *testing.T) {
	// A staging copy indexed next to production is a duplicate-content
	// problem that is tedious to undo, so staging must disallow everything.
	svc := &Service{cfg: &config.Config{App: config.App{Env: "development", URL: "http://localhost:3000"}}}

	got := svc.BuildRobotsTxt()
	if !strings.Contains(got, "Disallow: /") {
		t.Error("non-production robots.txt must disallow everything")
	}
	if strings.Contains(got, "Sitemap:") {
		t.Error("non-production robots.txt should not advertise a sitemap")
	}
}

func TestRobotsTxtProductionAllowsContentAndBlocksPrivateAreas(t *testing.T) {
	svc := &Service{cfg: &config.Config{App: config.App{
		Env: "production", URL: "https://example.com", SiteName: "Cambodia Fast News",
	}}}

	got := svc.BuildRobotsTxt()

	for _, want := range []string{
		"Allow: /news/", "Allow: /category/",
		"Sitemap: https://example.com/sitemap.xml",
		"Sitemap: https://example.com/news-sitemap.xml",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("production robots.txt is missing %q", want)
		}
	}

	// §32 and §50: admin, the API and search must stay out of the index.
	for _, want := range []string{"Disallow: /admin", "Disallow: /api/", "Disallow: /search"} {
		if !strings.Contains(got, want) {
			t.Errorf("production robots.txt is missing %q", want)
		}
	}
}
