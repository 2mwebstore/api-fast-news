package services

import (
	"testing"
	"time"

	"github.com/cambodia-fast-news/backend/internal/models"
)

func TestAdvertisementIsServable(t *testing.T) {
	now := time.Now().UTC()
	window := func(status models.AdStatus, start, end time.Duration) *models.Advertisement {
		return &models.Advertisement{Status: status, StartAt: now.Add(start), EndAt: now.Add(end)}
	}

	if !window(models.AdActive, -time.Hour, time.Hour).IsServable(now) {
		t.Error("an active, in-window creative should serve")
	}
	if window(models.AdPaused, -time.Hour, time.Hour).IsServable(now) {
		t.Error("a paused creative must not serve")
	}
	if window(models.AdDraft, -time.Hour, time.Hour).IsServable(now) {
		t.Error("a draft creative must not serve")
	}
	if window(models.AdActive, time.Hour, 2*time.Hour).IsServable(now) {
		t.Error("a creative whose flight has not started must not serve")
	}
	if window(models.AdActive, -2*time.Hour, -time.Hour).IsServable(now) {
		t.Error("an expired creative must not serve")
	}
}

func TestMatchesCategoryTargeting(t *testing.T) {
	untargeted := models.Advertisement{}
	if !matchesCategory(untargeted, "sports") {
		t.Error("an untargeted creative should run everywhere")
	}
	if !matchesCategory(untargeted, "") {
		t.Error("an untargeted creative should run on pages with no category")
	}

	targeted := models.Advertisement{TargetCategories: models.StringSlice{"sports", "kun-khmer"}}
	if !matchesCategory(targeted, "sports") {
		t.Error("targeted creative should match its category")
	}
	if matchesCategory(targeted, "business") {
		t.Error("targeted creative must not run outside its categories")
	}
	if matchesCategory(targeted, "") {
		t.Error("a category-targeted creative must not run where there is no category")
	}
}

func TestCreativeFallsBackAcrossDevices(t *testing.T) {
	desktopOnly := models.Advertisement{DesktopImageURL: "d.jpg"}
	if got := creativeFor(desktopOnly, "mobile"); got != "d.jpg" {
		t.Errorf("mobile should fall back to the desktop creative, got %q", got)
	}

	mobileOnly := models.Advertisement{MobileImageURL: "m.jpg"}
	if got := creativeFor(mobileOnly, "desktop"); got != "m.jpg" {
		t.Errorf("desktop should fall back to the mobile creative, got %q", got)
	}

	both := models.Advertisement{DesktopImageURL: "d.jpg", MobileImageURL: "m.jpg"}
	if got := creativeFor(both, "mobile"); got != "m.jpg" {
		t.Errorf("mobile should prefer the mobile creative, got %q", got)
	}
}

func TestDimensionsAlwaysResolveSoSpaceCanBeReserved(t *testing.T) {
	// A creative with no usable dimensions would render an unsized box and
	// cause the layout shift §38 exists to prevent.
	ad := models.Advertisement{DesktopWidth: 970, DesktopHeight: 90}
	if w, h := dimensionsFor(ad, "mobile"); w != 970 || h != 90 {
		t.Errorf("mobile with no mobile size should inherit desktop, got %dx%d", w, h)
	}

	mobileOnly := models.Advertisement{MobileWidth: 320, MobileHeight: 50}
	if w, h := dimensionsFor(mobileOnly, "desktop"); w != 320 || h != 50 {
		t.Errorf("desktop with no desktop size should inherit mobile, got %dx%d", w, h)
	}
}

func TestCTR(t *testing.T) {
	ad := models.Advertisement{ImpressionCount: 1000, ClickCount: 25}
	if got := ad.CTR(); got != 2.5 {
		t.Errorf("CTR() = %f, want 2.5", got)
	}

	unserved := models.Advertisement{}
	if got := unserved.CTR(); got != 0 {
		t.Errorf("an unserved creative should have CTR 0, got %f", got)
	}
}
