# 随机名字 API — 架构描述

## 模块职责

```
+--------------------+      +-----------------------+
| cmd/api (main)     | --> | internal/api          | HTTP 路由 + 中间件链
| - 启动 http.Server |      | - router.go           |
| - 优雅停机         |      | - middleware.go: Auth -> RateLimit -> Handler
+--------------------+      | - handler_health/help/random/name
                            | - deps.go: 进程缓存 charDb / candidatesBySource / surnames
                            +-----+-------------------+
                                  |
                                  v
+--------------------+      +-----------------------+
| internal/core      | <-- | internal/sampler      |
| - 评分主循环       |      | - uniform/weighted    |
| - 与 TS 严格镜像   |      +-----------------------+
+--------------------+
                                  ^
                                  |
                            +-----+-------------------+
                            | internal/db            |
                            | - pgx pool             |
                            | - schema up/down (embed)|
                            | - 6 表: chars/sources/ |
                            |   candidates/name_source|
                            |   _names/api_keys/     |
                            |   surnames             |
                            +-----------------------+
                                  ^
                                  |
+--------------------+      +-----+-------------------+
| internal/auth      | <--- | internal/ratelimit    |
| - api_keys PG 校验 |      | - per-IP token bucket |
| - 30s LRU cache    |      +-----------------------+
+--------------------+

+--------------------+      +-----------------------+
| cmd/import (一次性)| --> | PG (+ surnames 自     |
| - JSON -> CopyFrom |      |   baijiaxing.json)    |
+--------------------+      +-----------------------+

+--------------------+      +-----------------------+
| cmd/keymgmt        | --> | PG (api_keys)         |
| - issue/list/revoke|      +-----------------------+
+--------------------+
```

## 关键设计决策

### 把 TS name-core 移植到 Go 而非用 tsx 跑服务
- name-core 是纯函数, 跨语言移植成本可量化 (9 个文件, ~700 行).
- Go 二进制部署成本低 (单进程, 无 Node VM 起动开销).
- 代价: TS 端变更需 Go 端同步. 用 `PORTING.md` 记录映射, `core_test.go` 12 套 fixture 守栏.

### 复用 `api/database/candidate/*.json` 作为数据源
- 不再生产第二份候选名数据; import 脚本读现有 JSON 一次入 PG, 后续 API 全部走 PG.
- 另读 `baijiaxing.json` → `surnames`（仅保留 chars 中存在且校验能出名的姓）.

### 缺省 source = 全源合并、缺省 surname = 百家姓随机
- 随机起名「不挑源 / 不挑姓」是更自然的默认; 显式参数收窄范围.
- 全源按 priority 顺序 hydrate 后按 name 去重, 保留首次出现 (高 priority).
- surnames 表在 import 时已过滤 charDb 缺失与「评不出任何名」的姓, 运行时直接抽, 无二次校验.

### 缺省 strategy = uniform
- weighted 需要调 alpha, 对「随便来几个」不友好; uniform 更可预期.
- 需要偏高分时显式 `strategy=weighted`.

### 进程级缓存 charDb + candidatesBySource + surnames
- 不做 LRU 淘汰, 5 source 总内存可控.
- 进程重启即清.

### 自写 token bucket, 不引 `golang.org/x/time/rate`
- `x/time/rate` 初 tokens=0, 冷启动第一次请求必被拒, 不符合"匿名 RPM 立即可用"语义.
- 自写版本预填 burst, 仅 stdlib 依赖.
- 全部匿名 `/api/*`（含 help/health）扣桶; 成功响应暂不回写 RateLimit 头（仅 429 带）.

### PG 多 key 表, key 管理 CLI 而非 HTTP 端点
- YAGNI: 没人在生产 HTTP 上做 key 自助发放, 反引攻击面.
- CLI 通过容器 exec / SSH 走. 等真有"SaaS 自助"需求再补 HTTP.

## 数据流

```
匿名 GET /api/random  (无 surname / 无 source)
  -> AuthMiddleware (ctx authed=false)
  -> RateLimitMiddleware.Allow(ip) not OK -> 429 + Retry-After + 限流头
                                 OK -> 继续
  -> handleRandom
       deps.GetCharDb (cached)
       surname 空 -> pickRandomSurname(deps.GetSurnames)  // 表空兜底「张」
       source 空  -> loadAllCandidates (五源 hydrate + name 去重)
       core.QueryNames(candidates, charDb, query)  // sourcePreference="default"
       sampler.Sample(..., strategy 默认 uniform)
       EncodeJSON(200, {source:{id:"all",label:"全部来源",...}, ...}, rateInfo=nil)

带 source=academic + X-API-Key
  -> AuthMiddleware -> authed=true
  -> RateLimitMiddleware 跳过
  -> GetCandidateDb("academic")
  -> sourcePreference="academic", source.id="academic"
```

## 性能现状 (2026-09 实测, 真实库 + PG 容器)

| source | 候选数 | 通过集 | 端到端耗时 | 优化前 |
|--------|--------|--------|------------|--------|
| academic | 8627 | 4075 | 16 ms | 21 ms |
| ancient_names | 9518 | — | ~30 ms | ~60 ms |
| imperial_exam | 27377 | — | ~80 ms | ~170 ms |
| modern_people | 60240 | 31011 | 114 ms | 234 ms |
| wealth | 57327 | — | ~130 ms | ~260 ms |
| **all (合并)** | 去重后 138856 | 68210 | **247-273 ms**（首访 555 ms 含合并） | 586 ms |

目标 PRD p95 < 100 ms：学术/古人云已达标，大源与全源仍超出，但全源已从 586 ms 降到约 260 ms（-56%）。

### 已完成的优化（2026-09）

1. **结果集改轻量中间体**（`query.go`）：`ScoredCandidate` 实测 704 字节、含 11 个指针字段，原先直接累积十万级结果 —— 切片扩容反复复制重结构体（单次调用分配约 900 MB，GC 扫描占 CPU 约 40%）。改为只累积 88 字节的 `lightResult`（名字 + 排序键 + 总分 + 音韵/语义摘要），排序截断后仅为最终入选的 limit 条构造完整对象。
   实测：分配 1361 MB → 712 MB，`QueryNames` 486 ms → 246 ms。
2. **排序键简化**：排序不再每次比较都 `SplitChars` + 两次 map 查找，改为填充时预计算逐字 `拼音键+调号+原字符`，语义等价（fixture 逐字段对照 12 套 / 2091 条结果通过）。
3. **全源合并结果缓存**（`handler_random.go` + `deps.go`）：合并 13.8 万条去重实测分配约 32 MB，而候选池进程内静态，改为构造一次后复用（双检锁）。全源请求 586 ms → 热路径约 260 ms。
4. **算分只做一次**：`scoreTotal` 一次求和, 排序与最终构造复用（`ScoreCandidateInput.TotalScore`），避免对同一候选重复累计。

### 后续优化方向（按实测收益排序）

1. **音韵评估仍是最大热点**：`EvaluatePhonetics` 单独测算 88 ms（全源路径的约 1/3），其 `checkPair` 每个候选分配一个 issue 切片（84 MB/3 次调用）。可做 (姓末字, 字1, 字2, style) 缓存 —— 同姓氏下大量候选共享相同字对；注意返回的 `Issues` 切片会被共享，必须只读使用。
2. **只为选中结果构造完整对象**：`/api/random` 只采样 n 条（默认 5），当前仍为 limit 条（默认 200）构造完整 `ScoredCandidate` 与 Reasons 字符串。把采样下移到轻量结果层可省这部分。
3. 在 SQL 阶段预过滤 `must` / `avoid`，减少进入评分循环的候选。
4. 全源路径并行 hydrate。

### 已被实测否定的假设

- ~~"加权采样 O(N·K) 是瓶颈"~~ —— 实测 uniform 246 ms vs weighted 238 ms，差异在噪声内，采样不是瓶颈。
- ~~"切片扩容是主因，改成完整预分配即可"~~ —— 单独完整预分配只带来约 9% 改善（486→443 ms），真正的原因是重结构体被反复复制，需要换掉累积结构而非只调容量。

## 已知差异

| 位置 | TS 行为 | Go 行为 | 触发影响 |
|---|---|---|---|
| 同分内排序 | `localeCompare("zh-Hans-CN")` ICU | 主拼音 + Unicode 字节 tiebreak | 极少数同分同拼音不同字序差异; fixture 严格集对比覆盖. |
| CharDb 缺失容差 | 过滤掉缺失候选 | 同 TS | 无. |
| RateLimit 成功头 | (N/A, 原前端无) | 成功响应不带 X-RateLimit-* | 客户端只能在 429 看到桶状态. |
