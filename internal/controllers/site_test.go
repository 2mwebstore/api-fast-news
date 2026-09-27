package controllers

import (
	"strings"
	"testing"

	"github.com/cambodia-fast-news/backend/internal/models"
)

func TestValidateSiteValue(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		ok    bool
	}{
		{"uploaded logo", models.SettingSiteLogoURL, "https://media.example.com/site/logo.png", true},
		{"logo on this site", models.SettingSiteLogoURL, "/icons/icon-512.png", true},
		{"no logo", models.SettingSiteLogoURL, "", true},
		{"plain http logo", models.SettingSiteLogoURL, "http://example.com/logo.png", false},
		{"protocol-relative logo", models.SettingSiteLogoURL, "//evil.example/logo.png", false},
		{"script logo", models.SettingSiteLogoURL, "javascript:alert(1)", false},

		{"khmer name", models.SettingSiteNameKh, "ព័ត៌មានលឿនរហ័សកម្ពុជា", true},
		{"empty name falls back", models.SettingSiteName, "", true},
		// Counted in characters, not bytes: 80 Khmer characters is ~240 bytes.
		{"80 khmer characters", models.SettingSiteNameKh, strings.Repeat("ក", 80), true},
		{"name too long", models.SettingSiteName, strings.Repeat("a", 81), false},

		{"show name on", models.SettingSiteLogoShowName, "true", true},
		{"show name off", models.SettingSiteLogoShowName, "false", true},
		{"show name junk", models.SettingSiteLogoShowName, "yes", false},

		{"unrelated key", "site.tagline_en", "anything", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateSiteValue(tc.key, tc.value)
			if tc.ok && msg != "" {
				t.Fatalf("expected %q to be accepted, got %q", tc.value, msg)
			}
			if !tc.ok && msg == "" {
				t.Fatalf("expected %q to be rejected", tc.value)
			}
		})
	}
}
