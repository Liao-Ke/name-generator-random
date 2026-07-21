// Package api 路由组装.
package api

import (
	"net/http"
	"strings"

	"github.com/namegen/server/internal/auth"
	"github.com/namegen/server/internal/ratelimit"
)

// BuildMux 装配 REST 路由. Go 1.22+ ServeMux 支持 {path} 通配与 method 限定.
func BuildMux(deps *Deps, authn *auth.Auth, rl *ratelimit.Limiter) *http.ServeMux {
	mux := http.NewServeMux()

	health := AuthMiddleware(authn)(RateLimitMiddleware(rl)(HealthHandler()))
	mux.Handle("GET /api/health", health)

	help := AuthMiddleware(authn)(RateLimitMiddleware(rl)(HelpHandler()))
	mux.Handle("GET /api/help", help)

	random := AuthMiddleware(authn)(RateLimitMiddleware(rl)(RandomHandler(deps)))
	mux.Handle("GET /api/random", random)

	name := AuthMiddleware(authn)(RateLimitMiddleware(rl)(NameHandler(deps)))
	mux.Handle("GET /api/name/{fullName}", name)

	// 兜底 404
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			WriteError(w, http.StatusNotFound, "not_found", "未知端点: "+r.URL.Path)
			return
		}
		WriteError(w, http.StatusNotFound, "not_found", "仅 /api/* 路由开放")
	})

	return mux
}