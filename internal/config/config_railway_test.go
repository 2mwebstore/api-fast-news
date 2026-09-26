package config

import (
	"os"
	"testing"
)

// Managed platforms inject one URL and their own variable names; the loader has
// to understand both, and PORT has to win over APP_PORT or the platform routes
// traffic to a port nothing is listening on.
func TestPortPrefersPlatformPORT(t *testing.T) {
	t.Setenv("PORT", "4321")
	t.Setenv("APP_PORT", "8080")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.App.Port != "4321" {
		t.Errorf("port = %q, want the injected PORT 4321", cfg.App.Port)
	}
}

func TestAppPortStillWorksWithoutPORT(t *testing.T) {
	os.Unsetenv("PORT")
	t.Setenv("APP_PORT", "9090")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.App.Port != "9090" {
		t.Errorf("port = %q, want 9090", cfg.App.Port)
	}
}

func TestDatabaseURLOverridesDiscreteVars(t *testing.T) {
	t.Setenv("DATABASE_HOST", "ignored")
	t.Setenv("DATABASE_URL", "mysql://user:p%40ss@db.internal:3307/newsdb")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	db := cfg.DB
	if db.Host != "db.internal" || db.Port != "3307" || db.User != "user" || db.Name != "newsdb" {
		t.Errorf("parsed = %+v", db)
	}
	// A percent-encoded password must arrive decoded, or the connection fails
	// with a wrong-password error that looks like a credentials problem.
	if db.Password != "p@ss" {
		t.Errorf("password = %q, want the decoded p@ss", db.Password)
	}
}

func TestRailwayMySQLVarNames(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("MYSQL_URL")
	for _, key := range []string{"DATABASE_HOST", "DATABASE_PORT", "DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_NAME"} {
		os.Unsetenv(key)
	}
	t.Setenv("MYSQLHOST", "mysql.railway.internal")
	t.Setenv("MYSQLPORT", "3306")
	t.Setenv("MYSQLUSER", "root")
	t.Setenv("MYSQLPASSWORD", "secret")
	t.Setenv("MYSQLDATABASE", "railway")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DB.Host != "mysql.railway.internal" || cfg.DB.Name != "railway" || cfg.DB.User != "root" {
		t.Errorf("db = %+v", cfg.DB)
	}
}

func TestMalformedDatabaseURLIsReported(t *testing.T) {
	t.Setenv("DATABASE_URL", "mysql://")
	if _, err := Load(); err == nil {
		t.Error("a URL with no host should fail loudly, not fall back silently")
	}
}
