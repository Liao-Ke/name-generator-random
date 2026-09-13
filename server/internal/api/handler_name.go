// Package api /api/name/{fullName} 端点. 单名详情.
package api

import (
	"net/http"
	"strings"

	"github.com/namegen/server/internal/core"
)

type NameDetailResponse struct {
	core.PublicResult
	Found bool `json:"found"`
}

// NameHandler 创建 /api/name/{fullName} 处理函数.
// 在指定 source 内 (默认全部来源) 查找该名字. 模式 fullName = 姓+2 字名 (3 字).
func NameHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET")
			return
		}
		ctx := r.Context()

		// Go 1.22+ ServeMux 用 r.PathValue("fullName") 取路径参数
		fullName := r.PathValue("fullName")
		if fullName == "" {
			WriteError(w, http.StatusBadRequest, "missing_name", "路径缺少名字参数")
			return
		}

		// 入参: 姓 + 2 字名 = 3 字; 走 strip 留中文字符
		stripped := core.StripNonChinese(fullName)
		if len(strings.SplitAfter(stripped, "")) < 3 {
			WriteError(w, http.StatusBadRequest, "invalid_name", "名字应为 姓+2 字 (共 3 字)")
			return
		}
		surnameRunes := []rune(stripped)
		if len(surnameRunes) < 3 {
			WriteError(w, http.StatusBadRequest, "invalid_name", "名字不够长")
			return
		}
		surname := string(surnameRunes[0])
		name := string(surnameRunes[1:]) // 2 字名

		// 在用户指定 source 单独查; 不指定则遍历所有 source 取第一个命中 (按 priority).
		sourcePref := strings.TrimSpace(r.URL.Query().Get("source"))
		searchSources := []string{}
		if sourcePref != "" {
			id := resolveSourceID(sourcePref)
			if id == "" {
				WriteError(w, http.StatusBadRequest, "unknown_source", "未知来源: "+sourcePref)
				return
			}
			searchSources = []string{id}
		} else {
			for _, s := range core.SourceConfigs {
				searchSources = append(searchSources, s.ID)
			}
		}

		charDb, ok := deps.GetCharDb(r.Context())
		if !ok {
			WriteError(w, http.StatusInternalServerError, "char_db_loading", "字库尚未准备好")
			return
		}

		// 朴素查询: 在每个 source 跑 queryNames with must 该 name + limit high, 找匹配 fullName 的候选.
		// 单候选直接拼 ScoredCandidate 返回.
		for _, sid := range searchSources {
			candidates := deps.GetCandidateDb(ctx, sid)
			if len(candidates) == 0 {
				continue
			}
			for _, c := range candidates {
				if c.Name != name {
					continue
				}
				info0, ok0 := charDb[c.Chars[0]]
				info1, ok1 := charDb[c.Chars[1]]
				if !ok0 || !ok1 {
					continue
				}
				chars := [2]core.CharInfo{info0, info1}
				surnameLast, err := core.GetSurnameLastCharInfo(charDb, surname)
				if err != nil {
					WriteError(w, http.StatusUnprocessableEntity, "surname_not_in_char_db", err.Error())
					return
				}
				semantic := core.EvaluateSemanticSafety(stripped, c.Name)
				phonetic := core.EvaluatePhonetics(surnameLast, chars[0], chars[1], "any")
				sc := core.ScoreCandidate(core.ScoreCandidateInput{
					Surname:   surname,
					Candidate: c,
					Chars:     chars,
					Phonetic:  phonetic,
					Semantic:  semantic,
				})
				pr := core.ToPublicResult(sc)
				EncodeJSON(w, http.StatusOK, NameDetailResponse{PublicResult: pr, Found: true}, nil, AuthedFromCtx(ctx))
				return
			}
		}

		WriteError(w, http.StatusNotFound, "not_found", "未找到名字: "+stripped)
	}
}
