package services

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/models"
)

// AdService selects creatives and buffers impression/click events (§35–§40).
type AdService struct {
	db    *gorm.DB
	cache *cache.Cache
}

func NewAdService(db *gorm.DB, c *cache.Cache) *AdService { return &AdService{db: db, cache: c} }

// ServedAd is the payload <AdSlot> renders. Dimensions are always present so
// the component can reserve exact space before the creative loads, which is
// what keeps ads from contributing to CLS (§38, §56).
type ServedAd struct {
	ID          uint   `json:"id"`
	Position    string `json:"position"`
	ImageURL    string `json:"imageUrl"`
	HTMLSnippet string `json:"htmlSnippet,omitempty"`
	TargetURL   string `json:"targetUrl"`
	AltText     string `json:"altText"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	// Label is the disclosure text rendered above every creative. Readers must
	// always be able to tell an ad from editorial content (§42).
	//
	// Both languages are returned rather than one: the API does not know which
	// language the reader chose, and picking here meant every reader saw the
	// Khmer label whatever they had selected. The client renders the one that
	// matches its locale.
	Label   string `json:"label"` // deprecated: kept so existing clients keep working
	LabelKh string `json:"labelKh"`
	LabelEn string `json:"labelEn"`
}

const (
	adLabelKh = "ការផ្សាយពាណិជ្ជកម្ម"
	adLabelEn = "Advertisement"
)

// Serve picks one creative for a position. A miss returns nil with no error:
// an empty slot is a normal state, and the component renders its reserved
// placeholder box.
func (s *AdService) Serve(ctx context.Context, position, device, categorySlug string) (*ServedAd, error) {
	now := time.Now().UTC()

	var ads []models.Advertisement
	err := s.db.WithContext(ctx).
		Joins("JOIN advertisement_campaigns c ON c.id = advertisements.campaign_id").
		Where("advertisements.position = ?", position).
		Where("advertisements.status = ?", models.AdActive).
		Where("advertisements.start_at <= ? AND advertisements.end_at > ?", now, now).
		Where("advertisements.deleted_at IS NULL").
		// The campaign must also be live and — critically — approved. An
		// advertiser-submitted campaign never serves on its own (§41).
		Where("c.status = ?", models.AdActive).
		Where("c.approved_at IS NOT NULL").
		Where("c.start_at <= ? AND c.end_at > ?", now, now).
		Where("c.deleted_at IS NULL").
		Where("advertisements.target_device IN ?", []string{"all", device}).
		Order("advertisements.priority DESC").
		Find(&ads).Error
	if err != nil {
		return nil, fmt.Errorf("load ads for %s: %w", position, err)
	}

	eligible := make([]models.Advertisement, 0, len(ads))
	for _, ad := range ads {
		if !matchesCategory(ad, categorySlug) {
			continue
		}
		if creativeFor(ad, device) == "" && strings.TrimSpace(ad.HTMLSnippet) == "" {
			// No usable creative for this form factor — skip rather than serve
			// a blank box.
			continue
		}
		eligible = append(eligible, ad)
	}
	if len(eligible) == 0 {
		return nil, nil
	}

	chosen := pickWeighted(eligible)
	width, height := dimensionsFor(chosen, device)

	return &ServedAd{
		ID:          chosen.ID,
		Position:    chosen.Position,
		ImageURL:    creativeFor(chosen, device),
		HTMLSnippet: chosen.HTMLSnippet,
		TargetURL:   chosen.TargetURL,
		AltText:     chosen.AltText,
		Width:       width,
		Height:      height,
		Label:       adLabelKh,
		LabelKh:     adLabelKh,
		LabelEn:     adLabelEn,
	}, nil
}

// matchesCategory applies page targeting. An empty target list means the
// creative runs everywhere.
func matchesCategory(ad models.Advertisement, categorySlug string) bool {
	if len(ad.TargetCategories) == 0 {
		return true
	}
	if categorySlug == "" {
		return false
	}
	for _, c := range ad.TargetCategories {
		if c == categorySlug {
			return true
		}
	}
	return false
}

func creativeFor(ad models.Advertisement, device string) string {
	if device == "mobile" {
		if ad.MobileImageURL != "" {
			return ad.MobileImageURL
		}
		// Falling back to the desktop creative is better than an empty slot,
		// and the reserved box still uses the desktop dimensions so nothing
		// shifts.
		return ad.DesktopImageURL
	}
	if ad.DesktopImageURL != "" {
		return ad.DesktopImageURL
	}
	return ad.MobileImageURL
}

func dimensionsFor(ad models.Advertisement, device string) (int, int) {
	if device == "mobile" && ad.MobileWidth > 0 {
		return ad.MobileWidth, ad.MobileHeight
	}
	if ad.DesktopWidth > 0 {
		return ad.DesktopWidth, ad.DesktopHeight
	}
	return ad.MobileWidth, ad.MobileHeight
}

// pickWeighted chooses among equally-prioritised creatives by weight, so two
// advertisers in the same slot share it in the proportion they bought.
func pickWeighted(ads []models.Advertisement) models.Advertisement {
	topPriority := ads[0].Priority
	pool := make([]models.Advertisement, 0, len(ads))
	total := 0
	for _, ad := range ads {
		if ad.Priority != topPriority {
			break // the query ordered by priority DESC
		}
		weight := ad.Weight
		if weight < 1 {
			weight = 1
		}
		total += weight
		pool = append(pool, ad)
	}
	if len(pool) == 1 {
		return pool[0]
	}

	roll := rand.Intn(total)
	for _, ad := range pool {
		weight := ad.Weight
		if weight < 1 {
			weight = 1
		}
		if roll < weight {
			return ad
		}
		roll -= weight
	}
	return pool[0]
}

// Buffer keys for ad events, drained by FlushEvents.
const (
	adImpressionKey = "cfn:buf:ad:impressions"
	adClickKey      = "cfn:buf:ad:clicks"
)

// RecordImpression and RecordClick buffer to Redis like article views do:
// a homepage render fires several impressions, and those must not become
// several synchronous inserts.
func (s *AdService) RecordImpression(ctx context.Context, adID uint, position, device string) {
	s.bufferEvent(ctx, adImpressionKey, adID, position, device)
}

func (s *AdService) RecordClick(ctx context.Context, adID uint, position, device string) {
	s.bufferEvent(ctx, adClickKey, adID, position, device)
}

func (s *AdService) bufferEvent(ctx context.Context, key string, adID uint, position, device string) {
	if !s.cache.Enabled() {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	field := fmt.Sprintf("%d|%s|%s|%s", adID, position, device, day)
	if err := s.cache.Client().HIncrBy(ctx, key, field, 1).Err(); err != nil {
		slog.Warn("could not buffer ad event", "key", key, "ad", adID, "error", err)
	}
}

// FlushEvents writes buffered impressions and clicks to MySQL.
func (s *AdService) FlushEvents(ctx context.Context) error {
	if !s.cache.Enabled() {
		return nil
	}
	if err := s.flushImpressions(ctx); err != nil {
		return err
	}
	return s.flushClicks(ctx)
}

// adEventField parses "adID|position|device|day", and resolves the campaign.
type adEvent struct {
	adID     uint
	position string
	device   string
	day      string
	count    int64
}

func parseAdEvents(raw map[string]int64) []adEvent {
	out := make([]adEvent, 0, len(raw))
	for field, count := range raw {
		parts := strings.Split(field, "|")
		if len(parts) != 4 {
			continue
		}
		id, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			continue
		}
		out = append(out, adEvent{
			adID: uint(id), position: parts[1], device: parts[2], day: parts[3], count: count,
		})
	}
	return out
}

// campaignIDs resolves ad -> campaign in one query rather than per row.
func (s *AdService) campaignIDs(ctx context.Context, events []adEvent) map[uint]uint {
	ids := make([]uint, 0, len(events))
	seen := make(map[uint]bool)
	for _, e := range events {
		if !seen[e.adID] {
			seen[e.adID] = true
			ids = append(ids, e.adID)
		}
	}

	var rows []struct {
		ID         uint
		CampaignID uint
	}
	if err := s.db.WithContext(ctx).Model(&models.Advertisement{}).
		Select("id, campaign_id").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		slog.Warn("could not resolve ad campaigns", "error", err)
		return nil
	}

	out := make(map[uint]uint, len(rows))
	for _, r := range rows {
		out[r.ID] = r.CampaignID
	}
	return out
}

func (s *AdService) flushImpressions(ctx context.Context) error {
	raw, err := drainHash(ctx, s.cache.Client(), adImpressionKey)
	if err != nil || len(raw) == 0 {
		return err
	}
	events := parseAdEvents(raw)
	campaigns := s.campaignIDs(ctx, events)

	rows := make([]models.AdvertisementImpression, 0, len(events))
	totals := make(map[uint]int64)
	for _, e := range events {
		campaignID, ok := campaigns[e.adID]
		if !ok {
			continue // the creative was deleted between serve and flush
		}
		parsedDay, _ := time.Parse("2006-01-02", e.day)
		rows = append(rows, models.AdvertisementImpression{
			CreatedAt: parsedDay, AdID: e.adID, CampaignID: campaignID,
			Position: e.position, Device: e.device, Day: e.day, Count: int(e.count),
		})
		totals[e.adID] += e.count
	}
	if len(rows) == 0 {
		return nil
	}

	if err := s.db.WithContext(ctx).Clauses(clause.Insert{}).CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("flush ad impressions: %w", err)
	}
	for adID, count := range totals {
		s.db.WithContext(ctx).Model(&models.Advertisement{}).Where("id = ?", adID).
			UpdateColumn("impression_count", gorm.Expr("impression_count + ?", count))
	}
	return nil
}

func (s *AdService) flushClicks(ctx context.Context) error {
	raw, err := drainHash(ctx, s.cache.Client(), adClickKey)
	if err != nil || len(raw) == 0 {
		return err
	}
	events := parseAdEvents(raw)
	campaigns := s.campaignIDs(ctx, events)

	rows := make([]models.AdvertisementClick, 0, len(events))
	totals := make(map[uint]int64)
	for _, e := range events {
		campaignID, ok := campaigns[e.adID]
		if !ok {
			continue
		}
		parsedDay, _ := time.Parse("2006-01-02", e.day)
		rows = append(rows, models.AdvertisementClick{
			CreatedAt: parsedDay, AdID: e.adID, CampaignID: campaignID,
			Position: e.position, Device: e.device, Day: e.day, Count: int(e.count),
		})
		totals[e.adID] += e.count
	}
	if len(rows) == 0 {
		return nil
	}

	if err := s.db.WithContext(ctx).CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("flush ad clicks: %w", err)
	}
	for adID, count := range totals {
		s.db.WithContext(ctx).Model(&models.Advertisement{}).Where("id = ?", adID).
			UpdateColumn("click_count", gorm.Expr("click_count + ?", count))
	}
	return nil
}

// ExpireCampaigns moves finished campaigns and creatives to expired so the
// admin list reflects reality without anyone having to touch them (§35).
func (s *AdService) ExpireCampaigns(ctx context.Context) {
	now := time.Now().UTC()

	err := s.db.WithContext(ctx).Model(&models.Advertisement{}).
		Where("status IN ?", []models.AdStatus{models.AdActive, models.AdScheduled}).
		Where("end_at <= ?", now).
		Update("status", models.AdExpired).Error
	if err != nil {
		slog.Warn("could not expire advertisements", "error", err)
	}

	err = s.db.WithContext(ctx).Model(&models.AdvertisementCampaign{}).
		Where("status IN ?", []models.AdStatus{models.AdActive, models.AdScheduled}).
		Where("end_at <= ?", now).
		Update("status", models.AdExpired).Error
	if err != nil {
		slog.Warn("could not expire campaigns", "error", err)
	}

	// Scheduled campaigns that have reached their start date go live — but only
	// if an ad manager approved them.
	err = s.db.WithContext(ctx).Model(&models.AdvertisementCampaign{}).
		Where("status = ?", models.AdScheduled).
		Where("approved_at IS NOT NULL").
		Where("start_at <= ? AND end_at > ?", now, now).
		Update("status", models.AdActive).Error
	if err != nil {
		slog.Warn("could not activate campaigns", "error", err)
	}

	s.cache.DeletePrefix(ctx, "cfn:ads:")
}
