// Package api 白盒单元测试: 限流客户端 IP 解析 + 伪造 XFF 不可绕过限流.
// 不依赖 PG, 不带 integration build tag, 可随时 `go test ./internal/api` 运行.
package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// --- CORS ---

// TestCORS_PreflightShortCircuits 守栏: 预检请求不进下游(认证/限流), 直接 204 + 允许头.
// 浏览器预检不携带 X-API-Key, 若让它走限流桶, 额度耗尽时跨域调用会整体失败.
func TestCORS_PreflightShortCircuits(t *testing.T) {
	called := false
	h := CORS(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodOptions, "/api/random", nil)
	r.Header.Set("Origin", "https://example.com")
	r.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if called {
		t.Error("预检不应进入下游 handler")
	}
	if w.Code != http.StatusNoContent {
		t.Errorf("预检应 204, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin 应为通配, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-API-Key") {
		t.Errorf("Allow-Headers 应含 X-API-Key, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "GET") {
		t.Errorf("Allow-Methods 应含 GET, got %q", got)
	}
}

// TestCORS_SimpleRequestExposesRateHeaders 简单请求: 回 Allow-Origin 与 Expose-Headers.
// 跨域下 JS 默认读不到 X-RateLimit-* / Retry-After, 不暴露等于调用方无法按 429 退避.
func TestCORS_SimpleRequestExposesRateHeaders(t *testing.T) {
	h := CORS(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodGet, "/api/random", nil)
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin 应为通配, got %q", got)
	}
	expose := w.Header().Get("Access-Control-Expose-Headers")
	for _, h := range []string{"X-RateLimit-Remaining", "Retry-After", "X-Authed-Authed"} {
		if !strings.Contains(expose, h) {
			t.Errorf("Expose-Headers 应含 %s, got %q", h, expose)
		}
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("本 API 不使用 Cookie 凭证, 不得下发 Allow-Credentials")
	}
}

// TestCORS_WhitelistAndNoOrigin 白名单模式: 命中回显 Origin 并声明 Vary; 未命中不下发 CORS
// 头但请求照常处理(非浏览器调用方不受影响); 无 Origin 头时完全不加 CORS 头.
func TestCORS_WhitelistAndNoOrigin(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := CORS([]string{"https://ok.example"})(okHandler)
	// 1. 白名单命中
	r := httptest.NewRequest(http.MethodGet, "/api/random", nil)
	r.Header.Set("Origin", "https://ok.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://ok.example" {
		t.Errorf("命中白名单应回显 Origin, got %q", got)
	}
	if !strings.Contains(w.Header().Get("Vary"), "Origin") {
		t.Error("白名单模式必须声明 Vary: Origin, 否则缓存会串味")
	}
	// 2. 未命中白名单
	r2 := httptest.NewRequest(http.MethodGet, "/api/random", nil)
	r2.Header.Set("Origin", "https://evil.example")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("未命中来源不应下发 Allow-Origin, got %q", got)
	}
	if w2.Code != http.StatusOK {
		t.Errorf("来源不合法不影响服务端调用, 应照常 200, got %d", w2.Code)
	}
	// 3. 无 Origin (服务端到服务端)
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/api/random", nil))
	if got := w3.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("无 Origin 不应下发 CORS 头, got %q", got)
	}
}

// --- 在途并发上限 ---

// TestInflight_RejectsWhenFull 守栏: 名额满时立即 503 + Retry-After, 不排队等待; 释放后恢复.
// NOTE 用 entered 通道确认占位请求已进入 handler, 不能靠轮询或 sleep 判时序 —— 那会让测试自身死锁.
func TestInflight_RejectsWhenFull(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	h := InflightMiddleware(1)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{} // 告知测试: 名额已被占用
		<-release             // 占住名额, 直到测试放行
		w.WriteHeader(http.StatusOK)
	}))

	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/random", nil))
		done <- w.Code
	}()
	<-entered // 名额确定被占住, 后续请求必然被拒

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/random", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("名额占满应 503, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("503 应带 Retry-After, 否则客户端只会立刻重试")
	}

	close(release)
	if got := <-done; got != http.StatusOK {
		t.Errorf("被放行的请求应 200, got %d", got)
	}
	// 名额释放后必须恢复可用
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/random", nil))
	if w2.Code != http.StatusOK {
		t.Errorf("名额释放后应恢复 200, got %d", w2.Code)
	}
}

// TestInflight_Disabled 上限 <=0 表示不限制: 中间件退化为直通.
func TestInflight_Disabled(t *testing.T) {
	h := InflightMiddleware(0)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/random", nil))
	if w.Code != http.StatusOK {
		t.Errorf("不限流模式应 200, got %d", w.Code)
	}
}

// --- 访问日志 / panic 兜底 ---

// TestStatusRecorder_CapturesStatus 状态码捕获: 隐式 200 / 显式状态码 / 重复 WriteHeader 取首次.
func TestStatusRecorder_CapturesStatus(t *testing.T) {
	implicit := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if _, err := implicit.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if implicit.status != http.StatusOK || implicit.bytes != 1 {
		t.Errorf("隐式写入应记 200 与 1 字节, got %d/%d", implicit.status, implicit.bytes)
	}
	explicit := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	explicit.WriteHeader(http.StatusNotFound)
	explicit.WriteHeader(http.StatusInternalServerError) // net/http 忽略第二次
	if explicit.status != http.StatusNotFound {
		t.Errorf("应只记录首次状态码 404, got %d", explicit.status)
	}
}

// TestRequestLog_ReportsAuthedFlag 守栏: 内层写入 reqInfo 的认证结果必须出现在访问日志里.
// 若有人把 reqInfo 改回不可变的 ctx 值, 日志中的 authed 会永远为 false —— 限流排障会失去依据.
func TestRequestLog_ReportsAuthedFlag(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(old) })

	h := RequestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟 AuthMiddleware 的写法: 经 reqInfo 指针回写认证结果
		if info := reqInfoFrom(r); info != nil {
			info.authed = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/random", nil))

	if !strings.Contains(buf.String(), "authed=true") {
		t.Errorf("访问日志未记录内层写入的认证结果: %s", buf.String())
	}
}

// TestRecoverMiddleware_TurnsPanicInto500 panic 不应断开连接, 应转为 500 JSON.
func TestRecoverMiddleware_TurnsPanicInto500(t *testing.T) {
	h := RecoverMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/random", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("panic 应转 500, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "internal_error") {
		t.Errorf("500 响应体应为统一错误结构, got %s", w.Body.String())
	}
}

// TestCORS_WhitelistPreflight 白名单模式下的预检: 命中回显 Origin 与允许头;
// 未命中来源同样 204 但不带任何 CORS 头(交给浏览器拒绝), 两种情况都不进下游 handler.
func TestCORS_WhitelistPreflight(t *testing.T) {
	h := CORS([]string{"https://ok.example"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("预检不应进入下游 handler")
		w.WriteHeader(http.StatusOK)
	}))

	preflight := func(origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodOptions, "/api/random", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Access-Control-Request-Method", "GET")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	hit := preflight("https://ok.example")
	if hit.Code != http.StatusNoContent {
		t.Errorf("命中白名单的预检应 204, got %d", hit.Code)
	}
	if got := hit.Header().Get("Access-Control-Allow-Origin"); got != "https://ok.example" {
		t.Errorf("命中白名单应回显 Origin, got %q", got)
	}
	if got := hit.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-API-Key") {
		t.Errorf("预检应声明 Allow-Headers, got %q", got)
	}

	miss := preflight("https://evil.example")
	if miss.Code != http.StatusNoContent {
		t.Errorf("未命中来源的预检也应 204, got %d", miss.Code)
	}
	if got := miss.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("未命中来源不得下发 Allow-Origin, got %q", got)
	}
}

// forgedIP 生成互不相同的伪造 IP, 模拟攻击者每请求换一个值.
func forgedIP(i int) string {
	return "9.9.9." + string(rune('0'+i%10))
}
