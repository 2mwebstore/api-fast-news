package auth

import "testing"

// A grant naming a permission that does not exist silently grants nothing —
// the seeder looks it up by name and finds no row. A typo would therefore
// quietly remove access rather than fail loudly.
func TestEveryGrantReferencesARealPermission(t *testing.T) {
	known := make(map[string]bool)
	for _, p := range All() {
		known[p.Name] = true
	}

	for role, grants := range RoleGrants() {
		for _, name := range grants {
			if !known[name] {
				t.Errorf("role %q is granted %q, which is not in the permission catalogue", role, name)
			}
		}
	}
}

func TestPermissionCatalogueHasNoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range All() {
		if seen[p.Name] {
			t.Errorf("permission %q is declared twice", p.Name)
		}
		seen[p.Name] = true
		if p.Group == "" || p.Description == "" {
			t.Errorf("permission %q is missing a group or description", p.Name)
		}
	}
}

// union() copies rather than appending into a shared backing array. Without the
// copy, two roles built from the same base can overwrite each other's grants.
func TestUnionDoesNotLeakBetweenRoles(t *testing.T) {
	base := make([]string, 0, 16) // capacity deliberately exceeds length
	base = append(base, NewsView, NewsEdit)

	first := union(base, TelegramManage)
	second := union(base, TrafficManage)

	if contains(first, TrafficManage) {
		t.Error("the second role's permission leaked into the first")
	}
	if contains(second, TelegramManage) {
		t.Error("the first role's permission leaked into the second")
	}
	if len(first) != 3 || len(second) != 3 {
		t.Errorf("unexpected lengths: first=%d second=%d", len(first), len(second))
	}
}

func TestUnionDeduplicates(t *testing.T) {
	got := union([]string{NewsView, NewsEdit}, NewsView, AnalyticsView)
	if len(got) != 3 {
		t.Errorf("union() = %v, want 3 unique entries", got)
	}
}

// Separation of duties: the powers that change who can do what, or how the
// platform is configured, must not spread to editorial roles.
func TestPrivilegedPermissionsAreNotSpread(t *testing.T) {
	restricted := map[string]string{
		UsersManage:    "create and manage accounts",
		RolesManage:    "change what roles can do",
		SettingsManage: "change platform configuration",
	}

	allowed := map[string]map[string]bool{
		"admin": {UsersManage: true, SettingsManage: true},
	}

	for role, grants := range RoleGrants() {
		for _, name := range grants {
			if _, isRestricted := restricted[name]; !isRestricted {
				continue
			}
			if allowed[role][name] {
				continue
			}
			t.Errorf("role %q should not be able to %s (%s)", role, restricted[name], name)
		}
	}
}

// RolesManage belongs to Super Admin alone, which holds it implicitly through
// User.Can rather than through a grant.
func TestNoRoleIsGrantedRolesManage(t *testing.T) {
	for role, grants := range RoleGrants() {
		if contains(grants, RolesManage) {
			t.Errorf("role %q is granted roles.manage; that stays with Super Admin", role)
		}
	}
}

// A journalist writing their own copy must not also be able to publish it —
// that is the whole point of the review step (§16).
func TestJournalistCannotPublishOrDelete(t *testing.T) {
	grants := RoleGrants()["journalist"]
	for _, forbidden := range []string{NewsPublish, NewsReview, NewsDelete} {
		if contains(grants, forbidden) {
			t.Errorf("journalist must not hold %q", forbidden)
		}
	}
}

// Editing your own article's metadata is a different job from owning the
// site's redirect table.
func TestSiteWideSEOIsNotGrantedToSectionEditors(t *testing.T) {
	grants := RoleGrants()
	for _, role := range []string{"sports-editor", "journalist", "video-editor", "moderator"} {
		if contains(grants[role], SEOManage) {
			t.Errorf("role %q should not hold %q (site-wide redirects and health)", role, SEOManage)
		}
	}
	// But they do need to write a meta description on their own work.
	if !contains(grants["sports-editor"], SEOEdit) {
		t.Errorf("sports-editor needs %q to edit their articles' metadata", SEOEdit)
	}
}

// Roles that reach the admin screens need the permission those screens gate on,
// or the sidebar links to a page the API will refuse.
func TestEditorCanReachTheScreensItsNavOffers(t *testing.T) {
	grants := RoleGrants()["editor"]
	for _, needed := range []string{
		NewsView, NewsPublish, VideoManage, TelegramManage,
		CommentsModerate, TipsReview, AnalyticsView, SEOEdit, CategoriesManage,
	} {
		if !contains(grants, needed) {
			t.Errorf("editor is missing %q", needed)
		}
	}
}

// Reshaping the site's sections changes public URLs, so it belongs to the
// roles that own the site structure — not to everyone who can file a story.
func TestCategoriesManageIsLimitedToSectionOwners(t *testing.T) {
	expected := map[string]bool{"admin": true, "editor": true}

	for role, grants := range RoleGrants() {
		has := contains(grants, CategoriesManage)
		if has && !expected[role] {
			t.Errorf("role %q should not be able to add or delete sections", role)
		}
		if !has && expected[role] {
			t.Errorf("role %q needs categories.manage for the Sections screen its nav offers", role)
		}
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
