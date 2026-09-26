package controllers

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// AdminAdController manages campaigns and creatives (§35, §41).
type AdminAdController struct {
	db    *gorm.DB
	ads   *services.AdService
	audit *services.AuditService
}

func NewAdminAdController(db *gorm.DB, ads *services.AdService, audit *services.AuditService) *AdminAdController {
	return &AdminAdController{db: db, ads: ads, audit: audit}
}

// Campaigns handles GET /api/admin/ads/campaigns.
func (ctl *AdminAdController) Campaigns(c *gin.Context) {
	page := queryInt(c, "page", 1, 1, 200)
	limit := queryInt(c, "limit", 25, 1, 100)

	q := ctl.db.WithContext(c.Request.Context()).Model(&models.AdvertisementCampaign{})
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	if c.Query("pendingApproval") == "true" {
		q = q.Where("approved_at IS NULL")
	}

	var total int64
	q.Session(&gorm.Session{}).Count(&total)

	var campaigns []models.AdvertisementCampaign
	err := q.Preload("Advertisements").Order("created_at DESC").
		Limit(limit).Offset((page - 1) * limit).Find(&campaigns).Error
	if err != nil {
		httpx.Internal(c, "Could not load campaigns")
		return
	}
	httpx.OKList(c, campaigns, httpx.NewMeta(page, limit, total))
}

type campaignRequest struct {
	Name         string    `json:"name" binding:"required"`
	Advertiser   string    `json:"advertiser" binding:"required"`
	ContactEmail string    `json:"contactEmail"`
	StartAt      time.Time `json:"startAt" binding:"required"`
	EndAt        time.Time `json:"endAt" binding:"required"`
	Notes        string    `json:"notes"`
}

// CreateCampaign handles POST /api/admin/ads/campaigns.
//
// A new campaign is always a draft, and always unapproved. Approval is a
// separate, permissioned action — an advertiser account must not be able to
// create a live campaign in one call (§41).
func (ctl *AdminAdController) CreateCampaign(c *gin.Context) {
	var req campaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Name, advertiser and a date range are required")
		return
	}
	if !req.EndAt.After(req.StartAt) {
		httpx.BadRequest(c, httpx.CodeValidation, "The end date must be after the start date")
		return
	}

	campaign := models.AdvertisementCampaign{
		Name: req.Name, Advertiser: req.Advertiser, ContactEmail: req.ContactEmail,
		Status: models.AdDraft, StartAt: req.StartAt.UTC(), EndAt: req.EndAt.UTC(),
		Notes: req.Notes, SubmittedByID: middleware.CurrentUserID(c),
	}
	if err := ctl.db.WithContext(c.Request.Context()).Create(&campaign).Error; err != nil {
		httpx.Internal(c, "Could not create the campaign")
		return
	}
	httpx.Created(c, campaign)
}

// ApproveCampaign handles POST /api/admin/ads/campaigns/:id/approve.
func (ctl *AdminAdController) ApproveCampaign(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid campaign id")
		return
	}
	ctx := c.Request.Context()

	var campaign models.AdvertisementCampaign
	if err := ctl.db.WithContext(ctx).First(&campaign, id).Error; err != nil {
		httpx.NotFound(c, "CAMPAIGN_NOT_FOUND", "Campaign not found")
		return
	}

	now := time.Now().UTC()
	userID := middleware.CurrentUserID(c)

	// A campaign whose window has already opened goes live immediately;
	// otherwise it waits for the scheduler.
	status := models.AdScheduled
	if !now.Before(campaign.StartAt) && now.Before(campaign.EndAt) {
		status = models.AdActive
	}

	err := ctl.db.WithContext(ctx).Model(&campaign).Updates(map[string]any{
		"approved_by_id": userID, "approved_at": now, "status": status,
	}).Error
	if err != nil {
		httpx.Internal(c, "Could not approve the campaign")
		return
	}

	ctl.audit.Record(ctx, userID, "ads.approve_campaign", "campaign", &id, campaign.Name, nil)
	httpx.OK(c, gin.H{"id": id, "status": status, "approvedAt": now})
}

type advertisementRequest struct {
	CampaignID uint   `json:"campaignId" binding:"required"`
	Name       string `json:"name" binding:"required"`
	Position   string `json:"position" binding:"required"`

	DesktopImageURL string `json:"desktopImageUrl"`
	DesktopWidth    int    `json:"desktopWidth"`
	DesktopHeight   int    `json:"desktopHeight"`
	MobileImageURL  string `json:"mobileImageUrl"`
	MobileWidth     int    `json:"mobileWidth"`
	MobileHeight    int    `json:"mobileHeight"`
	HTMLSnippet     string `json:"htmlSnippet"`

	TargetURL        string    `json:"targetUrl"`
	AltText          string    `json:"altText"`
	StartAt          time.Time `json:"startAt" binding:"required"`
	EndAt            time.Time `json:"endAt" binding:"required"`
	Priority         int       `json:"priority"`
	Weight           int       `json:"weight"`
	TargetDevice     string    `json:"targetDevice"`
	TargetCategories []string  `json:"targetCategories"`
}

// CreateAd handles POST /api/ads.
func (ctl *AdminAdController) CreateAd(c *gin.Context) {
	var req advertisementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Campaign, name, position and dates are required")
		return
	}
	if err := validateCreative(req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, err.Error())
		return
	}

	device := req.TargetDevice
	if device != "desktop" && device != "mobile" {
		device = "all"
	}
	weight := req.Weight
	if weight < 1 {
		weight = 1
	}

	ad := models.Advertisement{
		CampaignID: req.CampaignID, Name: req.Name, Position: req.Position,
		Status:          models.AdDraft,
		DesktopImageURL: req.DesktopImageURL, DesktopWidth: req.DesktopWidth, DesktopHeight: req.DesktopHeight,
		MobileImageURL:  req.MobileImageURL, MobileWidth: req.MobileWidth, MobileHeight: req.MobileHeight,
		HTMLSnippet:     req.HTMLSnippet,
		TargetURL:       req.TargetURL, AltText: req.AltText,
		StartAt:         req.StartAt.UTC(), EndAt: req.EndAt.UTC(),
		Priority:        req.Priority, Weight: weight,
		TargetDevice:    device,
		TargetCategories: models.StringSlice(req.TargetCategories),
	}
	if err := ctl.db.WithContext(c.Request.Context()).Create(&ad).Error; err != nil {
		httpx.Internal(c, "Could not create the advertisement")
		return
	}
	httpx.Created(c, ad)
}

// UpdateAdStatus handles PUT /api/ads/:id/status.
func (ctl *AdminAdController) UpdateAdStatus(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid advertisement id")
		return
	}

	var req struct {
		Status models.AdStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A status is required")
		return
	}
	switch req.Status {
	case models.AdDraft, models.AdScheduled, models.AdActive, models.AdPaused, models.AdExpired:
	default:
		httpx.BadRequest(c, httpx.CodeValidation, "Unknown advertisement status")
		return
	}

	ctx := c.Request.Context()
	// Activating a creative under an unapproved campaign would bypass §41,
	// so check the parent before allowing it.
	if req.Status == models.AdActive {
		var campaign models.AdvertisementCampaign
		err := ctl.db.WithContext(ctx).
			Joins("JOIN advertisements a ON a.campaign_id = advertisement_campaigns.id").
			Where("a.id = ?", id).First(&campaign).Error
		if err != nil {
			httpx.NotFound(c, httpx.CodeAdNotFound, "Advertisement not found")
			return
		}
		if campaign.ApprovedAt == nil {
			httpx.BadRequest(c, httpx.CodeValidation, "The campaign must be approved before its ads can run")
			return
		}
	}

	if err := ctl.db.WithContext(ctx).Model(&models.Advertisement{}).
		Where("id = ?", id).Update("status", req.Status).Error; err != nil {
		httpx.Internal(c, "Could not update the advertisement")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "ads."+string(req.Status), "advertisement", &id, "", nil)
	httpx.OK(c, gin.H{"id": id, "status": req.Status})
}

// Performance handles GET /api/admin/ads/performance (§40).
func (ctl *AdminAdController) Performance(c *gin.Context) {
	days := queryInt(c, "days", 30, 1, 365)
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	type row struct {
		AdID        uint    `json:"adId"`
		Name        string  `json:"name"`
		Position    string  `json:"position"`
		Campaign    string  `json:"campaign"`
		Impressions int64   `json:"impressions"`
		Clicks      int64   `json:"clicks"`
		CTR         float64 `json:"ctr"`
	}

	var rows []row
	err := ctl.db.WithContext(c.Request.Context()).Raw(`
		SELECT a.id AS ad_id, a.name, a.position, c.name AS campaign,
		       COALESCE(i.total, 0) AS impressions,
		       COALESCE(k.total, 0) AS clicks
		FROM advertisements a
		JOIN advertisement_campaigns c ON c.id = a.campaign_id
		LEFT JOIN (SELECT ad_id, SUM(count) AS total FROM advertisement_impressions
		           WHERE day >= ? GROUP BY ad_id) i ON i.ad_id = a.id
		LEFT JOIN (SELECT ad_id, SUM(count) AS total FROM advertisement_clicks
		           WHERE day >= ? GROUP BY ad_id) k ON k.ad_id = a.id
		WHERE a.deleted_at IS NULL
		ORDER BY impressions DESC LIMIT 200`, since, since).Scan(&rows).Error
	if err != nil {
		httpx.Internal(c, "Could not load advertisement performance")
		return
	}

	for i := range rows {
		if rows[i].Impressions > 0 {
			rows[i].CTR = float64(rows[i].Clicks) / float64(rows[i].Impressions) * 100
		}
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, gin.H{"days": days, "rows": rows})
}

// Campaign handles GET /api/admin/ads/campaigns/:id.
func (ctl *AdminAdController) Campaign(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid campaign id")
		return
	}

	var campaign models.AdvertisementCampaign
	err := ctl.db.WithContext(c.Request.Context()).
		Preload("Advertisements").First(&campaign, id).Error
	if err != nil {
		httpx.NotFound(c, "CAMPAIGN_NOT_FOUND", "Campaign not found")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, campaign)
}

// UpdateCampaign handles PUT /api/admin/ads/campaigns/:id.
//
// Changing the flight dates or the advertiser re-opens the approval question,
// so an approved campaign drops back to needing sign-off. Otherwise a campaign
// could be approved for one week and then silently edited to run for a year.
func (ctl *AdminAdController) UpdateCampaign(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid campaign id")
		return
	}

	var req campaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Name, advertiser and a date range are required")
		return
	}
	if !req.EndAt.After(req.StartAt) {
		httpx.BadRequest(c, httpx.CodeValidation, "The end date must be after the start date")
		return
	}

	ctx := c.Request.Context()
	var campaign models.AdvertisementCampaign
	if err := ctl.db.WithContext(ctx).First(&campaign, id).Error; err != nil {
		httpx.NotFound(c, "CAMPAIGN_NOT_FOUND", "Campaign not found")
		return
	}

	flightChanged := !campaign.StartAt.Equal(req.StartAt.UTC()) ||
		!campaign.EndAt.Equal(req.EndAt.UTC()) ||
		campaign.Advertiser != req.Advertiser

	campaign.Name = req.Name
	campaign.Advertiser = req.Advertiser
	campaign.ContactEmail = req.ContactEmail
	campaign.StartAt = req.StartAt.UTC()
	campaign.EndAt = req.EndAt.UTC()
	campaign.Notes = req.Notes

	if flightChanged && campaign.ApprovedAt != nil {
		campaign.ApprovedAt = nil
		campaign.ApprovedByID = nil
		campaign.Status = models.AdDraft
	}

	if err := ctl.db.WithContext(ctx).Save(&campaign).Error; err != nil {
		httpx.Internal(c, "Could not update the campaign")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "ads.update_campaign", "campaign", &id, campaign.Name, nil)
	httpx.OK(c, gin.H{"campaign": campaign, "reapprovalRequired": flightChanged})
}

// DeleteCampaign handles DELETE /api/admin/ads/campaigns/:id.
func (ctl *AdminAdController) DeleteCampaign(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid campaign id")
		return
	}

	ctx := c.Request.Context()
	var campaign models.AdvertisementCampaign
	if err := ctl.db.WithContext(ctx).First(&campaign, id).Error; err != nil {
		httpx.NotFound(c, "CAMPAIGN_NOT_FOUND", "Campaign not found")
		return
	}

	// Retire the creatives with the campaign. Leaving them behind would mean
	// orphans that the serve query still considers.
	err := ctl.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("campaign_id = ?", id).Delete(&models.Advertisement{}).Error; err != nil {
			return err
		}
		return tx.Delete(&campaign).Error
	})
	if err != nil {
		httpx.Internal(c, "Could not delete the campaign")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "ads.delete_campaign", "campaign", &id, campaign.Name, nil)
	httpx.NoData(c)
}

// Ad handles GET /api/admin/ads/:id.
func (ctl *AdminAdController) Ad(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid advertisement id")
		return
	}

	var ad models.Advertisement
	if err := ctl.db.WithContext(c.Request.Context()).Preload("Campaign").First(&ad, id).Error; err != nil {
		httpx.NotFound(c, httpx.CodeAdNotFound, "Advertisement not found")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, ad)
}

// UpdateAd handles PUT /api/ads/:id.
func (ctl *AdminAdController) UpdateAd(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid advertisement id")
		return
	}

	var req advertisementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Campaign, name, position and dates are required")
		return
	}
	if err := validateCreative(req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, err.Error())
		return
	}

	ctx := c.Request.Context()
	var ad models.Advertisement
	if err := ctl.db.WithContext(ctx).First(&ad, id).Error; err != nil {
		httpx.NotFound(c, httpx.CodeAdNotFound, "Advertisement not found")
		return
	}

	device := req.TargetDevice
	if device != "desktop" && device != "mobile" {
		device = "all"
	}
	weight := req.Weight
	if weight < 1 {
		weight = 1
	}

	ad.CampaignID = req.CampaignID
	ad.Name = req.Name
	ad.Position = req.Position
	ad.DesktopImageURL, ad.DesktopWidth, ad.DesktopHeight = req.DesktopImageURL, req.DesktopWidth, req.DesktopHeight
	ad.MobileImageURL, ad.MobileWidth, ad.MobileHeight = req.MobileImageURL, req.MobileWidth, req.MobileHeight
	ad.HTMLSnippet = req.HTMLSnippet
	ad.TargetURL, ad.AltText = req.TargetURL, req.AltText
	ad.StartAt, ad.EndAt = req.StartAt.UTC(), req.EndAt.UTC()
	ad.Priority, ad.Weight = req.Priority, weight
	ad.TargetDevice = device
	ad.TargetCategories = models.StringSlice(req.TargetCategories)

	if err := ctl.db.WithContext(ctx).Save(&ad).Error; err != nil {
		httpx.Internal(c, "Could not update the advertisement")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "ads.update", "advertisement", &id, ad.Name, nil)
	httpx.OK(c, ad)
}

// DeleteAd handles DELETE /api/ads/:id.
func (ctl *AdminAdController) DeleteAd(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid advertisement id")
		return
	}

	ctx := c.Request.Context()
	var ad models.Advertisement
	if err := ctl.db.WithContext(ctx).First(&ad, id).Error; err != nil {
		httpx.NotFound(c, httpx.CodeAdNotFound, "Advertisement not found")
		return
	}
	if err := ctl.db.WithContext(ctx).Delete(&ad).Error; err != nil {
		httpx.Internal(c, "Could not delete the advertisement")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "ads.delete", "advertisement", &id, ad.Name, nil)
	httpx.NoData(c)
}

// Ads handles GET /api/admin/ads — every creative, for the management list.
func (ctl *AdminAdController) Ads(c *gin.Context) {
	page := queryInt(c, "page", 1, 1, 500)
	limit := queryInt(c, "limit", 25, 1, 100)

	q := ctl.db.WithContext(c.Request.Context()).Model(&models.Advertisement{})
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	if position := c.Query("position"); position != "" {
		q = q.Where("position = ?", position)
	}
	if search := c.Query("search"); search != "" {
		q = q.Where("name LIKE ?", "%"+search+"%")
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		httpx.Internal(c, "Could not load advertisements")
		return
	}

	var ads []models.Advertisement
	err := q.Preload("Campaign").Order("created_at DESC").
		Limit(limit).Offset((page - 1) * limit).Find(&ads).Error
	if err != nil {
		httpx.Internal(c, "Could not load advertisements")
		return
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OKList(c, ads, httpx.NewMeta(page, limit, total))
}

// validateCreative enforces what the ad server needs to render without causing
// layout shift: a usable creative, and dimensions to reserve space with (§38).
func validateCreative(req advertisementRequest) error {
	if req.DesktopImageURL == "" && req.MobileImageURL == "" && req.HTMLSnippet == "" {
		return errors.New("a creative is required: an image for at least one device, or an ad tag")
	}
	if req.DesktopImageURL != "" && (req.DesktopWidth == 0 || req.DesktopHeight == 0) {
		return errors.New("desktop creative width and height are required")
	}
	if req.MobileImageURL != "" && (req.MobileWidth == 0 || req.MobileHeight == 0) {
		return errors.New("mobile creative width and height are required")
	}
	if !req.EndAt.After(req.StartAt) {
		return errors.New("the end date must be after the start date")
	}
	return nil
}
