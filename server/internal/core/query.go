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

// lightResult 结果累积用的轻量中间体.
//
// 为什么需要它: ScoredCandidate 结构体实测 704 字节且含 11 个指针字段(字符串/切片),
// 直接累积十万级结果会有两个代价 —— 切片扩容时反复复制重结构体(实测单次调用分配
// 约 900MB), 以及 GC 扫描指针的 CPU 开销(实测占 CPU 约 40%).
// 这里只保留排序与最终构造所必需的字段, 完整对象只为最终入选的 limit 条构造.
type lightResult struct {
	idx      int32  // 在 candidateDb 中的下标, 最终构造时直接取回候选, 避免再次查找
	name     string // 2 字名
	nameKey  string // 排序键, 语义见 nameSortKey (拼音主序 + 调号序 + 字符码点)
	score    int
	phonetic PhoneticResult
	semantic SemanticResult
}

// QueryNames 主查询流程. 对齐 TS.queryNames.
func QueryNames(candidateDb []CandidateName, charDb CharDb, query QueryConfig) ([]ScoredCandidate, error) {
	normalized := NormalizeQueryConfig(query)
	surnameLast, err := GetSurnameLastCharInfo(charDb, normalized.Surname)
	if err != nil {
		return nil, err
	}
	light := make([]lightResult, 0, len(candidateDb))

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

		light = append(light, lightResult{
			idx:      int32(i),
			name:     candidate.Name,
			nameKey:  nameSortKey(info0, info1),
			score:    scoreTotal(*candidate, chars, phonetic, semantic),
			phonetic: phonetic,
			semantic: semantic,
		})
	}

	// 排序与截断都在轻量集合上做, 避免对重结构体排序/复制.
	sortLight(light)
	if normalized.Limit > 0 && len(light) > normalized.Limit {
		light = light[:normalized.Limit]
	}

	// 只为最终入选结果构造完整对象 (含 Reasons 等字符串).
	results := make([]ScoredCandidate, 0, len(light))
	for i := range light {
		lr := &light[i]
		candidate := &candidateDb[lr.idx]
		results = append(results, ScoreCandidate(ScoreCandidateInput{
			Surname:    normalized.Surname,
			Candidate:  *candidate,
			Chars:      lightChars(charDb, lr.name),
			Phonetic:   lr.phonetic,
			Semantic:   lr.semantic,
			TotalScore: lr.score,
		}))
	}
	return results, nil
}

// nameSortKey 构造二字名的排序键: 逐字拼接 "拼音(去调)" + "调号" + 原字符.
// 对该键做字节序比较, 等价于「拼音主序 → 调号数值序 → 字符 unicode 码点」的逐字比较,
// 即复刻 Node localeCompare("zh-Hans-CN") 在二字名上的行为 (差异见 docs/arch 已知差异表).
func nameSortKey(a, b CharInfo) string {
	return a.PinyinNoTone + itoaTone(a.Tone) + a.Char +
		b.PinyinNoTone + itoaTone(b.Tone) + b.Char
}

// itoaTone 调号转单字符, 保证字典序与数值序一致 (调号仅 1-4).
func itoaTone(t Tone) string {
	if t < 0 || t > 9 {
		return "?"
	}
	return string(rune('0' + t))
}

// lightLess 轻量集合的排序准则: score 降序, 同分按排序键升序.
func lightLess(a, b lightResult) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	return a.nameKey < b.nameKey
}

// sortLight 用 sort.Slice (非 stable) —— 排序键已包含字符本身,
// 同名必然同键, 因此不存在"同键不同序"的稳定性依赖.
func sortLight(rs []lightResult) {
	sort.Slice(rs, func(i, j int) bool { return lightLess(rs[i], rs[j]) })
}

// lightChars 为最终构造取回 2 字名的字库信息 (已验证存在).
func lightChars(charDb CharDb, name string) [2]CharInfo {
	cs := SplitChars(name)
	return [2]CharInfo{charDb[cs[0]], charDb[cs[1]]}
}
