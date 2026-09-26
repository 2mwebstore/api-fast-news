package models

import "time"

// ArticleView is a per-day aggregate, not a per-request row (§76). The API
// increments Redis counters; a background flush folds them into these rows.
type ArticleView struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ArticleID   uint      `gorm:"uniqueIndex:idx_article_day;not null" json:"articleId"`
	Day         string    `gorm:"size:10;uniqueIndex:idx_article_day;index;not null" json:"day"`
	Views       int64     `gorm:"default:0" json:"views"`
	UniqueViews int64     `gorm:"default:0" json:"uniqueViews"`
	ReadSeconds int64     `gorm:"default:0" json:"readSeconds"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ArticleShare counts outbound shares per network (§43).
// VideoShare counts shares of a video page, per network per day. Same shape as
// ArticleShare rather than one polymorphic table: a shared table would need a
// nullable article_id and a nullable video_id, and the unique index that makes
// the daily upsert work cannot span nullable columns reliably.
type VideoShare struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	VideoID uint   `gorm:"uniqueIndex:idx_video_share_day;not null" json:"videoId"`
	Network string `gorm:"size:32;uniqueIndex:idx_video_share_day;not null" json:"network"`
	Day     string `gorm:"size:10;uniqueIndex:idx_video_share_day;index;not null" json:"day"`
	Count   int64  `gorm:"default:0" json:"count"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ArticleShare struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ArticleID uint      `gorm:"uniqueIndex:idx_share_day;not null" json:"articleId"`
	Network   string    `gorm:"size:32;uniqueIndex:idx_share_day;not null" json:"network"`
	Day       string    `gorm:"size:10;uniqueIndex:idx_share_day;index;not null" json:"day"`
	Count     int64     `gorm:"default:0" json:"count"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PageView is the site-wide daily roll-up behind the analytics dashboard.
// Only coarse dimensions are stored — no IP addresses, no per-visitor rows (§43).
type PageView struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Day       string    `gorm:"size:10;uniqueIndex:idx_pv;index;not null" json:"day"`
	Path      string    `gorm:"size:255;uniqueIndex:idx_pv;not null" json:"path"`
	Device    string    `gorm:"size:16;uniqueIndex:idx_pv;not null" json:"device"`
	Country   string    `gorm:"size:2;uniqueIndex:idx_pv;not null" json:"country"`
	Referrer  string    `gorm:"size:191;uniqueIndex:idx_pv;not null" json:"referrer"` // host only
	Views     int64     `gorm:"default:0" json:"views"`
	Visitors  int64     `gorm:"default:0" json:"visitors"`
	UpdatedAt time.Time `json:"updatedAt"`
}
