// Package api CORS 中间件: 让浏览器页面可以跨域调用本 API.
package api

import (
	"net/http"
	"strings"
)

// corsAllowedMethods 预检响应的允许方法. 当前全部端点均为只读 GET.
const corsAllowedMethods = "GET, OPTIONS"

// corsAllowedHeaders 预检响应的允许请求头: 认证入口 + 内容类型.
// 用固定白名单而非回显 Access-Control-Request-Headers, 避免把任意头放大成允许头.
// 若调用方需要自定义头参与预检, 必须在此显式追加.
const corsAllowedHeaders = "X-API-Key, Authorization, Content-Type"

// corsExposeHeaders 暴露给浏览器 JS 的响应头.
// 跨域下 JS 默认只能读 6 个安全头, 限流状态与认证标记必须显式暴露, 否则调用方
// 拿不到 X-RateLimit-* / Retry-After, 无法按 429 自行退避.
const corsExposeHeaders = "X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Retry-After, X-Authed-Authed"

// corsMaxAge 预检结果缓存秒数 (24 小时), 减少浏览器的重复 OPTIONS 请求.
const corsMaxAge = "86400"

// CORS 按允许来源列表放行跨域请求.
//
// SAFE 安全边界: allowed 为空等价于 {"*"} —— 本 API 是公开只读接口且不使用 Cookie 凭证,
// 因此不下发 Access-Control-Allow-Credentials, 通配符不会让第三方站点冒用用户身份.
// 需要收窄时用 CORS_ALLOWED_ORIGINS 显式列白名单 (精确匹配 Origin, 大小写敏感).
//
// NOTE 预检请求 (OPTIONS + Access-Control-Request-Method) 在此短路返回 204, 不进认证与
// 限流链: 浏览器预检按规范不携带 X-API-Key, 若让它走限流桶会白扣匿名额度, 并在额度耗尽时
// 返回 429 导致跨域调用整体失败.
func CORS(allowed []string) func(http.Handler) http.Handler {
	wildcard := len(allowed) == 0
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		o = strings.TrimSpace(o)
		if o == "*" {
			wildcard = true
			continue
		}
		if o != "" {
			set[o] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r) // 非浏览器请求: 无 CORS 语义, 零开销直通
				return
			}

			allowedOrigin := ""
			if wildcard {
				allowedOrigin = "*"
			} else if _, ok := set[origin]; ok {
				allowedOrigin = origin
				// 白名单模式的响应随 Origin 变化, 必须声明 Vary 以免缓存串味
				w.Header().Add("Vary", "Origin")
			}

			isPreflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
			if allowedOrigin != "" {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", allowedOrigin)
				h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
				if isPreflight {
					h.Set("Access-Control-Allow-Methods", corsAllowedMethods)
					h.Set("Access-Control-Allow-Headers", corsAllowedHeaders)
					h.Set("Access-Control-Max-Age", corsMaxAge)
				}
			}

			if isPreflight {
				// 来源不合法时同样返回 204 但不带 CORS 头, 浏览器按规范拒绝该跨域请求
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
