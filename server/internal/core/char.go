// Package core 字符工具. 对齐 packages/name-core/src/char.ts.
package core

import "unicode/utf8"

// CJK_Rune_CJK 表意文字区扩展 A + 基本. 与 TS 版 `[\u3400-\u9fff]` 对齐.
func isCJK(r rune) bool {
	return r >= 0x3400 && r <= 0x9fff
}

// IsChineseChar 单个字符首 rune 是否为 CJK. 对齐 TS.isChineseChar.
func IsChineseChar(s string) bool {
	if s == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s)
	return isCJK(r)
}

// SplitChars 按 Unicode code point 切分字符串, 对齐 TS.splitChars (Array.from).
func SplitChars(input string) []string {
	out := make([]string, 0, utf8.RuneCountInString(input))
	for _, r := range input {
		out = append(out, string(r))
	}
	return out
}

// StripNonChinese 仅保留 CJK 字符. 对齐 TS.stripNonChinese.
func StripNonChinese(input string) string {
	out := make([]rune, 0, len(input))
	for _, r := range input {
		if isCJK(r) {
			out = append(out, r)
		}
	}
	return string(out)
}

// GetCharInfo 从字库取字符 CharInfo, 不存在返回 nil.
// 对齐 TS.getCharInfo.
func GetCharInfo(charDb CharDb, char string) (CharInfo, bool) {
	c, ok := charDb[char]
	return c, ok
}

// GetStringCharInfos 取整串字符信息, 过滤缺失字符.
// 对齐 TS.getStringCharInfos.
func GetStringCharInfos(charDb CharDb, input string) []CharInfo {
	chars := SplitChars(StripNonChinese(input))
	out := make([]CharInfo, 0, len(chars))
	for _, ch := range chars {
		if info, ok := charDb[ch]; ok {
			out = append(out, info)
		}
	}
	return out
}

// GetSurnameLastCharInfo 取姓氏最后一字的 CharInfo, 缺失报错.
// 对齐 TS.getSurnameLastCharInfo.
func GetSurnameLastCharInfo(charDb CharDb, surname string) (CharInfo, error) {
	infos := GetStringCharInfos(charDb, surname)
	if len(infos) == 0 {
		return CharInfo{}, ErrSurnameNotInCharDb("姓氏「" + surname + "」读音未知, 检查字库是否包含")
	}
	return infos[len(infos)-1], nil
}

// ErrSurnameNotInCharDb 姓氏不在字库的错误类型.
type ErrSurnameNotInCharDb string

func (e ErrSurnameNotInCharDb) Error() string { return string(e) }
