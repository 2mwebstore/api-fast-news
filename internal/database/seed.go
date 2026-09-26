package database

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/cambodia-fast-news/backend/internal/auth"
	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/models"
)

// Options controls what a seed run installs.
type Options struct {
	// Demo adds sample authors, articles, videos and an ad campaign so a fresh
	// install has something to look at. Turn it off for production.
	Demo bool
}

// Result is the summary printed at the end of a run.
type Result struct {
	Steps         []Step
	AdminEmail    string
	AdminPassword string // set only when the seeder generated one
	// CustomisedRoles are roles whose permissions an operator edited by hand,
	// so this run left them alone. Reported so nobody wonders why a new
	// permission did not reach them.
	CustomisedRoles []string
}

// Step records what one stage of the seed did.
type Step struct {
	Name     string
	Created  int
	Existing int
}

// stage is one step of a seed run. Each returns how many records it created
// and how many it found already present.
type stage struct {
	name string
	run  func() (int, int, error)
}

// seeder carries the shared state for a run: a silent database session and the
// running report.
type seeder struct {
	db     *gorm.DB
	cfg    *config.Config
	result *Result
}

// Seed brings a database up to a working state: schema, reference data, the
// first administrator, and optionally demo content.
//
// It is idempotent. Running it twice creates nothing new and changes nothing
// that already exists, so it is safe to run on every deploy.
func Seed(db *gorm.DB, cfg *config.Config, opts Options) (*Result, error) {
	// Seeding issues hundreds of statements. Logging them all buries the one
	// line that matters — the generated administrator password.
	quiet := db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})

	s := &seeder{db: quiet, cfg: cfg, result: &Result{}}

	stages := []stage{
		{"Permissions", s.permissions},
		{"Roles", s.roles},
		{"Administrator", s.admin},
		{"Categories", s.categories},
		{"Ad slots", s.adSlots},
		{"Traffic routes", s.traffic},
		{"Pages", s.pages},
	}
	if opts.Demo {
		stages = append(stages,
			stage{"Authors (demo)", s.demoAuthors},
			stage{"Articles (demo)", s.demoArticles},
			stage{"Videos (demo)", s.demoVideos},
			stage{"Advertising (demo)", s.demoAdvertising},
		)
	}

	for _, stage := range stages {
		created, existing, err := stage.run()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", strings.ToLower(stage.name), err)
		}
		s.result.Steps = append(s.result.Steps, Step{Name: stage.name, Created: created, Existing: existing})
	}

	return s.result, nil
}

// Print writes the human-readable summary.
func (r *Result) Print() {
	fmt.Println()
	fmt.Println("  Database ready")
	fmt.Println("  ─────────────────────────────────────────────")
	for _, step := range r.Steps {
		status := fmt.Sprintf("%d new", step.Created)
		if step.Created == 0 {
			status = "already present"
		} else if step.Existing > 0 {
			status = fmt.Sprintf("%d new, %d existing", step.Created, step.Existing)
		}
		fmt.Printf("  %-22s %s\n", step.Name, status)
	}
	fmt.Println("  ─────────────────────────────────────────────")

	if len(r.CustomisedRoles) > 0 {
		fmt.Println()
		fmt.Printf("  Left untouched (edited in Admin → Roles): %s\n",
			strings.Join(r.CustomisedRoles, ", "))
		fmt.Println("  New permissions are not added to these roles automatically.")
	}

	if r.AdminPassword != "" {
		// Printed to stdout, never through the structured logger, so it does
		// not end up in a log aggregator.
		fmt.Println()
		fmt.Println("  Administrator account created")
		fmt.Printf("    Email:    %s\n", r.AdminEmail)
		fmt.Printf("    Password: %s\n", r.AdminPassword)
		fmt.Println()
		fmt.Println("  Save this now. It is not stored anywhere and will not be")
		fmt.Println("  shown again. Change it after your first sign-in.")
	} else if r.AdminEmail != "" {
		fmt.Printf("\n  Sign in at /admin as %s\n", r.AdminEmail)
	}
	fmt.Println()
}

// ── Reference data ──────────────────────────────────────────────────────

func (s *seeder) permissions() (int, int, error) {
	created, existing := 0, 0
	for _, p := range auth.All() {
		record := models.Permission{Name: p.Name, Group: p.Group, Description: p.Description}
		result := s.db.Where("name = ?", p.Name).
			Attrs(models.Permission{Group: p.Group, Description: p.Description}).
			FirstOrCreate(&record)
		if result.Error != nil {
			return created, existing, result.Error
		}
		if result.RowsAffected > 0 {
			created++
		} else {
			existing++
		}
	}
	return created, existing, nil
}

// roleDefinitions are the nine roles from §66.
var roleDefinitions = []struct{ Slug, Name, Description string }{
	{models.RoleSuperAdmin, "Super Admin", "Full access to everything, including roles and settings"},
	{models.RoleAdmin, "Admin", "Manages the newsroom, advertising and users"},
	{models.RoleEditor, "Editor", "Assigns, reviews and publishes news"},
	{models.RoleJournalist, "Journalist", "Writes and submits articles for review"},
	{models.RoleSportsEd, "Sports Editor", "Publishes sports and Kun Khmer coverage"},
	{models.RoleVideoEd, "Video Editor", "Produces and publishes video"},
	{models.RoleSEOManager, "SEO Manager", "Owns metadata, redirects and SEO health"},
	{models.RoleAdManager, "Advertisement Manager", "Manages campaigns, creatives and approvals"},
	{models.RoleModerator, "Moderator", "Moderates comments and citizen submissions"},
}

func (s *seeder) roles() (int, int, error) {
	grants := auth.RoleGrants()
	created, existing := 0, 0

	for _, def := range roleDefinitions {
		role := models.Role{Slug: def.Slug, Name: def.Name, Description: def.Description, IsSystem: true}
		result := s.db.Where("slug = ?", def.Slug).
			Attrs(models.Role{Name: def.Name, Description: def.Description, IsSystem: true}).
			FirstOrCreate(&role)
		if result.Error != nil {
			return created, existing, result.Error
		}
		if result.RowsAffected > 0 {
			created++
		} else {
			existing++
		}

		// Super Admin holds every permission implicitly via User.Can, so it
		// needs no explicit grants.
		names, ok := grants[def.Slug]
		if !ok {
			continue
		}
		// An operator who edited this role in Admin → Roles owns it now.
		// Reconciling would quietly undo their change on the next deploy.
		if role.PermissionsCustomised {
			s.result.CustomisedRoles = append(s.result.CustomisedRoles, def.Slug)
			continue
		}
		var permissions []models.Permission
		if err := s.db.Where("name IN ?", names).Find(&permissions).Error; err != nil {
			return created, existing, err
		}
		// Replace rather than append, so removing a permission from the
		// catalogue actually revokes it on the next run.
		if err := s.db.Model(&role).Association("Permissions").Replace(permissions); err != nil {
			return created, existing, err
		}
	}
	return created, existing, nil
}

// admin creates the first Super Admin.
//
// The password comes from ADMIN_PASSWORD when set; otherwise a strong random
// one is generated and reported once. No default password is ever baked in — a
// known first-boot credential is how self-hosted deployments get taken over.
func (s *seeder) admin() (int, int, error) {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_EMAIL")))
	if email == "" {
		email = "admin@cambodiafastnews.local"
	}
	s.result.AdminEmail = email

	var existing models.User
	if err := s.db.Where("email = ?", email).First(&existing).Error; err == nil {
		return 0, 1, nil
	}

	var role models.Role
	if err := s.db.Where("slug = ?", models.RoleSuperAdmin).First(&role).Error; err != nil {
		return 0, 0, fmt.Errorf("super admin role missing: %w", err)
	}

	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		buf := make([]byte, 18)
		if _, err := rand.Read(buf); err != nil {
			return 0, 0, fmt.Errorf("generate password: %w", err)
		}
		password = base64.RawURLEncoding.EncodeToString(buf)
		s.result.AdminPassword = password
	} else if len(password) < 12 {
		return 0, 0, fmt.Errorf("ADMIN_PASSWORD must be at least 12 characters")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return 0, 0, err
	}

	user := models.User{
		Name: "Newsroom Administrator", Email: email,
		PasswordHash: hash, IsActive: true, RoleID: role.ID, TokenVersion: 1,
	}
	if err := s.db.Create(&user).Error; err != nil {
		return 0, 0, err
	}
	return 1, 0, nil
}

// categorySeed mirrors the sections in §13. Parent is the slug of the parent
// section, empty for a top-level one.
type categorySeed struct {
	Slug, NameKh, NameEn, Icon, Color, Parent string
	Position                                  int
	InNav                                     bool
}

var categorySeeds = []categorySeed{
	{Slug: "cambodia", NameKh: "កម្ពុជា", NameEn: "Cambodia", Icon: "🇰🇭", Color: "#1E3A8A", Position: 1, InNav: true},
	{Slug: "phnom-penh", NameKh: "ភ្នំពេញ", NameEn: "Phnom Penh", Icon: "🏙", Color: "#2563EB", Position: 2, InNav: true, Parent: "cambodia"},
	{Slug: "politics", NameKh: "នយោបាយ", NameEn: "Politics", Icon: "🏛", Color: "#172554", Position: 3, InNav: true},
	{Slug: "world", NameKh: "អន្តរជាតិ", NameEn: "World", Icon: "🌏", Color: "#0F766E", Position: 4, InNav: true},
	{Slug: "business", NameKh: "សេដ្ឋកិច្ច", NameEn: "Business", Icon: "📈", Color: "#B45309", Position: 5, InNav: true},
	{Slug: "economy", NameKh: "សេដ្ឋកិច្ចជាតិ", NameEn: "Economy", Icon: "💹", Color: "#B45309", Position: 6, InNav: false, Parent: "business"},
	{Slug: "sports", NameKh: "កីឡា", NameEn: "Sports", Icon: "⚽", Color: "#16A34A", Position: 7, InNav: true},
	{Slug: "kun-khmer", NameKh: "គុនខ្មែរ", NameEn: "Kun Khmer", Icon: "🥊", Color: "#DC2626", Position: 8, InNav: true, Parent: "sports"},
	{Slug: "technology", NameKh: "បច្ចេកវិទ្យា", NameEn: "Technology", Icon: "💻", Color: "#4F46E5", Position: 9, InNav: true},
	{Slug: "entertainment", NameKh: "កម្សាន្ត", NameEn: "Entertainment", Icon: "🎬", Color: "#DB2777", Position: 10, InNav: true},
	{Slug: "lifestyle", NameKh: "ជីវិត", NameEn: "Lifestyle", Icon: "🌿", Color: "#059669", Position: 11, InNav: true},
	{Slug: "traffic", NameKh: "ចរាចរណ៍", NameEn: "Traffic", Icon: "🚦", Color: "#F59E0B", Position: 12, InNav: false},
	{Slug: "video", NameKh: "វីដេអូ", NameEn: "Video", Icon: "🎥", Color: "#7C3AED", Position: 13, InNav: true},
}

// restoreSoftDeleted brings back a row the seeder owns that was soft-deleted.
// Without this, FirstOrCreate cannot see the row but the INSERT still collides
// with its slug on the unique index, and the whole seed run fails.
func restoreSoftDeleted(db *gorm.DB, model any, slug string) (restored bool) {
	var count int64
	db.Unscoped().Model(model).
		Where("slug = ? AND deleted_at IS NOT NULL", slug).Count(&count)
	if count == 0 {
		return false
	}
	db.Unscoped().Model(model).Where("slug = ?", slug).
		UpdateColumn("deleted_at", nil)
	return true
}

func (s *seeder) categories() (int, int, error) {
	created, existing := 0, 0

	// Two passes: create everything, then link parents, so a child defined
	// before its parent still resolves.
	for _, seed := range categorySeeds {
		category := models.Category{
			Slug: seed.Slug, NameKh: seed.NameKh, NameEn: seed.NameEn,
			Icon: seed.Icon, Color: seed.Color, Position: seed.Position,
			InNav: seed.InNav, IsActive: true,
			DescKh: fmt.Sprintf("ព័ត៌មានថ្មីៗអំពី%s", seed.NameKh),
			DescEn: fmt.Sprintf("Latest %s news from Cambodia Fast News", seed.NameEn),
		}
		// A seeded section that somebody deleted comes back rather than failing
		// the run on its own slug.
		if restoreSoftDeleted(s.db, &models.Category{}, seed.Slug) {
			existing++
			continue
		}
		result := s.db.Where("slug = ?", seed.Slug).Attrs(category).FirstOrCreate(&category)
		if result.Error != nil {
			return created, existing, result.Error
		}
		if result.RowsAffected > 0 {
			created++
		} else {
			existing++
		}

		// Force the flags on every run, not just on create. Attrs() only
		// applies when a row is new, so a database seeded before the boolean
		// fix would keep the wrong nav visibility forever. UpdateColumns is
		// used deliberately: it writes false, where Updates() on a struct
		// would skip it as a zero value.
		err := s.db.Model(&models.Category{}).Where("id = ?", category.ID).
			UpdateColumns(map[string]any{"in_nav": seed.InNav, "is_active": true}).Error
		if err != nil {
			return created, existing, fmt.Errorf("sync category flags for %s: %w", seed.Slug, err)
		}
	}

	for _, seed := range categorySeeds {
		if seed.Parent == "" {
			continue
		}
		var parent, child models.Category
		if s.db.Where("slug = ?", seed.Parent).First(&parent).Error != nil {
			continue
		}
		if s.db.Where("slug = ?", seed.Slug).First(&child).Error != nil {
			continue
		}
		if child.ParentID == nil {
			if err := s.db.Model(&child).Update("parent_id", parent.ID).Error; err != nil {
				return created, existing, err
			}
		}
	}
	return created, existing, nil
}

// adSlotSeeds are the positions from §36 with the sizes from §37.
var adSlotSeeds = []models.AdvertisementSlot{
	{Position: models.SlotHomeTop, Label: "Homepage — top leaderboard", DesktopWidth: 970, DesktopHeight: 90, MobileWidth: 320, MobileHeight: 100},
	{Position: models.SlotHomeAfterHero, Label: "Homepage — after hero", DesktopWidth: 970, DesktopHeight: 250, MobileWidth: 300, MobileHeight: 250},
	{Position: models.SlotHomeSidebar, Label: "Homepage — sidebar", DesktopWidth: 300, DesktopHeight: 250, MobileWidth: 300, MobileHeight: 250},
	{Position: models.SlotHomeInFeed, Label: "Homepage — in feed", DesktopWidth: 728, DesktopHeight: 90, MobileWidth: 336, MobileHeight: 280},
	{Position: models.SlotArticleTop, Label: "Article — top", DesktopWidth: 728, DesktopHeight: 90, MobileWidth: 320, MobileHeight: 100},
	{Position: models.SlotArticleMiddle, Label: "Article — mid-body", DesktopWidth: 336, DesktopHeight: 280, MobileWidth: 336, MobileHeight: 280},
	{Position: models.SlotArticleSidebar, Label: "Article — sidebar", DesktopWidth: 300, DesktopHeight: 600, MobileWidth: 300, MobileHeight: 250},
	{Position: models.SlotArticleBottom, Label: "Article — bottom", DesktopWidth: 728, DesktopHeight: 90, MobileWidth: 336, MobileHeight: 280},
	{Position: models.SlotCategoryTop, Label: "Category — top", DesktopWidth: 970, DesktopHeight: 90, MobileWidth: 320, MobileHeight: 100},
	{Position: models.SlotCategorySidebar, Label: "Category — sidebar", DesktopWidth: 300, DesktopHeight: 250, MobileWidth: 300, MobileHeight: 250},
	// Sticky units ship disabled. §39 requires them to be dismissible and
	// optional, so turning one on is a deliberate decision, not a default.
	{Position: models.SlotMobileSticky, Label: "Mobile — sticky bottom", MobileWidth: 320, MobileHeight: 50, Description: "Dismissible. Disabled by default."},
	{Position: models.SlotDesktopSticky, Label: "Desktop — sticky", DesktopWidth: 728, DesktopHeight: 90, Description: "Dismissible. Disabled by default."},
}

func (s *seeder) adSlots() (int, int, error) {
	created, existing := 0, 0
	for _, slot := range adSlotSeeds {
		record := slot
		record.IsEnabled = slot.Position != models.SlotMobileSticky && slot.Position != models.SlotDesktopSticky

		result := s.db.Where("position = ?", slot.Position).Attrs(record).FirstOrCreate(&record)
		if result.Error != nil {
			return created, existing, result.Error
		}
		if result.RowsAffected > 0 {
			created++
		} else {
			existing++
		}
	}
	return created, existing, nil
}

// trafficSeeds are the routes on the board (§71). They start at "normal" with
// an explicit human source label — there is no live feed behind them.
var trafficSeeds = []struct{ RouteKh, RouteEn string }{
	{"ផ្លូវជាតិលេខ ១", "National Road 1"},
	{"ផ្លូវជាតិលេខ ៤", "National Road 4"},
	{"ផ្លូវជាតិលេខ ៥", "National Road 5"},
	{"មហាវិថីម៉ៅសេទុង", "Mao Tse Toung Boulevard"},
	{"មហាវិថីមុនីវង្ស", "Monivong Boulevard"},
	{"មហាវិថីរុស្ស៊ី", "Russian Boulevard"},
}

func (s *seeder) traffic() (int, int, error) {
	created, existing := 0, 0
	now := time.Now().UTC()

	for i, route := range trafficSeeds {
		record := models.TrafficStatus{
			RouteKh: route.RouteKh, RouteEn: route.RouteEn,
			Level: models.TrafficNormal, Position: i + 1, IsActive: true,
			SourceLabel: "CFN newsroom", ObservedAt: now,
		}
		result := s.db.Where("route_kh = ?", route.RouteKh).Attrs(record).FirstOrCreate(&record)
		if result.Error != nil {
			return created, existing, result.Error
		}
		if result.RowsAffected > 0 {
			created++
		} else {
			existing++
		}
	}
	return created, existing, nil
}
