package auth

// Permission names (§67). Handlers reference these constants rather than raw
// strings so a typo is a compile error, not a silent authorisation hole.
const (
	NewsView    = "news.view"
	NewsCreate  = "news.create"
	NewsEdit    = "news.edit"
	NewsReview  = "news.review"
	NewsPublish = "news.publish"
	NewsDelete  = "news.delete"

	AdsView    = "ads.view"
	AdsCreate  = "ads.create"
	AdsEdit    = "ads.edit"
	AdsPublish = "ads.publish"
	AdsDelete  = "ads.delete"

	MediaUpload = "media.upload"
	MediaDelete = "media.delete"

	UsersManage = "users.manage"
	RolesManage = "roles.manage"

	// SEOEdit covers one article's own metadata — titles, descriptions, OG
	// tags. Anyone who can edit an article needs it.
	SEOEdit = "seo.edit"
	// SEOManage is site-wide: redirects and the SEO health dashboard. That is
	// a different job from writing a meta description, so it is a different
	// permission — otherwise every section editor inherits control over the
	// site's redirect table.
	SEOManage      = "seo.manage"
	AnalyticsView  = "analytics.view"
	TelegramManage = "telegram.manage"
	SettingsManage = "settings.manage"

	VideoManage    = "video.manage"
	// CategoriesManage covers the section structure itself. Separate from
	// news.edit: filing a story in a section is editorial work, adding or
	// removing sections reshapes the site and its URLs.
	CategoriesManage = "categories.manage"
	// PagesManage covers the policy pages, About and Contact. Separate from
	// news.edit because these are the newsroom's standing commitments — the
	// privacy policy and the editorial standards — not day-to-day copy.
	PagesManage = "pages.manage"
	CommentsModerate = "comments.moderate"
	TipsReview     = "tips.review"
	AIUse          = "ai.use"
	TrafficManage  = "traffic.manage"
)

// All returns every permission with its group and description, used to seed
// the permissions table.
func All() []struct{ Name, Group, Description string } {
	return []struct{ Name, Group, Description string }{
		{NewsView, "news", "View articles in the admin"},
		{NewsCreate, "news", "Create article drafts"},
		{NewsEdit, "news", "Edit articles"},
		{NewsReview, "news", "Review and approve submitted articles"},
		{NewsPublish, "news", "Publish, schedule and unpublish articles"},
		{NewsDelete, "news", "Delete articles"},
		{AdsView, "ads", "View advertisements and campaigns"},
		{AdsCreate, "ads", "Create advertisements and campaigns"},
		{AdsEdit, "ads", "Edit advertisements and campaigns"},
		{AdsPublish, "ads", "Approve and activate campaigns"},
		{AdsDelete, "ads", "Delete advertisements and campaigns"},
		{MediaUpload, "media", "Upload files to the media library"},
		{MediaDelete, "media", "Delete files from the media library"},
		{UsersManage, "users", "Create and manage user accounts"},
		{RolesManage, "users", "Create and manage roles and permissions"},
		{SEOEdit, "seo", "Edit an article's own SEO metadata"},
		{SEOManage, "seo", "Manage redirects and site-wide SEO health"},
		{AnalyticsView, "analytics", "View analytics dashboards"},
		{TelegramManage, "distribution", "Manage Telegram publishing"},
		{SettingsManage, "settings", "Change platform settings"},
		{VideoManage, "video", "Create and publish videos"},
		{CategoriesManage, "news", "Add, edit and remove news sections"},
		{PagesManage, "settings", "Edit the policy, About and Contact pages"},
		{CommentsModerate, "moderation", "Moderate comments and reports"},
		{TipsReview, "moderation", "Review citizen submissions"},
		{AIUse, "ai", "Use the AI newsroom assistants"},
		{TrafficManage, "traffic", "Update the traffic status board"},
	}
}

// RoleGrants maps each seeded role to the permissions it receives (§66).
// Super Admin is intentionally absent: User.Can grants it everything.
//
// Grants are built with union() rather than append() on a shared slice.
// append() on a base slice is a well-known Go trap: two roles appending to the
// same slice can write into the same backing array, so one role silently
// overwrites part of another's permissions. It happens to be safe while the
// base literal's length equals its capacity, which is exactly the kind of
// accident that breaks during a later refactor.
func RoleGrants() map[string][]string {
	// What any desk editor needs to run their section. Note SEOEdit, not
	// SEOManage: editing your own article's metadata is not the same as owning
	// the site's redirect table.
	editorial := []string{
		NewsView, NewsCreate, NewsEdit, NewsReview, NewsPublish,
		MediaUpload, AIUse, SEOEdit,
	}

	return map[string][]string{
		"admin": {
			NewsView, NewsCreate, NewsEdit, NewsReview, NewsPublish, NewsDelete,
			AdsView, AdsCreate, AdsEdit, AdsPublish, AdsDelete,
			MediaUpload, MediaDelete, UsersManage,
			SEOEdit, SEOManage, AnalyticsView,
			TelegramManage, SettingsManage, VideoManage, CommentsModerate,
			TipsReview, AIUse, TrafficManage, CategoriesManage, PagesManage,
			// RolesManage is deliberately absent: changing who can do what
			// stays with Super Admin.
		},

		// The senior editorial role: runs the desk, the channel and moderation,
		// and owns video.
		"editor": union(editorial,
			VideoManage, AnalyticsView, TelegramManage,
			CommentsModerate, TipsReview, TrafficManage, CategoriesManage,
		),

		// Writes and submits. No NewsPublish — that is what review is for.
		"journalist": {NewsView, NewsCreate, NewsEdit, MediaUpload, AIUse},

		// Same editorial powers, scoped by convention to sport. There is no
		// per-category permission yet, so this is a narrower job description
		// rather than a narrower grant.
		"sports-editor": union(editorial, AnalyticsView),

		"video-editor": {NewsView, VideoManage, MediaUpload, MediaDelete, AIUse},

		// Owns metadata and the site-wide SEO surface.
		"seo-manager": {NewsView, NewsEdit, SEOEdit, SEOManage, AnalyticsView, AIUse},

		"advertisement-manager": {
			AdsView, AdsCreate, AdsEdit, AdsPublish, AdsDelete,
			MediaUpload, AnalyticsView,
		},

		"moderator": {NewsView, CommentsModerate, TipsReview},
	}
}

// union copies the base and adds extras, skipping duplicates. The copy is the
// point: it keeps one role's grants from reaching into another's.
func union(base []string, extra ...string) []string {
	out := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]bool, len(base)+len(extra))

	for _, name := range append(append([]string{}, base...), extra...) {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
