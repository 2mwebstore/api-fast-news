package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/cambodia-fast-news/backend/internal/config"
)

// ConnectRedis opens the Redis client used for caching, counters and rate
// limits. A failure here is not fatal: the caller decides whether to run
// degraded (cache misses fall through to MySQL).
func ConnectRedis(cfg *config.Config) (*redis.Client, error) {
	opt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	opt.PoolSize = 50
	opt.MinIdleConns = 5

	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	slog.Info("redis connected", "addr", opt.Addr)
	return client, nil
}
