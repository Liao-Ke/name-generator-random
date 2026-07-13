// Package core 查询. 对齐 packages/name-core/src/queryName.ts.
package core

import "sort"

// NormalizeQueryConfig 填充默认值, strip 非中文字符.
// 对齐 TS.normalizeQueryConfig.
func NormalizeQueryConfig(query QueryConfig) QueryConfig {
	out := QueryConfig{
		Surname:          StripNonChinese(query.Surname),
		Avoid:            query.Avoid,
		Must:             query.Must,
		MustPosition:     query.MustPosition,
		Style:            query.Style,
		SourcePreference: query.SourcePreference,
		Limit:            query.Limit,
		OutputPath:       query.OutputPath,
	}
	if out.Avoid == nil {
		out.Avoid = []string{}
	}
	if out.Must == nil {
		out.Must = []string{}
	}
	if out.MustPosition == "" {
		out.MustPosition = "any"
	}
	if out.Style == "" {
		out.Style = "any"
	}
	if out.SourcePreference == "" {
		out.SourcePreference = "default"
	}
	if out.Limit == 0 {
		out.Limit = 30
	}
	return out
}

// QueryNames 主查询流程. 对齐 TS.queryNames.
func QueryNames(candidateDb []CandidateName, charDb CharDb, query QueryConfig) ([]ScoredCandidate, error) {
	normalized := NormalizeQueryConfig(query)
	surnameLast, err := GetSurnameLastCharInfo(charDb, normalized.Surname)
	if err != nil {
		return nil, err
	}
	results := make([]ScoredCandidate, 0, 256)

	for i := range candidateDb {
		candidate := &candidateDb[i]
		info0, ok0 := charDb[candidate.Chars[0]]
		info1, ok1 := charDb[candidate.Chars[1]]
		if !ok0 || !ok1 {
			continue
		}
		chars := [2]CharInfo{info0, info1}

		if !CheckMustRules(CheckMustInput{
			Candidate:    *candidate,
			Must:         normalized.Must,
			MustPosition: normalized.MustPosition,
		}) {
			continue
		}

		avoid := CheckAvoidRules(CheckAvoidInput{
			Candidate: *candidate,
			Surname:   normalized.Surname,
			Avoid:     normalized.Avoid,
			CharDb:    charDb,
		})
		if !avoid.Pass {
			continue
		}

		fullName := normalized.Surname + candidate.Name
		semantic := EvaluateSemanticSafety(fullName, candidate.Name)
		if !semantic.Pass {
			continue
		}

		phonetic := EvaluatePhonetics(surnameLast, chars[0], chars[1], normalized.Style)
		if !phonetic.Pass {
			continue
		}

		results = append(results, ScoreCandidate(ScoreCandidateInput{
			Surname:   normalized.Surname,
			Candidate: *candidate,
			Chars:     chars,
			Phonetic:  phonetic,
			Semantic:  semantic,
		}))
	}

	sortResults(charDb, results)
	if normalized.Limit > 0 && len(results) > normalized.Limit {
		results = results[:normalized.Limit]
	}
	return results, nil
}

// sortResults 按 score 降序, 同分按 name 拼音字典序(node zh-Hans-CN 语义) 升序.
// 算法: 主 key 拼音 (PinyinNoTone 字母序 + Tone 数值), tiebreak 用字符 unicode 码点.
// 该实现刻意复刻 Node localeCompare("zh-Hans-CN") 行为; 未引入第三方 collate.
// 使用 sort.SliceStable 保稳定 (TS 默认 Array.sort 是稳定, Go 不保证 stable 排序故锁死).
func sortResults(charDb CharDb, rs []ScoredCandidate) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Score != rs[j].Score {
			return rs[i].Score > rs[j].Score
		}
		return compareZhName(charDb, rs[i].Name, rs[j].Name) < 0
	})
}

// compareZhName 按拼音主序 + 字符 unicode 码点 tiebreak 比较 2 字名 a 和 b.
// 返回 -1 / 0 / 1. 字符 charDb 缺失时该位置以字符零值 PinyinNoTone="" / Tone=0 参与比较,
// 与 Node 行为不严格一致, 但 queryNames 已经过滤掉字库缺失候选, 实际不会触发.
func compareZhName(charDb CharDb, a, b string) int {
	ra := SplitChars(a)
	rb := SplitChars(b)
	n := len(ra)
	if len(rb) < n {
		n = len(rb)
	}
	for i := 0; i < n; i++ {
		ca := charDb[ra[i]]
		cb := charDb[rb[i]]
		if ca.PinyinNoTone < cb.PinyinNoTone {
			return -1
		}
		if ca.PinyinNoTone > cb.PinyinNoTone {
			return 1
		}
		if ca.Tone < cb.Tone {
			return -1
		}
		if ca.Tone > cb.Tone {
			return 1
		}
		// 同拼同调: 用字符 unicode 字节序 tiebreak (与 Node localeCompare 一致)
		if ra[i] < rb[i] {
			return -1
		}
		if ra[i] > rb[i] {
			return 1
		}
	}
	return len(ra) - len(rb)
}