// Package models holds the GORM schema for the whole platform.
//
// Conventions used throughout:
//   - Khmer is the primary language; every public-facing text field has a
//     `*Kh` form. The matching `*En` field is optional and, when empty, means
//     no English version exists (which is what suppresses hreflang alternates).
//   - Soft deletes are used for editorial content so nothing is lost, and hard
//     deletes for high-volume analytics rows.
package models

import (
	"time"

	"gorm.io/gorm"
)

// Base is embedded by every editorially-owned table.
type Base struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// ArticleStatus is the editorial lifecycle from §16.
type ArticleStatus string

const (
	StatusDraft     ArticleStatus = "draft"
	StatusReview    ArticleStatus = "review"
	StatusApproved  ArticleStatus = "approved"
	StatusScheduled ArticleStatus = "scheduled"
	StatusPublished ArticleStatus = "published"
	StatusArchived  ArticleStatus = "archived"
	StatusRejected  ArticleStatus = "rejected"
)

// IsPublic reports whether an article in this status may be served to readers
// and included in sitemaps.
func (s ArticleStatus) IsPublic() bool { return s == StatusPublished }

// AssignmentStatus is the newsroom workflow state from §17.
type AssignmentStatus string

const (
	AssignAssigned  AssignmentStatus = "assigned"
	AssignWriting   AssignmentStatus = "writing"
	AssignReview    AssignmentStatus = "review"
	AssignRevision  AssignmentStatus = "revision"
	AssignApproved  AssignmentStatus = "approved"
	AssignScheduled AssignmentStatus = "scheduled"
	AssignPublished AssignmentStatus = "published"
)

// ContentType separates editorial reporting from paid placements (§42).
// This drives the on-page disclosure label, so it is never inferred.
type ContentType string

const (
	ContentEditorial ContentType = "editorial"
	ContentSponsored ContentType = "sponsored"
	ContentAdvertise ContentType = "advertisement"
	ContentPaid      ContentType = "paid"
)

// RequiresDisclosure reports whether the renderer must show a paid-content
// label. Anything that is not plain editorial must be labelled.
func (c ContentType) RequiresDisclosure() bool { return c != ContentEditorial }

// SourceType enumerates the provenance categories from §19.
type SourceType string

const (
	SourceOfficialStatement SourceType = "official_statement"
	SourcePressRelease      SourceType = "press_release"
	SourceInterview         SourceType = "interview"
	SourceReporter          SourceType = "reporter"
	SourceOfficialWebsite   SourceType = "official_website"
	SourceSocialMedia       SourceType = "social_media"
	SourceOther             SourceType = "other"
)

// VerificationStatus records how far a source has been checked. Social-media
// posts start unverified and must be promoted deliberately by a human (§19).
type VerificationStatus string

const (
	VerifyUnverified VerificationStatus = "unverified"
	VerifyPending    VerificationStatus = "pending"
	VerifyVerified   VerificationStatus = "verified"
	VerifyDisputed   VerificationStatus = "disputed"
)

// TipStatus is the moderation state of a citizen submission (§34).
type TipStatus string

const (
	TipPending   TipStatus = "pending"
	TipReviewing TipStatus = "reviewing"
	TipVerified  TipStatus = "verified"
	TipRejected  TipStatus = "rejected"
	TipPublished TipStatus = "published"
)

// AdStatus is the campaign/creative lifecycle from §35.
type AdStatus string

const (
	AdDraft     AdStatus = "draft"
	AdScheduled AdStatus = "scheduled"
	AdActive    AdStatus = "active"
	AdPaused    AdStatus = "paused"
	AdExpired   AdStatus = "expired"
)

// TrafficLevel is the manually-curated road condition indicator (§71).
type TrafficLevel string

const (
	TrafficNormal   TrafficLevel = "normal"
	TrafficModerate TrafficLevel = "moderate"
	TrafficHeavy    TrafficLevel = "heavy"
)

// AllModels returns every table in migration order. Used by AutoMigrate and by
// the schema test that guards against a model being added but never migrated.
func AllModels() []any {
	return []any{
		&Permission{}, &Role{}, &User{}, &Author{},
		&Category{}, &Tag{}, &Article{}, &ArticleTag{}, &ArticleCategory{},
		&Source{}, &ArticleSource{}, &ArticleRevision{}, &ArticleCorrection{},
		&Assignment{},
		&Media{}, &Video{},
		&BreakingNews{}, &TrendingNews{}, &TrafficStatus{},
		&AdvertisementCampaign{}, &AdvertisementSlot{}, &Advertisement{},
		&AdvertisementImpression{}, &AdvertisementClick{},
		&Page{},
		&ArticleView{}, &ArticleShare{}, &VideoShare{}, &PageView{},
		&Comment{}, &CommentReport{},
		&TelegramPost{}, &PushSubscription{},
		&SEOMetadata{}, &Redirect{}, &NewsTip{}, &AuditLog{}, &Setting{},
	}
}
