// Package config loads all runtime configuration from the environment.
// Nothing else in the codebase reads os.Getenv directly.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	App      App
	DB       DB
	Redis    Redis
	JWT      JWT
	R2       R2
	Telegram Telegram
	Push     Push
	AI       AI
	Limits   Limits
	CORS     CORS

	// SettingsKey encrypts settings stored in the database. Keep it separate
	// from JWT_SECRET: rotating the JWT secret would otherwise make saved
	// credentials unreadable.
	SettingsKey string

	// SeedImageSource selects the imagery demo content uses: "photo" for real
	// photographs from a placeholder service, "svg" for self-contained inline
	// graphics that work offline.
	SeedImageSource string

	// TrustedProxies lists the CIDRs whose forwarding headers may be believed.
	// Empty means the built-in default: loopback plus the private ranges where
	// the Nuxt server and Cloudflare's tunnel sit.
	TrustedProxies []string
}

type App struct {
	Env        string
	URL        string // public site URL, used for canonicals and sitemaps
	APIURL     string
	Port       string
	SiteName   string
	SiteNameKH string
}

func (a App) IsProduction() bool { return a.Env == "production" }

type DB struct {
	Host, Port, User, Password, Name string
}

// DSN returns a GORM-compatible MySQL connection string.
func (d DB) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&collation=utf8mb4_unicode_ci&parseTime=True&loc=UTC",
		d.User, d.Password, d.Host, d.Port, d.Name,
	)
}

// loadDB assembles the database settings.
//
// Managed platforms hand out one connection URL rather than five variables, and
// each uses its own names: Railway's MySQL plugin sets MYSQL_URL plus MYSQLHOST,
// MYSQLUSER and friends. Reading a URL first, then those names, then our own
// DATABASE_* means a deploy needs no variable juggling — and getting it wrong is
// the kind of mistake that only shows up as a connection refused at boot.
//
// Precedence: DATABASE_URL, MYSQL_URL, then discrete variables.
func loadDB() (DB, error) {
	db := DB{
		Host:     firstEnv([]string{"DATABASE_HOST", "MYSQLHOST"}, "127.0.0.1"),
		Port:     firstEnv([]string{"DATABASE_PORT", "MYSQLPORT"}, "3306"),
		User:     firstEnv([]string{"DATABASE_USER", "MYSQLUSER"}, "cfn"),
		Password: firstEnv([]string{"DATABASE_PASSWORD", "MYSQLPASSWORD"}, "cfn_password"),
		Name:     firstEnv([]string{"DATABASE_NAME", "MYSQLDATABASE"}, "cambodia_fast_news"),
	}

	raw := firstEnv([]string{"DATABASE_URL", "MYSQL_URL"}, "")
	if raw == "" {
		return db, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return db, fmt.Errorf("could not parse the database URL: %w", err)
	}
	if parsed.Host == "" {
		return db, fmt.Errorf("the database URL has no host: %q", parsed.Scheme)
	}

	db.Host = parsed.Hostname()
	if port := parsed.Port(); port != "" {
		db.Port = port
	}
	if parsed.User != nil {
		db.User = parsed.User.Username()
		if password, ok := parsed.User.Password(); ok {
			db.Password = password
		}
	}
	if name := strings.TrimPrefix(parsed.Path, "/"); name != "" {
		db.Name = name
	}
	return db, nil
}

// firstEnv returns the first of these variables that is set and non-empty.
func firstEnv(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

type Redis struct{ URL string }

type JWT struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type R2 struct {
	AccountID, AccessKeyID, SecretAccessKey, Bucket, PublicURL string
}

func (r R2) Configured() bool {
	return r.AccountID != "" && r.AccessKeyID != "" && r.SecretAccessKey != "" && r.Bucket != ""
}

// Endpoint is the S3-compatible endpoint Cloudflare exposes for R2.
func (r R2) Endpoint() string {
	return fmt.Sprintf("https://%s.r2.cloudflarestorage.com", r.AccountID)
}

type Telegram struct {
	BotToken    string
	ChannelID   string
	AutoPublish bool
}

func (t Telegram) Configured() bool { return t.BotToken != "" && t.ChannelID != "" }

type Push struct {
	PublicKey, PrivateKey, Subject string
}

func (p Push) Configured() bool { return p.PublicKey != "" && p.PrivateKey != "" }

type AI struct {
	APIKey  string
	Model   string
	Enabled bool
}

func (a AI) Configured() bool { return a.Enabled && a.APIKey != "" }

// Limits holds per-minute request ceilings for each class of endpoint.
//
// These are per client IP. One page view costs several API calls — a homepage
// render alone fetches the nav, breaking bar, hero, feed, trending, pulse and
// video rail — so a read ceiling that looks generous per request is not
// generous per reader.
type Limits struct {
	PublicPerMin, AdsPerMin, BeaconPerMin, AuthPerMin, AIPerMin, UploadPerMin int
}

type CORS struct{ AllowedOrigins []string }

// Load reads configuration from the environment, applying development-safe
// defaults. It returns an error only for values that cannot be defaulted
// safely in production.
func Load() (*Config, error) {
	database, err := loadDB()
	if err != nil {
		return nil, err
	}

	c := &Config{
		App: App{
			Env:    env("APP_ENV", "development"),
			URL:    strings.TrimRight(env("APP_URL", "http://localhost:3000"), "/"),
			APIURL: strings.TrimRight(env("API_URL", "http://localhost:8080"), "/"),
			// PORT first: every managed platform (Railway, Render, Fly, Heroku)
			// injects it and routes traffic there. Reading APP_PORT alone meant
			// the server listened on 8080 while the platform health-checked a
			// different port, which presents as a deploy that never goes live.
			Port:       firstEnv([]string{"PORT", "APP_PORT"}, "8080"),
			SiteName:   env("SITE_NAME", "Cambodia Fast News"),
			SiteNameKH: env("SITE_NAME_KH", "ព័ត៌មានលឿនរហ័សកម្ពុជា"),
		},
		DB:    database,
		Redis: Redis{URL: env("REDIS_URL", "redis://127.0.0.1:6379/0")},
		JWT: JWT{
			Secret:     env("JWT_SECRET", "dev-only-insecure-secret"),
			AccessTTL:  envDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTTL: envDuration("JWT_REFRESH_TTL", 30*24*time.Hour),
		},
		R2: R2{
			AccountID:       env("R2_ACCOUNT_ID", ""),
			AccessKeyID:     env("R2_ACCESS_KEY_ID", ""),
			SecretAccessKey: env("R2_SECRET_ACCESS_KEY", ""),
			Bucket:          env("R2_BUCKET", ""),
			PublicURL:       strings.TrimRight(env("R2_PUBLIC_URL", ""), "/"),
		},
		Telegram: Telegram{
			BotToken:    env("TELEGRAM_BOT_TOKEN", ""),
			ChannelID:   env("TELEGRAM_CHANNEL_ID", ""),
			AutoPublish: envBool("TELEGRAM_AUTO_PUBLISH", false),
		},
		Push: Push{
			PublicKey:  env("PUSH_PUBLIC_KEY", ""),
			PrivateKey: env("PUSH_PRIVATE_KEY", ""),
			Subject:    env("PUSH_SUBJECT", ""),
		},
		AI: AI{
			APIKey:  env("ANTHROPIC_API_KEY", ""),
			Model:   env("AI_MODEL", "claude-opus-5"),
			Enabled: envBool("AI_ENABLED", true),
		},
		Limits: Limits{
			PublicPerMin: envInt("RATE_LIMIT_PUBLIC_PER_MIN", 600),
			AdsPerMin:    envInt("RATE_LIMIT_ADS_PER_MIN", 300),
			BeaconPerMin: envInt("RATE_LIMIT_BEACON_PER_MIN", 600),
			AuthPerMin:   envInt("RATE_LIMIT_AUTH_PER_MIN", 10),
			AIPerMin:     envInt("RATE_LIMIT_AI_PER_MIN", 20),
			UploadPerMin: envInt("RATE_LIMIT_UPLOAD_PER_MIN", 30),
		},
		CORS:            CORS{AllowedOrigins: envList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:3000"})},
		TrustedProxies:  envList("TRUSTED_PROXIES", nil),
		SeedImageSource: env("SEED_IMAGE_SOURCE", "photo"),
		SettingsKey:     env("SETTINGS_KEY", ""),
	}

	// Refuse to boot production with the development JWT secret — a predictable
	// signing key would let anyone mint an admin token.
	if c.App.IsProduction() {
		if c.JWT.Secret == "" || c.JWT.Secret == "dev-only-insecure-secret" || c.JWT.Secret == "change-me-in-production" {
			return nil, fmt.Errorf("JWT_SECRET must be set to a strong unique value when APP_ENV=production")
		}
		if len(c.JWT.Secret) < 32 {
			return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters in production")
		}
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, err := strconv.Atoi(env(key, "")); err == nil {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v, err := strconv.ParseBool(env(key, "")); err == nil {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v, err := time.ParseDuration(env(key, "")); err == nil {
		return v
	}
	return fallback
}

func envList(key string, fallback []string) []string {
	raw := env(key, "")
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
