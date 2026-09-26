package auth

import (
	"testing"
	"time"

	"github.com/cambodia-fast-news/backend/internal/config"
)

func testService() *Service {
	return NewService(&config.Config{JWT: config.JWT{
		Secret:     "test-secret-that-is-long-enough-for-hmac",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 24 * time.Hour,
	}})
}

func TestIssueAndParse(t *testing.T) {
	svc := testService()

	pair, err := svc.Issue(42, "editor@example.com", "editor", 1)
	if err != nil {
		t.Fatalf("Issue() failed: %v", err)
	}

	claims, err := svc.Parse(pair.AccessToken, TokenAccess)
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if claims.UserID != 42 || claims.RoleSlug != "editor" || claims.TokenVersion != 1 {
		t.Errorf("claims did not round-trip: %+v", claims)
	}
}

func TestRefreshTokenCannotBeUsedAsAccessToken(t *testing.T) {
	// Without this check a long-lived refresh token would authenticate
	// requests for its whole lifetime.
	svc := testService()

	pair, err := svc.Issue(1, "a@example.com", "admin", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Parse(pair.RefreshToken, TokenAccess); err != ErrWrongType {
		t.Errorf("expected ErrWrongType, got %v", err)
	}
	if _, err := svc.Parse(pair.AccessToken, TokenRefresh); err != ErrWrongType {
		t.Errorf("expected ErrWrongType, got %v", err)
	}
}

func TestTokenSignedWithAnotherSecretIsRejected(t *testing.T) {
	issuer := testService()
	pair, err := issuer.Issue(1, "a@example.com", "admin", 1)
	if err != nil {
		t.Fatal(err)
	}

	other := NewService(&config.Config{JWT: config.JWT{
		Secret: "a-completely-different-signing-secret-x", AccessTTL: time.Minute, RefreshTTL: time.Hour,
	}})
	if _, err := other.Parse(pair.AccessToken, TokenAccess); err == nil {
		t.Error("a token signed with another secret was accepted")
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	svc := NewService(&config.Config{JWT: config.JWT{
		Secret: "test-secret-that-is-long-enough-for-hmac",
		// Already expired when issued.
		AccessTTL: -time.Minute, RefreshTTL: time.Hour,
	}})

	pair, err := svc.Issue(1, "a@example.com", "admin", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Parse(pair.AccessToken, TokenAccess); err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestGarbageTokenIsRejected(t *testing.T) {
	svc := testService()
	for _, bad := range []string{"", "not-a-token", "a.b.c", "Bearer x"} {
		if _, err := svc.Parse(bad, TokenAccess); err == nil {
			t.Errorf("Parse(%q) should have failed", bad)
		}
	}
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() failed: %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password was stored in plaintext")
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Error("the correct password was rejected")
	}
	if CheckPassword(hash, "wrong password") {
		t.Error("an incorrect password was accepted")
	}
}

func TestHashesAreSalted(t *testing.T) {
	a, _ := HashPassword("same-password")
	b, _ := HashPassword("same-password")
	if a == b {
		t.Error("identical passwords produced identical hashes; bcrypt salting is not working")
	}
}

func TestRoleGrantsDoNotLeakAdminPowers(t *testing.T) {
	grants := RoleGrants()

	for _, role := range []string{"journalist", "moderator", "seo-manager", "video-editor"} {
		for _, permission := range grants[role] {
			if permission == UsersManage || permission == RolesManage || permission == SettingsManage {
				t.Errorf("role %q should not hold %q", role, permission)
			}
		}
	}

	// A journalist writes but must not be able to publish their own copy.
	for _, permission := range grants["journalist"] {
		if permission == NewsPublish {
			t.Error("journalist must not hold news.publish; that is what the review step is for")
		}
	}
}
