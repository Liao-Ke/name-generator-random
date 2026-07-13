// Package core 公共输出. 对齐 packages/name-core/src/explainName.ts.
package core

import (
	"fmt"
	"strings"
)

// ExplainCandidate 单名综合文字说明. 对齐 TS.explainCandidate.
func ExplainCandidate(result ScoredCandidate) string {
	sourceNames := make([]string, 0, len(result.Candidate.Sources))
	for _, s := range result.Candidate.Sources {
		sourceNames = append(sourceNames, s.Label)
	}
	sourceNamesStr := strings.Join(sourceNames, "、")
	if sourceNamesStr == "" {
		sourceNamesStr = "未知"
	}
	parts := []string{
		fmt.Sprintf("%s：总分 %d", result.FullName, result.Score),
		fmt.Sprintf("来源：%s", sourceNamesStr),
	}
	if result.Semantic.Summary != "" {
		parts = append(parts, "避讳："+result.Semantic.Summary)
	}
	parts = append(parts, "音律："+result.Phonetic.Summary)
	out := []string{}
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "；")
}

// ToPublicResult 将内部 ScoredCandidate 投射为对外结构.
// 对齐 TS.toPublicResult, JSON 字段顺序也尽量一致.
func ToPublicResult(result ScoredCandidate) PublicResult {
	sources := make([]string, 0, len(result.Candidate.Sources))
	for _, s := range result.Candidate.Sources {
		sources = append(sources, s.Label)
	}
	pinyin := make([]string, 0, len(result.Chars))
	for _, c := range result.Chars {
		pinyin = append(pinyin, c.Pinyin)
	}
	sourceNames := result.Candidate.SourceNames
	if sourceNames == nil {
		sourceNames = []string{}
	}
	return PublicResult{
		FullName:    result.FullName,
		Name:        result.Name,
		Score:       result.Score,
		Breakdown:   result.Breakdown,
		Sources:     sources,
		SourceNames: sourceNames,
		Pinyin:      pinyin,
		TonePattern: result.Phonetic.TonePattern,
		Semantic:    result.Semantic.Summary,
		Phonetic:    result.Phonetic.Summary,
		Reasons:     result.Reasons,
		Explanation: ExplainCandidate(result),
	}
}