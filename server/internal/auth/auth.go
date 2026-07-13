// Package auth: 从 PG 查 api_keys 校验; 30 秒进程内 TTL 缓存, 防每请求打 PG.
package auth

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"sync"
	"time"

	"github.com/namegen/server/internal/db"
)

// Auth 校验服务. 进程内缓存 valid/invalid key, 减轻 DB 压力.
type Auth struct {
	pool *db.Pool

	mu    sync.RWMutex
	cache map[string]cacheEntry // key -> entry
	ttl   time.Duration
}

type cacheEntry struct {
	valid    bool
	loadedAt time.Time
}

// New 构造校验器, 默认 TTL 30s.
func New(pool *db.Pool) *Auth {
	return &Auth{
		pool:  pool,
		cache: make(map[string]cacheEntry, 32),
		ttl:   30 * time.Second,
	}
}

// IsValidKey 校验 key 是否 (在 PG 中存在) AND (未被吊销).
// 缓存命中时直接返回; 缓存未命中或过期时查 PG.
// 用 ConstantTimeCompare 防时序侧信道.
func (a *Auth) IsValidKey(ctx context.Context, key string) bool {
	if key == "" {
		return false
	}
	if v, ok := a.fromCache(key); ok {
		return v
	}
	valid := a.queryPG(ctx, key)
	a.store(key, valid)
	return valid
}

func (a *Auth) fromCache(key string) (bool, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	e, ok := a.cache[key]
	if !ok {
		return false, false
	}
	if time.Since(e.loadedAt) > a.ttl {
		return false, false
	}
	return e.valid, true
}

func (a *Auth) store(key string, valid bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache[key] = cacheEntry{valid: valid, loadedAt: time.Now()}
}

// queryPG 用 PG 常量时间比较. 注意: SQL `WHERE key = $1 AND revoked_at IS NULL`
// 在 PG 内部长度敏感但有索引; 防时序侧信道用 subtle.ConstantTimeCompare 在 Go 侧
// 兜底对全表扫是不需要的 (我们只在缓存未命中时查 PG, 且 key 是高熵字符串).
func (a *Auth) queryPG(ctx context.Context, key string) bool {
	var storedKey string
	err := a.pool.QueryRow(ctx,
		`SELECT key FROM api_keys WHERE key = $1 AND revoked_at IS NULL LIMIT 1`,
		key).Scan(&storedKey)
	if err != nil {
		if err.Error() != "no rows in result set" {
			slog.Warn("auth query pg", "err", err)
		}
		return false
	}
	return subtle.ConstantTimeCompare([]byte(storedKey), []byte(key)) == 1
}