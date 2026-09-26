package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/httpx"
)

// CORS allows only the configured origins. A wildcard is never echoed back
// when credentials are in play.
func CORS(cfg *config.Config) gin.HandlerFunc {
	allowed := make(map[string]bool, len(cfg.CORS.AllowedOrigins))
	for _, o := range cfg.CORS.AllowedOrigins {
		allowed[strings.TrimRight(o, "/")] = true
	}

	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		if origin != "" && allowed[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, X-CFN-Device")
			c.Header("Access-Control-Expose-Headers", "X-RateLimit-Limit, X-RateLimit-Remaining, Retry-After")
			c.Header("Access-Control-Max-Age", "86400")
			c.Header("Vary", "Origin")
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

// SecurityHeaders sets the defensive headers that do not depend on the page
// being rendered (§74). The frontend owns its own CSP.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	}
}

// Logger emits one structured line per request. It deliberately logs no
// request bodies and no Authorization header (§79).
func Logger(resolver *IPResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", resolver.ClientIP(c),
		}
		if user := CurrentUser(c); user != nil {
			attrs = append(attrs, "user_id", user.ID)
		}

		switch {
		case c.Writer.Status() >= 500:
			slog.Error("request failed", attrs...)
		case c.Writer.Status() >= 400:
			slog.Warn("request rejected", attrs...)
		default:
			slog.Info("request", attrs...)
		}
	}
}

// Recovery converts a panic into a 500 in the standard envelope, logging the
// stack without leaking it to the client.
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		slog.Error("panic recovered",
			"error", recovered,
			"path", c.Request.URL.Path,
			"method", c.Request.Method,
		)
		httpx.Internal(c, "An unexpected error occurred")
	})
}

// Device classifies the request as mobile or desktop so ad selection and
// analytics can bucket it (§37). The client may assert X-CFN-Device; otherwise
// the User-Agent is used.
func Device() gin.HandlerFunc {
	mobileHints := []string{"Mobi", "Android", "iPhone", "iPod", "IEMobile", "Opera Mini"}
	return func(c *gin.Context) {
		device := strings.ToLower(c.GetHeader("X-CFN-Device"))
		if device != "mobile" && device != "desktop" {
			device = "desktop"
			ua := c.GetHeader("User-Agent")
			for _, hint := range mobileHints {
				if strings.Contains(ua, hint) {
					device = "mobile"
					break
				}
			}
		}
		c.Set("cfn.device", device)
		c.Next()
	}
}

// CurrentDevice returns "mobile" or "desktop" for the request.
func CurrentDevice(c *gin.Context) string {
	if v, ok := c.Get("cfn.device"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "desktop"
}
