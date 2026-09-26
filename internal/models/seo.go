package models

import "time"

// SEOMetadata holds the per-article editorial SEO overrides (§58). Every field
// is optional: when empty the renderer falls back to a derived default, so an
// article is never published without metadata.
type SEOMetadata struct {
	Base
	ArticleID *uint `gorm:"uniqueIndex" json:"articleId"`
	// CategoryID lets the same table serve category pages.
	CategoryID *uint `gorm:"uniqueIndex" json:"categoryId"`
	// VideoID does the same for video pages. One row belongs to exactly one of
	// the three; the unique indexes are on nullable columns, so MySQL allows
	// many rows with NULL here.
	VideoID *uint `gorm:"uniqueIndex" json:"videoId"`

	SEOTitle       string      `gorm:"size:255" json:"seoTitle"`
	SEODescription string      `gorm:"size:512" json:"seoDescription"`
	SEOKeywords    StringSlice `gorm:"type:json" json:"seoKeywords,omitempty"`
	CanonicalURL   string      `gorm:"size:512" json:"canonicalUrl"`

	OGTitle       string `gorm:"size:255" json:"ogTitle"`
	OGDescription string `gorm:"size:512" json:"ogDescription"`
	OGImage       string `gorm:"size:512" json:"ogImage"`

	TwitterTitle       string `gorm:"size:255" json:"twitterTitle"`
	TwitterDescription string `gorm:"size:512" json:"twitterDescription"`
	TwitterImage       string `gorm:"size:512" json:"twitterImage"`

	// Robots defaults to index,follow for articles. Search and filtered pages
	// are set to noindex,follow by the renderer, not stored here (§32).
	Robots string `gorm:"size:64;default:index,follow" json:"robots"`

	// AIGenerated marks metadata that came from the SEO assistant and still
	// needs a human pass (§23).
	AIGenerated bool       `gorm:"default:false" json:"aiGenerated"`
	ReviewedAt  *time.Time `json:"reviewedAt"`
	ReviewedByID *uint     `gorm:"index" json:"reviewedById"`
}

// Redirect is an admin-managed 301 (§61), used when a slug changes.
type Redirect struct {
	Base
	FromPath    string     `gorm:"size:512;uniqueIndex;not null" json:"fromPath"`
	ToPath      string     `gorm:"size:512;not null" json:"toPath"`
	StatusCode  int        `gorm:"default:301;not null" json:"statusCode"`
	IsActive    bool       `gorm:"default:true;index" json:"isActive"`
	HitCount    int64      `gorm:"default:0" json:"hitCount"`
	LastHitAt   *time.Time `json:"lastHitAt"`
	CreatedByID *uint      `gorm:"index" json:"createdById"`
	Note        string     `gorm:"size:255" json:"note"`
}
