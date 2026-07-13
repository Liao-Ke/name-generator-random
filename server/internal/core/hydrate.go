// Package core 候选名水合. 对齐 packages/name-core/src/candidateRuntime.ts.
package core

// IsCandidateName 判断 entry 是否已是完整 CandidateName.
// 字段名与 TS isCandidateName 一致: name/sources/sourceIds/sourceReasons/chars/flags.
func IsCandidateName(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	for _, k := range []string{"name", "sources", "sourceIds", "sourceReasons", "chars", "flags"} {
		if _, has := m[k]; !has {
			return false
		}
	}
	return true
}

// ToCandidateSource 按 sourceId 取静态配置生成 CandidateSource.
// 对齐 TS.toCandidateSource.
func ToCandidateSource(sourceID string) CandidateSource {
	s := GetSourceConfig(sourceID)
	return CandidateSource{
		ID:       s.ID,
		Label:    s.Label,
		Priority: s.Priority,
		Weight:   s.Weight,
		Category: s.Category,
		Reason:   s.Description,
	}
}

// fallbackSourceNames 与 TS.getFallbackSourceNames 一致.
func fallbackSourceNames(sourceID, name string) []string {
	if sourceID == "wealth" {
		return []string{name}
	}
	return nil
}

// HydrateInput hydrateCandidateDb 的入参.
type HydrateInput struct {
	Data             []string           // 紧凑名列表
	SourceID         string
	CharDb           CharDb
	SourceNamesByName map[string][]string // 可空
}

// HydrateCandidateDb 把紧凑字符数组转换为完整 CandidateName.
// 对齐 TS.hydrateCandidateDb. 跳过非 2 字 + 字库未覆盖项.
func HydrateCandidateDb(in HydrateInput) []CandidateName {
	source := ToCandidateSource(in.SourceID)
	out := make([]CandidateName, 0, len(in.Data))
	for _, name := range in.Data {
		chars := SplitChars(name)
		if len(chars) != 2 {
			continue
		}
		info0, ok0 := in.CharDb[chars[0]]
		info1, ok1 := in.CharDb[chars[1]]
		if !ok0 || !ok1 {
			continue
		}
		var sourceNames []string
		if in.SourceNamesByName != nil {
			sourceNames = in.SourceNamesByName[name]
		}
		if len(sourceNames) == 0 {
			sourceNames = fallbackSourceNames(in.SourceID, name)
		}
		var charInfos = [2]CharInfo{info0, info1}
		hasRare := (charInfos[0].Count > 0 && charInfos[0].Count < 3) || (charInfos[1].Count > 0 && charInfos[1].Count < 3)
		isCommon := charInfos[0].Count >= 100 && charInfos[1].Count >= 100
		out = append(out, CandidateName{
			Name:          name,
			Sources:       []CandidateSource{source},
			SourceIDs:     []string{source.ID},
			SourceReasons: []string{source.Reason},
			SourceNames:   sourceNames,
			Chars:         [2]string{chars[0], chars[1]},
			Flags: CandidateFlags{
				HasRareChar: hasRare,
				HasRiskChar: false,
				IsCommon:    isCommon,
			},
		})
	}
	return out
}