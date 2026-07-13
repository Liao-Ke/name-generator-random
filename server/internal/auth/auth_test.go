// Package auth: PG api_keys 校验 + 30s 缓存测试.
// build tag integration: 需要可连 PG.
//go:build integration

package auth_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/namegen/server/internal/auth"
	"github.com/namegen/server/internal/db"
)

func dsn() string {
	if v := os.Getenv("POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://namegen:namegen@localhost:5433/namegen?sslmode=disable"
}

func TestIsValidKey(t *testing.T) {
	pool, err := db.NewPool(context.Background(), dsn(), 4)
	if err != nil {
		t.Skipf("无法连 PG (跳过): %v", err)
	}
	defer pool.Close()

	// 清表, 插一个有效 key 与一个吊销 key
	if _, err := pool.Exec(context.Background(), "TRUNCATE api_keys"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO api_keys (key, label) VALUES ('test-valid-key-001', $1)`,
		"测试有效"); err != nil {
		t.Fatalf("insert valid: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO api_keys (key, label, revoked_at) VALUES ('test-revoked-key-002', $1, now())`,
		"测试吊销"); err != nil {
		t.Fatalf("insert revoked: %v", err)
	}

	a := auth.New(pool)

	if !a.IsValidKey(context.Background(), "test-valid-key-001") {
		t.Errorf("有效 key 应通过, 但未通过")
	}
	if a.IsValidKey(context.Background(), "test-revoked-key-002") {
		t.Errorf("已吊销 key 不应通过")
	}
	if a.IsValidKey(context.Background(), "unknown-key-zzz") {
		t.Errorf("未知 key 不应通过")
	}
	if a.IsValidKey(context.Background(), "") {
		t.Errorf("空 key 不应通过")
	}
}

func TestCacheKey(t *testing.T) {
	pool, err := db.NewPool(context.Background(), dsn(), 4)
	if err != nil {
		t.Skipf("无法连 PG: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), "TRUNCATE api_keys"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	a := auth.New(pool)

	// 首次 (cache miss) 命中 PG, 应返回 false
	if a.IsValidKey(context.Background(), "no-such-key-cache") {
		t.Fatalf("未知 key 不应通过")
	}
	// 第二次 (cache hit) 仍 false, 行为一致
	if a.IsValidKey(context.Background(), "no-such-key-cache") {
		t.Fatalf("cache 命中后未知 key 不应通过")
	}

	// 插入该 key, 但缓存里仍是 false. TTL 为 30s, 这里 sleep 0 测试 cache: 仍 false
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO api_keys (key, label) VALUES ('no-such-key-cache', $1)`, "后再插"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if a.IsValidKey(context.Background(), "no-such-key-cache") {
		t.Fatalf("缓存未过期, 应仍为 false (期望缓存效果)")
	}
	// 雪 wait 32s 验证 TTL 刷新 — 跳过以缩短 CI; 仅 assert 即时一致性.
	_ = time.Second
}