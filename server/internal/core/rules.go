// Package core 名字规则: 避讳, 必选字, 语义安全. 对齐 packages/name-core/src/nameRules.ts.
package core

import (
	"strings"
)

// 负面字表. 对齐 TS.NEGATIVE_CHARS.
var negativeCharsSet = setFromRunes("病痛贫穷丧死鬼凶恶丑残废毒赌骗贱祸灾")

// 高风险名占位. 对齐 TS.HARD_RISK_NAMES.
var hardRiskNamesSet = map[string]struct{}{
	"赵钱孙": {}, "钱孙": {}, "建国": {}, "待删": {}, "删除": {}, "无名": {},
}

// 不像人名的普通词. 对齐 TS.NON_NAME_WORDS.
var nonNameWordsSet = map[string]struct{}{
	"帮扶": {}, "不繁": {}, "不绝": {}, "蜂巢": {}, "赫兹": {}, "盖伦": {},
	"伽罗": {}, "伽缘": {}, "麦兹": {}, "柒零": {}, "轻盐": {}, "熵零": {},
	"狮王": {}, "视频": {}, "仙桃": {}, "耕耘": {},
}

// 单字风险集. 对齐 TS.NON_NAME_CHARS.
var nonNameCharsSet = setFromRunes("频熵巢狮盐兹柒零")

// 老套名集. 对齐 TS.CLICHE_NAMES.
var clicheNamesSet = map[string]struct{}{
	"建军": {}, "建华": {}, "建民": {}, "建平": {}, "国庆": {}, "国强": {},
	"伟强": {}, "志强": {}, "富贵": {}, "发财": {},
}

func setFromRunes(s string) map[string]struct{} {
	m := make(map[string]struct{}, len([]rune(s)))
	for _, r := range s {
		m[string(r)] = struct{}{}
	}
	return m
}

// NormalizeMustPosition 必选字位置归一. 对齐 TS.normalizeMustPosition.
func NormalizeMustPosition(p string) string {
	switch p {
	case "第二位", "second":
		return "second"
	case "第三位", "third":
		return "third"
	default:
		return "any"
	}
}

// EvaluateSemanticSafety 语义安全评分. 对齐 TS.evaluateSemanticSafety.
func EvaluateSemanticSafety(fullName, name string) SemanticResult {
	issues := []string{}
	score := 35

	runes := SplitChars(name)
	if len(runes) != 2 {
		issues = append(issues, "候选名不是二字名，不符合当前项目目标")
		score = 0
	}

	if _, ok := hardRiskNamesSet[fullName]; ok {
		issues = append(issues, "命中明显风险名或占位式组合")
		score = 0
	}
	if _, ok := hardRiskNamesSet[name]; ok {
		issues = append(issues, "命中明显风险名或占位式组合")
		score = 0
	}

	if _, ok := nonNameWordsSet[name]; ok {
		issues = append(issues, "更像产品名、概念词或普通词语，不像人名")
		if score > 10 {
			score = 10
		}
	}

	if _, ok := clicheNamesSet[name]; ok {
		issues = append(issues, "命中过于常见或时代感过强的组合")
		if score > 8 {
			score = 8
		}
	}

	for _, ch := range SplitChars(name) {
		if _, ok := negativeCharsSet[ch]; ok {
			issues = append(issues, "包含明显负面字「"+ch+"」")
			score = 0
		}
		if _, ok := nonNameCharsSet[ch]; ok {
			issues = append(issues, "「"+ch+"」用于人名风险较高")
			if score > 12 {
				score = 12
			}
		}
	}

	summary := ""
	if len(issues) > 0 {
		summary = strings.Join(issues, "；")
	}
	return SemanticResult{
		Pass:    score > 0,
		Score:   score,
		Issues:  issues,
		Summary: summary,
	}
}

// CheckAvoidRules 检查与避讳名单的冲突. 对齐 TS.checkAvoidRules.
type CheckAvoidInput struct {
	Candidate CandidateName
	Surname   string
	Avoid     []string
	CharDb    CharDb
}

func CheckAvoidRules(in CheckAvoidInput) AvoidResult {
	var issues []string
	if len(in.Avoid) == 0 {
		return AvoidResult{Pass: true, Issues: issues}
	}

	fullName := in.Surname + in.Candidate.Name
	rawAvoidSet := make(map[string]struct{}, len(in.Avoid))
	for _, item := range in.Avoid {
		v := StripNonChinese(item)
		if v != "" {
			rawAvoidSet[v] = struct{}{}
		}
	}
	if _, ok := rawAvoidSet[fullName]; ok {
		issues = append(issues, "候选名与避讳名单完全相同")
	}
	if _, ok := rawAvoidSet[in.Candidate.Name]; ok {
		if !contains(issues, "候选名与避讳名单完全相同") {
			issues = append(issues, "候选名与避讳名单完全相同")
		}
	}

	avoidChars := make(map[string]struct{})
	avoidPinyinNoTone := make(map[string]struct{})
	for _, item := range in.Avoid {
		for _, ch := range SplitChars(StripNonChinese(item)) {
			avoidChars[ch] = struct{}{}
			if info, ok := in.CharDb[ch]; ok {
				avoidPinyinNoTone[info.PinyinNoTone] = struct{}{}
			}
		}
	}

	for _, ch := range in.Candidate.Chars {
		info, ok := in.CharDb[ch]
		if !ok {
			issues = append(issues, "字库缺少「"+ch+"」")
			continue
		}
		if _, ok := avoidChars[info.Char]; ok {
			issues = append(issues, "包含需要避开的字「"+info.Char+"」")
		}
		if _, ok := avoidPinyinNoTone[info.PinyinNoTone]; ok {
			issues = append(issues, "「"+info.Char+"」与避讳字存在同音风险")
		}
	}

	return AvoidResult{
		Pass:   len(issues) == 0,
		Issues: issues,
	}
}

func contains(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}

// CheckMustRules 检查必选字规则. 对齐 TS.checkMustRules.
type CheckMustInput struct {
	Candidate    CandidateName
	Must         []string
	MustPosition string
}

func CheckMustRules(in CheckMustInput) bool {
	mustChars := make(map[string]struct{})
	for _, item := range in.Must {
		for _, ch := range SplitChars(StripNonChinese(item)) {
			mustChars[ch] = struct{}{}
		}
	}
	if len(mustChars) == 0 {
		return true
	}

	position := NormalizeMustPosition(in.MustPosition)
	first := in.Candidate.Chars[0]
	second := in.Candidate.Chars[1]
	switch position {
	case "second":
		_, ok := mustChars[first]
		return ok
	case "third":
		_, ok := mustChars[second]
		return ok
	default:
		_, okFirst := mustChars[first]
		_, okSecond := mustChars[second]
		return okFirst || okSecond
	}
}

// CollectKnownPinyin 取已知拼音(不含音调). 对齐 TS.collectKnownPinyin.
func CollectKnownPinyin(charDb CharDb, input string) []string {
	infos := GetStringCharInfos(charDb, input)
	out := make([]string, 0, len(infos))
	for _, info := range infos {
		out = append(out, info.PinyinNoTone)
	}
	return out
}
