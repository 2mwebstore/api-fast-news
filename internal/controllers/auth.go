package controllers

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/auth"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// AuthController handles sign-in, refresh and the current-user endpoint.
type AuthController struct {
	db     *gorm.DB
	tokens *auth.Service
	audit  *services.AuditService
}

func NewAuthController(db *gorm.DB, tokens *auth.Service, audit *services.AuditService) *AuthController {
	return &AuthController{db: db, tokens: tokens, audit: audit}
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// Login handles POST /api/auth/login.
func (ctl *AuthController) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Email and password are required")
		return
	}

	ctx := c.Request.Context()
	var user models.User
	err := ctl.db.WithContext(ctx).Preload("Role.Permissions").Preload("Author").
		Where("email = ?", strings.ToLower(strings.TrimSpace(req.Email))).First(&user).Error

	// Wrong email and wrong password return the same message, so the endpoint
	// cannot be used to enumerate which accounts exist.
	if err != nil || !auth.CheckPassword(user.PasswordHash, req.Password) {
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			httpx.Internal(c, "Could not sign you in")
			return
		}
		httpx.Fail(c, 401, httpx.CodeInvalidCreds, "Email or password is incorrect")
		return
	}
	if !user.IsActive {
		httpx.Forbidden(c, "This account has been deactivated")
		return
	}

	roleSlug := ""
	if user.Role != nil {
		roleSlug = user.Role.Slug
	}
	pair, err := ctl.tokens.Issue(user.ID, user.Email, roleSlug, user.TokenVersion)
	if err != nil {
		httpx.Internal(c, "Could not issue a session")
		return
	}

	now := time.Now().UTC()
	ctl.db.WithContext(ctx).Model(&user).UpdateColumn("last_login_at", now)
	ctl.audit.RecordRequest(ctx, &user.ID, user.Email, "auth.login", "user", &user.ID,
		"signed in", c.ClientIP(), c.GetHeader("User-Agent"))

	httpx.OK(c, gin.H{"tokens": pair, "user": toUserRef(&user)})
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

// Refresh handles POST /api/auth/refresh.
func (ctl *AuthController) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A refresh token is required")
		return
	}

	claims, err := ctl.tokens.Parse(req.RefreshToken, auth.TokenRefresh)
	if err != nil {
		httpx.Fail(c, 401, httpx.CodeTokenInvalid, "Refresh token is invalid or expired")
		return
	}

	var user models.User
	if err := ctl.db.WithContext(c.Request.Context()).Preload("Role").
		First(&user, claims.UserID).Error; err != nil {
		httpx.Unauthorized(c, "Account no longer exists")
		return
	}
	if !user.IsActive || user.TokenVersion != claims.TokenVersion {
		httpx.Fail(c, 401, httpx.CodeTokenInvalid, "Session has been revoked")
		return
	}

	roleSlug := ""
	if user.Role != nil {
		roleSlug = user.Role.Slug
	}
	pair, err := ctl.tokens.Issue(user.ID, user.Email, roleSlug, user.TokenVersion)
	if err != nil {
		httpx.Internal(c, "Could not refresh the session")
		return
	}
	httpx.OK(c, gin.H{"tokens": pair})
}

// Me handles GET /api/auth/me.
func (ctl *AuthController) Me(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		httpx.Unauthorized(c, "Authentication required")
		return
	}
	httpx.OK(c, toUserRef(user))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required,min=12"`
}

// ChangePassword handles POST /api/auth/password. Bumping TokenVersion signs
// every other session out, which is the behaviour a password change should have.
func (ctl *AuthController) ChangePassword(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		httpx.Unauthorized(c, "Authentication required")
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A new password of at least 12 characters is required")
		return
	}
	if !auth.CheckPassword(user.PasswordHash, req.CurrentPassword) {
		httpx.Fail(c, 401, httpx.CodeInvalidCreds, "Current password is incorrect")
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		httpx.Internal(c, "Could not update your password")
		return
	}

	ctx := c.Request.Context()
	err = ctl.db.WithContext(ctx).Model(user).Updates(map[string]any{
		"password_hash": hash,
		"token_version": gorm.Expr("token_version + 1"),
	}).Error
	if err != nil {
		httpx.Internal(c, "Could not update your password")
		return
	}

	ctl.audit.RecordRequest(ctx, &user.ID, user.Email, "auth.password_change", "user", &user.ID,
		"changed password", c.ClientIP(), c.GetHeader("User-Agent"))
	httpx.OK(c, gin.H{"message": "Password updated. Other sessions have been signed out."})
}

// UserRef is the safe projection of a user — no hash, no token version.
type UserRef struct {
	ID          uint       `json:"id"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	AvatarURL   string     `json:"avatarUrl,omitempty"`
	RoleSlug    string     `json:"roleSlug"`
	RoleName    string     `json:"roleName"`
	Permissions []string   `json:"permissions"`
	Author      *AuthorRef `json:"author,omitempty"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

func toUserRef(u *models.User) UserRef {
	ref := UserRef{
		ID: u.ID, Name: u.Name, Email: u.Email,
		AvatarURL: u.AvatarURL, LastLoginAt: u.LastLoginAt,
		Permissions: []string{},
	}
	if u.Role != nil {
		ref.RoleSlug, ref.RoleName = u.Role.Slug, u.Role.Name
		// Super Admin holds every permission implicitly, so send the full list
		// rather than an empty one — the admin UI gates on this.
		if u.Role.Slug == models.RoleSuperAdmin {
			for _, p := range auth.All() {
				ref.Permissions = append(ref.Permissions, p.Name)
			}
		} else {
			for _, p := range u.Role.Permissions {
				ref.Permissions = append(ref.Permissions, p.Name)
			}
		}
	}
	if u.Author != nil {
		ref.Author = &AuthorRef{
			ID: u.Author.ID, Slug: u.Author.Slug, NameKh: u.Author.NameKh,
			NameEn: u.Author.NameEn, Title: u.Author.Title, PhotoURL: u.Author.PhotoURL,
		}
	}
	return ref
}
