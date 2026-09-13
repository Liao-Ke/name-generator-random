// Package api /api/help 端点. 静态 API 帮助信息.
package api

import (
	"net/http"

	"github.com/namegen/server/internal/core"
)

type helpParam struct {
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Default  string   `json:"default,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	Range    string   `json:"range,omitempty"`
	Desc     string   `json:"desc"`
}

type helpEndpoint struct {
	Method  string               `json:"method"`
	Path    string               `json:"path"`
	Auth    string               `json:"auth"`
	Summary string               `json:"summary"`
	Query   map[string]helpParam `json:"query,omitempty"`
	Errors  []string             `json:"errors,omitempty"`
}

type helpSource struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type HelpResponse struct {
	OK          bool           `json:"ok"`
	Description string         `json:"description"`
	Endpoints   []helpEndpoint `json:"endpoints"`
	Sources     []helpSource   `json:"sources"`
	Auth        map[string]any `json:"auth"`
	RateLimit   map[string]any `json:"rateLimit"`
	ErrorFormat map[string]any `json:"errorFormat"`
}

// HelpHandler 返回静态 API 帮助. 无 DB 调用.
func HelpHandler() http.HandlerFunc {
	resp := buildHelpResponse()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET")
			return
		}
		EncodeJSON(w, http.StatusOK, resp, nil, AuthedFromCtx(r.Context()))
	}
}

func buildHelpResponse() HelpResponse {
	sources := make([]helpSource, 0, len(core.SourceConfigs))
	for _, s := range core.SourceConfigs {
		sources = append(sources, helpSource{ID: s.ID, Label: s.Label})
	}
	return HelpResponse{
		OK:          true,
		Description: "中文随机起名 API",
		Endpoints: []helpEndpoint{
			{
				Method:  "GET",
				Path:    "/api/help",
				Auth:    "optional",
				Summary: "本帮助信息",
			},
			{
				Method:  "GET",
				Path:    "/api/health",
				Auth:    "optional",
				Summary: "探活 + 来源清单",
			},
			{
				Method:  "GET",
				Path:    "/api/random",
				Auth:    "optional",
				Summary: "按条件随机采样候选名",
				Query: map[string]helpParam{
					"surname":      {Type: "string", Default: "百家姓随机", Desc: "姓氏，1 个汉字"},
					"n":            {Type: "int", Default: "5", Range: "1..50", Desc: "返回个数"},
					"strategy":     {Type: "string", Default: "uniform", Enum: []string{"uniform", "weighted"}, Desc: "采样策略"},
					"source":       {Type: "string", Default: "全部来源合并", Desc: "来源 id 或中文 label"},
					"avoid":        {Type: "csv", Desc: "避开的姓名/字，逗号分隔"},
					"must":         {Type: "csv", Desc: "名中必须出现的字"},
					"mustPosition": {Type: "string", Default: "any", Enum: []string{"any", "second", "third"}, Desc: "must 字在名中的位置"},
					"style":        {Type: "string", Default: "any", Enum: []string{"any", "loud", "soft"}, Desc: "音律风格"},
					"seed":         {Type: "int", Default: "0", Desc: "0=非确定；非 0 可复现"},
					"alpha":        {Type: "float", Default: "0.15", Desc: "weighted 锐度"},
				},
				Errors: []string{
					"unknown_source", "source_empty", "method_not_allowed",
					"surname_not_in_char_db", "rate_limited", "char_db_loading",
				},
			},
			{
				Method:  "GET",
				Path:    "/api/name/{fullName}",
				Auth:    "optional",
				Summary: "单名详情；fullName = 姓+2字名（共3字）",
				Query: map[string]helpParam{
					"source": {Type: "string", Desc: "指定来源；不传则按 priority 查全部"},
				},
				Errors: []string{
					"missing_name", "invalid_name", "unknown_source", "not_found",
					"surname_not_in_char_db", "rate_limited", "char_db_loading",
				},
			},
		},
		Sources: sources,
		Auth: map[string]any{
			"headers": []string{"X-API-Key: <KEY>", "Authorization: Bearer <KEY>"},
			"note":    "有效 key 跳过匿名限流；无效/缺失 key 仍可访问，走匿名限流",
		},
		RateLimit: map[string]any{
			"anonymous": "默认 30 req/min（可配）",
			"authed":    "不限流",
			"headers":   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "Retry-After"},
		},
		ErrorFormat: map[string]any{
			"error":   "机器可读错误码",
			"message": "人类可读说明，末尾带「详见 GET /api/help」",
		},
	}
}
