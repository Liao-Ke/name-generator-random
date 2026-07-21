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

## 性能现状 (单源缓存命中后, p95 单次 QueryNames)

| source         | 候选数 | QueryNames 耗时 (cached DB) |
|----------------|--------|------------------------------|
| academic       | 8627   | ~50 ms                       |
| ancient_names  | 9518   | ~60 ms                       |
| imperial_exam  | 27377  | ~170 ms                      |
| wealth         | 57327  | ~260 ms                      |
| modern_people  | 60240  | ~310 ms                      |
| **all (合并)** | ~去重后 ≤163k | 冷/热均更重, 本期接受 |

目标 PRD p95 < 100ms, 目前只有 academic/ancient 单源满足. 优化方向 (按优先级):

1. 在 SQL 阶段预过滤 `must` (first_char / second_char 条件), 减半候选名单.
2. 把 `EvaluatePhonetics` 的 tonePattern 与 hard-issue 部分 SQL 化.
3. 把 `QueryNames` top-LIMIT 切片做成 early-termination heap, 不全排序.
4. 全源路径: 并行 hydrate 或按源采样后再合并.

## 已知差异

| 位置 | TS 行为 | Go 行为 | 触发影响 |
|---|---|---|---|
| 同分内排序 | `localeCompare("zh-Hans-CN")` ICU | 主拼音 + Unicode 字节 tiebreak | 极少数同分同拼音不同字序差异; fixture 严格集对比覆盖. |
| CharDb 缺失容差 | 过滤掉缺失候选 | 同 TS | 无. |
| RateLimit 成功头 | (N/A, 原前端无) | 成功响应不带 X-RateLimit-* | 客户端只能在 429 看到桶状态. |
