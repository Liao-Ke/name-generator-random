// Package core 评分. 对齐 packages/name-core/src/scoreName.ts.
package core

import (
	"fmt"
	"strings"
)

// scoreSource 来源评分. 对齐 TS.scoreSource.
func scoreSource(candidate CandidateName) int {
	if len(candidate.Sources) == 0 {
		return 0
	}
	best := 0
	for _, s := range candidate.Sources {
		if s.Weight > best {
			best = s.Weight
		}
	}
	hasPersonSource := false
	for _, s := range candidate.Sources {
		if s.Category != "wealth" {
			hasPersonSource = true
			break
		}
	}
	multiSourceBonus := (len(candidate.Sources) - 1) * 2
	if multiSourceBonus > 5 {
		multiSourceBonus = 5
	}
	personSourceBonus := 0
	if hasPersonSource {
		personSourceBonus = 3
	}
	result := best + multiSourceBonus + personSourceBonus
	if result > 22 {
		result = 22
	}
	return result
}

// scoreCharQuality 单字质量评分. 对齐 TS.scoreCharQuality.
func scoreCharQuality(chars [2]CharInfo) int {
	charScores := make([]int, 2)
	for i, char := range chars {
		switch {
		case char.Count >= 100:
			charScores[i] = 8
		case char.Count >= 50:
			charScores[i] = 7
		case char.Count >= 10:
			charScores[i] = 6
		case char.Count >= 3:
			charScores[i] = 4
		case char.Count > 0:
			charScores[i] = 2
		default:
			charScores[i] = 1
		}
	}
	sum := 0
	for _, s := range charScores {
		sum += s
	}
	// 与 TS 一致: Math.round(sum/length)
	return jsRound(float64(sum) / float64(len(charScores)))
}

// scoreRarity 稀有度评分. 对齐 TS.scoreRarity.
func scoreRarity(chars [2]CharInfo) int {
	total := 0
	for _, char := range chars {
		total += char.Count
	}
	avg := float64(total) / float64(len(chars))
	if avg <= 0 {
		return 1
	}
	if avg < 3 {
		return 2
	}
	if avg <= 300 {
		return 5
	}
	if avg <= 800 {
		return 4
	}
	return 2
}

// scoreExplainability 可解释性评分. 对齐 TS.scoreExplainability.
func scoreExplainability(candidate CandidateName, phonetic PhoneticResult, semantic SemanticResult) int {
	score := 2
	if len(candidate.SourceReasons) > 0 {
		score += 2
	}
	if phonetic.Summary != "" {
		score += 1
	}
	if semantic.Summary != "" {
		score += 1
	}
	if score > 6 {
		score = 6
	}
	return score
}

type ScoreCandidateInput struct {
	Surname   string
	Candidate CandidateName
	Chars     [2]CharInfo
	Phonetic  PhoneticResult
	Semantic  SemanticResult
	// TotalScore 非 0 时直接作为总分, 跳过内部累计.
	// 供 QueryNames 复用已算好的分数, 避免对同一候选二次求和 (10 万级候选下是双倍算术).
	// NOTE 0 表示"未提供", 走内部累计; 总分理论下限大于 0, 不会与真实值冲突.
	TotalScore int
}

// scoreTotal 六项分项之和. QueryNames 一次算分, 供排序与最终构造共用.
func scoreTotal(candidate CandidateName, chars [2]CharInfo, phonetic PhoneticResult, semantic SemanticResult) int {
	return semantic.Score +
		phonetic.Score +
		scoreSource(candidate) +
		scoreExplainability(candidate, phonetic, semantic) +
		scoreCharQuality(chars) +
		scoreRarity(chars)
}

// ScoreCandidate 主评分. 对齐 TS.scoreCandidate.
func ScoreCandidate(in ScoreCandidateInput) ScoredCandidate {
	breakdown := ScoreBreakdown{
		Semantic:       in.Semantic.Score,
		Phonetic:       in.Phonetic.Score,
		Source:         scoreSource(in.Candidate),
		Explainability: scoreExplainability(in.Candidate, in.Phonetic, in.Semantic),
		CharQuality:    scoreCharQuality(in.Chars),
		Rarity:         scoreRarity(in.Chars),
	}
	score := in.TotalScore
	if score == 0 {
		score = breakdown.Semantic + breakdown.Phonetic + breakdown.Source +
			breakdown.Explainability + breakdown.CharQuality + breakdown.Rarity
	}

	sourceNames := make([]string, 0, len(in.Candidate.Sources))
	for _, s := range in.Candidate.Sources {
		sourceNames = append(sourceNames, s.Label)
	}
	sourceNamesStr := strings.Join(sourceNames, "、")
	if sourceNamesStr == "" {
		sourceNamesStr = "未知"
	}

	hasRareClause := "用字频率可接受"
	if in.Candidate.Flags.HasRareChar {
		hasRareClause = "包含低频字，已降低用字分"
	}

	reasons := []string{}
	if in.Semantic.Summary != "" {
		reasons = append(reasons, in.Semantic.Summary)
	}
	if in.Phonetic.Summary != "" {
		reasons = append(reasons, in.Phonetic.Summary)
	}
	reasons = append(reasons, fmt.Sprintf("来源：%s", sourceNamesStr))
	reasons = append(reasons, hasRareClause)

	return ScoredCandidate{
		FullName:  in.Surname + in.Candidate.Name,
		Name:      in.Candidate.Name,
		Score:     score,
		Breakdown: breakdown,
		Candidate: in.Candidate,
		Chars:     in.Chars,
		Phonetic:  in.Phonetic,
		Semantic:  in.Semantic,
		Reasons:   reasons,
	}
}

// jsRound 复刻 JS Math.round: 0.5 进位 (正数方向).
func jsRound(f float64) int {
	// Math.round(-2.5) = -2; Math.round(2.5) = 3.
	// 在我们的语境下 sum/2 全为非负, 直接 0.5 进位即可.
	if f >= 0 {
		return int(f + 0.5)
	}
	// 向 +Inf 舍入
	return int(f - 0.5) // 实际为负场景, 与本项目无关
}
