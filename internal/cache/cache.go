// Package cache wraps Redis with the key scheme from §63 and degrades to a
// pass-through when Redis is unavailable, so a cache outage cannot take the
// site down.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Key builders (§63). Every cached value is namespaced so invalidation can
// scan a prefix without touching unrelated keys.
const (
	prefix = "cfn:"

	KeyLatest   = prefix + "news:latest"
	KeyBreaking = prefix + "news:breaking"
	KeyTrending = prefix + "news:trending"
	KeyFiveMin  = prefix + "news:five-minute"
	KeyPulse    = prefix + "news:pulse"
	KeyNav      = prefix + "categories:nav"
	KeyTraffic  = prefix + "traffic:status"
)

func KeyCategory(slug string) string { return prefix + "news:category:" + slug }
func KeyArticle(slug string) string  { return prefix + "news:article:" + slug }
func KeyAds(position string) string  { return prefix + "ads:" + position }
func KeyVideo(slug string) string    { return prefix + "video:" + slug }
func KeySitemap(kind string) string  { return prefix + "sitemap:" + kind }

// Default TTLs. Breaking news is deliberately short-lived: it is pushed over
// WebSocket anyway, so the cache only absorbs a reconnect stampede.
const (
	TTLShort  = 30 * time.Second
	TTLMedium = 5 * time.Minute
	TTLLong   = 30 * time.Minute
	TTLHour   = time.Hour
)

type Cache struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Cache { return &Cache{rdb: rdb} }

// Enabled reports whether a Redis client is actually attached.
func (c *Cache) Enabled() bool { return c != nil && c.rdb != nil }

// Client exposes the raw client for counters and rate limiting.
func (c *Cache) Client() *redis.Client {
	if c == nil {
		return nil
	}
	return c.rdb
}

// GetJSON unmarshals a cached value into dest. It returns false on any miss,
// decode failure or Redis error — every one of those means "go to the source".
func (c *Cache) GetJSON(ctx context.Context, key string, dest any) bool {
	if !c.Enabled() {
		return false
	}
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err != redis.Nil {
			slog.Warn("cache get failed", "key", key, "error", err)
		}
		return false
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		slog.Warn("cache decode failed, discarding", "key", key, "error", err)
		c.rdb.Del(ctx, key)
		return false
	}
	return true
}

// SetJSON stores a value. Failures are logged and swallowed: a write-through
// miss must never fail the request that produced the data.
func (c *Cache) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) {
	if !c.Enabled() {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		slog.Warn("cache encode failed", "key", key, "error", err)
		return
	}
	if err := c.rdb.Set(ctx, key, raw, ttl).Err(); err != nil {
		slog.Warn("cache set failed", "key", key, "error", err)
	}
}

// Delete removes specific keys.
func (c *Cache) Delete(ctx context.Context, keys ...string) {
	if !c.Enabled() || len(keys) == 0 {
		return
	}
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		slog.Warn("cache delete failed", "error", err)
	}
}

// DeletePrefix removes every key under a prefix using SCAN, which — unlike
// KEYS — does not block Redis while it walks the keyspace.
func (c *Cache) DeletePrefix(ctx context.Context, p string) {
	if !c.Enabled() {
		return
	}
	iter := c.rdb.Scan(ctx, 0, p+"*", 200).Iterator()
	batch := make([]string, 0, 200)
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) >= 200 {
			c.rdb.Del(ctx, batch...)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		c.rdb.Del(ctx, batch...)
	}
	if err := iter.Err(); err != nil {
		slog.Warn("cache scan failed", "prefix", p, "error", err)
	}
}

// InvalidateContent clears every cache entry that a publish or edit can affect.
// Called from the article service after any state change (§63).
func (c *Cache) InvalidateContent(ctx context.Context) {
	c.Delete(ctx, KeyLatest, KeyBreaking, KeyTrending, KeyFiveMin, KeyPulse)
	c.DeletePrefix(ctx, prefix+"news:category:")
	c.DeletePrefix(ctx, prefix+"sitemap:")
}

// InvalidateArticle clears one article plus the aggregate lists it appears in.
func (c *Cache) InvalidateArticle(ctx context.Context, slug string) {
	c.Delete(ctx, KeyArticle(slug))
	c.InvalidateContent(ctx)
}

// Incr bumps a counter and sets its TTL on first write. Used by the view
// buffer and the rate limiter.
func (c *Cache) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if !c.Enabled() {
		return 0, fmt.Errorf("cache disabled")
	}
	pipe := c.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}
