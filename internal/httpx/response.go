// Package httpx defines the single response envelope every endpoint uses (§78).
package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Envelope is the success shape: {"success": true, "data": {...}}.
type Envelope struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Meta    *Meta  `json:"meta,omitempty"`
}

// ErrorEnvelope is the failure shape: {"success": false, "message", "code"}.
type ErrorEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Code    string `json:"code"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Meta carries pagination for list endpoints.
type Meta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"totalPages"`
	HasMore    bool  `json:"hasMore"`
}

// NewMeta builds pagination metadata, guarding against a zero limit.
func NewMeta(page, limit int, total int64) *Meta {
	if limit < 1 {
		limit = 1
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	return &Meta{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasMore:    page < totalPages,
	}
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Success: true, Data: data})
}

func OKList(c *gin.Context, data any, meta *Meta) {
	c.JSON(http.StatusOK, Envelope{Success: true, Data: data, Meta: meta})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{Success: true, Data: data})
}

func NoData(c *gin.Context) {
	c.JSON(http.StatusOK, Envelope{Success: true})
}

// Fail writes an error envelope and aborts the handler chain.
func Fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, ErrorEnvelope{Success: false, Message: message, Code: code})
}

// FailFields is Fail with per-field validation detail.
func FailFields(c *gin.Context, status int, code, message string, fields map[string]string) {
	c.AbortWithStatusJSON(status, ErrorEnvelope{
		Success: false, Message: message, Code: code, Fields: fields,
	})
}

// Common failures, named so handlers read consistently.
func BadRequest(c *gin.Context, code, msg string)   { Fail(c, http.StatusBadRequest, code, msg) }
func Unauthorized(c *gin.Context, msg string)       { Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", msg) }
func Forbidden(c *gin.Context, msg string)          { Fail(c, http.StatusForbidden, "FORBIDDEN", msg) }
func NotFound(c *gin.Context, code, msg string)     { Fail(c, http.StatusNotFound, code, msg) }
func Conflict(c *gin.Context, code, msg string)     { Fail(c, http.StatusConflict, code, msg) }
func TooMany(c *gin.Context, msg string)            { Fail(c, http.StatusTooManyRequests, "RATE_LIMITED", msg) }
func Internal(c *gin.Context, msg string)           { Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", msg) }

// Error codes referenced across the API.
const (
	CodeArticleNotFound  = "ARTICLE_NOT_FOUND"
	CodeCategoryNotFound = "CATEGORY_NOT_FOUND"
	CodeAuthorNotFound   = "AUTHOR_NOT_FOUND"
	CodeVideoNotFound    = "VIDEO_NOT_FOUND"
	CodeAdNotFound       = "AD_NOT_FOUND"
	CodeValidation       = "VALIDATION_ERROR"
	CodeDuplicateSlug    = "DUPLICATE_SLUG"
	CodeInvalidCreds     = "INVALID_CREDENTIALS"
	CodeTokenExpired     = "TOKEN_EXPIRED"
	CodeTokenInvalid     = "TOKEN_INVALID"
	CodeAINotConfigured  = "AI_NOT_CONFIGURED"
	CodeUploadRejected   = "UPLOAD_REJECTED"
	CodeNotConfigured    = "NOT_CONFIGURED"
)
