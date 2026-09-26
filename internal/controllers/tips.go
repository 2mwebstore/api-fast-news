package controllers

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// TipController accepts citizen submissions (§34).
type TipController struct{ db *gorm.DB }

func NewTipController(db *gorm.DB) *TipController { return &TipController{db: db} }

type tipRequest struct {
	Name        string   `json:"name"`
	Contact     string   `json:"contact"`
	Location    string   `json:"location"`
	Description string   `json:"description" binding:"required,min=10"`
	PhotoURLs   []string `json:"photoUrls"`
	VideoURLs   []string `json:"videoUrls"`
}

// Submit handles POST /api/tip.
//
// Submissions are stored as pending and never published automatically — §34 is
// explicit, and a tip is unverified by definition. All text is stored as plain
// text, never HTML.
func (ctl *TipController) Submit(c *gin.Context) {
	var req tipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Please describe what you saw (at least 10 characters)")
		return
	}

	tip := models.NewsTip{
		Name:        utils.PlainText(req.Name, 120),
		Contact:     utils.PlainText(req.Contact, 180),
		Location:    utils.PlainText(req.Location, 180),
		Description: utils.PlainText(req.Description, 4000),
		Status:      models.TipPending,
		IPHash:      visitorHash(c),
	}

	// Only accept media URLs that we issued — a submitted link to an arbitrary
	// host would let a tip embed content we do not control.
	tip.PhotoURLs = filterOwnMedia(ctl.db, req.PhotoURLs)
	tip.VideoURLs = filterOwnMedia(ctl.db, req.VideoURLs)

	if err := ctl.db.WithContext(c.Request.Context()).Create(&tip).Error; err != nil {
		httpx.Internal(c, "Could not submit your report")
		return
	}

	httpx.Created(c, gin.H{
		"id":      tip.ID,
		"status":  tip.Status,
		"message": "សូមអរគុណ។ ព័ត៌មានរបស់អ្នកនឹងត្រូវបានពិនិត្យដោយអ្នកកែសម្រួល។",
		"messageEn": "Thank you. Your report will be reviewed by an editor before any of it is published.",
	})
}

// filterOwnMedia keeps only URLs that exist in our own media library.
func filterOwnMedia(db *gorm.DB, urls []string) models.StringSlice {
	if len(urls) == 0 {
		return nil
	}
	if len(urls) > 10 {
		urls = urls[:10]
	}

	cleaned := make([]string, 0, len(urls))
	for _, u := range urls {
		if u = strings.TrimSpace(u); u != "" {
			cleaned = append(cleaned, u)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}

	var known []string
	db.Model(&models.Media{}).Where("url IN ?", cleaned).Pluck("url", &known)
	if len(known) == 0 {
		return nil
	}
	return models.StringSlice(known)
}

// TrafficController serves the manually-curated traffic board (§71).
type TrafficController struct{ db *gorm.DB }

func NewTrafficController(db *gorm.DB) *TrafficController { return &TrafficController{db: db} }

// Status handles GET /api/traffic-status.
//
// Every row carries who assessed it and when. CFN has no live traffic feed, so
// the response must never read as real-time data (§71).
func (ctl *TrafficController) Status(c *gin.Context) {
	var rows []models.TrafficStatus
	err := ctl.db.WithContext(c.Request.Context()).
		Where("is_active = ?", true).
		Order("position ASC, id ASC").Find(&rows).Error
	if err != nil {
		httpx.Internal(c, "Could not load traffic status")
		return
	}

	var updated *time.Time
	for i := range rows {
		if updated == nil || rows[i].ObservedAt.After(*updated) {
			updated = &rows[i].ObservedAt
		}
	}

	c.Header("Cache-Control", "public, max-age=120")
	httpx.OK(c, gin.H{
		"routes":      rows,
		"lastUpdated": updated,
		"isLiveData":  false,
		"disclaimer":  "Manually assessed by the CFN newsroom. This is not a live traffic feed.",
		"disclaimerKh": "វាយតម្លៃដោយដៃដោយបុគ្គលិក CFN។ នេះមិនមែនជាទិន្នន័យចរាចរណ៍ផ្ទាល់ទេ។",
	})
}
