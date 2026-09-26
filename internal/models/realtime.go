package models

import "time"

// BreakingNews is the banner/ticker record driving the red bar and /live (§7, §8).
// It is a separate table from Article so a breaking alert can be raised, edited
// and retired without touching the story's editorial history.
type BreakingNews struct {
	Base
	ArticleID *uint    `gorm:"index" json:"articleId"`
	Article   *Article `gorm:"foreignKey:ArticleID" json:"article,omitempty"`

	HeadlineKh string `gorm:"size:512;not null" json:"headlineKh"`
	HeadlineEn string `gorm:"size:512" json:"headlineEn"`
	URL        string `gorm:"size:512" json:"url"`

	// Severity drives the label: breaking | urgent | alert | live.
	Severity string `gorm:"size:16;index;not null;default:breaking" json:"severity"`

	StartedAt time.Time  `gorm:"index;not null" json:"startedAt"`
	EndedAt   *time.Time `gorm:"index" json:"endedAt"`
	Priority  int        `gorm:"default:0;index" json:"priority"`

	CreatedByID *uint `gorm:"index" json:"createdById"`
}

// IsActive reports whether this alert should be on screen right now.
func (b *BreakingNews) IsActive(now time.Time) bool {
	if b.StartedAt.After(now) {
		return false
	}
	return b.EndedAt == nil || b.EndedAt.After(now)
}

// TrendingNews is a materialised trending slot (§44). Scores are recomputed on
// a schedule rather than per request so the homepage stays cacheable.
type TrendingNews struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ArticleID   uint      `gorm:"uniqueIndex;not null" json:"articleId"`
	Article     *Article  `gorm:"foreignKey:ArticleID" json:"article,omitempty"`
	Score       float64   `gorm:"index;not null" json:"score"`
	Position    int       `gorm:"index;not null" json:"position"`
	// Column name is time_window, not window: "window" is a reserved word in
	// MySQL 8 and breaks any raw SQL that references it unquoted.
	Window      string    `gorm:"column:time_window;size:16;index;not null;default:24h" json:"window"`
	ComputedAt  time.Time `gorm:"index;not null" json:"computedAt"`
}

// TrafficStatus is the manually-curated road condition board (§71).
// CFN does not have a live traffic feed, so every row records who set it and
// when — the UI shows that attribution rather than implying live data.
type TrafficStatus struct {
	Base
	RouteKh     string       `gorm:"size:191;not null" json:"routeKh"`
	RouteEn     string       `gorm:"size:191" json:"routeEn"`
	Level       TrafficLevel `gorm:"size:16;index;not null;default:normal" json:"level"`
	NoteKh      string       `gorm:"size:512" json:"noteKh"`
	Position    int          `gorm:"default:0;index" json:"position"`
	IsActive    bool         `gorm:"default:true;index" json:"isActive"`

	// Provenance. SourceLabel names where the assessment came from
	// ("CFN newsroom", "Traffic Police"); ObservedAt is when, not "now".
	SourceLabel string    `gorm:"size:128;not null;default:CFN newsroom" json:"sourceLabel"`
	ObservedAt  time.Time `gorm:"index;not null" json:"observedAt"`
	UpdatedByID *uint     `gorm:"index" json:"updatedById"`
}
