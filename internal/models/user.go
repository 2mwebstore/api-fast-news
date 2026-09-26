package models

import "time"

// Permission is a single capability string such as "news.publish" (§67).
type Permission struct {
	Base
	Name        string `gorm:"size:64;uniqueIndex;not null" json:"name"`
	// Column is permission_group: GROUP is a reserved word in MySQL.
	Group       string `gorm:"column:permission_group;size:32;index" json:"group"`
	Description string `gorm:"size:255" json:"description"`
}

// Role bundles permissions and is assigned to users (§66).
type Role struct {
	Base
	Name        string       `gorm:"size:64;uniqueIndex;not null" json:"name"`
	Slug        string       `gorm:"size:64;uniqueIndex;not null" json:"slug"`
	Description string       `gorm:"size:255" json:"description"`
	IsSystem    bool         `gorm:"default:false" json:"isSystem"` // system roles cannot be deleted
	// PermissionsCustomised records that an operator edited this role's grants
	// by hand. The seeder reconciles a system role's permissions on every run,
	// which would silently revert that edit on the next deploy — so it skips
	// any role carrying this flag. No `default:` tag: GORM would treat an
	// explicit false as unset and write the default instead.
	PermissionsCustomised bool `gorm:"index" json:"permissionsCustomised"`
	Permissions []Permission `gorm:"many2many:role_permissions" json:"permissions,omitempty"`
}

type User struct {
	Base
	Name         string     `gorm:"size:128;not null" json:"name"`
	Email        string     `gorm:"size:191;uniqueIndex;not null" json:"email"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"`
	AvatarURL    string     `gorm:"size:512" json:"avatarUrl"`
	IsActive     bool       `gorm:"default:true;index" json:"isActive"`
	LastLoginAt  *time.Time `json:"lastLoginAt"`

	// TokenVersion is bumped on password change or forced logout; refresh
	// tokens carrying an older version are rejected, which is what makes
	// "sign out everywhere" possible without a token blacklist.
	TokenVersion int `gorm:"default:1;not null" json:"-"`

	RoleID uint  `gorm:"index;not null" json:"roleId"`
	Role   *Role `gorm:"foreignKey:RoleID" json:"role,omitempty"`

	// AuthorID links a staff account to its public byline, when it has one.
	AuthorID *uint   `gorm:"index" json:"authorId"`
	Author   *Author `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
}

// Can reports whether the user's role grants the named permission.
// Super Admin is granted everything implicitly.
func (u *User) Can(permission string) bool {
	if u == nil || u.Role == nil {
		return false
	}
	if u.Role.Slug == RoleSuperAdmin {
		return true
	}
	for _, p := range u.Role.Permissions {
		if p.Name == permission {
			return true
		}
	}
	return false
}

// Role slugs seeded by the installer (§66).
const (
	RoleSuperAdmin  = "super-admin"
	RoleAdmin       = "admin"
	RoleEditor      = "editor"
	RoleJournalist  = "journalist"
	RoleSportsEd    = "sports-editor"
	RoleVideoEd     = "video-editor"
	RoleSEOManager  = "seo-manager"
	RoleAdManager   = "advertisement-manager"
	RoleModerator   = "moderator"
)

// Author is the public byline shown on articles and at /author/[slug] (§18).
// It is deliberately separate from User: freelancers and wire bylines need a
// profile page without a login, and staff can leave without their archive
// losing attribution.
type Author struct {
	Base
	Slug      string `gorm:"size:191;uniqueIndex;not null" json:"slug"`
	NameKh    string `gorm:"size:128;not null" json:"nameKh"`
	NameEn    string `gorm:"size:128" json:"nameEn"`
	Title     string `gorm:"size:128" json:"title"` // e.g. "Senior Political Reporter"
	BioKh     string `gorm:"type:text" json:"bioKh"`
	BioEn     string `gorm:"type:text" json:"bioEn"`
	PhotoURL  string `gorm:"size:512" json:"photoUrl"`
	Email     string `gorm:"size:191" json:"email"`
	Facebook  string `gorm:"size:255" json:"facebook"`
	Telegram  string `gorm:"size:255" json:"telegram"`
	X         string `gorm:"size:255" json:"x"`
	IsActive  bool   `gorm:"default:true;index" json:"isActive"`
	ArticleCount int `gorm:"default:0" json:"articleCount"` // denormalised for listing pages
}

// AuditLog records every privileged action (§66, §79).
type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CreatedAt  time.Time `gorm:"index" json:"createdAt"`
	UserID     *uint     `gorm:"index" json:"userId"`
	UserEmail  string    `gorm:"size:191" json:"userEmail"`
	Action     string    `gorm:"size:64;index;not null" json:"action"` // e.g. "article.publish"
	EntityType string    `gorm:"size:64;index" json:"entityType"`
	EntityID   *uint     `gorm:"index" json:"entityId"`
	Summary    string    `gorm:"size:512" json:"summary"`
	Changes    JSONMap   `gorm:"type:json" json:"changes,omitempty"`
	IPAddress  string    `gorm:"size:64" json:"ipAddress"`
	UserAgent  string    `gorm:"size:255" json:"userAgent"`
}
