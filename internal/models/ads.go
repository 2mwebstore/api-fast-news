package models

import "time"

// AdvertisementCampaign groups creatives under one advertiser and flight (§35).
type AdvertisementCampaign struct {
	Base
	Name         string    `gorm:"size:191;not null" json:"name"`
	Advertiser   string    `gorm:"size:191;not null" json:"advertiser"`
	ContactEmail string    `gorm:"size:191" json:"contactEmail"`
	Status       AdStatus  `gorm:"size:16;index;not null;default:draft" json:"status"`
	StartAt      time.Time `gorm:"index;not null" json:"startAt"`
	EndAt        time.Time `gorm:"index;not null" json:"endAt"`
	Notes        string    `gorm:"type:text" json:"notes"`

	// Advertiser-submitted campaigns stay unapproved until an ad manager signs
	// off (§41) — ApprovedByID nil means "never serve".
	SubmittedByID *uint      `gorm:"index" json:"submittedById"`
	ApprovedByID  *uint      `gorm:"index" json:"approvedById"`
	ApprovedAt    *time.Time `json:"approvedAt"`

	Advertisements []Advertisement `gorm:"foreignKey:CampaignID" json:"advertisements,omitempty"`
}

// AdvertisementSlot is a named position on the site (§36). Sizes live here so
// the frontend can reserve exact space before any creative loads (§38).
type AdvertisementSlot struct {
	Base
	Position     string `gorm:"size:64;uniqueIndex;not null" json:"position"` // e.g. HOME_TOP
	Label        string `gorm:"size:128;not null" json:"label"`
	DesktopWidth  int   `gorm:"default:0" json:"desktopWidth"`
	DesktopHeight int   `gorm:"default:0" json:"desktopHeight"`
	MobileWidth   int   `gorm:"default:0" json:"mobileWidth"`
	MobileHeight  int   `gorm:"default:0" json:"mobileHeight"`
	IsEnabled    bool   `gorm:"default:true;index" json:"isEnabled"`
	Description  string `gorm:"size:255" json:"description"`
}

// Ad slot position constants (§36).
const (
	SlotHomeTop        = "HOME_TOP"
	SlotHomeAfterHero  = "HOME_AFTER_HERO"
	SlotHomeSidebar    = "HOME_SIDEBAR"
	SlotHomeInFeed     = "HOME_IN_FEED"
	SlotArticleTop     = "ARTICLE_TOP"
	SlotArticleSidebar = "ARTICLE_SIDEBAR"
	SlotArticleMiddle  = "ARTICLE_MIDDLE"
	SlotArticleBottom  = "ARTICLE_BOTTOM"
	SlotCategoryTop    = "CATEGORY_TOP"
	SlotCategorySidebar = "CATEGORY_SIDEBAR"
	SlotMobileSticky   = "MOBILE_STICKY"
	SlotDesktopSticky  = "DESKTOP_STICKY"
)

// Advertisement is one creative bound to a slot.
type Advertisement struct {
	Base
	CampaignID uint                   `gorm:"index;not null" json:"campaignId"`
	Campaign   *AdvertisementCampaign `gorm:"foreignKey:CampaignID" json:"campaign,omitempty"`

	Name     string   `gorm:"size:191;not null" json:"name"`
	Position string   `gorm:"size:64;index;not null" json:"position"`
	Status   AdStatus `gorm:"size:16;index;not null;default:draft" json:"status"`

	// Separate creatives per form factor (§37). Dimensions are stored with the
	// creative so <AdSlot> can render an exact-size placeholder.
	DesktopImageURL string `gorm:"size:512" json:"desktopImageUrl"`
	DesktopWidth    int    `gorm:"default:0" json:"desktopWidth"`
	DesktopHeight   int    `gorm:"default:0" json:"desktopHeight"`
	MobileImageURL  string `gorm:"size:512" json:"mobileImageUrl"`
	MobileWidth     int    `gorm:"default:0" json:"mobileWidth"`
	MobileHeight    int    `gorm:"default:0" json:"mobileHeight"`

	// HTMLSnippet supports third-party ad tags. It is rendered in a sandboxed
	// iframe, never injected into the page DOM.
	HTMLSnippet string `gorm:"type:text" json:"htmlSnippet"`

	TargetURL string `gorm:"size:512" json:"targetUrl"`
	AltText   string `gorm:"size:255" json:"altText"`

	StartAt  time.Time `gorm:"index;not null" json:"startAt"`
	EndAt    time.Time `gorm:"index;not null" json:"endAt"`
	Priority int       `gorm:"default:0;index" json:"priority"`
	Weight   int       `gorm:"default:1" json:"weight"` // relative share when several compete

	// Targeting (§35). Empty means "no restriction".
	TargetDevice     string      `gorm:"size:16;index;default:all" json:"targetDevice"` // all|desktop|mobile
	TargetCategories StringSlice `gorm:"type:json" json:"targetCategories,omitempty"`

	ImpressionCount int64 `gorm:"default:0" json:"impressionCount"`
	ClickCount      int64 `gorm:"default:0" json:"clickCount"`
}

// IsServable reports whether this creative may be returned for a request now.
// Campaign approval is checked by the service layer, which has the campaign
// loaded; this covers the creative's own state.
func (a *Advertisement) IsServable(now time.Time) bool {
	if a.Status != AdActive {
		return false
	}
	return !now.Before(a.StartAt) && now.Before(a.EndAt)
}

// CTR returns clicks divided by impressions as a percentage, 0 when unserved.
func (a *Advertisement) CTR() float64 {
	if a.ImpressionCount == 0 {
		return 0
	}
	return float64(a.ClickCount) / float64(a.ImpressionCount) * 100
}

// AdvertisementImpression and AdvertisementClick are append-only event rows
// (§40). They are written from a buffered queue, not inline with the request.
type AdvertisementImpression struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"index" json:"createdAt"`
	AdID      uint      `gorm:"index;not null" json:"adId"`
	CampaignID uint     `gorm:"index;not null" json:"campaignId"`
	Position  string    `gorm:"size:64;index;not null" json:"position"`
	Device    string    `gorm:"size:16;index" json:"device"`
	Day       string    `gorm:"size:10;index;not null" json:"day"` // YYYY-MM-DD, for cheap grouping
	Count     int       `gorm:"default:1" json:"count"`
}

type AdvertisementClick struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"index" json:"createdAt"`
	AdID      uint      `gorm:"index;not null" json:"adId"`
	CampaignID uint     `gorm:"index;not null" json:"campaignId"`
	Position  string    `gorm:"size:64;index;not null" json:"position"`
	Device    string    `gorm:"size:16;index" json:"device"`
	Day       string    `gorm:"size:10;index;not null" json:"day"`
	Count     int       `gorm:"default:1" json:"count"`
}
