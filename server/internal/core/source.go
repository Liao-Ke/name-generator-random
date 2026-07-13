// Package core 来源于静态配置. 对齐 packages/name-core/src/sourceConfig.ts.
package core

// SourceConfigs 与 TS SOURCE_CONFIGS 完全一致. 顺序即 priority.
var SourceConfigs = []SourceConfig{
	{ID: "wealth", Label: "财富论", Priority: 1, Weight: 14, Category: "wealth",
		Description: "私募基金和公司名中的二字词，按出现频率分位筛选"},
	{ID: "academic", Label: "五道口", Priority: 2, Weight: 17, Category: "academic",
		Description: "科研基金负责人、两院院士等现代稳重姓名，按出现频率分位筛选"},
	{ID: "modern_people", Label: "他山石", Priority: 3, Weight: 16, Category: "modern",
		Description: "现代公开姓名，贴近现实语感"},
	{ID: "imperial_exam", Label: "登科录", Priority: 4, Weight: 9, Category: "historic",
		Description: "历代进士姓名、字号和别号"},
	{ID: "ancient_names", Label: "古人云", Priority: 5, Weight: 8, Category: "historic",
		Description: "古人姓名与字，文化来源较强"},
}

// DefaultSourceID 默认来源. 对齐 TS DEFAULT_SOURCE_ID = SOURCE_CONFIGS[0].id.
const DefaultSourceID = "wealth"

// GetSourceConfig 按 id 取配置. 不存在返回带 fallback 标记的占位配置.
// 对齐 TS.getSourceConfig.
func GetSourceConfig(id string) SourceConfig {
	for _, s := range SourceConfigs {
		if s.ID == id {
			return s
		}
	}
	return SourceConfig{
		ID:          id,
		Label:       id,
		Priority:    99,
		Weight:      1,
		Category:    "modern",
		Description: "未知来源",
	}
}