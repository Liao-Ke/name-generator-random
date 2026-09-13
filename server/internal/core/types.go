// Package core 是 packages/name-core/src 的 Go 移植.
// 与 TS 版逐字段对照, 评分行为必须位级一致, 由 PORTING.md 维护映射.
// 移植原则: 字符表硬编码原样搬运, 不重新设计算法.
package core

// Tone 声调, 1=阴平 2=阳平 3=上声 4=去声. 对应 TS Tone.
type Tone = int

// CandidateName 候选名结构. 对齐 packages/name-core/src/types.ts.CandidateName.
type CandidateName struct {
	Name          string            `json:"name"`
	Sources       []CandidateSource `json:"sources"`
	SourceIDs     []string          `json:"sourceIds"`
	SourceReasons []string          `json:"sourceReasons"`
	SourceNames   []string          `json:"sourceNames,omitempty"`
	Chars         [2]string         `json:"chars"`
	Flags         CandidateFlags    `json:"flags"`
}

type CandidateFlags struct {
	HasRareChar bool `json:"hasRareChar"`
	HasRiskChar bool `json:"hasRiskChar"`
	IsCommon    bool `json:"isCommon"`
}

// CharInfo 汉字信息. 对齐 packages/name-core/src/types.ts.CharInfo.
type CharInfo struct {
	Char          string `json:"char"`
	Pinyin        string `json:"pinyin"`
	Tone          Tone   `json:"tone"`
	PinyinNoTone  string `json:"pinyinWithoutTone"`
	Initial       string `json:"initial"`
	InitialMethod string `json:"initialMethod"`
	InitialPlace  string `json:"initialPlace"`
	Vowel         string `json:"vowel"`
	VowelType     string `json:"vowelType"`
	Count         int    `json:"count"`
	IsPolyphone   bool   `json:"isPolyphone"`
}

// CharDb 一个 char -> CharInfo 的 map. 对齐 TS CharDb.
type CharDb = map[string]CharInfo

// CandidateSource 单候选的来源信息. 对齐 TS.CandidateSource.
type CandidateSource struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

// SourceConfig 来源静态配置. 对齐 TS.SourceConfig.
type SourceConfig struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Priority    int    `json:"priority"`
	Weight      int    `json:"weight"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// QueryConfig 查询参数. 对齐 TS.QueryConfig.
type QueryConfig struct {
	Surname          string   `json:"surname"`
	Avoid            []string `json:"avoid,omitempty"`
	Must             []string `json:"must,omitempty"`
	MustPosition     string   `json:"mustPosition,omitempty"`
	Style            string   `json:"style,omitempty"`
	SourcePreference string   `json:"sourcePreference,omitempty"`
	Limit            int      `json:"limit,omitempty"`
	OutputPath       string   `json:"outputPath,omitempty"`
}

// PhoneticIssue 单条发音问题. 对齐 TS.PhoneticIssue.
type PhoneticIssue struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PhoneticResult 发音整体结果. 对齐 TS.PhoneticResult.
type PhoneticResult struct {
	Pass        bool            `json:"pass"`
	Score       int             `json:"score"`
	ToneScore   int             `json:"toneScore"`
	TonePattern string          `json:"tonePattern"`
	Issues      []PhoneticIssue `json:"issues"`
	Summary     string          `json:"summary"`
}

// SemanticResult 语义结果. 对齐 TS.SemanticResult.
type SemanticResult struct {
	Pass    bool     `json:"pass"`
	Score   int      `json:"score"`
	Issues  []string `json:"issues"`
	Summary string   `json:"summary"`
}

// AvoidResult 避讳检查结果. 对齐 TS.AvoidResult.
type AvoidResult struct {
	Pass   bool     `json:"pass"`
	Issues []string `json:"issues"`
}

// ScoreBreakdown 各分项分数. 对齐 TS.ScoreBreakdown.
type ScoreBreakdown struct {
	Semantic       int `json:"semantic"`
	Phonetic       int `json:"phonetic"`
	Source         int `json:"source"`
	Explainability int `json:"explainability"`
	CharQuality    int `json:"charQuality"`
	Rarity         int `json:"rarity"`
}

// ScoredCandidate 评分后的候选. 对齐 TS.ScoredCandidate.
type ScoredCandidate struct {
	FullName  string         `json:"fullName"`
	Name      string         `json:"name"`
	Score     int            `json:"score"`
	Breakdown ScoreBreakdown `json:"breakdown"`
	Candidate CandidateName  `json:"candidate"`
	Chars     [2]CharInfo    `json:"chars"`
	Phonetic  PhoneticResult `json:"phonetic"`
	Semantic  SemanticResult `json:"semantic"`
	Reasons   []string       `json:"reasons"`
}

// PublicResult 对外发布的精简结构. 对齐 TS.toPublicResult 的输出顺序.
type PublicResult struct {
	FullName    string         `json:"fullName"`
	Name        string         `json:"name"`
	Score       int            `json:"score"`
	Breakdown   ScoreBreakdown `json:"breakdown"`
	Sources     []string       `json:"sources"`
	SourceNames []string       `json:"sourceNames"`
	Pinyin      []string       `json:"pinyin"`
	TonePattern string         `json:"tonePattern"`
	Semantic    string         `json:"semantic"`
	Phonetic    string         `json:"phonetic"`
	Reasons     []string       `json:"reasons"`
	Explanation string         `json:"explanation"`
}
