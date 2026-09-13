// Package api HTTP 响应封装. 仅 stdlib.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitInfo 限流标记头. 仅匿名时使用.
type RateLimitInfo struct {
	Limit     int
	Remaining int
	ResetUnix int64 // epoch 秒, 0 表示不详
}

// EncodeJSON 写 JSON 响应, 附加 X-Authed 与 (可选) X-RateLimit-* 头.
func EncodeJSON(w http.ResponseWriter, status int, body any, rateInfo *RateLimitInfo, authed bool) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Authed-Authed", boolStr(authed))
	if rateInfo != nil {
		h.Set("X-RateLimit-Limit", strconv.Itoa(rateInfo.Limit))
		h.Set("X-RateLimit-Remaining", strconv.Itoa(rateInfo.Remaining))
		if rateInfo.ResetUnix > 0 {
			h.Set("X-RateLimit-Reset", strconv.FormatInt(rateInfo.ResetUnix, 10))
		}
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json", "err", err, "status", status)
	}
}

// ErrorMessage 统一错误响应体.
type ErrorMessage struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

const helpHint = "详见 GET /api/help"

// appendHelp 在错误 message 末尾附加 help 引导.
func appendHelp(msg string) string {
	if msg == "" {
		return helpHint
	}
	return msg + "。" + helpHint
}

// WriteError 写 HTTP 错误响应, 不带限流头. message 自动带 help 引导.
func WriteError(w http.ResponseWriter, status int, code, msg string) {
	EncodeJSON(w, status, ErrorMessage{Error: code, Message: appendHelp(msg)}, nil, false)
}

// WriteRateLimited 写 429 + Retry-After + 限流头. 仅匿名超额时调用.
func WriteRateLimited(w http.ResponseWriter, msg string, rate RateLimitInfo, retryAfterSec int) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Authed-Authed", "false")
	h.Set("X-RateLimit-Limit", strconv.Itoa(rate.Limit))
	h.Set("X-RateLimit-Remaining", "0")
	h.Set("X-RateLimit-Reset", strconv.FormatInt(rate.ResetUnix, 10))
	if retryAfterSec > 0 {
		h.Set("Retry-After", strconv.Itoa(retryAfterSec))
	}
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(ErrorMessage{Error: "rate_limited", Message: appendHelp(msg)})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// nowUnix 当前 epoch 秒.
func nowUnix() int64 { return time.Now().Unix() }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
