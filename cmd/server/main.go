// Command server runs the Cambodia Fast News API.
//
// Flags:
//
//	-migrate   run migrations and exit
//	-seed      run migrations, seed reference data, and exit
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/cambodia-fast-news/backend/internal/ai"
	"github.com/cambodia-fast-news/backend/internal/auth"
	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/database"
	"github.com/cambodia-fast-news/backend/internal/media"
	"github.com/cambodia-fast-news/backend/internal/repositories"
	"github.com/cambodia-fast-news/backend/internal/routes"
	"github.com/cambodia-fast-news/backend/internal/seo"
	"github.com/cambodia-fast-news/backend/internal/services"
	"github.com/cambodia-fast-news/backend/internal/telegram"
	ws "github.com/cambodia-fast-news/backend/internal/websocket"
)

func main() {
	migrateOnly := flag.Bool("migrate", false, "apply database migrations and exit")
	seed := flag.Bool("seed", false, "apply migrations, install reference and demo data, then exit")
	noDemo := flag.Bool("no-demo", false, "with -seed: install reference data only, no sample content")
	flag.Parse()

	// .env is a developer convenience; in production the environment is set by
	// the orchestrator, and a missing file is expected.
	//
	// Order matters: godotenv does not overwrite a value it has already seen, so
	// the first file listed wins. This service's own .env comes first, and the
	// repository root is only a fallback for installs that predate the split
	// into backend/.env and web/.env.
	_ = godotenv.Load(".env", "../.env")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	setupLogging(cfg)

	db, err := database.Connect(cfg)
	if err != nil {
		slog.Error("could not connect to the database", "error", err)
		os.Exit(1)
	}

	if err := database.Migrate(db); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")

	if *migrateOnly {
		return
	}
	if *seed {
		// Demo content is on by default so a fresh install has a working site
		// to look at, and off in production, where sample articles would be a
		// liability rather than a convenience.
		demo := !*noDemo && !cfg.App.IsProduction()

		result, err := database.Seed(db, cfg, database.Options{Demo: demo})
		if err != nil {
			slog.Error("seeding failed", "error", err)
			os.Exit(1)
		}
		result.Print()
		return
	}

	// Redis is optional: without it the site serves from MySQL, rate limits
	// fall back to per-process counters, and view counts are not buffered.
	// That is degraded, not down.
	var c *cache.Cache
	if rdb, err := database.ConnectRedis(cfg); err != nil {
		slog.Warn("redis unavailable, running without cache", "error", err)
		c = cache.New(nil)
	} else {
		c = cache.New(rdb)
		defer rdb.Close()
	}

	hub := ws.NewHub()
	go hub.Run()
	defer hub.Close()

	tokens := auth.NewService(cfg)
	mediaSvc := media.NewService(cfg)
	aiSvc := ai.NewService(cfg)
	telegramSvc := telegram.NewService(cfg)

	settingsSvc, err := services.NewSettingsService(db, cfg)
	if err != nil {
		slog.Error("could not initialise settings", "error", err)
		os.Exit(1)
	}
	// From here on, Telegram reads its credentials through settings: database
	// first, environment as the fallback.
	telegramSvc.WithCredentials(settingsSvc.TelegramCredentials)

	articleRepo := repositories.NewArticleRepository(db)
	auditSvc := services.NewAuditService(db)
	articleSvc := services.NewArticleService(db, articleRepo, c, hub, telegramSvc, auditSvc, cfg)
	trendingSvc := services.NewTrendingService(db, c)
	viewSvc := services.NewViewService(db, c)
	adSvc := services.NewAdService(db, c)
	seoSvc := seo.NewService(db, articleRepo, cfg)

	logStartupCapabilities(context.Background(), cfg, mediaSvc, aiSvc, telegramSvc)

	router := routes.Register(&routes.Dependencies{
		Cfg: cfg, DB: db, Cache: c, Hub: hub, Tokens: tokens,
		Media: mediaSvc, AI: aiSvc, Telegram: telegramSvc, SEO: seoSvc,
		Articles: articleRepo, ArticleSvc: articleSvc, Trending: trendingSvc,
		Views: viewSvc, Ads: adSvc, Audit: auditSvc, Settings: settingsSvc,
	})

	server := &http.Server{
		Addr:    ":" + cfg.App.Port,
		Handler: router,
		// Generous write timeout: AI drafting endpoints legitimately run long.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	background, stopBackground := context.WithCancel(context.Background())
	go runBackgroundJobs(background, articleSvc, trendingSvc, viewSvc, adSvc)

	go func() {
		slog.Info("api listening", "port", cfg.App.Port, "env", cfg.App.Env)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown: stop accepting requests, then flush the counters that
	// are still buffered in Redis so a deploy does not lose them.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down")

	stopBackground()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	if err := viewSvc.Flush(shutdownCtx); err != nil {
		slog.Error("final view flush failed", "error", err)
	}
	if err := adSvc.FlushEvents(shutdownCtx); err != nil {
		slog.Error("final ad event flush failed", "error", err)
	}
	slog.Info("stopped")
}

// runBackgroundJobs owns the recurring work: flushing buffered counters,
// publishing scheduled articles, recomputing trending and expiring campaigns.
func runBackgroundJobs(
	ctx context.Context,
	articles *services.ArticleService,
	trending *services.TrendingService,
	views *services.ViewService,
	ads *services.AdService,
) {
	flush := time.NewTicker(30 * time.Second)
	schedule := time.NewTicker(time.Minute)
	recompute := time.NewTicker(5 * time.Minute)
	maintenance := time.NewTicker(15 * time.Minute)
	defer func() {
		flush.Stop()
		schedule.Stop()
		recompute.Stop()
		maintenance.Stop()
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case <-flush.C:
			if err := views.Flush(ctx); err != nil {
				slog.Error("view flush failed", "error", err)
			}
			if err := ads.FlushEvents(ctx); err != nil {
				slog.Error("ad event flush failed", "error", err)
			}

		case <-schedule.C:
			articles.PublishDue(ctx)

		case <-recompute.C:
			if err := trending.Recompute(ctx); err != nil {
				slog.Error("trending recompute failed", "error", err)
			}

		case <-maintenance.C:
			ads.ExpireCampaigns(ctx)
		}
	}
}

func setupLogging(cfg *config.Config) {
	level := slog.LevelDebug
	if cfg.App.IsProduction() {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler = slog.NewTextHandler(os.Stdout, opts)
	if cfg.App.IsProduction() {
		// Structured JSON in production so logs are queryable.
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// logStartupCapabilities states plainly which optional integrations are live,
// so an operator is never guessing why Telegram or uploads are silent.
func logStartupCapabilities(ctx context.Context, cfg *config.Config, m *media.Service, a *ai.Service, t *telegram.Service) {
	// Telegram credentials can now come from the database, so this reflects
	// the effective configuration rather than just the environment.
	telegramReady := t.Configured(ctx)

	slog.Info("capabilities",
		"media_r2", m.Configured(),
		"ai_assistant", a.Configured(),
		"telegram", telegramReady,
		"telegram_auto_publish", t.AutoPublishEnabled(ctx),
		"web_push", cfg.Push.Configured(),
	)
	if !m.Configured() {
		slog.Warn("R2 is not configured: media uploads will be rejected")
	}
	if !a.Configured() {
		slog.Warn("ANTHROPIC_API_KEY is not set: the AI assistants are disabled")
	}
	if !telegramReady {
		slog.Warn("Telegram is not configured: set a bot token in Admin → Settings, or TELEGRAM_BOT_TOKEN")
	}
}
