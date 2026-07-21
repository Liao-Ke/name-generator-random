# 数据库设计: 随机名字 API

**数据库类型：** PostgreSQL 16  
**真相源：** `server/internal/db/schema.sql`（embed 进 api/import/keymgmt，启动时 `ApplySchema` 幂等执行）  
**回滚：** `server/internal/db/schema.down.sql`

## 表清单

| 表名 | 说明 |
|------|------|
| `chars` | 字库 CharInfo，评分/音韵依赖 |
| `sources` | 五来源元数据（id/label/priority/weight…） |
| `candidates` | 二字段名候选，按 source 分区 |
| `name_source_names` | 候选对应的出处人名/项目名，详情卡用 |
| `api_keys` | API key，可吊销 |
| `surnames` | 百家姓单字池；`/api/random` 缺省姓时随机抽 |

## 关键设计说明

- **为何候选按 source 分行、不用全局 name PK**  
  同一「名」可出现在多源；详情与评分权重依赖来源。唯一约束是 `(source_id, name)`。

- **为何有 `first_char` / `second_char` 冗余列 + 索引**  
  为后续 SQL 预过滤 `must`/`mustPosition` 预留；当前 hydrate 仍是「整源拉 name 列表再内存评分」。

- **`name_source_names` 不挂 name 文本而挂 `candidate_id`**  
  与 candidates 生命周期一致，`ON DELETE CASCADE`；import 时先 candidates 再反查 id 批量 CopyFrom。

- **`surnames` 独立表且 FK → `chars`**  
  缺省姓必须保证在字库内，否则 `GetSurnameLastCharInfo` 会 422。import 额外 `validateSurnames`：对池中每个姓跑一遍能否生成 ≥1 名，删掉不可用姓，避免随机抽到「永远空结果」的姓。

- **`api_keys.revoked_at IS NULL` = 有效**  
  吊销软删，list 可审计；校验路径 + 30s 进程缓存，ConstantTimeCompare。

- **无 migration 版本表**  
  Schema 小且一次性产品，`CREATE IF NOT EXISTS` + import `TRUNCATE` 可重入。破坏性变更走 down + up + 重 import。

## 数据填充

| 步骤 | 来源文件 | 目标表 |
|------|----------|--------|
| chars | `api/database/candidate/candidate_char_db.json` | `chars` |
| sources | `source_index.json` → sourcePriority | `sources` |
| candidates | 各源 `sources/*.json`（index 中 file） | `candidates` |
| name_source_names | 各源 sourceNameFile | `name_source_names` |
| surnames | `baijiaxing.json`（过滤 + validate） | `surnames` |

期望量级（与 source_index 对齐）：chars≈7474，sources=5，candidates=163089，name_source_names≈223405；surnames 为百家姓过滤后子集。

## 索引（见 schema）

- candidates: source / first / second / (source, first) / (source, second) / **uniq (source, name)**
- name_source_names: candidate_id
- api_keys: revoked_at
