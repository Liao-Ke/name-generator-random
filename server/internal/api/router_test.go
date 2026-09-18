// Package api 装配级测试: 用真实 BuildMux 链路验证中间件顺序 (不需要 PG).
// 依赖注入的 Deps / Auth 只做指针持有, 匿名请求不会触碰连接池.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/namegen/server/internal/auth"
	"github.com/namegen/server/internal/ratelimit"
)

// newTestMux 装配真实路由链, 返回 handler 与限流器.
func newTestMux(t *testing.T, burst int) http.Handler {
	t.Helper()
	rl := ratelimit.New(30, burst)
	t.Cleanup(rl.Close)
	return BuildMux(NewDeps(nil), auth.New(nil), rl, Options{MaxInflight: 4})
}

// TestBuildMux_CORSPreflightAtEdge CORS 必须是最外层: 预检不经过认证/限流, 直接 204.
func TestBuildMux_CORSPreflightAtEdge(t *testing.T) {
	h := newTestMux(t, 1) // burst=1: 预检若扣桶, 第二次必然 429
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodOptions, "/api/random", nil)
		r.Header.Set("Origin", "https://example.com")
		r.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent {
			t.Fatalf("第 %d 次预检应 204, got %d", i+1, w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("预检缺少 Allow-Origin, got %q", got)
		}
	}
}

// TestBuildMux_ProbesBypassRateLimit 守栏: health 不消耗匿名额度, help 照常限流.
// 探针被限流是部署期最致命的一类问题 (健康实例被摘除), 这里锁死行为.
func TestBuildMux_ProbesBypassRateLimit(t *testing.T) {
	h := newTestMux(t, 3)
	for i := 0; i < 6; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("health 第 %d 次应 200, got %d", i+1, w.Code)
		}
	}
	passed, blocked := 0, 0
	for i := 0; i < 6; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/help", nil))
		switch w.Code {
		case http.StatusOK:
			passed++
		case http.StatusTooManyRequests:
			blocked++
		default:
			t.Fatalf("help 意外状态码: %d", w.Code)
		}
	}
	if passed != 3 || blocked == 0 {
		t.Errorf("help 应受匿名限流: burst=3 时通过 %d 次, 拒绝 %d 次", passed, blocked)
	}
}

// TestBuildMux_UnknownEndpoint404 未知端点返回统一错误结构而不是空响应.
func TestBuildMux_UnknownEndpoint404(t *testing.T) {
	h := newTestMux(t, 10)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("应 404, got %d", w.Code)
	}
	var body ErrorMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应应为 JSON: %v (%s)", err, w.Body.String())
	}
	if body.Error != "not_found" {
		t.Errorf("error 字段 = %q", body.Error)
	}
}

// TestBuildMux_AuthedHeaderOnHelp 匿名请求的认证标记必须一直被写入响应头.
func TestBuildMux_AuthedHeaderOnHelp(t *testing.T) {
	h := newTestMux(t, 10)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/help", nil))
	if got := w.Header().Get("X-Authed-Authed"); got != "false" {
		t.Errorf("匿名请求 X-Authed-Authed = %q, 期望 false", got)
	}
}
