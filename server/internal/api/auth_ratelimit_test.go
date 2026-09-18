// Package api: 认证 + 限流集成测试.
// build tag integration: 需要可连 PG; 服务由 httptest 起.
//go:build integration

package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/namegen/server/internal/api"
	"github.com/namegen/server/internal/auth"
	"github.com/namegen/server/internal/db"
	"github.com/namegen/server/internal/ratelimit"
)

func dsn() string {
	if v := os.Getenv("POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://namegen:namegen@localhost:5433/namegen?sslmode=disable"
}

// newServer 启动一个 httptest.Server, 携带真实 PG 认证 + 限流链.
func newServer(t *testing.T, rpm, burst int) (*httptest.Server, *db.Pool) {
	t.Helper()
	pool, err := db.NewPool(context.Background(), dsn(), 4)
	if err != nil {
		t.Skipf("无法连 PG: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	if err := db.ApplySchema(context.Background(), pool); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "TRUNCATE api_keys"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	deps := api.NewDeps(pool)
	authn := auth.New(pool)
	rl := ratelimit.New(rpm, burst)
	t.Cleanup(func() { rl.Close() })

	mux := api.BuildMux(deps, authn, rl, api.Options{MaxInflight: 8})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })
	return srv, pool
}

func setValidKey(t *testing.T, pool *db.Pool, key string) {
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO api_keys (key, label) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET revoked_at = NULL`,
		key, "test"); err != nil {
		t.Fatalf("upsert key: %v", err)
	}
}

func TestRateLimit_Anonymous429AfterBurst(t *testing.T) {
	srv, _ := newServer(t, 30, 5) // rpm=30, burst=5
	c := srv.Client()
	var firstStatus int
	passed, blocked := 0, 0
	// 用 /api/help 而非 /api/health: 探针已移出限流链, 不再消耗匿名额度
	for i := 0; i < 30; i++ {
		resp, err := c.Get(srv.URL + "/api/help")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			passed++
		}
		if resp.StatusCode == 429 {
			blocked++
		}
		if i == 0 {
			firstStatus = resp.StatusCode
		}
	}
	if firstStatus != 200 {
		t.Errorf("首请求应 200, got %d", firstStatus)
	}
	if blocked == 0 {
		t.Errorf("应有被 429 的请求; 全通过 %d", passed)
	}
}

func TestRateLimit_AuthedSkipsLimit(t *testing.T) {
	srv, pool := newServer(t, 30, 2) // burst 极小: 2
	setValidKey(t, pool, "test-rate-bypass-key")
	c := srv.Client()
	passed := 0
	for i := 0; i < 8; i++ {
		req, _ := http.NewRequest("GET", srv.URL+"/api/help", nil)
		req.Header.Set("X-API-Key", "test-rate-bypass-key")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			passed++
		}
		if resp.Header.Get("X-Authed-Authed") != "true" {
			t.Errorf("should authed=true on attempt %d", i)
		}
	}
	if passed != 8 {
		t.Errorf("有 key 应不限流, 应全 8 通过, got %d", passed)
	}
}

func TestAuth_BogusKeyMarkedAnon(t *testing.T) {
	srv, _ := newServer(t, 30, 30)
	c := srv.Client()
	req, _ := http.NewRequest("GET", srv.URL+"/api/health", nil)
	req.Header.Set("X-API-Key", "bogus-key-unknown")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Authed-Authed") != "false" {
		t.Errorf("bogus key 应标 authed=false")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health 应 200, got %d", resp.StatusCode)
	}
}

func TestAuth_BearerHeaderAccepted(t *testing.T) {
	srv, pool := newServer(t, 30, 30)
	setValidKey(t, pool, "test-bearer-key")
	c := srv.Client()
	req, _ := http.NewRequest("GET", srv.URL+"/api/health", nil)
	req.Header.Set("Authorization", "Bearer test-bearer-key")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Authed-Authed") != "true" {
		t.Errorf("Authorization: Bearer 应识别为有效 key, authed=true")
	}
}

func TestHealth_OkBody(t *testing.T) {
	srv, _ := newServer(t, 30, 30)
	resp, err := srv.Client().Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health 应 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "\"ok\":true") {
		t.Errorf("health body missing ok:true: %s", string(body))
	}
}

// TestProbes_NotRateLimited 守栏: 探针不消耗匿名额度 (burst=1 时连发多次仍应全 200).
// 探针若被限流, 负载均衡会把健康实例摘掉或编排系统反复重启 —— 这是部署期最容易踩的坑.
func TestProbes_NotRateLimited(t *testing.T) {
	srv, _ := newServer(t, 30, 1)
	c := srv.Client()
	for _, p := range []string{"/api/health", "/api/ready"} {
		for i := 0; i < 3; i++ {
			resp, err := c.Get(srv.URL + p)
			if err != nil {
				t.Fatalf("get %s: %v", p, err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s 第 %d 次应 200, got %d", p, i+1, resp.StatusCode)
			}
		}
	}
}

// TestReady_PingsPostgres 就绪探针必须真实探测依赖: PG 可用时 200 + ok:true.
func TestReady_PingsPostgres(t *testing.T) {
	srv, _ := newServer(t, 30, 30)
	resp, err := srv.Client().Get(srv.URL + "/api/ready")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ready 应 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "\"ok\":true") {
		t.Errorf("ready 响应缺少 ok:true: %s", string(body))
	}
}
