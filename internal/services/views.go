package services

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/models"
)

// ViewService buffers article views in Redis and folds them into MySQL on a
// ticker (§76). A breaking story can take tens of thousands of views a minute;
// one INSERT per view would put that load straight onto the primary database.
type ViewService struct {
	db    *gorm.DB
	cache *cache.Cache
}

func NewViewService(db *gorm.DB, c *cache.Cache) *ViewService {
	return &ViewService{db: db, cache: c}
}

// Redis key layout. Counters are hashes keyed by "articleID:day" so one HGETALL
// drains a whole batch.
const (
	viewsKey    = "cfn:buf:views"
	uniquesKey  = "cfn:buf:uniques"
	readKey     = "cfn:buf:readsecs"
	sharesKey   = "cfn:buf:shares"
	vSharesKey  = "cfn:buf:vshares"
	visitorSet  = "cfn:seen:"       // per-article-per-day HyperLogLog of visitors
	visitorTTL  = 36 * time.Hour
)

// RecordView counts one view. visitorHash is a salted hash of IP+User-Agent —
// enough to deduplicate a refresh without storing anything identifying (§43).
//
// Errors are logged, never returned: analytics must never fail a page load.
func (s *ViewService) RecordView(ctx context.Context, articleID uint, visitorHash string, readSeconds int) {
	if !s.cache.Enabled() {
		return
	}
	rdb := s.cache.Client()
	day := time.Now().UTC().Format("2006-01-02")
	field := fmt.Sprintf("%d:%s", articleID, day)

	pipe := rdb.Pipeline()
	pipe.HIncrBy(ctx, viewsKey, field, 1)
	if readSeconds > 0 && readSeconds < 3600 {
		pipe.HIncrBy(ctx, readKey, field, int64(readSeconds))
	}

	// PFADD returns 1 only when the visitor was not already in the set, which
	// is what makes unique counting cheap — a HyperLogLog is ~12 KB per
	// article-day regardless of audience size.
	var added *redis.IntCmd
	if visitorHash != "" {
		added = pipe.PFAdd(ctx, visitorSet+field, visitorHash)
		pipe.Expire(ctx, visitorSet+field, visitorTTL)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("could not buffer article view", "article", articleID, "error", err)
		return
	}
	if added != nil && added.Val() == 1 {
		if err := rdb.HIncrBy(ctx, uniquesKey, field, 1).Err(); err != nil {
			slog.Warn("could not buffer unique view", "article", articleID, "error", err)
		}
	}
}

// RecordShare counts an outbound share to a network (§43).
func (s *ViewService) RecordShare(ctx context.Context, articleID uint, network string) {
	if !s.cache.Enabled() {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	field := fmt.Sprintf("%d:%s:%s", articleID, network, day)
	if err := s.cache.Client().HIncrBy(ctx, sharesKey, field, 1).Err(); err != nil {
		slog.Warn("could not buffer share", "article", articleID, "error", err)
	}
}

// RecordVideoShare counts one video share. Buffered exactly like article shares
// so a burst of shares costs one Redis field increment each, not a write.
func (s *ViewService) RecordVideoShare(ctx context.Context, videoID uint, network string) {
	if !s.cache.Enabled() {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	field := fmt.Sprintf("%d:%s:%s", videoID, network, day)
	if err := s.cache.Client().HIncrBy(ctx, vSharesKey, field, 1).Err(); err != nil {
		slog.Warn("could not buffer video share", "video", videoID, "error", err)
	}
}

// Flush drains the Redis buffers into MySQL. Called on a ticker from main and
// once more at shutdown so an in-flight batch is not lost on deploy.
func (s *ViewService) Flush(ctx context.Context) error {
	if !s.cache.Enabled() {
		return nil
	}
	if err := s.flushViews(ctx); err != nil {
		return err
	}
	if err := s.flushShares(ctx); err != nil {
		return err
	}
	return s.flushVideoShares(ctx)
}

func (s *ViewService) flushViews(ctx context.Context) error {
	rdb := s.cache.Client()

	// Rename before reading so views arriving mid-flush land in a fresh hash
	// and are picked up next cycle instead of being double-counted or dropped.
	views, err := drainHash(ctx, rdb, viewsKey)
	if err != nil {
		return err
	}
	uniques, err := drainHash(ctx, rdb, uniquesKey)
	if err != nil {
		return err
	}
	readSecs, err := drainHash(ctx, rdb, readKey)
	if err != nil {
		return err
	}
	if len(views) == 0 && len(uniques) == 0 && len(readSecs) == 0 {
		return nil
	}

	// Union of every field seen across the three hashes.
	fields := make(map[string]struct{}, len(views))
	for f := range views {
		fields[f] = struct{}{}
	}
	for f := range uniques {
		fields[f] = struct{}{}
	}
	for f := range readSecs {
		fields[f] = struct{}{}
	}

	articleTotals := make(map[uint]struct{ views, uniques int64 })
	rows := make([]models.ArticleView, 0, len(fields))

	for field := range fields {
		articleID, day, ok := splitField(field)
		if !ok {
			continue
		}
		rows = append(rows, models.ArticleView{
			ArticleID:   articleID,
			Day:         day,
			Views:       views[field],
			UniqueViews: uniques[field],
			ReadSeconds: readSecs[field],
		})
		t := articleTotals[articleID]
		t.views += views[field]
		t.uniques += uniques[field]
		articleTotals[articleID] = t
	}
	if len(rows) == 0 {
		return nil
	}

	// One upsert for the whole batch. The ON DUPLICATE clause adds to the
	// existing day's totals rather than overwriting them.
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "article_id"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{
			"views":        gorm.Expr("article_views.views + VALUES(views)"),
			"unique_views": gorm.Expr("article_views.unique_views + VALUES(unique_views)"),
			"read_seconds": gorm.Expr("article_views.read_seconds + VALUES(read_seconds)"),
			"updated_at":   time.Now().UTC(),
		}),
	}).CreateInBatches(rows, 200).Error
	if err != nil {
		return fmt.Errorf("flush article views: %w", err)
	}

	// Keep the denormalised counters on articles in step.
	for articleID, t := range articleTotals {
		if t.views == 0 && t.uniques == 0 {
			continue
		}
		err := s.db.WithContext(ctx).Model(&models.Article{}).Where("id = ?", articleID).
			UpdateColumns(map[string]any{
				"view_count":        gorm.Expr("view_count + ?", t.views),
				"unique_view_count": gorm.Expr("unique_view_count + ?", t.uniques),
			}).Error
		if err != nil {
			slog.Warn("could not update article view counter", "article", articleID, "error", err)
		}
	}

	slog.Debug("flushed article views", "rows", len(rows))
	return nil
}

func (s *ViewService) flushVideoShares(ctx context.Context) error {
	shares, err := drainHash(ctx, s.cache.Client(), vSharesKey)
	if err != nil || len(shares) == 0 {
		return err
	}

	rows := make([]models.VideoShare, 0, len(shares))
	for field, count := range shares {
		parts := strings.Split(field, ":")
		if len(parts) != 3 {
			continue
		}
		id, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			continue
		}
		rows = append(rows, models.VideoShare{
			VideoID: uint(id), Network: parts[1], Day: parts[2], Count: count,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "video_id"}, {Name: "network"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{
			"count":      gorm.Expr("video_shares.count + VALUES(count)"),
			"updated_at": time.Now().UTC(),
		}),
	}).CreateInBatches(rows, 200).Error
	if err != nil {
		return fmt.Errorf("flush video shares: %w", err)
	}
	return nil
}

func (s *ViewService) flushShares(ctx context.Context) error {
	shares, err := drainHash(ctx, s.cache.Client(), sharesKey)
	if err != nil || len(shares) == 0 {
		return err
	}

	rows := make([]models.ArticleShare, 0, len(shares))
	totals := make(map[uint]int64)
	for field, count := range shares {
		parts := strings.Split(field, ":")
		if len(parts) != 3 {
			continue
		}
		id, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			continue
		}
		rows = append(rows, models.ArticleShare{
			ArticleID: uint(id), Network: parts[1], Day: parts[2], Count: count,
		})
		totals[uint(id)] += count
	}
	if len(rows) == 0 {
		return nil
	}

	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "article_id"}, {Name: "network"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{
			"count":      gorm.Expr("article_shares.count + VALUES(count)"),
			"updated_at": time.Now().UTC(),
		}),
	}).CreateInBatches(rows, 200).Error
	if err != nil {
		return fmt.Errorf("flush shares: %w", err)
	}

	for articleID, count := range totals {
		s.db.WithContext(ctx).Model(&models.Article{}).Where("id = ?", articleID).
			UpdateColumn("share_count", gorm.Expr("share_count + ?", count))
	}
	return nil
}

// drainHash atomically takes ownership of a counter hash and returns its
// contents. RENAME on a missing key is not an error condition here — it just
// means nothing was buffered this cycle.
func drainHash(ctx context.Context, rdb *redis.Client, key string) (map[string]int64, error) {
	tmp := key + ":flush:" + strconv.FormatInt(time.Now().UnixNano(), 36)

	if err := rdb.Rename(ctx, key, tmp).Err(); err != nil {
		if err == redis.Nil || strings.Contains(err.Error(), "no such key") {
			return nil, nil
		}
		return nil, fmt.Errorf("rename %s: %w", key, err)
	}
	defer rdb.Del(ctx, tmp)

	raw, err := rdb.HGetAll(ctx, tmp).Result()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", tmp, err)
	}

	out := make(map[string]int64, len(raw))
	for field, value := range raw {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil && n != 0 {
			out[field] = n
		}
	}
	return out, nil
}

func splitField(field string) (uint, string, bool) {
	idx := strings.IndexByte(field, ':')
	if idx <= 0 {
		return 0, "", false
	}
	id, err := strconv.ParseUint(field[:idx], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return uint(id), field[idx+1:], true
}
