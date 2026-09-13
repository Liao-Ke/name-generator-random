// Package api /api/health 端点. 免认证.
package api

import (
	"net/http"

	"github.com/namegen/server/internal/core"
)

type HealthResponse struct {
	OK      bool                `json:"ok"`
	Sources []core.SourceConfig `json:"sources"`
}

// HealthHandler 返回 ok 标志与来源静态配置清单.
// 来源清单固定, 无 DB 调用, 用于探活与 API 元数据.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		EncodeJSON(w, http.StatusOK, HealthResponse{
			OK:      true,
			Sources: core.SourceConfigs,
		}, nil, AuthedFromCtx(r.Context()))
	}
}
