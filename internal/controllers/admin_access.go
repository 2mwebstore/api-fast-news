package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/cambodia-fast-news/backend/internal/auth"
	"github.com/cambodia-fast-news/backend/internal/httpx"
	"github.com/cambodia-fast-news/backend/internal/middleware"
	"github.com/cambodia-fast-news/backend/internal/models"
	"github.com/cambodia-fast-news/backend/internal/services"
)

// AdminAccessController exposes roles, permissions and user accounts (§66, §67).
//
// Two different writes live here, with deliberately different gates.
//
// Assigning a role to a person is routine newsroom administration, so it sits
// behind users.manage. Rewriting what a role can do is a security boundary — an
// Advertisement Manager granting themselves users.manage would be one careless
// click — so it sits behind roles.manage, which only Super Admin holds, and it
// refuses to touch the Super Admin role or to hand out roles.manage itself.
// See SetRolePermissions for the full list of guards.
type AdminAccessController struct {
	db    *gorm.DB
	audit *services.AuditService
}

func NewAdminAccessController(db *gorm.DB, audit *services.AuditService) *AdminAccessController {
	return &AdminAccessController{db: db, audit: audit}
}

// Roles handles GET /api/admin/roles.
func (ctl *AdminAccessController) Roles(c *gin.Context) {
	var roles []models.Role
	err := ctl.db.WithContext(c.Request.Context()).
		Preload("Permissions").Order("id ASC").Find(&roles).Error
	if err != nil {
		httpx.Internal(c, "Could not load roles")
		return
	}

	// Count holders so an operator can see which roles are actually in use
	// before wondering whether one is safe to stop assigning.
	type roleCount struct {
		RoleID uint
		Total  int
	}
	var counts []roleCount
	ctl.db.WithContext(c.Request.Context()).Model(&models.User{}).
		Select("role_id, COUNT(*) AS total").Group("role_id").Scan(&counts)

	byRole := make(map[uint]int, len(counts))
	for _, row := range counts {
		byRole[row.RoleID] = row.Total
	}

	type rolePayload struct {
		ID          uint     `json:"id"`
		Slug        string   `json:"slug"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		IsSystem    bool     `json:"isSystem"`
		UserCount   int      `json:"userCount"`
		Permissions []string `json:"permissions"`
		// HoldsEverything marks Super Admin, whose access is implicit rather
		// than a list of grants — showing it with zero permissions would read
		// as "can do nothing", which is the opposite of the truth.
		HoldsEverything bool `json:"holdsEverything"`
		// Customised tells the UI this role no longer tracks the defaults in
		// code, so the seeder will leave it alone from now on.
		Customised bool `json:"permissionsCustomised"`
	}

	out := make([]rolePayload, 0, len(roles))
	for i := range roles {
		role := roles[i]
		names := make([]string, 0, len(role.Permissions))
		for _, p := range role.Permissions {
			names = append(names, p.Name)
		}

		payload := rolePayload{
			ID: role.ID, Slug: role.Slug, Name: role.Name,
			Description: role.Description, IsSystem: role.IsSystem,
			UserCount: byRole[role.ID], Permissions: names,
			HoldsEverything: role.Slug == models.RoleSuperAdmin,
			Customised:      role.PermissionsCustomised,
		}
		if payload.HoldsEverything {
			for _, p := range auth.All() {
				payload.Permissions = append(payload.Permissions, p.Name)
			}
		}
		out = append(out, payload)
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, out)
}

// Permissions handles GET /api/admin/permissions, grouped for display.
func (ctl *AdminAccessController) Permissions(c *gin.Context) {
	var permissions []models.Permission
	err := ctl.db.WithContext(c.Request.Context()).
		Order("permission_group ASC, name ASC").Find(&permissions).Error
	if err != nil {
		httpx.Internal(c, "Could not load permissions")
		return
	}

	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	type group struct {
		Group   string  `json:"group"`
		Entries []entry `json:"entries"`
	}

	order := []string{}
	byGroup := map[string][]entry{}
	for _, p := range permissions {
		name := p.Group
		if name == "" {
			name = "other"
		}
		if _, seen := byGroup[name]; !seen {
			order = append(order, name)
		}
		byGroup[name] = append(byGroup[name], entry{Name: p.Name, Description: p.Description})
	}

	out := make([]group, 0, len(order))
	for _, name := range order {
		out = append(out, group{Group: name, Entries: byGroup[name]})
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OK(c, out)
}

// Users handles GET /api/admin/users.
func (ctl *AdminAccessController) Users(c *gin.Context) {
	page := queryInt(c, "page", 1, 1, 500)
	limit := queryInt(c, "limit", 25, 1, 100)

	q := ctl.db.WithContext(c.Request.Context()).Model(&models.User{})
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + search + "%"
		q = q.Where("name LIKE ? OR email LIKE ?", like, like)
	}
	if role := c.Query("role"); role != "" {
		q = q.Where("role_id IN (SELECT id FROM roles WHERE slug = ?)", role)
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		httpx.Internal(c, "Could not load users")
		return
	}

	var users []models.User
	err := q.Preload("Role").Preload("Author").
		Order("created_at ASC").Limit(limit).Offset((page - 1) * limit).Find(&users).Error
	if err != nil {
		httpx.Internal(c, "Could not load users")
		return
	}

	// toUserRef omits the password hash and token version by construction.
	out := make([]UserRef, 0, len(users))
	for i := range users {
		out = append(out, toUserRef(&users[i]))
	}

	c.Header("Cache-Control", "private, no-store")
	httpx.OKList(c, out, httpx.NewMeta(page, limit, total))
}

// SetUserRole handles PUT /api/admin/users/:id/role.
func (ctl *AdminAccessController) SetUserRole(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid user id")
		return
	}

	var req struct {
		RoleSlug string `json:"roleSlug" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A role is required")
		return
	}

	ctx := c.Request.Context()
	actor := middleware.CurrentUser(c)

	var role models.Role
	if err := ctl.db.WithContext(ctx).Where("slug = ?", req.RoleSlug).First(&role).Error; err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "Unknown role")
		return
	}

	// Only a Super Admin may mint another one. Otherwise anyone holding
	// users.manage could promote themselves past every other check.
	if role.Slug == models.RoleSuperAdmin && (actor == nil || actor.Role == nil || actor.Role.Slug != models.RoleSuperAdmin) {
		httpx.Forbidden(c, "Only a Super Admin can grant the Super Admin role")
		return
	}

	var target models.User
	if err := ctl.db.WithContext(ctx).Preload("Role").First(&target, id).Error; err != nil {
		httpx.NotFound(c, "USER_NOT_FOUND", "User not found")
		return
	}

	// Refuse to remove the last Super Admin: that would lock everyone out of
	// role management permanently, with no way back through the UI.
	if target.Role != nil && target.Role.Slug == models.RoleSuperAdmin && role.Slug != models.RoleSuperAdmin {
		var remaining int64
		ctl.db.WithContext(ctx).Model(&models.User{}).
			Where("role_id = ? AND is_active = ? AND id <> ?", target.RoleID, true, target.ID).
			Count(&remaining)
		if remaining == 0 {
			httpx.BadRequest(c, "LAST_SUPER_ADMIN",
				"This is the only Super Admin. Promote someone else first.")
			return
		}
	}

	previous := ""
	if target.Role != nil {
		previous = target.Role.Slug
	}

	if err := ctl.db.WithContext(ctx).Model(&target).Update("role_id", role.ID).Error; err != nil {
		httpx.Internal(c, "Could not change the role")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "users.set_role", "user", &target.ID,
		target.Email+": "+previous+" -> "+role.Slug, nil)

	httpx.OK(c, gin.H{"id": target.ID, "roleSlug": role.Slug, "previousRoleSlug": previous})
}

// SetUserActive handles PUT /api/admin/users/:id/active.
func (ctl *AdminAccessController) SetUserActive(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid user id")
		return
	}

	var req struct {
		IsActive *bool `json:"isActive" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "isActive is required")
		return
	}

	ctx := c.Request.Context()
	actor := middleware.CurrentUser(c)

	// Locking yourself out is never the intent.
	if actor != nil && actor.ID == id && !*req.IsActive {
		httpx.BadRequest(c, "CANNOT_DEACTIVATE_SELF", "You cannot deactivate your own account")
		return
	}

	var target models.User
	if err := ctl.db.WithContext(ctx).Preload("Role").First(&target, id).Error; err != nil {
		httpx.NotFound(c, "USER_NOT_FOUND", "User not found")
		return
	}

	if !*req.IsActive && target.Role != nil && target.Role.Slug == models.RoleSuperAdmin {
		var remaining int64
		ctl.db.WithContext(ctx).Model(&models.User{}).
			Where("role_id = ? AND is_active = ? AND id <> ?", target.RoleID, true, target.ID).
			Count(&remaining)
		if remaining == 0 {
			httpx.BadRequest(c, "LAST_SUPER_ADMIN", "This is the only active Super Admin")
			return
		}
	}

	// Bumping token_version signs the account out everywhere immediately,
	// rather than leaving a live session for up to the access-token lifetime.
	updates := map[string]any{"is_active": *req.IsActive}
	if !*req.IsActive {
		updates["token_version"] = gorm.Expr("token_version + 1")
	}
	if err := ctl.db.WithContext(ctx).Model(&target).Updates(updates).Error; err != nil {
		httpx.Internal(c, "Could not update the account")
		return
	}

	action := "users.activate"
	if !*req.IsActive {
		action = "users.deactivate"
	}
	ctl.audit.Record(ctx, middleware.CurrentUserID(c), action, "user", &target.ID, target.Email, nil)

	httpx.OK(c, gin.H{"id": target.ID, "isActive": *req.IsActive})
}

// SetRolePermissions handles PUT /api/admin/roles/:id/permissions.
//
// Editing what a role can do is the most dangerous write in the admin: it is
// the one action that can hand out every other action. So it is fenced in:
//
//   - Super Admin only (RolesManage, which no role is granted — it comes from
//     the implicit Super Admin check in User.Can). An operator with
//     users.manage can move people between roles but cannot redefine them.
//   - The Super Admin role itself is not editable. Its access is implicit, so
//     a grant list would be fiction, and a "revoke" there could lock the
//     platform out of its own administration.
//   - roles.manage can never be granted. Handing it to another role would make
//     the Super Admin gate above decorative.
//   - Only names in the compiled catalogue are accepted. A typo would
//     otherwise be stored as a grant that silently matches nothing.
//
// Permissions are read from the database on every request (auth middleware
// preloads Role.Permissions), so a revocation takes effect on the affected
// users' next call rather than whenever their access token happens to expire.
func (ctl *AdminAccessController) SetRolePermissions(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid role id")
		return
	}

	var req struct {
		// Not omitempty-friendly: an empty list is a legitimate request that
		// strips a role back to nothing, so the field must be present.
		Permissions *[]string `json:"permissions" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, httpx.CodeValidation, "A list of permissions is required")
		return
	}

	ctx := c.Request.Context()
	var role models.Role
	if err := ctl.db.WithContext(ctx).Preload("Permissions").First(&role, id).Error; err != nil {
		httpx.NotFound(c, "ROLE_NOT_FOUND", "Role not found")
		return
	}
	if role.Slug == models.RoleSuperAdmin {
		httpx.Forbidden(c, "The Super Admin role holds every permission implicitly and cannot be edited.")
		return
	}

	// Deduplicate and validate against the compiled catalogue in one pass.
	catalogue := make(map[string]bool, len(auth.All()))
	for _, p := range auth.All() {
		catalogue[p.Name] = true
	}

	seen := map[string]bool{}
	wanted := make([]string, 0, len(*req.Permissions))
	for _, name := range *req.Permissions {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		if !catalogue[name] {
			httpx.BadRequest(c, httpx.CodeValidation,
				fmt.Sprintf("%q is not a known permission", name))
			return
		}
		if name == auth.RolesManage {
			httpx.Forbidden(c,
				"roles.manage cannot be granted to a role. It stays with Super Admin, otherwise any holder could grant themselves everything.")
			return
		}
		seen[name] = true
		wanted = append(wanted, name)
	}

	var permissions []models.Permission
	if len(wanted) > 0 {
		if err := ctl.db.WithContext(ctx).Where("name IN ?", wanted).Find(&permissions).Error; err != nil {
			httpx.Internal(c, "Could not load the selected permissions")
			return
		}
	}

	// The diff, for the audit trail. "Changed the editor role" is useless six
	// months later; "granted ads.approve, revoked news.delete" is not.
	before := map[string]bool{}
	for _, p := range role.Permissions {
		before[p.Name] = true
	}
	added, removed := []string{}, []string{}
	for _, name := range wanted {
		if !before[name] {
			added = append(added, name)
		}
	}
	for name := range before {
		if !seen[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)

	err := ctl.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&role).Association("Permissions").Replace(permissions); err != nil {
			return err
		}
		// UpdateColumn, not Save: a bool that GORM considers a zero value is
		// exactly how flags like this fail to persist.
		return tx.Model(&models.Role{}).Where("id = ?", role.ID).
			UpdateColumn("permissions_customised", true).Error
	})
	if err != nil {
		httpx.Internal(c, "Could not save the role's permissions")
		return
	}

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "role.permissions.set", "role", &role.ID,
		fmt.Sprintf("%s: +%d, -%d", role.Slug, len(added), len(removed)),
		models.JSONMap{"granted": added, "revoked": removed, "total": len(wanted)},
	)

	httpx.OK(c, gin.H{
		"id": role.ID, "slug": role.Slug,
		"permissions": wanted, "granted": added, "revoked": removed,
		"permissionsCustomised": true,
		"userCount":             ctl.usersInRole(ctx, role.ID),
	})
}

// usersInRole reports how many accounts the change just affected, so the UI can
// say "3 accounts updated" rather than leaving the operator to guess.
func (ctl *AdminAccessController) usersInRole(ctx context.Context, roleID uint) int64 {
	var total int64
	ctl.db.WithContext(ctx).Model(&models.User{}).Where("role_id = ?", roleID).Count(&total)
	return total
}

// ResetRolePermissions handles DELETE /api/admin/roles/:id/permissions.
//
// Puts a role back to the grants defined in auth.RoleGrants and clears the
// customised flag, so the seeder resumes keeping it in step with the code. This
// is the only way back: once a role is edited by hand the seeder deliberately
// stops touching it, and without a reset an operator who experimented would be
// stranded with no record of what the defaults were.
func (ctl *AdminAccessController) ResetRolePermissions(c *gin.Context) {
	id, ok := paramUint(c, "id")
	if !ok {
		httpx.BadRequest(c, httpx.CodeValidation, "Invalid role id")
		return
	}

	ctx := c.Request.Context()
	var role models.Role
	if err := ctl.db.WithContext(ctx).Preload("Permissions").First(&role, id).Error; err != nil {
		httpx.NotFound(c, "ROLE_NOT_FOUND", "Role not found")
		return
	}
	if role.Slug == models.RoleSuperAdmin {
		httpx.Forbidden(c, "The Super Admin role holds every permission implicitly and cannot be edited.")
		return
	}

	defaults, ok := auth.RoleGrants()[role.Slug]
	if !ok {
		httpx.BadRequest(c, "NO_DEFAULTS",
			"This role has no defaults in code, so there is nothing to reset to.")
		return
	}

	var permissions []models.Permission
	if err := ctl.db.WithContext(ctx).Where("name IN ?", defaults).Find(&permissions).Error; err != nil {
		httpx.Internal(c, "Could not load the default permissions")
		return
	}

	err := ctl.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&role).Association("Permissions").Replace(permissions); err != nil {
			return err
		}
		return tx.Model(&models.Role{}).Where("id = ?", role.ID).
			UpdateColumn("permissions_customised", false).Error
	})
	if err != nil {
		httpx.Internal(c, "Could not reset the role")
		return
	}

	names := make([]string, 0, len(permissions))
	for _, p := range permissions {
		names = append(names, p.Name)
	}
	sort.Strings(names)

	ctl.audit.Record(ctx, middleware.CurrentUserID(c), "role.permissions.reset", "role", &role.ID,
		fmt.Sprintf("%s reset to defaults (%d permissions)", role.Slug, len(names)),
		models.JSONMap{"permissions": names},
	)

	httpx.OK(c, gin.H{
		"id": role.ID, "slug": role.Slug, "permissions": names,
		"permissionsCustomised": false,
		"userCount":             ctl.usersInRole(ctx, role.ID),
	})
}
