// Package api /api/random 端点. 全查询参数, 复用 core.QueryNames + sampler.
package api

import (
	"context"
	"math/rand"
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
	defaultStrategy    = "uniform"
	defaultAlpha       = 0.15
	defaultNum         = 5
	maxNum             = 50
)

// allSourceIDs 按 priority 顺序列出的全部来源 id.
// source 参数缺省时, /api/random 合并全部来源候选跑一次 queryNames.
var allSourceIDs = []string{
	"wealth",
	"academic",
	"modern_people",
	"imperial_exam",
	"ancient_names",
}

// fallbackSurname 当 surnames 表为空 (未导入) 时使用的兜底姓.
const fallbackSurname = "张"

// pickRandomSurname 从 deps 缓存的百家姓列表随机抽一个. 抽不到时回退 fallbackSurname.
// 不依赖 charDb 校验: surnames 表已保证入库字在 chars 表存在.
func pickRandomSurname(surnames []string, rng *rand.Rand) string {
	if len(surnames) == 0 {
		return fallbackSurname
	}
	return surnames[rng.Intn(len(surnames))]
}

// loadAllCandidates 合并全部来源的候选名, 按 name 去重 (保留首次出现的, 即 priority 高的来源).
// 用于 source 缺省时一次 queryNames 调用覆盖所有来源.
func loadAllCandidates(ctx context.Context, deps *Deps) []core.CandidateName {
	all := make([]core.CandidateName, 0, 163000)
	seen := make(map[string]struct{}, 163000)
	for _, sid := range allSourceIDs {
		for _, c := range deps.GetCandidateDb(ctx, sid) {
			if _, ok := seen[c.Name]; ok {
				continue
			}
			seen[c.Name] = struct{}{}
			all = append(all, c)
		}
	}
	return all
}

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
	seed := int64(parseIntDefault(q.Get("seed"), 0))
	alpha := parseFloatDefault(q.Get("alpha"), defaultAlpha)
	rng := deps.NewRNG(seed)

	// source 缺省 = 所有来源合并; 显式传则单来源
	sourcePref := strings.TrimSpace(firstNonEmpty(q.Get("source"), q.Get("sourcePreference")))
	isAllSource := sourcePref == ""
	var sourceID string
	if !isAllSource {
		sourceID = resolveSourceID(sourcePref)
		if sourceID == "" {
			WriteError(w, http.StatusBadRequest, "unknown_source", "未知来源: "+sourcePref)
			return
		}
	}

	charDb, ok := deps.GetCharDb(ctx)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "char_db_loading", "字库尚未准备好")
		return
	}

	// surname 缺省: 从百家姓 (PG surnames 表, 已过滤 charDb 缺失项) 随机抽一个
	surname := strings.TrimSpace(q.Get("surname"))
	if surname == "" {
		surname = pickRandomSurname(deps.GetSurnames(ctx), rng)
	}

	query := core.QueryConfig{
		Surname:      surname,
		Avoid:        splitCS(q.Get("avoid")),
		Must:         splitCS(q.Get("must")),
		MustPosition: firstNonEmpty(q.Get("mustPosition"), "any"),
		Style:        firstNonEmpty(q.Get("style"), "any"),
		Limit:        0, // 内部用大 limit 采样, 非 user-facing 切
	}
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

	// 加载候选: 单来源 or 全部合并 (去重)
	var candidates []core.CandidateName
	if isAllSource {
		candidates = loadAllCandidates(ctx, deps)
	} else {
		candidates = deps.GetCandidateDb(ctx, sourceID)
	}
	if len(candidates) == 0 {
		WriteError(w, http.StatusBadRequest, "source_empty", "来源无可选候选, 请检查来源 ID")
		return
	}

	// 跑核心查询 (取大池子供采样, 不切 limit)
	if isAllSource {
		query.SourcePreference = "default"
	} else {
		query.SourcePreference = sourceID
	}
	results, err := core.QueryNames(candidates, charDb, query)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "surname_not_in_char_db", err.Error())
		return
	}

	// 采样 (rng 已在参数解析阶段构造, 与 pickRandomSurname 共用同一随机源)
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

	// 响应里的 source 字段: 单来源时给 id/label/count; 全来源时 id="all"
	resp := RandomResponse{
		Query: query,
		Source: RandomSourceLabel{
			ID:     ternaryString(isAllSource, "all", sourceID),
			Label:  ternaryString(isAllSource, "全部来源", core.GetSourceConfig(sourceID).Label),
			Count:  len(candidates),
		},
		Strategy:      strategy,
		Seed:          seed,
		TotalFiltered: len(results),
		Results:       public,
	}

	// 限流与认证由 middleware 在 chain 中调用时设置 RateLimitInfo; 这里默认 nil.
	EncodeJSON(w, http.StatusOK, resp, nil, AuthedFromCtx(ctx))
}

func ternaryString(cond bool, ifTrue, ifFalse string) string {
	if cond {
		return ifTrue
	}
	return ifFalse
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