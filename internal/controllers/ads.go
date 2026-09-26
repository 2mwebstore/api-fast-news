package controllers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// AdController serves creatives to <AdSlot> and records their events.
type AdController struct {
	ads *services.AdService
	db  *gorm.DB
}

func NewAdController(ads *services.AdService, db *gorm.DB) *AdController {
	return &AdController{ads: ads, db: db}
}

// Serve handles GET /api/ads?position=HOME_TOP.
//
// It always returns 200 with a possibly-null ad plus the slot's reserved
// dimensions, so the component can size its placeholder even on a miss — an
// empty slot that collapses is a layout shift (§38).
func (ctl *AdController) Serve(c *gin.Context) {
	ctx := c.Request.Context()
	position := strings.ToUpper(strings.TrimSpace(c.Query("position")))
	if position == "" {
		httpx.BadRequest(c, httpx.CodeValidation, "A position is required")
		return
	}

	dev := device(c)
	ad, err := ctl.ads.Serve(ctx, position, dev, c.Query("category"))
	if err != nil {
		httpx.Internal(c, "Could not load advertisement")
		return
	}

	var slot models.AdvertisementSlot
	ctl.db.WithContext(ctx).Where("position = ?", position).First(&slot)

	reservedW, reservedH := slot.DesktopWidth, slot.DesktopHeight
	if dev == "mobile" {
		reservedW, reservedH = slot.MobileWidth, slot.MobileHeight
	}

	// Ads are per-device and change often; never let a shared cache hold one.
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, gin.H{
		"ad":             ad,
		"position":       position,
		"enabled":        slot.ID == 0 || slot.IsEnabled,
		"reservedWidth":  reservedW,
		"reservedHeight": reservedH,
	})
}

// Impression handles POST /api/ads/:id/impression.
func (ctl *AdController) Impression(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		c.Status(http.StatusNoContent)
		return
	}
	ctl.ads.RecordImpression(c.Request.Context(), id, c.Query("position"), device(c))
	c.Status(http.StatusNoContent)
}

// Click handles POST /api/ads/:id/click.
func (ctl *AdController) Click(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		c.Status(http.StatusNoContent)
		return
	}
	ctl.ads.RecordClick(c.Request.Context(), id, c.Query("position"), device(c))
	c.Status(http.StatusNoContent)
}

// Slots handles GET /api/ads/slots — the slot definitions the frontend uses to
// reserve space before any request for a creative is made.
func (ctl *AdController) Slots(c *gin.Context) {
	var slots []models.AdvertisementSlot
	err := ctl.db.WithContext(c.Request.Context()).
		Where("is_enabled = ?", true).Find(&slots).Error
	if err != nil {
		httpx.Internal(c, "Could not load ad slots")
		return
	}
	c.Header("Cache-Control", "public, max-age=600")
	httpx.OK(c, slots)
}
