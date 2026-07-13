# name-core TS ↔ Go core 映射

本目录是 `packages/name-core/src` 的逐文件镜像移植。TS 版是事实标准，Go 是镜像实现，
输出必须与 TS 版逐位一致（由 `core_test.go` 的 12 套 fixtures 守护）。

## 文件映射

| TS 源 (`packages/name-core/src/`) | Go 目标 (`server/internal/core/`) | 主符号 |
|---|---|---|
| `types.ts` | `types.go` | CandidateName / CharInfo / QueryConfig / PhoneticResult / ScoredCandidate / PublicResult |
| `char.ts` | `char.go` | SplitChars / StripNonChinese / GetSurnameLastCharInfo |
| `sourceConfig.ts` | `source.go` | SourceConfigs / DefaultSourceID / GetSourceConfig |
| `phoneticRules.ts` | `phonetic.go` | ToneScoreMap / NormalizeStyle / EvaluatePhonetics / checkPair |
| `nameRules.ts` | `rules.go` | Negative/Hard/NonName/Cliche 表 / EvaluateSemanticSafety / CheckAvoidRules / CheckMustRules |
| `scoreName.ts` | `score.go` | scoreSource / scoreCharQuality / scoreRarity / scoreExplainability / ScoreCandidate |
| `queryName.ts` | `query.go` | NormalizeQueryConfig / QueryNames / sortResults |
| `candidateRuntime.ts` | `hydrate.go` | IsCandidateName / ToCandidateSource / HydrateCandidateDb |
| `explainName.ts` | `explain.go` | ExplainCandidate / ToPublicResult |

## 同步约定

- 改 TS 必须改 Go。两边都要重跑 fixtures 对照。
- 硬编码字符表 (`negativeCharsSet` 等) 直接搬运，无算法调整。
- `ToneScoreMap` 严格按 key 与 TS 完全一致。
- JSON 字段名（`json:"…"`）与 TS `toPublicResult` 的输出键名严格一致，前端不需要再适配。

## 已知差异（非 bug）

| 位置 | TS | Go | 影响 |
|---|---|---|---|
| 同分排序 | `a.name.localeCompare(b.name, "zh-Hans-CN")` | 字节序 `<` 比较 | 同分时顺序可能不同。Go 未引入 `golang.org/x/text/collate` 以省依赖；2 字中文名按 rune 字符字节序几乎一致；fixtures 已覆盖此差异未触发分歧。若日后出现分歧，引入 `x/text/collate` 与 zh-Hans-CN 表。 |
| `Math.round(x)` | JS 浮点进位 | `jsRound` 仅对正数 +0.5 进位 | scoreCharQuality / scoreRarity 都是正数，行为一致。 |
| 浮点 NaN/Inf | TS `Math.max(0, Math.min(30, base))` | Go 显式 clamp | 等价。 |

## FIXTURES 跑法

```bash
# 1) 生成 TS 对照 fixtures
pnpm install --ignore-scripts
pnpm exec tsx scripts/generateFixtures.ts
# 输出到 server/testdata/fixtures/*.json (12 个)

# 2) Go 对照测试
cd server && go test ./internal/core
```

fixtures 不包含 char_db（约 1.6MB），测试时直接从 PG 拉取字库与候选; 见 `core_test.go`。
PG 由 `podman compose up -d postgres` + `go run ./cmd/import` 启动并填充。