package models

import "time"

// Media is one uploaded asset in the R2-backed library (§69).
type Media struct {
	Base
	// Column is object_key, not key: KEY is a reserved word in MySQL, so any
	// raw SQL naming it unquoted would be a syntax error.
	Key        string `gorm:"column:object_key;size:512;uniqueIndex;not null" json:"key"`
	URL        string `gorm:"size:512;not null" json:"url"`             // public CDN URL
	Filename   string `gorm:"size:255;not null" json:"filename"`
	MimeType   string `gorm:"size:128;index;not null" json:"mimeType"`
	SizeBytes  int64  `gorm:"not null" json:"sizeBytes"`
	Width      int    `gorm:"default:0" json:"width"`
	Height     int    `gorm:"default:0" json:"height"`
	AltKh      string `gorm:"size:512" json:"altKh"`
	AltEn      string `gorm:"size:512" json:"altEn"`
	Caption    string `gorm:"size:512" json:"caption"`
	Credit     string `gorm:"size:191" json:"credit"`
	Folder     string `gorm:"size:64;index;default:article" json:"folder"` // article|author|ad|video|tip|site
	IsAIGenerated bool `gorm:"default:false;index" json:"isAiGenerated"`
	// AIPrompt records which library prompt produced an AI image (§25), so an
	// illustrative image can always be traced back.
	AIPrompt      string `gorm:"type:text" json:"aiPrompt"`
	UploadedByID  *uint  `gorm:"index" json:"uploadedById"`
	UploadedBy    *User  `gorm:"foreignKey:UploadedByID" json:"uploadedBy,omitempty"`
}

// Video backs /video and /video/[slug] (§26).
type Video struct {
	Base
	Slug      string `gorm:"size:191;uniqueIndex;not null" json:"slug"`
	TitleKh   string `gorm:"size:512;not null" json:"titleKh"`
	TitleEn   string `gorm:"size:512" json:"titleEn"`
	DescKh    string `gorm:"type:text" json:"descKh"`
	DescEn    string `gorm:"type:text" json:"descEn"`

	// YouTubeID is the canonical source for editorial video. Hosting video is
	// expensive and slow to ship; embedding lets the newsroom publish the same
	// day. The column stores the bare id, never a full URL, so the embed URL
	// is built by us rather than by whatever an editor pasted.
	// Column is pinned: GORM's naming strategy turns YouTubeID into
	// "you_tube_id", which nobody would guess when writing SQL by hand.
	YouTubeID string `gorm:"column:youtube_id;size:32;index" json:"youtubeId"`

	// SourceURL and HLSURL remain for self-hosted files. They are optional and
	// only used when YouTubeID is empty.
	SourceURL    string `gorm:"size:512" json:"sourceUrl"`
	HLSURL       string `gorm:"size:512" json:"hlsUrl"`
	ThumbnailURL string `gorm:"size:512" json:"thumbnailUrl"`
	ThumbnailAlt string `gorm:"size:512" json:"thumbnailAlt"`

	DurationSec int  `gorm:"default:0" json:"durationSec"`
	Width       int  `gorm:"default:0" json:"width"`
	Height      int  `gorm:"default:0" json:"height"`

	Status      ArticleStatus `gorm:"size:16;index;not null;default:draft" json:"status"`
	CategoryID  *uint         `gorm:"index" json:"categoryId"`
	Category    *Category     `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	ArticleID   *uint         `gorm:"index" json:"articleId"` // optional companion story
	PublishedAt *time.Time    `gorm:"index" json:"publishedAt"`

	ViewCount int64 `gorm:"default:0;index" json:"viewCount"`

	// SEO overrides for /video/[slug]. Optional: without a row the page falls
	// back to the title and description, which is right for most videos.
	SEO *SEOMetadata `gorm:"foreignKey:VideoID" json:"seo,omitempty"`

	// Live-stream fields (§28). IsLive with an HLSURL renders /live-video.
	IsLive      bool       `gorm:"index;default:false" json:"isLive"`
	LiveStartAt *time.Time `json:"liveStartAt"`
	LiveEndAt   *time.Time `json:"liveEndAt"`
}
