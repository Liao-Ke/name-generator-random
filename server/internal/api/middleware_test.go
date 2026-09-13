// Package api 白盒单元测试: 限流客户端 IP 解析 + 伪造 XFF 不可绕过限流.
// 不依赖 PG, 不带 integration build tag, 可随时 `go test ./internal/api` 运行.
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/namegen/server/internal/ratelimit"
)

// TestClientIP_XFFLastHopOnly 守栏: 只认 X-Forwarded-For 末段(可信反代写入), 不认首段.
// 若实现被改回"取第一段", 本测试立即失败 —— 那是客户端可伪造、能绕限流的写法.
func TestClientIP_XFFLastHopOnly(t *testing.T) {
	cases := []struct {
		name string
		xff  string
		addr string
		want string
	}{
		{"多跳取末段", "1.2.3.4, 10.0.0.1, 203.0.113.9", "10.0.0.5:1234", "203.0.113.9"},
		{"单值即末段", "203.0.113.9", "10.0.0.5:1234", "203.0.113.9"},
		{"首段伪造值不被采信", "9.9.9.9, 203.0.113.9", "10.0.0.5:1234", "203.0.113.9"},
		{"末段带空格", "1.2.3.4,   203.0.113.9  ", "10.0.0.5:1234", "203.0.113.9"},
		{"末段为空回退 RemoteAddr", "1.2.3.4, ", "10.0.0.5:1234", "10.0.0.5"},
		{"XFF 全空白回退 RemoteAddr", "   ", "10.0.0.5:1234", "10.0.0.5"},
		{"无 XFF 用 RemoteAddr", "", "10.0.0.5:1234", "10.0.0.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			r.RemoteAddr = c.addr
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if got := clientIP(r); got != c.want {
				t.Errorf("clientIP = %q, 期望 %q", got, c.want)
			}
		})
	}
}

// TestClientIP_RemoteAddrWithoutPort 无端口的 RemoteAddr 原样返回 (net/http 测试场景常见).
func TestClientIP_RemoteAddrWithoutPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.RemoteAddr = "192.0.2.7"
	if got := clientIP(r); got != "192.0.2.7" {
		t.Errorf("clientIP = %q, 期望 %q", got, "192.0.2.7")
	}
}

// TestRateLimit_ForgedXFFCannotBypass 端到端守栏: 攻击者每次换一个伪造的 XFF 首段,
// 若这些请求被判成不同客户端, 各自拿到满桶就永远不会触发限流.
// 期望: 伪造值被忽略, 同一末段 IP 共享一个桶 —— 因此必须出现 429.
func TestRateLimit_ForgedXFFCannotBypass(t *testing.T) {
	const burst = 3
	rl := ratelimit.New(30, burst)
	t.Cleanup(rl.Close)

	h := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const attempts = 8
	passed, blocked := 0, 0
	for i := 0; i < attempts; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		// 首段每次变化(伪造), 末段固定 = 同一个真实客户端
		r.Header.Set("X-Forwarded-For", forgedIP(i)+", 203.0.113.9")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		switch w.Code {
		case http.StatusOK:
			passed++
		case http.StatusTooManyRequests:
			blocked++
		default:
			t.Fatalf("第 %d 次请求状态码异常: %d", i+1, w.Code)
		}
	}

	if blocked == 0 {
		t.Errorf("伪造 XFF 首段未被忽略: %d 次请求全部放行, 限流被绕过", attempts)
	}
	if passed != burst {
		t.Errorf("burst=%d 时应有 %d 次放行, 实际 %d 次", burst, burst, passed)
	}
}

// forgedIP 生成互不相同的伪造 IP, 模拟攻击者每请求换一个值.
func forgedIP(i int) string {
	return "9.9.9." + string(rune('0'+i%10))
}
