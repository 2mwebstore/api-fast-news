package controllers

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// MetaController serves the option lists the UI needs to build its forms and
// filters.
//
// These used to be duplicated as hard-coded arrays in the frontend, which
// meant adding a content type or a status silently produced a dropdown that
// disagreed with what the API would accept. Deriving them from the same Go
// constants the validation uses keeps the two in step.
type MetaController struct {
	db       *gorm.DB
	articles *services.ArticleService
}

func NewMetaController(db *gorm.DB, articles *services.ArticleService) *MetaController {
	return &MetaController{db: db, articles: articles}
}

// Option is one selectable value with labels in both languages.
type Option struct {
	Value  string `json:"value"`
	LabelKh string `json:"labelKh"`
	LabelEn string `json:"labelEn"`
	// Hint carries a short explanation where the choice has consequences.
	Hint string `json:"hint,omitempty"`
}

// ContentTypeOption adds the disclosure rule, so the form knows when to
// require a sponsor name without hard-coding the list of paid types (§42).
type ContentTypeOption struct {
	Option
	RequiresDisclosure bool `json:"requiresDisclosure"`
}

// StatusOption carries whether a status is publicly visible and which
// transitions are legal from it (§16).
type StatusOption struct {
	Option
	IsPublic bool     `json:"isPublic"`
	CanMoveTo []string `json:"canMoveTo"`
}

// Meta handles GET /api/meta.
func (ctl *MetaController) Meta(c *gin.Context) {
	// Reference data changes only when the code does, so it can be cached
	// hard. The admin refetches on load anyway.
	c.Header("Cache-Control", "public, max-age=600")

	httpx.OK(c, gin.H{
		"contentTypes":         contentTypeOptions(),
		"articleStatuses":      articleStatusOptions(),
		"videoStatuses":        videoStatusOptions(),
		"sourceTypes":          sourceTypeOptions(),
		"verificationStatuses": verificationOptions(),
		"trafficLevels":        trafficLevelOptions(),
		"pushTopics":           pushTopicOptions(),
		"adPositions":          ctl.adPositionOptions(c),
		"roles":                ctl.roleOptions(c),
	})
}

func contentTypeOptions() []ContentTypeOption {
	defs := []struct {
		value   models.ContentType
		kh, en, hint string
	}{
		{models.ContentEditorial, "វិចារណកថា", "Editorial", "Independent reporting by the newsroom."},
		{models.ContentSponsored, "ខ្លឹមសារឧបត្ថម្ភ", "Sponsored", "Paid for by a sponsor. Requires a sponsor name and is labelled to readers."},
		{models.ContentAdvertise, "ពាណិជ្ជកម្ម", "Advertisement", "Advertising copy. Requires a sponsor name and is labelled."},
		{models.ContentPaid, "ខ្លឹមសារបង់ប្រាក់", "Paid content", "Paid placement. Requires a sponsor name and is labelled."},
	}

	out := make([]ContentTypeOption, 0, len(defs))
	for _, d := range defs {
		out = append(out, ContentTypeOption{
			Option:             Option{Value: string(d.value), LabelKh: d.kh, LabelEn: d.en, Hint: d.hint},
			RequiresDisclosure: d.value.RequiresDisclosure(),
		})
	}
	return out
}

func articleStatusOptions() []StatusOption {
	defs := []struct {
		value  models.ArticleStatus
		kh, en string
	}{
		{models.StatusDraft, "សេចក្តីព្រាង", "Draft"},
		{models.StatusReview, "រង់ចាំពិនិត្យ", "In review"},
		{models.StatusApproved, "អនុម័ត", "Approved"},
		{models.StatusScheduled, "កំណត់ពេល", "Scheduled"},
		{models.StatusPublished, "ផ្សាយ", "Published"},
		{models.StatusRejected, "បដិសេធ", "Rejected"},
		{models.StatusArchived, "បណ្ណសារ", "Archived"},
	}

	out := make([]StatusOption, 0, len(defs))
	for _, d := range defs {
		// The allowed moves come from the same table the API enforces, so a
		// workflow change updates the UI automatically.
		var moves []string
		for _, candidate := range defs {
			if candidate.value != d.value && services.CanTransition(d.value, candidate.value) {
				moves = append(moves, string(candidate.value))
			}
		}
		out = append(out, StatusOption{
			Option:    Option{Value: string(d.value), LabelKh: d.kh, LabelEn: d.en},
			IsPublic:  d.value.IsPublic(),
			CanMoveTo: moves,
		})
	}
	return out
}

// videoStatusOptions is the narrower set video actually uses: a video has no
// review workflow, so offering one would be a dropdown of dead ends.
func videoStatusOptions() []Option {
	return []Option{
		{Value: string(models.StatusDraft), LabelKh: "សេចក្តីព្រាង", LabelEn: "Draft"},
		{Value: string(models.StatusPublished), LabelKh: "ផ្សាយ", LabelEn: "Published"},
		{Value: string(models.StatusArchived), LabelKh: "បណ្ណសារ", LabelEn: "Archived"},
	}
}

func sourceTypeOptions() []Option {
	return []Option{
		{Value: string(models.SourceOfficialStatement), LabelKh: "សេចក្តីថ្លែងការណ៍ផ្លូវការ", LabelEn: "Official statement"},
		{Value: string(models.SourcePressRelease), LabelKh: "សេចក្តីប្រកាសព័ត៌មាន", LabelEn: "Press release"},
		{Value: string(models.SourceInterview), LabelKh: "បទសម្ភាសន៍", LabelEn: "Interview"},
		{Value: string(models.SourceReporter), LabelKh: "អ្នកយកព័ត៌មាន", LabelEn: "Reporter"},
		{Value: string(models.SourceOfficialWebsite), LabelKh: "គេហទំព័រផ្លូវការ", LabelEn: "Official website"},
		{Value: string(models.SourceSocialMedia), LabelKh: "បណ្តាញសង្គម", LabelEn: "Social media",
			Hint: "Unverified by default — social posts must be checked before they count as fact."},
		{Value: string(models.SourceOther), LabelKh: "ផ្សេងៗ", LabelEn: "Other"},
	}
}

func verificationOptions() []Option {
	return []Option{
		{Value: string(models.VerifyUnverified), LabelKh: "មិនទាន់ផ្ទៀងផ្ទាត់", LabelEn: "Unverified"},
		{Value: string(models.VerifyPending), LabelKh: "កំពុងផ្ទៀងផ្ទាត់", LabelEn: "Checking"},
		{Value: string(models.VerifyVerified), LabelKh: "ផ្ទៀងផ្ទាត់រួច", LabelEn: "Verified"},
		{Value: string(models.VerifyDisputed), LabelKh: "មានជម្លោះ", LabelEn: "Disputed"},
	}
}

func trafficLevelOptions() []Option {
	return []Option{
		{Value: string(models.TrafficNormal), LabelKh: "ធម្មតា", LabelEn: "Normal"},
		{Value: string(models.TrafficModerate), LabelKh: "មធ្យម", LabelEn: "Moderate"},
		{Value: string(models.TrafficHeavy), LabelKh: "កកស្ទះ", LabelEn: "Heavy"},
	}
}

func pushTopicOptions() []Option {
	return []Option{
		{Value: models.TopicBreaking, LabelKh: "ព័ត៌មានបន្ទាន់", LabelEn: "Breaking news"},
		{Value: models.TopicCambodia, LabelKh: "កម្ពុជា", LabelEn: "Cambodia"},
		{Value: models.TopicSports, LabelKh: "កីឡា", LabelEn: "Sports"},
		{Value: models.TopicKunKhmer, LabelKh: "គុនខ្មែរ", LabelEn: "Kun Khmer"},
		{Value: models.TopicBusiness, LabelKh: "សេដ្ឋកិច្ច", LabelEn: "Business"},
		{Value: models.TopicTechnology, LabelKh: "បច្ចេកវិទ្យា", LabelEn: "Technology"},
		{Value: models.TopicEntertainment, LabelKh: "កម្សាន្ត", LabelEn: "Entertainment"},
	}
}

// adPositionOptions reads the slots from the database, so a slot added by an
// operator appears in the creative form without a code change.
func (ctl *MetaController) adPositionOptions(c *gin.Context) []Option {
	var slots []models.AdvertisementSlot
	if err := ctl.db.WithContext(c.Request.Context()).
		Where("is_enabled = ?", true).Order("position ASC").Find(&slots).Error; err != nil {
		return []Option{}
	}

	out := make([]Option, 0, len(slots))
	for _, slot := range slots {
		out = append(out, Option{Value: slot.Position, LabelKh: slot.Label, LabelEn: slot.Label})
	}
	return out
}

func (ctl *MetaController) roleOptions(c *gin.Context) []Option {
	var roles []models.Role
	if err := ctl.db.WithContext(c.Request.Context()).Order("id ASC").Find(&roles).Error; err != nil {
		return []Option{}
	}

	out := make([]Option, 0, len(roles))
	for _, role := range roles {
		out = append(out, Option{Value: role.Slug, LabelKh: role.Name, LabelEn: role.Name})
	}
	return out
}
