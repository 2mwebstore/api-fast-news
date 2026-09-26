package models

import "time"

// Page is an editable standalone page: the policies, About and Contact (§93).
//
// These were Vue files, which meant a lawyer's wording change needed a deploy.
// They are rows now, edited in the admin, and the public route renders whichever
// language the reader has and the newsroom has written.
type Page struct {
	Base

	// Slug is the public URL: /privacy, /terms, /about. Changing it moves an
	// indexed address, so the controller leaves a redirect behind.
	Slug string `gorm:"size:191;uniqueIndex;not null" json:"slug"`

	TitleKh string `gorm:"size:255;not null" json:"titleKh"`
	TitleEn string `gorm:"size:255" json:"titleEn"`

	// Bodies are sanitised HTML. BodyEn is optional: a policy exists in Khmer
	// first, and an English page appears only once somebody writes one.
	BodyKh string `gorm:"type:longtext" json:"bodyKh"`
	BodyEn string `gorm:"type:longtext" json:"bodyEn"`

	MetaDescKh string `gorm:"size:512" json:"metaDescKh"`
	MetaDescEn string `gorm:"size:512" json:"metaDescEn"`

	// No `default:` on these. GORM treats a Go false as "unset" when a column
	// has a default and writes the default instead, so an explicit
	// IsPublished: false would silently become true. Callers set both.
	IsPublished bool `gorm:"index" json:"isPublished"`
	// ShowInFooter keeps a page reachable without listing it, for something
	// linked only from a form or an email.
	ShowInFooter bool `gorm:"index" json:"showInFooter"`

	Position int `gorm:"default:0;index" json:"position"`

	// IsSystem marks the pages a news site is expected to have. They can be
	// edited and unpublished but never deleted: a missing privacy policy is a
	// compliance problem, and it should not be one careless click away.
	IsSystem bool `gorm:"index" json:"isSystem"`

	UpdatedByID *uint      `gorm:"index" json:"updatedById"`
	PublishedAt *time.Time `json:"publishedAt"`
}
