package controllers

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// SiteController serves the editable chrome of the public site — the footer
// tagline, social profiles and contact details (§91).
//
// These were hardcoded in the footer component, which meant adding a Facebook
// page needed a deploy. They live in the settings table now, so an
// administrator can change them, and they are read through one cached public
// endpoint rather than being baked into the bundle.
type SiteController struct {
	settings *services.SettingsService
	audit    *services.AuditService
}

func NewSiteController(settings *services.SettingsService, audit *services.AuditService) *SiteController {
	return &SiteController{settings: settings, audit: audit}
}

// siteKeys are the settings this controller owns, with their defaults. Keeping
// the defaults here rather than in the footer means an unconfigured install
// still renders correct text.
var siteKeys = []struct{ key, fallback string }{
	{"site.tagline_kh", "ព័ត៌មានលឿនរហ័សកម្ពុជា — ព័ត៌មានទាន់ហេតុការណ៍ ត្រឹមត្រូវ និងអាចទុកចិត្តបាន។"},
	{"site.tagline_en", "Cambodia Fast News — timely, accurate and trustworthy reporting."},
	{"site.contact_email", ""},
	{"site.contact_phone", ""},
	{"site.address_kh", ""},
	{"site.address_en", ""},
	// Social profiles. Empty means "not listed": an unset profile must not
	// render as a dead link, and these also feed Organization sameAs (§47), so
	// claiming a profile we do not own would be a false statement about identity.
	{"site.facebook_url", ""},
	{"site.youtube_url", ""},
	{"site.telegram_url", ""},
	{"site.tiktok_url", ""},
	{"site.x_url", ""},
}

type socialLink struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Get handles GET /api/site.
func (ctl *SiteController) Get(c *gin.Context) {
	ctx := c.Request.Context()
	value := func(key string) string {
		for _, k := range siteKeys {
			if k.key == key {
				return strings.TrimSpace(ctl.settings.Get(ctx, key, k.fallback))
			}
		}
		return ""
	}

	// Only profiles with a URL are returned, so the footer never has to decide
	// whether a link is real.
	social := make([]socialLink, 0, 5)
	for _, s := range []struct{ key, label string }{
		{"site.facebook_url", "Facebook"},
		{"site.youtube_url", "YouTube"},
		{"site.telegram_url", "Telegram"},
		{"site.tiktok_url", "TikTok"},
		{"site.x_url", "X"},
	} {
		if url := value(s.key); url != "" {
			social = append(social, socialLink{
				Key: strings.TrimSuffix(strings.TrimPrefix(s.key, "site."), "_url"),
				Label: s.label, URL: url,
			})
		}
	}

	// Public and cacheable: it changes when an administrator edits it, not per
	// reader, and every page renders the footer.
	c.Header("Cache-Control", "public, max-age=300")
	httpx.OK(c, gin.H{
		"taglineKh":    value("site.tagline_kh"),
		"taglineEn":    value("site.tagline_en"),
		"contactEmail": value("site.contact_email"),
		"contactPhone": value("site.contact_phone"),
		"addressKh":    value("site.address_kh"),
		"addressEn":    value("site.address_en"),
		"social":       social,
	})
}

// AdminGet handles GET /api/admin/settings/site — the raw editable values.
func (ctl *SiteController) AdminGet(c *gin.Context) {
	ctx := c.Request.Context()
	out := gin.H{}
	for _, k := range siteKeys {
		out[k.key] = ctl.settings.Get(ctx, k.key, k.fallback)
	}
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, out)
}

// AdminSave handles PUT /api/admin/settings/site.
func (ctl *SiteController) AdminSave(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid request")
		return
	}

	ctx := c.Request.Context()
	userID := middleware.CurrentUserID(c)
	known := make(map[string]bool, len(siteKeys))
	for _, k := range siteKeys {
		known[k.key] = true
	}

	saved := 0
	for key, value := range req {
		// Only the keys this controller owns. Without this the endpoint would
		// write any setting, including the Telegram token's row.
		if !known[key] {
			continue
		}
		value = strings.TrimSpace(value)
		// A URL field must look like one, or a typo becomes a broken public link.
		if strings.HasSuffix(key, "_url") && value != "" && !strings.HasPrefix(value, "https://") {
			httpx.BadRequest(c, httpx.CodeValidation,
				"Social profile links must start with https://")
			return
		}
		if err := ctl.settings.Set(ctx, key, value, userID); err != nil {
			httpx.Internal(c, "Could not save the site details")
			return
		}
		saved++
	}

	ctl.audit.Record(ctx, userID, "settings.site.save", "settings", nil,
		"site details updated", nil)
	httpx.OK(c, gin.H{"saved": saved})
}
