package middleware

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cambodia-fast-news/backend/internal/cache"
	"github.com/cambodia-fast-news/backend/internal/httpx"
)

// RateLimit applies a fixed-window per-minute limit keyed by client IP and
// bucket name (§75). It prefers Redis so the limit holds across API replicas,
// and falls back to an in-process counter when Redis is down — degraded, but
// still a ceiling.
func RateLimit(c *cache.Cache, resolver *IPResolver, bucket string, perMinute int) gin.HandlerFunc {
	fallback := newLocalLimiter()

	return func(ctx *gin.Context) {
		if perMinute <= 0 {
			ctx.Next()
			return
		}
		ip := resolver.ClientIP(ctx)
		window := time.Now().UTC().Truncate(time.Minute).Unix()
		key := fmt.Sprintf("cfn:rl:%s:%s:%d", bucket, ip, window)

		var count int64
		if c.Enabled() {
			n, err := c.Incr(ctx, key, 2*time.Minute)
			if err != nil {
				count = fallback.incr(key)
			} else {
				count = n
			}
		} else {
			count = fallback.incr(key)
		}

		remaining := perMinute - int(count)
		if remaining < 0 {
			remaining = 0
		}
		ctx.Header("X-RateLimit-Limit", strconv.Itoa(perMinute))
		ctx.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if int(count) > perMinute {
			ctx.Header("Retry-After", "60")
			httpx.TooMany(ctx, "Too many requests. Please slow down.")
			return
		}
		ctx.Next()
	}
}

// localLimiter is the in-process fallback. Entries are swept lazily so the map
// cannot grow without bound.
type localLimiter struct {
	mu     sync.Mutex
	counts map[string]int64
	swept  time.Time
}

func newLocalLimiter() *localLimiter {
	return &localLimiter{counts: make(map[string]int64), swept: time.Now()}
}

func (l *localLimiter) incr(key string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.swept) > 2*time.Minute {
		l.counts = make(map[string]int64)
		l.swept = time.Now()
	}
	l.counts[key]++
	return l.counts[key]
}
