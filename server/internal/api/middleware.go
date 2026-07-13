// Package api 中间件: 认证 + 限流组装.
package api

import (
	"context"
	"net/http"

	"github.com/namegen/server/internal/auth"
	"github.com/namegen/server/internal/ratelimit"
)

// ctxKeyAuthed ctx key 类型, 防碰撞.
type ctxKeyAuthed struct{}

// AuthedFromCtx 取请求上下文中的 authed 标记. 未设置 = false.
func AuthedFromCtx(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxKeyAuthed{}).(bool); ok {
		return v
	}
	return false
}

// AuthMiddleware 校验 API key; 命中 -> ctx 标 authed=true.
// 失败 / 无 key -> ctx 标 authed=false, 不阻断, 进下游限流中间件.
func AuthMiddleware(authn *auth.Auth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authed := false
			if key := ExtractAPIKey(r); key != "" {
				authed = authn.IsValidKey(r.Context(), key)
			}
			ctx := context.WithValue(r.Context(), ctxKeyAuthed{}, authed)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RateLimitMiddleware 匿名限流. 已 authed=true 的请求直接放行.
func RateLimitMiddleware(rl *ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if AuthedFromCtx(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			ok, info, retryAfter := rl.Allow(clientIP(r))
			if !ok {
				WriteRateLimited(w, "请求过频, 请稍后再试", RateLimitInfo{
					Limit:     info.Limit,
					Remaining: info.Remaining,
					ResetUnix: info.ResetUnix,
				}, retryAfter)
				return
			}
			// 不能直接 modify headers before next writes; 把 info 传到 next via context
			ctx := context.WithValue(r.Context(), ctxKeyRate{}, info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type ctxKeyRate struct{}

// ExtractAPIKey 从 Authorization: Bearer KEY 或 X-API-Key 取 key.
func ExtractAPIKey(r *http.Request) string {
	if k := r.Header.Get("X-API-Key"); k != "" {
		return k
	}
	a := r.Header.Get("Authorization")
	if a == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(a) >= len(prefix) && a[:len(prefix)] == prefix {
		return a[len(prefix):]
	}
	return ""
}

// clientIP 取最前的可信 X-Forwarded-For; 缺失回 RemoteAddr (去端口).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	addr := r.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

// 注: formatUnix 已删, 不再保留无用占位.