package controllers

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// AdminVideoController manages editorial video.
//
// Video is published by embedding YouTube rather than hosting files: it ships
// same-day, costs nothing to serve, and avoids a transcode pipeline the
// newsroom does not have. Only the video id is stored, never a pasted URL, so
// the embed we render is one we constructed.
type AdminVideoController struct {
	db    *gorm.DB
	audit *services.AuditService
}

func NewAdminVideoController(db *gorm.DB, audit *services.AuditService) *AdminVideoController {
	return &AdminVideoController{db: db, audit: audit}
}

// List handles GET /api/admin/videos — every status, newest first.
func (ctl *AdminVideoController) List(c *gin.Context) {
	page := queryInt(c, "page", 1, 1, 500)
	limit := queryInt(c, "limit", 25, 1, 100)

	q := ctl.db.WithContext(c.Request.Context()).Model(&models.Video{})
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	if search := c.Query("search"); search != "" {
		like := "%" + search + "%"
		q = q.Where("title_kh LIKE ? OR title_en LIKE ?", like, like)
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		httpx.Internal(c, "Could not load videos")
		return
	}

	var videos []models.Video
	err := q.Preload("Category").Order("created_at DESC").
		Limit(limit).Offset((page - 1) * limit).Find(&videos).Error
	if err != nil {
		httpx.Internal(c, "Could not load videos")
		return
	}

	out := make([]gin.H, 0, len(videos))
	for i := range videos {
		out = append(out, adminVideoPayload(&videos[i]))
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OKList(c, out, httpx.NewMeta(page, limit, total))
}

// Get handles GET /api/admin/videos/:id.
func (ctl *AdminVideoController) Get(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid video id")
		return
	}

	var video models.Video
	if err := ctl.db.WithContext(c.Request.Context()).Preload("Category").Preload("SEO").
		First(&video, id).Error; err != nil {
		httpx.NotFound(c, httpx.CodeVideoNotFound, "Video not found")
		return
	}
	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, adminVideoPayload(&video))
}

type videoRequest struct {
	TitleKh string `json:"titleKh" binding:"required,min=3"`
	TitleEn string `json:"titleEn"`
	DescKh  string `json:"descKh"`
	DescEn  string `json:"descEn"`
	// Accepts any YouTube link form or a bare id.
	YouTubeURL   string `json:"youtubeUrl" binding:"required"`
	CategoryID   *uint  `json:"categoryId"`
	ArticleID    *uint  `json:"articleId"`
	DurationSec  int    `json:"durationSec"`
	ThumbnailURL string `json:"thumbnailUrl"`
	ThumbnailAlt string `json:"thumbnailAlt"`
	Slug         string `json:"slug"`

	// Optional SEO overrides. Nil means "leave whatever is stored alone", which
	// is not the same as an empty object — that clears the fields.
	SEO *videoSEORequest `json:"seo"`
}

// videoSEORequest mirrors the article SEO panel, minus the fields that make no
// sense for a video: there is no separate body to keyword-stuff, and robots is
// per-page rather than per-field.
type videoSEORequest struct {
	SEOTitle           string   `json:"seoTitle"`
	SEODescription     string   `json:"seoDescription"`
	SEOKeywords        []string `json:"seoKeywords"`
	CanonicalURL       string   `json:"canonicalUrl"`
	OGTitle            string   `json:"ogTitle"`
	OGDescription      string   `json:"ogDescription"`
	OGImage            string   `json:"ogImage"`
	TwitterTitle       string   `json:"twitterTitle"`
	TwitterDescription string   `json:"twitterDescription"`
	TwitterImage       string   `json:"twitterImage"`
	Robots             string   `json:"robots"`
}

// Create handles POST /api/videos.
func (ctl *AdminVideoController) Create(c *gin.Context) {
	var req videoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A title and a YouTube link are required")
		return
	}

	youtubeID := utils.ParseYouTubeID(req.YouTubeURL)
	if youtubeID == "" {
		httpx.BadRequest(c, "INVALID_YOUTUBE_URL",
			"That does not look like a YouTube link. Paste a watch, youtu.be, embed or Shorts URL.")
		return
	}

	ctx := c.Request.Context()
	slug, err := ctl.uniqueSlug(ctx, req.Slug, req.TitleEn, req.TitleKh, 0)
	if err != nil {
		httpx.Conflict(c, httpx.CodeDuplicateSlug, "Could not generate a unique slug")
		return
	}

	video := models.Video{
		Slug: slug, TitleKh: req.TitleKh, TitleEn: req.TitleEn,
		DescKh: req.DescKh, DescEn: req.DescEn,
		YouTubeID:  youtubeID,
		CategoryID: req.CategoryID, ArticleID: req.ArticleID,
		DurationSec: req.DurationSec,
		// Fall back to YouTube's own poster so a video always has a thumbnail.
		ThumbnailURL: firstNonEmpty(req.ThumbnailURL, utils.YouTubeThumbnailURL(youtubeID)),
		ThumbnailAlt: firstNonEmpty(req.ThumbnailAlt, req.TitleKh),
		Status:       models.StatusDraft,
	}
	// Every video is landscape now that the vertical feed is gone, so the
	// intrinsic size is fixed and cards can reserve space without a reflow.
	video.Width, video.Height = 1920, 1080

	if err := ctl.db.WithContext(ctx).Create(&video).Error; err != nil {
		httpx.Internal(c, "Could not create the video")
		return
	}

	if err := ctl.saveSEO(c, &video, req.SEO); err != nil {
		httpx.Internal(c, "The video was created but its SEO metadata could not be saved")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "video.create", "video", &video.ID, video.TitleKh, nil)
	httpx.Created(c, adminVideoPayload(&video))
}

// Update handles PUT /api/videos/:id.
func (ctl *AdminVideoController) Update(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid video id")
		return
	}

	var req videoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A title and a YouTube link are required")
		return
	}

	youtubeID := utils.ParseYouTubeID(req.YouTubeURL)
	if youtubeID == "" {
		httpx.BadRequest(c, "INVALID_YOUTUBE_URL", "That does not look like a YouTube link")
		return
	}

	ctx := c.Request.Context()
	var video models.Video
	if err := ctl.db.WithContext(ctx).First(&video, id).Error; err != nil {
		httpx.NotFound(c, httpx.CodeVideoNotFound, "Video not found")
		return
	}

	video.TitleKh, video.TitleEn = req.TitleKh, req.TitleEn
	video.DescKh, video.DescEn = req.DescKh, req.DescEn
	video.YouTubeID = youtubeID
	video.CategoryID, video.ArticleID = req.CategoryID, req.ArticleID
	video.DurationSec = req.DurationSec
	video.ThumbnailURL = firstNonEmpty(req.ThumbnailURL, utils.YouTubeThumbnailURL(youtubeID))
	video.ThumbnailAlt = firstNonEmpty(req.ThumbnailAlt, req.TitleKh)
	// Every video is landscape now that the vertical feed is gone, so the
	// intrinsic size is fixed and cards can reserve space without a reflow.
	video.Width, video.Height = 1920, 1080

	if err := ctl.db.WithContext(ctx).Save(&video).Error; err != nil {
		httpx.Internal(c, "Could not update the video")
		return
	}

	if err := ctl.saveSEO(c, &video, req.SEO); err != nil {
		httpx.Internal(c, "The video was saved but its SEO metadata could not be")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "video.update", "video", &video.ID, video.TitleKh, nil)
	httpx.OK(c, adminVideoPayload(&video))
}

// SetStatus handles POST /api/admin/videos/:id/status.
func (ctl *AdminVideoController) SetStatus(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid video id")
		return
	}

	var req struct {
		Status models.ArticleStatus `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A status is required")
		return
	}
	switch req.Status {
	case models.StatusDraft, models.StatusPublished, models.StatusArchived:
	default:
		httpx.BadRequest(c, httpx.CodeValidation, "Videos can be draft, published or archived")
		return
	}

	ctx := c.Request.Context()
	var video models.Video
	if err := ctl.db.WithContext(ctx).First(&video, id).Error; err != nil {
		httpx.NotFound(c, httpx.CodeVideoNotFound, "Video not found")
		return
	}

	video.Status = req.Status
	if req.Status == models.StatusPublished && video.PublishedAt == nil {
		now := time.Now().UTC()
		video.PublishedAt = &now
	}
	if err := ctl.db.WithContext(ctx).Save(&video).Error; err != nil {
		httpx.Internal(c, "Could not change the video status")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "video."+string(req.Status), "video", &id, video.TitleKh, nil)
	httpx.OK(c, adminVideoPayload(&video))
}

// Delete handles DELETE /api/videos/:id. Soft delete, like articles: editorial
// records are retired, not destroyed.
func (ctl *AdminVideoController) Delete(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid video id")
		return
	}

	ctx := c.Request.Context()
	var video models.Video
	if err := ctl.db.WithContext(ctx).First(&video, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.NotFound(c, httpx.CodeVideoNotFound, "Video not found")
			return
		}
		httpx.Internal(c, "Could not load the video")
		return
	}

	if err := ctl.db.WithContext(ctx).Delete(&video).Error; err != nil {
		httpx.Internal(c, "Could not delete the video")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "video.delete", "video", &id, video.TitleKh, nil)
	httpx.NoData(c)
}

// adminVideoPayload adds the derived embed fields the admin needs.
func adminVideoPayload(v *models.Video) gin.H {
	payload := gin.H{
		"id": v.ID, "slug": v.Slug,
		"titleKh": v.TitleKh, "titleEn": v.TitleEn,
		"descKh": v.DescKh, "descEn": v.DescEn,
		"youtubeId": v.YouTubeID,
		"embedUrl":  utils.YouTubeEmbedURL(v.YouTubeID),
		"watchUrl":  utils.YouTubeWatchURL(v.YouTubeID),
		"thumbnailUrl": v.ThumbnailURL, "thumbnailAlt": v.ThumbnailAlt,
		"durationSec": v.DurationSec,
		"status": v.Status, "publishedAt": v.PublishedAt,
		"viewCount": v.ViewCount, "createdAt": v.CreatedAt, "updatedAt": v.UpdatedAt,
	}
	if v.Category != nil {
		payload["category"] = gin.H{
			"id": v.Category.ID, "slug": v.Category.Slug,
			"nameKh": v.Category.NameKh, "nameEn": v.Category.NameEn,
		}
	}
	if v.SEO != nil {
		payload["seo"] = v.SEO
	}
	return payload
}

// saveSEO upserts the video's SEO row. A nil request leaves the stored row
// untouched — a form that does not send the block should not wipe metadata
// somebody else wrote.
func (ctl *AdminVideoController) saveSEO(c *gin.Context, video *models.Video, req *videoSEORequest) error {
	ctx := c.Request.Context()
	if req == nil {
		return nil
	}

	var metadata models.SEOMetadata
	ctl.db.WithContext(ctx).Where("video_id = ?", video.ID).FirstOrInit(&metadata)

	metadata.VideoID = &video.ID
	metadata.SEOTitle = req.SEOTitle
	metadata.SEODescription = req.SEODescription
	metadata.SEOKeywords = models.StringSlice(req.SEOKeywords)
	metadata.CanonicalURL = req.CanonicalURL
	metadata.OGTitle = req.OGTitle
	metadata.OGDescription = req.OGDescription
	metadata.OGImage = req.OGImage
	metadata.TwitterTitle = req.TwitterTitle
	metadata.TwitterDescription = req.TwitterDescription
	metadata.TwitterImage = req.TwitterImage
	if req.Robots != "" {
		metadata.Robots = req.Robots
	}
	// Written by hand through the video form, so it counts as reviewed.
	metadata.AIGenerated = false
	now := timeNow()
	metadata.ReviewedAt = &now
	metadata.ReviewedByID = middleware.CurrentUserID(c)

	if err := ctl.db.WithContext(ctx).Save(&metadata).Error; err != nil {
		return err
	}
	video.SEO = &metadata
	return nil
}

func (ctl *AdminVideoController) uniqueSlug(ctx context.Context, requested, titleEn, titleKh string, exceptID uint) (string, error) {
	base := utils.Slugify(requested)
	if base == "" {
		base = utils.SlugifyWithFallback(titleEn, titleKh, time.Now().UTC().Format("20060102"))
	}

	candidate := base
	for attempt := 0; attempt < 12; attempt++ {
		q := ctl.db.WithContext(ctx).Unscoped().Model(&models.Video{}).Where("slug = ?", candidate)
		if exceptID > 0 {
			q = q.Where("id <> ?", exceptID)
		}
		var count int64
		if err := q.Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
		candidate = base + "-" + utils.RandomHex(3)
	}
	return "", errors.New("could not generate a unique slug")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
