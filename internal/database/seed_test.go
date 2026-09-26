package database

import (
	"strings"
	"testing"

	"github.com/cambodia-fast-news/backend/internal/config"
)

// Demo image URLs are stored in a 512-character column. An overflow surfaces
// as "Data too long for column" partway through a seed, which is a confusing
// way to learn that a placeholder grew.
func TestPlaceholderImagesFitTheColumn(t *testing.T) {
	for _, category := range categorySeeds {
		got := placeholderImage(category.Icon, category.Color)
		if len(got) > maxImageURLLen {
			t.Errorf("placeholderImage(%q) is %d bytes, column holds %d",
				category.Slug, len(got), maxImageURLLen)
		}
		if !strings.HasPrefix(got, "data:image/svg+xml") {
			t.Errorf("placeholderImage(%q) is not a data URI: %q", category.Slug, got[:40])
		}
		// A raw '#' would terminate the URI at the fragment, leaving a
		// colourless placeholder.
		if strings.Contains(got[len("data:image/svg+xml;charset=utf-8,"):], "#") {
			t.Errorf("placeholderImage(%q) contains an unescaped '#'", category.Slug)
		}
	}

	for _, author := range demoAuthorSeeds {
		initial := string([]rune(author.NameEn)[:1])
		if got := avatarImage(initial, "#1E3A8A"); len(got) > maxImageURLLen {
			t.Errorf("avatarImage(%q) is %d bytes, column holds %d", author.Slug, len(got), maxImageURLLen)
		}
	}
}

func TestPhotoURLsAreDeterministicAndFitTheColumn(t *testing.T) {
	// A stable seed keeps each article on the same photograph across reseeds,
	// so a developer does not see the site reshuffle every time they run it.
	first := photoURL("phnom-penh-metro-feasibility-study", 1200, 675)
	second := photoURL("phnom-penh-metro-feasibility-study", 1200, 675)
	if first != second {
		t.Errorf("photoURL is not deterministic: %q vs %q", first, second)
	}
	if photoURL("other-slug", 1200, 675) == first {
		t.Error("different slugs produced the same photo URL")
	}

	for _, article := range demoArticleSeeds {
		if got := photoURL(article.Slug, 1200, 675); len(got) > maxImageURLLen {
			t.Errorf("photoURL(%q) is %d bytes, column holds %d", article.Slug, len(got), maxImageURLLen)
		}
	}
}

func TestSeedImageSourceSwitch(t *testing.T) {
	photos := &seeder{cfg: &config.Config{SeedImageSource: "photo"}}
	if got := photos.articleImage("a-slug", "X", "#000"); !strings.HasPrefix(got, "https://picsum.photos/") {
		t.Errorf("photo mode returned %q, want a photo URL", got)
	}

	// Offline mode must make no outbound request, so every image has to be
	// self-contained.
	offline := &seeder{cfg: &config.Config{SeedImageSource: "svg"}}
	if got := offline.articleImage("a-slug", "X", "#000"); !strings.HasPrefix(got, "data:image/svg+xml") {
		t.Errorf("svg mode returned %q, want an inline SVG", got)
	}
	// Case should not matter for an operator setting an env var.
	upper := &seeder{cfg: &config.Config{SeedImageSource: "SVG"}}
	if got := upper.articleImage("a-slug", "X", "#000"); !strings.HasPrefix(got, "data:image/svg+xml") {
		t.Errorf("SEED_IMAGE_SOURCE=SVG returned %q, want an inline SVG", got)
	}
	// An unset value falls back to photos rather than failing.
	unset := &seeder{cfg: &config.Config{}}
	if got := unset.articleImage("a-slug", "X", "#000"); !strings.HasPrefix(got, "https://picsum.photos/") {
		t.Errorf("unset source returned %q, want the photo default", got)
	}
}

// Author bylines must never use a photograph: attaching a real face to an
// invented journalist fabricates a person.
func TestAuthorAvatarsAreNeverPhotographs(t *testing.T) {
	for _, author := range demoAuthorSeeds {
		got := avatarImage(string([]rune(author.NameEn)[:1]), "#1E3A8A")
		if !strings.HasPrefix(got, "data:image/svg+xml") {
			t.Errorf("author %q has a non-SVG avatar: %q", author.Slug, got[:40])
		}
	}
}

// Every demo article must point at a category the seeder actually installs,
// or the seed aborts partway through.
func TestDemoArticlesReferenceRealCategories(t *testing.T) {
	known := make(map[string]bool, len(categorySeeds))
	for _, c := range categorySeeds {
		known[c.Slug] = true
	}
	authors := make(map[string]bool, len(demoAuthorSeeds))
	for _, a := range demoAuthorSeeds {
		authors[a.Slug] = true
	}

	slugs := make(map[string]bool, len(demoArticleSeeds))
	for _, article := range demoArticleSeeds {
		if !known[article.Category] {
			t.Errorf("demo article %q references unknown category %q", article.Slug, article.Category)
		}
		if article.AuthorSlug != "" && !authors[article.AuthorSlug] {
			t.Errorf("demo article %q references unknown author %q", article.Slug, article.AuthorSlug)
		}
		if slugs[article.Slug] {
			t.Errorf("duplicate demo article slug %q", article.Slug)
		}
		slugs[article.Slug] = true
	}
}

// Sponsored demo content must carry a sponsor name, or publishing it would be
// refused by the same validation the editor sees (§42).
func TestSponsoredDemoContentIsLabelled(t *testing.T) {
	for _, article := range demoArticleSeeds {
		if article.ContentType != "" && article.ContentType != "editorial" && article.Sponsor == "" {
			t.Errorf("demo article %q is %s but names no sponsor", article.Slug, article.ContentType)
		}
	}
}
