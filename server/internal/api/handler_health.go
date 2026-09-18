// Package api /api/health 与 /api/ready 端点. 只走认证, 不计入限流与在途名额.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/namegen/server/internal/core"
	"github.com/namegen/server/internal/db"
)

// readyTimeout 就绪探针的 PG Ping 超时. 必须显著小于编排系统的探针超时 (常见 1-3s),
// 否则探针自身会先被判定为超时失败.
const readyTimeout = 2 * time.Second

// HealthResponse 存活探针响应: ok 标志 + 来源静态清单.
type HealthResponse struct {
	OK      bool                `json:"ok"`
	Sources []core.SourceConfig `json:"sources"`
}

// ReadyResponse 就绪探针响应.
type ReadyResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// HealthHandler 存活探针 (liveness): 只表示进程活着, 永远 200, 不查任何依赖.
// 依赖不可用不应触发进程重启 —— 那是 ReadyHandler 的职责.
// 附带返回五来源静态清单, 用于探活与 API 元数据.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		EncodeJSON(w, http.StatusOK, HealthResponse{
			OK:      true,
			Sources: core.SourceConfigs,
		}, nil, AuthedFromCtx(r.Context()))
	}
}

// ReadyHandler 就绪探针 (readiness): 真实 Ping PG, 失败返回 503, 供负载均衡摘挂流量.
//
// NOTE 不做限流: 探针不能因为匿名额度耗尽而失败; 代价是 /api/ready 可被外部当作
// 廉价的 PG 连通性探测入口, 暴露公网时建议在反向代理层限制其来源网段.
func ReadyHandler(pool *db.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			slog.Warn("ready 探针失败", "err", err)
			WriteError(w, http.StatusServiceUnavailable, "postgres_unavailable", "数据库不可用")
			return
		}
		EncodeJSON(w, http.StatusOK, ReadyResponse{OK: true}, nil, AuthedFromCtx(ctx))
	}
}
