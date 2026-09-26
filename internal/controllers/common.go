// Package controllers holds the HTTP handlers. Handlers parse and validate
// input, call a service, and shape the response; business rules live in
// services.
package controllers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/repositories"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// queryInt reads a bounded integer query parameter.
func queryInt(c *gin.Context, key string, fallback, min, max int) int {
	raw := c.Query(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// queryDate parses a YYYY-MM-DD parameter, returning nil when absent or
// malformed — a bad date filter should narrow nothing rather than 400.
func queryDate(c *gin.Context, key string) *time.Time {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

// queryBool returns a tri-state flag: nil when the parameter is absent.
func queryBool(c *gin.Context, key string) *bool {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}
	return &v
}

// paramUint reads a numeric path parameter.
func paramUint(c *gin.Context, key string) (uint, bool) {
	v, err := strconv.ParseUint(c.Param(key), 10, 64)
	if err != nil || v == 0 {
		return 0, false
	}
	return uint(v), true
}

// buildArticleFilter maps the documented query parameters (§10) onto the
// repository filter. Public callers never get IncludeNonPublic.
func buildArticleFilter(c *gin.Context) repositories.ArticleFilter {
	f := repositories.ArticleFilter{
		Category:    strings.TrimSpace(c.Query("category")),
		Subcategory: strings.TrimSpace(c.Query("subcategory")),
		Tag:         strings.TrimSpace(c.Query("tag")),
		AuthorSlug:  strings.TrimSpace(c.Query("author")),
		Search:      utils.Truncate(strings.TrimSpace(c.Query("search")), 120),
		Sort:        strings.TrimSpace(c.Query("sort")),
		Breaking:    queryBool(c, "breaking"),
		Featured:    queryBool(c, "featured"),
		From:        queryDate(c, "from"),
		To:          queryDate(c, "to"),
		Page:        queryInt(c, "page", 1, 1, 500),
		Limit:       queryInt(c, "limit", 20, 1, 60),
	}
	// A `to` date means "through the end of that day", not midnight.
	if f.To != nil {
		end := f.To.Add(24*time.Hour - time.Second)
		f.To = &end
	}
	f.Normalize()
	return f
}

// visitorHash derives a rotating, salted identifier for view deduplication.
// The salt includes the current date, so the value cannot be used to follow a
// reader across days (§43).
func visitorHash(c *gin.Context) string {
	ip := c.ClientIP()
	if cf := c.GetHeader("CF-Connecting-IP"); cf != "" {
		ip = cf
	}
	salt := time.Now().UTC().Format("2006-01-02") + "|" + c.GetHeader("User-Agent")
	return utils.HashIP(ip, salt)
}

// device is a shorthand for the middleware-derived form factor.
func device(c *gin.Context) string { return middleware.CurrentDevice(c) }
