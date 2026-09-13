// Package api 中间件: 认证 + 限流组装.
package api

import (
	"context"
	"net/http"
	"strings"

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
			key := ExtractAPIKey(r)
			if key != "" {
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

// clientIP 取限流用的客户端 IP.
//
// SAFE 安全约束, 不可放宽: 只取 X-Forwarded-For 最后一段, 不取第一段.
// 原因: 第一段由客户端完全控制, 攻击者每次换一个伪造值即可获得全新的限流桶,
// 使 per-IP 限流失效. 末段是最近一跳(可信反代)写入的值.
//
// DEPEND 部署前提: 反向代理必须覆盖写入 XFF (如 nginx `proxy_set_header X-Forwarded-For $remote_addr`),
// 而不是用 `$proxy_add_x_forwarded_for` 追加. 追加模式下末段仍是客户端可控值, 限流可被绕过;
// 见 docs/deploy/random-name-api.md 上线检查清单.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.LastIndexByte(xff, ','); i >= 0 {
			if last := strings.TrimSpace(xff[i+1:]); last != "" {
				return last
			}
		} else if only := strings.TrimSpace(xff); only != "" {
			return only
		}
		// XFF 为空或全部是空白: 落到 RemoteAddr.
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
