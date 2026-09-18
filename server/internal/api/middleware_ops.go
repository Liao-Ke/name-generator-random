// Package api 运维中间件: 请求日志 / panic 兜底 / 在途并发上限.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// reqInfo 请求级可变信息. 用具名指针放进 ctx, 使外层中间件 (RequestLog) 能读到
// 内层中间件 (AuthMiddleware) 写入的字段 —— http.Request 是不可变的, ctx 值本身
// 无法跨层回写, 只有指针指向的对象可以.
type reqInfo struct {
	authed bool
}

type ctxKeyReqInfo struct{}

// reqInfoFrom 取请求级可变信息; 未安装时返回 nil.
func reqInfoFrom(r *http.Request) *reqInfo {
	if v, ok := r.Context().Value(ctxKeyReqInfo{}).(*reqInfo); ok {
		return v
	}
	return nil
}

// statusRecorder 记录响应状态码与字节数, 供访问日志使用.
// NOTE 包装后不再暴露 http.Flusher / Hijacker: 本 API 无 SSE / WebSocket 升级需求,
// 将来若新增流式端点, 需在此补 Flush/Hijack 透传.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code // 只记第一次, 与 net/http 的语义一致
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK // handler 未显式 WriteHeader 时隐式为 200
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// probePath 探针路径: 日志降到 Debug 级, 避免 10 秒一次的探活刷满生产日志.
func probePath(p string) bool { return p == "/api/health" || p == "/api/ready" }

// RequestLog 输出访问日志: 方法 / 路径 / 状态 / 耗时 / 客户端 IP / 是否带有效 key / 字节数.
// 级别规则: >=500 Error, >=400 Warn, 探针与预检 Debug, 其余 Info.
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		// 安装请求级可变信息, 供 AuthMiddleware 回写 authed (日志要记录认证结果)
		info := &reqInfo{}
		r = r.WithContext(context.WithValue(r.Context(), ctxKeyReqInfo{}, info))

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK // 无响应体且未写状态码的 handler
		}
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(),
			"ip", clientIP(r),
			"authed", info.authed,
			"bytes", rec.bytes,
		}
		switch {
		case rec.status >= http.StatusInternalServerError:
			slog.Error("request", attrs...)
		case rec.status >= http.StatusBadRequest:
			slog.Warn("request", attrs...)
		case probePath(r.URL.Path) || r.Method == http.MethodOptions:
			slog.Debug("request", attrs...)
		default:
			slog.Info("request", attrs...)
		}
	})
}

// RecoverMiddleware 捕获 handler panic: 返回 500 而非断开连接, 并记录堆栈.
// 位置在 RequestLog 内层, 使 panic 也能被访问日志记为 500.
func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				slog.Error("handler panic", "err", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				// NOTE 若 panic 发生在响应已开始写出之后, 此处只能写日志: net/http 会对
				// 重复的 WriteHeader 打印 "superfluous WriteHeader" 告警, 客户端拿到截断响应.
				WriteError(w, http.StatusInternalServerError, "internal_error", "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// InflightMiddleware 限制同时执行业务请求的数量, 超限立即返回 503 而不排队.
// max <= 0 表示不限制.
//
// 动机: /api/random 全源路径单请求约 260ms 纯 CPU, 而匿名限流是 per-IP 的 —— 换 IP
// 即可绕过; 没有在途上限时几十个并发就能把 CPU 打满, 连探针一起拖垮.
//
// NOTE 不排队是有意取舍: 排队会把尾延迟放大成雪崩, 立即失败并给出 Retry-After 让
// 客户端自行退避, 代价是瞬时高峰下部分请求拿不到结果.
// NOTE 探针 (/api/health, /api/ready) 不经过本中间件, 否则满载时探活失败会导致容器被重启.
func InflightMiddleware(max int) func(http.Handler) http.Handler {
	if max <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	sem := make(chan struct{}, max)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				next.ServeHTTP(w, r)
			default:
				slog.Warn("在途请求超限", "max", max, "path", r.URL.Path, "ip", clientIP(r))
				w.Header().Set("Retry-After", "1")
				WriteError(w, http.StatusServiceUnavailable, "server_busy", "服务繁忙, 请稍后重试")
			}
		})
	}
}
