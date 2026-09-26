package controllers

import (
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/media"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
)

// MediaController is the library at /admin/media (§69).
type MediaController struct {
	storage *media.Service
	db      *gorm.DB
}

func NewMediaController(storage *media.Service, db *gorm.DB) *MediaController {
	return &MediaController{storage: storage, db: db}
}

// Upload handles POST /api/media/upload.
func (ctl *MediaController) Upload(c *gin.Context) {
	if !ctl.storage.Configured() {
		httpx.Fail(c, 503, httpx.CodeNotConfigured,
			"Media storage is not configured. Set the R2_* environment variables to enable uploads.")
		return
	}

	header, err := c.FormFile("file")
	if err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "No file was uploaded")
		return
	}

	folder := media.Folder(c.DefaultPostForm("folder", string(media.FolderArticle)))
	switch folder {
	case media.FolderArticle, media.FolderAuthor, media.FolderAd, media.FolderVideo, media.FolderTip:
	default:
		httpx.BadRequest(c, httpx.CodeValidation, "Unknown media folder")
		return
	}

	upload, err := ctl.storage.UploadMultipart(c.Request.Context(), header, folder)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrTooLarge), errors.Is(err, media.ErrTypeRejected):
			httpx.BadRequest(c, httpx.CodeUploadRejected, err.Error())
		default:
			httpx.Internal(c, "Upload failed")
		}
		return
	}

	record := models.Media{
		Key: upload.Key, URL: upload.URL, Filename: upload.Filename,
		MimeType: upload.MimeType, SizeBytes: upload.SizeBytes,
		Folder:       string(folder),
		AltKh:        c.PostForm("altKh"),
		AltEn:        c.PostForm("altEn"),
		Caption:      c.PostForm("caption"),
		Credit:       c.PostForm("credit"),
		AIPrompt:     c.PostForm("aiPrompt"),
		UploadedByID: middleware.CurrentUserID(c),
	}
	// An AI-generated image is flagged at upload so the disclosure follows it
	// everywhere it is used (§25).
	if c.PostForm("isAiGenerated") == "true" || record.AIPrompt != "" {
		record.IsAIGenerated = true
	}

	if err := ctl.db.WithContext(c.Request.Context()).Create(&record).Error; err != nil {
		httpx.Internal(c, "File uploaded but could not be recorded in the library")
		return
	}
	httpx.Created(c, record)
}

// List handles GET /api/media.
func (ctl *MediaController) List(c *gin.Context) {
	page := queryInt(c, "page", 1, 1, 500)
	limit := queryInt(c, "limit", 40, 1, 100)

	q := ctl.db.WithContext(c.Request.Context()).Model(&models.Media{})
	if folder := c.Query("folder"); folder != "" {
		q = q.Where("folder = ?", folder)
	}
	if search := c.Query("search"); search != "" {
		like := "%" + search + "%"
		q = q.Where("filename LIKE ? OR alt_kh LIKE ? OR alt_en LIKE ? OR caption LIKE ?",
			like, like, like, like)
	}
	if mime := c.Query("type"); mime != "" {
		q = q.Where("mime_type LIKE ?", mime+"%")
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		httpx.Internal(c, "Could not load the media library")
		return
	}

	var items []models.Media
	err := q.Preload("UploadedBy").Order("created_at DESC").
		Limit(limit).Offset((page - 1) * limit).Find(&items).Error
	if err != nil {
		httpx.Internal(c, "Could not load the media library")
		return
	}
	httpx.OKList(c, items, httpx.NewMeta(page, limit, total))
}

// Update handles PATCH /api/media/:id — ALT text and caption editing (§55).
func (ctl *MediaController) Update(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid media id")
		return
	}

	var req struct {
		AltKh   string `json:"altKh"`
		AltEn   string `json:"altEn"`
		Caption string `json:"caption"`
		Credit  string `json:"credit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid request")
		return
	}

	err := ctl.db.WithContext(c.Request.Context()).Model(&models.Media{}).
		Where("id = ?", id).Updates(map[string]any{
		"alt_kh": req.AltKh, "alt_en": req.AltEn,
		"caption": req.Caption, "credit": req.Credit,
	}).Error
	if err != nil {
		httpx.Internal(c, "Could not update the media record")
		return
	}
	httpx.NoData(c)
}

// Delete handles DELETE /api/media/:id, removing the object and its record.
func (ctl *MediaController) Delete(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid media id")
		return
	}
	ctx := c.Request.Context()

	var record models.Media
	if err := ctl.db.WithContext(ctx).First(&record, id).Error; err != nil {
		httpx.NotFound(c, "MEDIA_NOT_FOUND", "Media not found")
		return
	}

	// Remove the object first. If that fails, keep the row: an orphaned record
	// pointing at a live object is recoverable, a dangling URL in a published
	// article is not.
	if ctl.storage.Configured() {
		if err := ctl.storage.Delete(ctx, record.Key); err != nil {
			httpx.Internal(c, "Could not delete the file from storage")
			return
		}
	}
	if err := ctl.db.WithContext(ctx).Delete(&record).Error; err != nil {
		httpx.Internal(c, "File deleted but the record could not be removed")
		return
	}
	httpx.NoData(c)
}
