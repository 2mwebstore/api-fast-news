// Package database owns the MySQL and Redis connections plus migrations.
package database

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/models"
)

// Connect opens the MySQL pool. It retries briefly so `docker compose up`
// works without a healthcheck race between the API and the database.
func Connect(cfg *config.Config) (*gorm.DB, error) {
	level := gormlogger.Warn
	if !cfg.App.IsProduction() {
		level = gormlogger.Info
	}

	var db *gorm.DB
	var err error
	for attempt := 1; attempt <= 10; attempt++ {
		db, err = gorm.Open(mysql.Open(cfg.DB.DSN()), &gorm.Config{
			Logger:                                   gormlogger.Default.LogMode(level),
			DisableForeignKeyConstraintWhenMigrating: true,
			NowFunc:                                  func() time.Time { return time.Now().UTC() },
		})
		if err == nil {
			break
		}
		slog.Warn("database not ready, retrying", "attempt", attempt, "error", err)
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("connect mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// Migrate creates or updates every table, then adds the composite indexes that
// AutoMigrate's struct tags cannot express (§77).
//
// It is idempotent and runs on every start, so a deploy never needs a separate
// migration step.
func Migrate(db *gorm.DB) error {
	// AutoMigrate issues hundreds of DDL and catalogue statements. At the
	// development log level that buries everything else in the startup output.
	quiet := db.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})

	if err := quiet.AutoMigrate(models.AllModels()...); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}
	if err := createCompositeIndexes(quiet); err != nil {
		return err
	}
	return dropRetiredColumns(quiet)
}

// retiredColumns are columns whose feature has been removed. AutoMigrate never
// drops anything, so without this an existing database keeps them forever —
// and a NOT NULL one would eventually break an insert. Listed as table/column
// pairs and dropped only if present, so this stays idempotent.
var retiredColumns = []struct{ table, column string }{
	// The vertical "Shorts" feed was removed; every video is landscape now.
	{"videos", "is_vertical"},
}

// retiredIndexes are indexes left behind by removed features.
var retiredIndexes = []struct{ table, name string }{
	{"videos", "idx_videos_shorts"},
}

func dropRetiredColumns(db *gorm.DB) error {
	// Indexes first: MySQL refuses to drop a column an index still covers.
	for _, ix := range retiredIndexes {
		var count int64
		err := db.Raw(
			`SELECT COUNT(1) FROM information_schema.statistics
			 WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
			ix.table, ix.name,
		).Scan(&count).Error
		if err != nil {
			return fmt.Errorf("check retired index %s: %w", ix.name, err)
		}
		if count == 0 {
			continue
		}
		if err := db.Exec(fmt.Sprintf("DROP INDEX %s ON %s", ix.name, ix.table)).Error; err != nil {
			return fmt.Errorf("drop retired index %s: %w", ix.name, err)
		}
		slog.Info("dropped retired index", "name", ix.name, "table", ix.table)
	}

	for _, col := range retiredColumns {
		var count int64
		err := db.Raw(
			`SELECT COUNT(1) FROM information_schema.columns
			 WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			col.table, col.column,
		).Scan(&count).Error
		if err != nil {
			return fmt.Errorf("check retired column %s.%s: %w", col.table, col.column, err)
		}
		if count == 0 {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", col.table, col.column)
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("drop retired column %s.%s: %w", col.table, col.column, err)
		}
		slog.Info("dropped retired column", "table", col.table, "column", col.column)
	}
	return nil
}

// compositeIndexes are the multi-column indexes behind the hot list queries:
// "published articles in a category, newest first" and friends.
var compositeIndexes = []struct{ name, table, columns string }{
	{"idx_articles_feed", "articles", "(status, published_at DESC)"},
	{"idx_articles_category_feed", "articles", "(category_id, status, published_at DESC)"},
	{"idx_articles_breaking_feed", "articles", "(is_breaking, status, published_at DESC)"},
	{"idx_articles_trending", "articles", "(status, trending_score DESC)"},
	{"idx_videos_feed", "videos", "(status, published_at DESC)"},
	{"idx_ads_serve", "advertisements", "(position, status, start_at, end_at)"},
	{"idx_breaking_active", "breaking_news", "(started_at DESC, ended_at)"},
}

func createCompositeIndexes(db *gorm.DB) error {
	for _, ix := range compositeIndexes {
		// MySQL has no CREATE INDEX IF NOT EXISTS, so check the catalog first.
		var count int64
		err := db.Raw(
			`SELECT COUNT(1) FROM information_schema.statistics
			 WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
			ix.table, ix.name,
		).Scan(&count).Error
		if err != nil {
			return fmt.Errorf("check index %s: %w", ix.name, err)
		}
		if count > 0 {
			continue
		}
		stmt := fmt.Sprintf("CREATE INDEX %s ON %s %s", ix.name, ix.table, ix.columns)
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create index %s: %w", ix.name, err)
		}
		slog.Debug("created index", "name", ix.name, "table", ix.table)
	}

	// Full-text index for Khmer/English article search (§32). Khmer has no
	// spaces between words, so ngram is the only parser that works here.
	var ftCount int64
	if err := db.Raw(
		`SELECT COUNT(1) FROM information_schema.statistics
		 WHERE table_schema = DATABASE() AND table_name = 'articles' AND index_name = 'ft_articles_search'`,
	).Scan(&ftCount).Error; err != nil {
		return fmt.Errorf("check fulltext index: %w", err)
	}
	if ftCount == 0 {
		err := db.Exec(
			`CREATE FULLTEXT INDEX ft_articles_search ON articles
			 (title_kh, title_en, summary_kh, summary_en) WITH PARSER ngram`,
		).Error
		if err != nil {
			// Not fatal: search falls back to LIKE if the parser is unavailable.
			slog.Warn("could not create fulltext index, search will use LIKE fallback", "error", err)
		} else {
			slog.Debug("created fulltext index", "name", "ft_articles_search")
		}
	}
	return nil
}
