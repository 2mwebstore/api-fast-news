package models

import "time"

// Comment is a reader comment held for moderation. Note §95 excludes emoji
// reactions; comments remain, moderated.
type Comment struct {
	Base
	ArticleID uint   `gorm:"index;not null" json:"articleId"`
	ParentID  *uint  `gorm:"index" json:"parentId"`
	Name      string `gorm:"size:128;not null" json:"name"`
	Body      string `gorm:"type:text;not null" json:"body"`
	Status    string `gorm:"size:16;index;not null;default:pending" json:"status"` // pending|approved|rejected|spam
	// IPHash is a salted hash, not a raw address — enough to rate-limit a
	// repeat abuser without storing the reader's IP (§43).
	IPHash        string     `gorm:"size:64;index" json:"-"`
	ModeratedByID *uint      `gorm:"index" json:"moderatedById"`
	ModeratedAt   *time.Time `json:"moderatedAt"`
}

type CommentReport struct {
	Base
	CommentID uint   `gorm:"index;not null" json:"commentId"`
	Reason    string `gorm:"size:64;not null" json:"reason"`
	Detail    string `gorm:"type:text" json:"detail"`
	Status    string `gorm:"size:16;index;not null;default:open" json:"status"` // open|actioned|dismissed
	IPHash    string `gorm:"size:64;index" json:"-"`
}

// NewsTip is a citizen submission (§34). Nothing here is ever auto-published:
// Status starts at pending and only a human can move it to published.
type NewsTip struct {
	Base
	Name        string    `gorm:"size:128" json:"name"`
	Contact     string    `gorm:"size:191" json:"contact"` // email or phone, optional
	Location    string    `gorm:"size:191" json:"location"`
	Description string    `gorm:"type:text;not null" json:"description"`
	PhotoURLs   StringSlice `gorm:"type:json" json:"photoUrls,omitempty"`
	VideoURLs   StringSlice `gorm:"type:json" json:"videoUrls,omitempty"`
	Status      TipStatus `gorm:"size:16;index;not null;default:pending" json:"status"`
	ReviewNote  string    `gorm:"type:text" json:"reviewNote"`
	ReviewedByID *uint    `gorm:"index" json:"reviewedById"`
	ReviewedAt  *time.Time `json:"reviewedAt"`
	ArticleID   *uint     `gorm:"index" json:"articleId"` // set if a story came from this tip
	IPHash      string    `gorm:"size:64;index" json:"-"`
}

// TelegramPost records one delivery attempt to the channel (§29, §70).
type TelegramPost struct {
	Base
	ArticleID *uint  `gorm:"index" json:"articleId"`
	VideoID   *uint  `gorm:"index" json:"videoId"`
	Kind      string `gorm:"size:16;index;not null;default:normal" json:"kind"` // breaking|normal|sports|video
	Text      string `gorm:"type:text;not null" json:"text"`
	ImageURL  string `gorm:"size:512" json:"imageUrl"`

	Status       string     `gorm:"size:16;index;not null;default:pending" json:"status"` // pending|sent|failed
	MessageID    int64      `gorm:"default:0" json:"messageId"`
	ErrorMessage string     `gorm:"size:512" json:"errorMessage"`
	Attempts     int        `gorm:"default:0" json:"attempts"`
	SentAt       *time.Time `gorm:"index" json:"sentAt"`
	TriggeredByID *uint     `gorm:"index" json:"triggeredById"`
}

// PushSubscription is a Web Push endpoint plus the topics the reader chose.
// Topics are opt-in per category so CFN is not pushing every article (§30).
type PushSubscription struct {
	Base
	Endpoint  string      `gorm:"size:512;uniqueIndex;not null" json:"endpoint"`
	P256dh    string      `gorm:"size:255;not null" json:"-"`
	Auth      string      `gorm:"size:255;not null" json:"-"`
	Topics    StringSlice `gorm:"type:json" json:"topics"`
	UserAgent string      `gorm:"size:255" json:"userAgent"`
	Lang      string      `gorm:"size:8;default:km" json:"lang"`
	IsActive  bool        `gorm:"default:true;index" json:"isActive"`
	LastSentAt *time.Time `json:"lastSentAt"`
	FailureCount int       `gorm:"default:0" json:"failureCount"`
}

// Push topic keys (§30).
const (
	TopicBreaking      = "breaking"
	TopicCambodia      = "cambodia"
	TopicSports        = "sports"
	TopicKunKhmer      = "kun-khmer"
	TopicBusiness      = "business"
	TopicTechnology    = "technology"
	TopicEntertainment = "entertainment"
)
