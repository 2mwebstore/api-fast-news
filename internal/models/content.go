package models

import "time"

// Category is a news section (§13). Categories nest one level: a parent such
// as "Cambodia" can hold "Phnom Penh" as a subcategory.
type Category struct {
	Base
	Slug     string `gorm:"size:191;uniqueIndex;not null" json:"slug"`
	NameKh   string `gorm:"size:128;not null" json:"nameKh"`
	NameEn   string `gorm:"size:128;not null" json:"nameEn"`
	DescKh   string `gorm:"type:text" json:"descKh"`
	DescEn   string `gorm:"type:text" json:"descEn"`
	Icon     string `gorm:"size:32" json:"icon"`  // emoji or icon key used in section headers
	Color    string `gorm:"size:16" json:"color"` // hex accent for the section rule
	Position int    `gorm:"default:0;index" json:"position"`
	// No `default:true` on these booleans. GORM treats a Go false as "unset"
	// when a column has a default, and writes the default instead — so an
	// explicit InNav: false silently became true. Callers set both fields.
	InNav    bool `gorm:"index" json:"inNav"`
	IsActive bool `gorm:"index" json:"isActive"`

	ParentID *uint     `gorm:"index" json:"parentId"`
	Parent   *Category `gorm:"foreignKey:ParentID" json:"parent,omitempty"`

	SEOTitleKh string `gorm:"size:255" json:"seoTitleKh"`
	SEODescKh  string `gorm:"size:512" json:"seoDescKh"`
	SEOTitleEn string `gorm:"size:255" json:"seoTitleEn"`
	SEODescEn  string `gorm:"size:512" json:"seoDescEn"`

	ArticleCount int `gorm:"default:0" json:"articleCount"`
}

type Tag struct {
	Base
	Slug   string `gorm:"size:191;uniqueIndex;not null" json:"slug"`
	NameKh string `gorm:"size:128;not null" json:"nameKh"`
	NameEn string `gorm:"size:128" json:"nameEn"`
	UseCount int  `gorm:"default:0;index" json:"useCount"`
}

// Article is the central editorial record.
type Article struct {
	Base

	Slug      string `gorm:"size:191;uniqueIndex;not null" json:"slug"`
	TitleKh   string `gorm:"size:512;not null" json:"titleKh"`
	TitleEn   string `gorm:"size:512" json:"titleEn"`
	SummaryKh string `gorm:"type:text" json:"summaryKh"`
	SummaryEn string `gorm:"type:text" json:"summaryEn"`

	// ContentKh/En hold the rendered HTML produced by the editor (§15).
	// ContentJSON holds the editor's own block document, which is what the
	// editor reloads — HTML is a derived, sanitised artefact.
	ContentKh   string  `gorm:"type:longtext" json:"contentKh"`
	ContentEn   string  `gorm:"type:longtext" json:"contentEn"`
	ContentJSON JSONMap `gorm:"type:json" json:"contentJson,omitempty"`

	Status      ArticleStatus `gorm:"size:16;index;not null;default:draft" json:"status"`
	ContentType ContentType   `gorm:"size:16;index;not null;default:editorial" json:"contentType"`

	// Sponsor fields are only meaningful when ContentType requires disclosure.
	SponsorName string `gorm:"size:191" json:"sponsorName"`
	SponsorURL  string `gorm:"size:512" json:"sponsorUrl"`

	CategoryID uint      `gorm:"index;not null" json:"categoryId"`
	Category   *Category `gorm:"foreignKey:CategoryID" json:"category,omitempty"`

	AuthorID *uint   `gorm:"index" json:"authorId"`
	Author   *Author `gorm:"foreignKey:AuthorID" json:"author,omitempty"`

	CreatedByID *uint `gorm:"index" json:"createdById"`
	CreatedBy   *User `gorm:"foreignKey:CreatedByID" json:"createdBy,omitempty"`
	ApprovedByID *uint `gorm:"index" json:"approvedById"`
	ApprovedBy   *User `gorm:"foreignKey:ApprovedByID" json:"approvedBy,omitempty"`

	// Imagery. Width/height are stored so the renderer can reserve space and
	// avoid layout shift (§56).
	ImageURL     string `gorm:"size:512" json:"imageUrl"`
	ImageAltKh   string `gorm:"size:512" json:"imageAltKh"`
	ImageAltEn   string `gorm:"size:512" json:"imageAltEn"`
	ImageWidth   int    `gorm:"default:0" json:"imageWidth"`
	ImageHeight  int    `gorm:"default:0" json:"imageHeight"`
	ImageCaption string `gorm:"size:512" json:"imageCaption"`
	// ImageIsAIGenerated forces the illustrative-image disclosure (§25).
	ImageIsAIGenerated bool `gorm:"default:false" json:"imageIsAiGenerated"`

	// Breaking-news state (§7).
	IsBreaking        bool       `gorm:"index;default:false" json:"isBreaking"`
	BreakingStartedAt *time.Time `gorm:"index" json:"breakingStartedAt"`
	BreakingEndedAt   *time.Time `json:"breakingEndedAt"`

	IsFeatured bool `gorm:"index;default:false" json:"isFeatured"`
	IsPinned   bool `gorm:"index;default:false" json:"isPinned"`

	PublishedAt *time.Time `gorm:"index" json:"publishedAt"`
	ScheduledAt *time.Time `gorm:"index" json:"scheduledAt"`
	UpdatedContentAt *time.Time `json:"updatedContentAt"` // drives dateModified

	ReadingMinutes int `gorm:"default:0" json:"readingMinutes"`
	WordCount      int `gorm:"default:0" json:"wordCount"`

	// Counters are denormalised from Redis in batches (§76) — never written
	// synchronously on a page view.
	ViewCount       int64 `gorm:"default:0;index" json:"viewCount"`
	UniqueViewCount int64 `gorm:"default:0" json:"uniqueViewCount"`
	ShareCount      int64 `gorm:"default:0" json:"shareCount"`
	TrendingScore   float64 `gorm:"default:0;index" json:"trendingScore"`

	// AISummary is generated on demand and always rendered behind an explicit
	// "AI summary" label, never merged into the article body (§24).
	AISummary        StringSlice `gorm:"type:json" json:"aiSummary,omitempty"`
	AISummaryAt      *time.Time  `json:"aiSummaryAt"`
	AIAssisted       bool        `gorm:"default:false" json:"aiAssisted"` // a draft began as an AI draft
	AIReviewedByID   *uint       `json:"aiReviewedById"`

	Tags       []Tag       `gorm:"many2many:article_tags" json:"tags,omitempty"`
	Sources    []ArticleSource `gorm:"foreignKey:ArticleID" json:"sources,omitempty"`
	SEO        *SEOMetadata    `gorm:"foreignKey:ArticleID" json:"seo,omitempty"`
	Corrections []ArticleCorrection `gorm:"foreignKey:ArticleID" json:"corrections,omitempty"`
}

// TableName keeps the plural table name explicit.
func (Article) TableName() string { return "articles" }

// HasEnglish reports whether a real English version exists. hreflang
// alternates are only emitted when this is true (§54).
func (a *Article) HasEnglish() bool {
	return a.TitleEn != "" && a.ContentEn != ""
}

// IsLiveBreaking reports whether the article should currently render the red
// breaking treatment: flagged, started, and not yet ended.
func (a *Article) IsLiveBreaking(now time.Time) bool {
	if !a.IsBreaking || a.BreakingStartedAt == nil {
		return false
	}
	if a.BreakingStartedAt.After(now) {
		return false
	}
	return a.BreakingEndedAt == nil || a.BreakingEndedAt.After(now)
}

// ArticleTag is the explicit join table, declared so migrations own its indexes.
type ArticleTag struct {
	ArticleID uint `gorm:"primaryKey"`
	TagID     uint `gorm:"primaryKey;index"`
}

// ArticleCategory lets a story appear in secondary sections without changing
// its canonical category (which stays Article.CategoryID).
type ArticleCategory struct {
	ArticleID  uint `gorm:"primaryKey"`
	CategoryID uint `gorm:"primaryKey;index"`
}

// Source is a reusable provenance record — a ministry, an agency, a person.
type Source struct {
	Base
	NameKh string     `gorm:"size:191;not null" json:"nameKh"`
	NameEn string     `gorm:"size:191" json:"nameEn"`
	URL    string     `gorm:"size:512" json:"url"`
	Type   SourceType `gorm:"size:32;index;not null" json:"type"`
	Notes  string     `gorm:"type:text" json:"notes"`
}

// ArticleSource attaches provenance to one story (§19). This is an internal
// editorial record: it is not rendered to readers automatically.
type ArticleSource struct {
	Base
	ArticleID uint     `gorm:"index;not null" json:"articleId"`
	SourceID  *uint    `gorm:"index" json:"sourceId"`
	Source    *Source  `gorm:"foreignKey:SourceID" json:"source,omitempty"`

	NameKh      string             `gorm:"size:191;not null" json:"nameKh"`
	URL         string             `gorm:"size:512" json:"url"`
	Type        SourceType         `gorm:"size:32;index;not null" json:"type"`
	RetrievedAt *time.Time         `json:"retrievedAt"`
	ReporterID  *uint              `gorm:"index" json:"reporterId"`
	Verification VerificationStatus `gorm:"size:16;index;not null;default:unverified" json:"verification"`
	Notes       string             `gorm:"type:text" json:"notes"`
}

// ArticleRevision is an immutable snapshot taken before each save of a
// published article, so a correction can always be reconstructed (§20).
type ArticleRevision struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"index" json:"createdAt"`
	ArticleID uint      `gorm:"index;not null" json:"articleId"`
	Version   int       `gorm:"not null" json:"version"`

	TitleKh   string `gorm:"size:512" json:"titleKh"`
	SummaryKh string `gorm:"type:text" json:"summaryKh"`
	ContentKh string `gorm:"type:longtext" json:"contentKh"`
	Status    ArticleStatus `gorm:"size:16" json:"status"`

	EditorID   *uint  `gorm:"index" json:"editorId"`
	EditorName string `gorm:"size:128" json:"editorName"`
	Reason     string `gorm:"size:512" json:"reason"`
}

// ArticleCorrection is the reader-facing correction notice (§20). Unlike a
// revision, this is published on the article page.
type ArticleCorrection struct {
	Base
	ArticleID  uint      `gorm:"index;not null" json:"articleId"`
	NoteKh     string    `gorm:"type:text;not null" json:"noteKh"`
	NoteEn     string    `gorm:"type:text" json:"noteEn"`
	Reason     string    `gorm:"size:512" json:"reason"`
	CorrectedAt time.Time `gorm:"index;not null" json:"correctedAt"`
	EditorID   *uint     `gorm:"index" json:"editorId"`
	EditorName string    `gorm:"size:128" json:"editorName"`
	RevisionID *uint     `gorm:"index" json:"revisionId"`
}

// Assignment is an editor's commission to a journalist (§17).
type Assignment struct {
	Base
	TitleKh     string           `gorm:"size:512;not null" json:"titleKh"`
	Brief       string           `gorm:"type:text" json:"brief"`
	Status      AssignmentStatus `gorm:"size:16;index;not null;default:assigned" json:"status"`
	CategoryID  *uint            `gorm:"index" json:"categoryId"`
	AssignedToID uint            `gorm:"index;not null" json:"assignedToId"`
	AssignedTo  *User            `gorm:"foreignKey:AssignedToID" json:"assignedTo,omitempty"`
	AssignedByID uint            `gorm:"index;not null" json:"assignedById"`
	AssignedBy  *User            `gorm:"foreignKey:AssignedByID" json:"assignedBy,omitempty"`
	ArticleID   *uint            `gorm:"index" json:"articleId"`
	DueAt       *time.Time       `gorm:"index" json:"dueAt"`
	Priority    int              `gorm:"default:0" json:"priority"`
	RevisionNote string          `gorm:"type:text" json:"revisionNote"`
}
