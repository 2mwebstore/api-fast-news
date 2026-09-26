package models

import (
	"testing"
	"time"
)

func TestIsLiveBreakingRespectsTheWindow(t *testing.T) {
	now := time.Now().UTC()
	ago := now.Add(-time.Hour)
	soon := now.Add(time.Hour)
	earlier := now.Add(-30 * time.Minute)

	cases := []struct {
		name string
		a    Article
		want bool
	}{
		{"flagged and started", Article{IsBreaking: true, BreakingStartedAt: &ago}, true},
		{"not flagged", Article{IsBreaking: false, BreakingStartedAt: &ago}, false},
		{"flagged but no start time", Article{IsBreaking: true}, false},
		{"start time in the future", Article{IsBreaking: true, BreakingStartedAt: &soon}, false},
		{"already ended", Article{IsBreaking: true, BreakingStartedAt: &ago, BreakingEndedAt: &earlier}, false},
		{"ends later", Article{IsBreaking: true, BreakingStartedAt: &ago, BreakingEndedAt: &soon}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.IsLiveBreaking(now); got != tc.want {
				t.Errorf("IsLiveBreaking() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasEnglishGatesHreflang(t *testing.T) {
	// hreflang must only be emitted when a real translation exists (§54).
	full := Article{TitleEn: "Title", ContentEn: "<p>Body</p>"}
	if !full.HasEnglish() {
		t.Error("a fully translated article should report HasEnglish")
	}

	for name, a := range map[string]Article{
		"title only":   {TitleEn: "Title"},
		"content only": {ContentEn: "<p>Body</p>"},
		"neither":      {},
	} {
		if a.HasEnglish() {
			t.Errorf("%s must not claim an English version exists", name)
		}
	}
}

func TestUserCanChecksPermissions(t *testing.T) {
	editor := &User{Role: &Role{
		Slug:        RoleEditor,
		Permissions: []Permission{{Name: "news.publish"}, {Name: "news.edit"}},
	}}
	if !editor.Can("news.publish") {
		t.Error("editor should hold news.publish")
	}
	if editor.Can("users.manage") {
		t.Error("editor must not hold users.manage")
	}

	// Super Admin is granted everything implicitly rather than by enumeration.
	superAdmin := &User{Role: &Role{Slug: RoleSuperAdmin}}
	if !superAdmin.Can("anything.at.all") {
		t.Error("super admin should hold every permission")
	}

	var nilUser *User
	if nilUser.Can("news.view") {
		t.Error("an unauthenticated user must hold no permissions")
	}
	if (&User{}).Can("news.view") {
		t.Error("a user with no role must hold no permissions")
	}
}

func TestBreakingNewsIsActive(t *testing.T) {
	now := time.Now().UTC()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)

	if !(&BreakingNews{StartedAt: past}).IsActive(now) {
		t.Error("a started alert with no end should be active")
	}
	if (&BreakingNews{StartedAt: future}).IsActive(now) {
		t.Error("an alert scheduled for later must not be active")
	}
	if (&BreakingNews{StartedAt: past, EndedAt: &past}).IsActive(now) {
		t.Error("a retired alert must not be active")
	}
	if !(&BreakingNews{StartedAt: past, EndedAt: &future}).IsActive(now) {
		t.Error("an alert that ends later should still be active")
	}
}

func TestArticleStatusIsPublic(t *testing.T) {
	if !StatusPublished.IsPublic() {
		t.Error("published articles must be public")
	}
	for _, s := range []ArticleStatus{
		StatusDraft, StatusReview, StatusApproved,
		StatusScheduled, StatusArchived, StatusRejected,
	} {
		if s.IsPublic() {
			t.Errorf("%s must not be served to readers", s)
		}
	}
}

// AllModels drives AutoMigrate, so a model missing from it would never get a
// table. This guards the count against a silent omission.
func TestAllModelsIsComplete(t *testing.T) {
	if got := len(AllModels()); got < 35 {
		t.Errorf("AllModels() returned %d models, which looks short — did a new model miss the list?", got)
	}
}
