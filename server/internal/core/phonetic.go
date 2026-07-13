// Package core 发音评分. 对齐 packages/name-core/src/phoneticRules.ts.
package core

import (
	"fmt"
	"strings"
)

// toneScoreMap 平仄组合评分, 与 TS TONE_SCORE_MAP 严格一致.
// key 由三个声调数字直接拼接 (e.g. "133").
var toneScoreMap = map[string]int{
	"111": 2, "112": 3, "113": 3, "114": 3,
	"121": 5, "122": 3, "123": 4, "124": 4,
	"131": 3, "132": 3, "133": 1, "134": 3,
	"141": 4, "142": 4, "143": 4, "144": 2,
	"211": 3, "212": 5, "213": 4, "214": 4,
	"221": 3, "222": 2, "223": 3, "224": 3,
	"231": 3, "232": 3, "233": 1, "234": 3,
	"241": 4, "242": 5, "243": 4, "244": 2,
	"311": 3, "312": 4, "313": 4, "314": 4,
	"321": 4, "322": 3, "323": 4, "324": 4,
	"331": 1, "332": 1, "333": 1, "334": 1,
	"341": 4, "342": 4, "343": 4, "344": 1,
	"411": 3, "412": 5, "413": 5, "414": 5,
	"421": 5, "422": 3, "423": 5, "424": 5,
	"431": 3, "432": 3, "433": 1, "434": 3,
	"441": 2, "442": 2, "443": 2, "444": 1,
}

// NormalizeStyle 风格归一. 对齐 TS.normalizeStyle.
// "响亮"/"loud" -> "loud"; "柔和"/"soft" -> "soft"; 其余 -> "any".
func NormalizeStyle(style string) string {
	switch style {
	case "响亮", "loud":
		return "loud"
	case "柔和", "soft":
		return "soft"
	default:
		return "any"
	}
}

// checkPair 检查相邻两字的发音冲突. 对齐 TS.checkPair.
func checkPair(left, right CharInfo, label string) []PhoneticIssue {
	var issues []PhoneticIssue

	if left.PinyinNoTone == right.PinyinNoTone {
		issues = append(issues, PhoneticIssue{
			Level: "hard", Code: "same_pinyin",
			Message: fmt.Sprintf("%s「%s%s」同音，读起来容易混", label, left.Char, right.Char),
		})
	}
	if left.Tone == right.Tone {
		issues = append(issues, PhoneticIssue{
			Level: "warn", Code: "same_tone",
			Message: fmt.Sprintf("%s「%s%s」声调相同，音律分降低", label, left.Char, right.Char),
		})
	}
	if left.InitialMethod == right.InitialMethod {
		issues = append(issues, PhoneticIssue{
			Level: "hard", Code: "same_initial_method",
			Message: fmt.Sprintf("%s「%s%s」声母发音方法重复", label, left.Char, right.Char),
		})
	}
	if left.InitialPlace == right.InitialPlace {
		issues = append(issues, PhoneticIssue{
			Level: "hard", Code: "same_initial_place",
			Message: fmt.Sprintf("%s「%s%s」声母发音部位重复", label, left.Char, right.Char),
		})
	}
	if left.VowelType == right.VowelType {
		issues = append(issues, PhoneticIssue{
			Level: "hard", Code: "same_vowel_type",
			Message: fmt.Sprintf("%s「%s%s」韵母类别重复，容易叠韵", label, left.Char, right.Char),
		})
	}
	return issues
}

// EvaluatePhonetics 三字名发音综合评分.
// 输入: 姓氏末字 + 名的第 1/2 字 + 风格.
// 对齐 TS.evaluatePhonetics.
func EvaluatePhonetics(surnameLast, first, second CharInfo, style string) PhoneticResult {
	normalizedStyle := NormalizeStyle(style)
	tonePattern := fmt.Sprintf("%d%d%d", surnameLast.Tone, first.Tone, second.Tone)
	toneScore, ok := toneScoreMap[tonePattern]
	if !ok {
		toneScore = 3
	}

	issues := []PhoneticIssue{}
	issues = append(issues, checkPair(surnameLast, first, "姓和名首字")...)
	issues = append(issues, checkPair(first, second, "名字内部")...)

	if toneScore <= 2 {
		issues = append(issues, PhoneticIssue{
			Level: "warn", Code: "weak_tone_pattern",
			Message: fmt.Sprintf("声调组合 %s 音律评分偏低", tonePattern),
		})
	}

	if normalizedStyle == "loud" && (second.Tone < 2 || second.Tone > 4) {
		issues = append(issues, PhoneticIssue{
			Level: "warn", Code: "style_loud_mismatch",
			Message: "响亮取向更适合二、三、四声收尾",
		})
	}
	if normalizedStyle == "soft" && (second.Tone != 1 && second.Tone != 3) {
		issues = append(issues, PhoneticIssue{
			Level: "warn", Code: "style_soft_mismatch",
			Message: "柔和取向更适合一声或三声收尾",
		})
	}

	hardIssueCount := 0
	warnIssueCount := 0
	for _, is := range issues {
		if is.Level == "hard" {
			hardIssueCount++
		} else {
			warnIssueCount++
		}
	}
	baseScore := toneScore*5 + 5
	score := baseScore - warnIssueCount*3 - hardIssueCount*8
	if score < 0 {
		score = 0
	}
	if score > 30 {
		score = 30
	}

	summary := "读音顺畅，未发现同音、叠声或叠韵问题"
	if len(issues) > 0 {
		parts := make([]string, 0, len(issues))
		for _, is := range issues {
			parts = append(parts, is.Message)
		}
		summary = strings.Join(parts, "；")
	}

	return PhoneticResult{
		Pass:        hardIssueCount == 0,
		Score:       score,
		ToneScore:   toneScore,
		TonePattern: tonePattern,
		Issues:      issues,
		Summary:     summary,
	}
}