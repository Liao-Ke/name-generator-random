// Package api 路由组装.
package api

import (
	"net/http"
	"strings"

	"github.com/namegen/server/internal/auth"
	"github.com/namegen/server/internal/ratelimit"
)

// Options 服务级装配选项.
// 零值可用: 空来源列表 = 放开跨域, MaxInflight <= 0 = 不限在途.
type Options struct {
	// AllowedOrigins CORS 允许来源白名单; 空 = 通配 (公开只读 API, 不使用 Cookie 凭证).
	AllowedOrigins []string
	// MaxInflight 业务端点同时执行的请求数上限; <=0 = 不限.
	MaxInflight int
}

// BuildMux 装配 REST 路由. Go 1.22+ ServeMux 支持 {path} 通配与 method 限定.
//
// 中间件链 (外 → 内): CORS → RequestLog → Recover → 路由.
//
// 路由分两类, 这是部署安全的前提:
//   - 探针 /api/health, /api/ready: 只走认证. 不限流, 不占在途名额 —— 探活被限流或
//     被满载挤掉, 会导致健康实例被负载均衡摘除或被编排系统反复重启.
//   - 业务 /api/help, /api/random, /api/name/{fullName}: 在途上限 → 认证 → 限流.
func BuildMux(deps *Deps, authn *auth.Auth, rl *ratelimit.Limiter, opts Options) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /api/health", AuthMiddleware(authn)(HealthHandler()))
	mux.Handle("GET /api/ready", AuthMiddleware(authn)(ReadyHandler(deps.Pool)))

	business := func(h http.Handler) http.Handler {
		return InflightMiddleware(opts.MaxInflight)(AuthMiddleware(authn)(RateLimitMiddleware(rl)(h)))
	}
	mux.Handle("GET /api/help", business(HelpHandler()))
	mux.Handle("GET /api/random", business(RandomHandler(deps)))
	mux.Handle("GET /api/name/{fullName}", business(NameHandler(deps)))

	// 兜底 404
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			WriteError(w, http.StatusNotFound, "not_found", "未知端点: "+r.URL.Path)
			return
		}
		WriteError(w, http.StatusNotFound, "not_found", "仅 /api/* 路由开放")
	})

	return CORS(opts.AllowedOrigins)(RequestLog(RecoverMiddleware(mux)))
}
