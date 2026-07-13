// Package api /api/random 端点. 全查询参数, 复用 core.QueryNames + sampler.
package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/namegen/server/internal/core"
	"github.com/namegen/server/internal/sampler"
)

type RandomResponse struct {
	Query         core.QueryConfig  `json:"query"`
	Source        RandomSourceLabel `json:"source"`
	Strategy      string            `json:"strategy"`
	Seed          int64             `json:"seed"`
	TotalFiltered int               `json:"total_filtered"`
	Results       []core.PublicResult `json:"results"`
}

type RandomSourceLabel struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
}

const (
	defaultSurname     = "张"
	defaultSource      = "wealth"
	defaultStrategy    = "weighted"
	defaultAlpha       = 0.15
	defaultNum         = 5
	maxNum             = 50
)

// RandomHandler 创建 /api/random 处理函数.
// deps 提供 charDb / candidateDb 缓存与 hydrate 能力.
func RandomHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleRandom(w, r, deps)
	}
}

func handleRandom(w http.ResponseWriter, r *http.Request, deps *Deps) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET")
		return
	}
	ctx := r.Context()

	// --- 解析查询参数 ---
	q := r.URL.Query()
	query := core.QueryConfig{
		Surname:      firstNonEmpty(q.Get("surname"), defaultSurname),
		Avoid:        splitCS(q.Get("avoid")),
		Must:         splitCS(q.Get("must")),
		MustPosition: firstNonEmpty(q.Get("mustPosition"), "any"),
		Style:        firstNonEmpty(q.Get("style"), "any"),
		Limit:        0, // 内部用大 limit 采样, 非 user-facing 切
	}
	sourcePref := firstNonEmpty(q.Get("source"), q.Get("sourcePreference"), defaultSource)
	n := parseIntDefault(q.Get("n"), defaultNum)
	if n < 1 {
		n = 1
	}
	if n > maxNum {
		n = maxNum
	}
	strategy := firstNonEmpty(q.Get("strategy"), defaultStrategy)
	if strategy != "uniform" && strategy != "weighted" {
		strategy = defaultStrategy
	}
	seed := int64(parseIntDefault(q.Get("seed"), 0))
	alpha := parseFloatDefault(q.Get("alpha"), defaultAlpha)

	sourceID := resolveSourceID(sourcePref)
	if sourceID == "" {
		WriteError(w, http.StatusBadRequest, "unknown_source", "未知来源: "+sourcePref)
		return
	}

	charDb, ok := deps.GetCharDb(ctx)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "char_db_loading", "字库尚未准备好")
		return
	}
	candidates := deps.GetCandidateDb(ctx, sourceID)
	if len(candidates) == 0 {
		// 该 source 未导入, 提示前端.
		WriteError(w, http.StatusBadRequest, "source_empty", "来源无可选候选, 请检查来源 ID")
		return
	}

	// 用 SourceConfig 取 label
	sourceConfig := core.GetSourceConfig(sourceID)

	// 跑核心查询 (取大池子供采样, 不切 limit)
	query.SourcePreference = sourceID
	results, err := core.QueryNames(candidates, charDb, query)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "surname_not_in_char_db", err.Error())
		return
	}

	// 采样
	var rng = deps.NewRNG(seed)
	sampled := sampler.Sample(sampler.SampleInput{
		Results:  results,
		N:        n,
		Strategy: strategy,
		Alpha:    alpha,
		Seed:     seed,
	}, rng)

	public := make([]core.PublicResult, 0, len(sampled))
	for _, sc := range sampled {
		public = append(public, core.ToPublicResult(sc))
	}

	resp := RandomResponse{
		Query: query,
		Source: RandomSourceLabel{
			ID:    sourceID,
			Label: sourceConfig.Label,
			Count: len(candidates),
		},
		Strategy:      strategy,
		Seed:          seed,
		TotalFiltered: len(results),
		Results:       public,
	}

	// 限流与认证由 middleware 在 chain 中调用时设置 RateLimitInfo; 这里默认 nil.
	EncodeJSON(w, http.StatusOK, resp, nil, AuthedFromCtx(ctx))
}

// resolveSourceID 接受 id 或中文 label, 返回标准 id. 不支持时返回 "".
func resolveSourceID(pref string) string {
	for _, s := range core.SourceConfigs {
		if s.ID == pref || s.Label == pref {
			return s.ID
		}
	}
	return ""
}

// splitCS 按逗号分隔, 跳过空段.
func splitCS(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func parseFloatDefault(s string, def float64) float64 {
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}