# 随机名字 API — 架构描述

## 模块职责

```
+--------------------+      +-----------------------+
| cmd/api (main)     | --> | internal/api          | HTTP 路由 + 中间件链
| - 启动 http.Server |      | - router.go           |
| - 优雅停机       |      | - middleware.go: Auth -> RateLimit -> Handler
+--------------------+      | - handler_health/random/name
                            | - deps.go: PG 缓存 (charDB 单例, candidateDB by sourceId)
                            +-----+-------------------+
                                  |
                                  v
+--------------------+      +-----------------------+
| internal/core      | <-- | internal/sampler      |
| - 评分主循环 5G    |      | - uniform/weighted    |
| - 与 TS 严格镜像   |      +-----------------------+
+--------------------+
                                  ^
                                  |
                            +-----+-------------------+
                            | internal/db            |
                            | - pgx pool             |
                            | - schema up/down (embed)|
                            +-----------------------+
                                  ^
                                  |
+--------------------+      +-----+-------------------+
| internal/auth      | <--- | internal/ratelimit    |
| - api_keys PG 校验 |      | - per-IP token bucket |
| - 30s LRU cache    |      +-----------------------+
+--------------------+

+--------------------+      +-----------------------+
| cmd/import (一次性)| --> | PG (chars/candidates/  + name_source_names)
| - JSON -> CopyFrom |      +-----------------------+
+--------------------+

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

### 进程级缓存 charDb (一次 ~3ms) + candidatesBySource (一次 ~50-300ms)
- 不做 LRU 淘汰, 由于 5 source 总内存 ~30 MB 可控.
- 进程重启即清,05284

### 自写 token bucket, 不引 `golang.org/x/time/rate`
- `x/time/rate` 初 tokens=0, 冷启动第一次请求必被拒, 不符合"匿名 RPM 立即可用"语义.
- 自写版本预填 burst, ~120 行单文件, 仅 stdlib 依赖.

### PG 多 key 表, key 管理 CLI 而非 HTTP 端点
- YAGNI: 没人在生产 HTTP 上做 key 自助发放, 反引攻击面对.
- CLI 通过容器 exec / SSH 走. 等真有"SaaS 自助"需求再补 HTTP.

## 数据流

```
匿名 GET /api/random
  -> AuthMiddleware (ctx.ctxKeyAuthed=false)
  -> RateLimitMiddleware.Allow(ip) not OK -> 429 + Retry-After
                                 OK -> 继续
  -> RandomHandler.handleRandom
       deps.GetCharDb (cached)
       deps.GetCandidateDb(sourceId, cached ~300ms cold)
       core.QueryNames(candidates, charDb, query)  // ~50-300ms
       sampler.Sample(results, n, strategy)
       *core.ToPublicResult
       EncodeJSON(200, response, nil, authed=false)

带 X-API-Key 的 GET /api/random
  -> AuthMiddleware -> ctx authed=true
  -> RateLimitMiddleware: AuthedFromCtx(ctx)=true -> 跳过限流
  -> RandomHandler -> EncodeJSON(200, ..., authed=true)
```

## 性能现状 (核心源缓存命中后, p95 单次 random)

| source          | 候选数   | QueryNames 耗时 (cached DB) |
|----------------|---------|------------------------------|
| academic       | 8627    | ~50 ms                       |
| ancient_names | 9518    | ~60 ms                       |
| imperial_exam  | 27377   | ~170 ms                      |
| wealth         | 57327   | ~260 ms                      |
| modern_people  | 60240   | ~310 ms                      |

目标 PRD p95 < 100ms, 目前只有 academic/ancient 满足. 优化方向 (按优先级):

1. 在 SQL 阶段预过滤 `must` (first_char / second_char 条件), 减半候选名单.
2. 把 `EvaluatePhonetics` 的 tonePattern 与 hard-issue 部分 SQL 化, 推下 DB 用 where 子句筛掉易错候选.
3. 把 `QueryNames` top-LIMIT 切片做成 early-termination heap, 不全排序.

## 已知差异

| 位置 | TS 行为 | Go 行为 | 触发影响 |
|---|---|---|---|
| 同分内排序 | `localeCompare("zh-Hans-CN")` ICU 拼音+unicode tiebreak | 主拼音 + Unicode 字节 tiebreak | 极少数同分同拼音不同字序差异; 不影响 score 字段; fixture 严格集对比覆盖. |
| CharDb 缺失容差 | TS 测试期只跑有 charDb 字符候选, Go 同; 均过滤掉缺失候选. | 同 TS. | 无. |