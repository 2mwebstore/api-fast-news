// Package middleware holds the Gin middleware chain: auth, RBAC, rate limits,
// CORS, logging and panic recovery.
package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/auth"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/models"
)

const contextUserKey = "cfn.user"

// RequireAuth validates the bearer token and loads the user with its role and
// permissions. The user is re-read on every request rather than trusted from
// the token, so a deactivated account or a changed role takes effect at once.
func RequireAuth(tokens *auth.Service, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := bearerToken(c)
		if raw == "" {
			httpx.Unauthorized(c, "Authentication required")
			return
		}

		claims, err := tokens.Parse(raw, auth.TokenAccess)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrTokenExpired):
				httpx.Fail(c, 401, httpx.CodeTokenExpired, "Access token expired")
			default:
				httpx.Fail(c, 401, httpx.CodeTokenInvalid, "Access token invalid")
			}
			return
		}

		var user models.User
		err = db.Preload("Role.Permissions").Preload("Author").
			First(&user, claims.UserID).Error
		if err != nil {
			httpx.Unauthorized(c, "Account no longer exists")
			return
		}
		if !user.IsActive {
			httpx.Forbidden(c, "Account is deactivated")
			return
		}
		// A bumped TokenVersion invalidates every token issued before a
		// password change or forced sign-out.
		if user.TokenVersion != claims.TokenVersion {
			httpx.Fail(c, 401, httpx.CodeTokenInvalid, "Session has been revoked")
			return
		}

		c.Set(contextUserKey, &user)
		c.Next()
	}
}

// OptionalAuth attaches the user when a valid token is present but never
// rejects the request. Used on public endpoints that render differently for
// signed-in staff (e.g. previewing a draft).
func OptionalAuth(tokens *auth.Service, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if raw := bearerToken(c); raw != "" {
			if claims, err := tokens.Parse(raw, auth.TokenAccess); err == nil {
				var user models.User
				if db.Preload("Role.Permissions").First(&user, claims.UserID).Error == nil &&
					user.IsActive && user.TokenVersion == claims.TokenVersion {
					c.Set(contextUserKey, &user)
				}
			}
		}
		c.Next()
	}
}

// RequirePermission gates a route on a single permission (§67).
func RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := CurrentUser(c)
		if user == nil {
			httpx.Unauthorized(c, "Authentication required")
			return
		}
		if !user.Can(permission) {
			httpx.Forbidden(c, "You do not have permission to perform this action")
			return
		}
		c.Next()
	}
}

// RequireAnyPermission passes when the user holds at least one of the listed
// permissions — used where several roles legitimately reach the same screen.
func RequireAnyPermission(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := CurrentUser(c)
		if user == nil {
			httpx.Unauthorized(c, "Authentication required")
			return
		}
		for _, p := range permissions {
			if user.Can(p) {
				c.Next()
				return
			}
		}
		httpx.Forbidden(c, "You do not have permission to perform this action")
	}
}

// CurrentUser returns the authenticated user, or nil on a public request.
func CurrentUser(c *gin.Context) *models.User {
	v, ok := c.Get(contextUserKey)
	if !ok {
		return nil
	}
	user, ok := v.(*models.User)
	if !ok {
		return nil
	}
	return user
}

// CurrentUserID returns the signed-in user's ID, or nil.
func CurrentUserID(c *gin.Context) *uint {
	if u := CurrentUser(c); u != nil {
		return &u.ID
	}
	return nil
}

func bearerToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
