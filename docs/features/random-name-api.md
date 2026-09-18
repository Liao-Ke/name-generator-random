# 随机名字 API — 功能记录

## 实施范围

把 `packages/name-core` (TS) 的中文评分能力移植到 Go, 在 PG 中存储候选名 + 字库,
构建 HTTP API 提供随机起名 + 单名详情. 与原 Taro 前端共存, 前端改动零.

### 改动文件 / 新增目录

| 路径 | 类型 | 说明 |
|------|------|------|
| `docker-compose.yml` | 新增 | Postgres 16 + api 两服务 |
| `server/go.mod` `server/go.sum` | 新增 | module `github.com/namegen/server` |
| `server/Dockerfile` | 新增 | 多阶段；产出 api / import / keymgmt 三二进制 |
| `server/internal/db/` | 新增 | `pg.go` + `schema.sql` / `schema.down.sql`（6 表） |
| `server/internal/config/config.go` | 新增 | env 读取 |
| `server/cmd/import/` | 新增 | JSON → PG（chars / sources / candidates / name_source_names / surnames） |
| `server/internal/core/` | 新增 | 9 文件镜像 `packages/name-core/src` |
| `server/internal/core/PORTING.md` | 新增 | TS↔Go 映射 + 已知差异 |
| `server/internal/core/core_test.go` | 新增 | `//go:build integration`, 12 套 fixture |
| `server/testdata/fixtures/*.json` | 新增 | 12 套 fixtures |
| `scripts/generateFixtures.ts` | 新增 | 生成 fixtures |
| `server/internal/sampler/` | 新增 | uniform / weighted |
| `server/internal/api/` | 新增 | router / handlers / middleware / deps / respond |
| `server/internal/auth/` | 新增 | api_keys + 缓存 |
| `server/internal/ratelimit/` | 新增 | 自写 token bucket |
| `server/cmd/api/main.go` | 新增 | 启动入口 |
| `server/cmd/keymgmt/main.go` | 新增 | issue / list / revoke |
| `docs/prd|arch|api|db|features|deploy/random-name-*.md` | 新增/维护 | 文档体系 |

仅修改: 根 `.gitignore` (`.data/` 忽略 PG 数据卷).

## 验证方式

| 验证项 | 命令 | 通过标准 |
|--------|------|----------|
| Schema up/down | `podman compose up -d postgres` + ApplySchema | 6 表建/清 |
| JSON → PG 导入 | `go run ./cmd/import` | chars/sources/candidates 对照 source_index；surnames 有日志 |
| core 与 TS 对照 | `go test -tags=integration ./internal/core` | 12 fixture PASS |
| health / help / random / name | `curl localhost:8080/api/...` | 见接口文档 |
| 限流 | `go test -tags=integration ./internal/api` | RateLimit 相关 PASS |
| key 全链 | `keymgmt issue/list/revoke` + 带 key 请求 | `X-Authed-Authed: true` |
| 部署 | `podman compose up` | 双服务；容器内可 `/app/import` |

## 增量: /api/help + 错误引导 (2026-07-21)

| 路径 | 说明 |
|------|------|
| `handler_help.go` | `GET /api/help` |
| `respond.go` | 错误 message 末尾 `。详见 GET /api/help` |

## 增量: 缺省行为 + surnames (对齐当前代码)

| 行为 | 实现 |
|------|------|
| `strategy` 默认 `uniform` | `handler_random.go` `defaultStrategy` |
| `source` 缺省 = 五源合并去重 | `loadAllCandidates`；响应 `source.id="all"` |
| `surname` 缺省 = 百家姓随机 | PG `surnames` + `pickRandomSurname`；空表兜底「张」 |
| import 写 surnames | `baijiaxing.json` + char 过滤 + validateSurnames |
| Dockerfile | `/app/api` `/app/import` `/app/keymgmt` |

## 增量: 采样池未被截断 + 限流 IP 取 XFF 末段 (2026-09)

| 路径 | 改动 | 说明 |
|------|------|------|
| `internal/api/handler_random.go` | `query.Limit` 由常量 0 改为 `len(candidates)` | 0 会被 `NormalizeQueryConfig` 当"未设置"补成默认 30，导致采样池恒为 30 条：`n>30` 拿不到足量结果，`weighted` 在池内候选同分时退化为 `uniform`。显式上界与前端一致（前端随机排序时传 `candidateDb.length`）。真实库实测通过率约 40%-50%（全源 163089 → 去重 138856 → 通过 68210），池子远小于候选数，不放大后续排序/采样成本 |
| `internal/api/middleware.go` | `clientIP` 由 XFF 首段改为末段 + 空值回退 `RemoteAddr` | 首段由客户端控制，每请求换一个伪造值即可获得全新限流桶，per-IP 限流形同虚设。末段是最近一跳（可信反代）写入的值 |
| `internal/api/middleware_test.go` | 新增（无 build tag，随时可跑） | XFF 解析 7 例 + 无端口 `RemoteAddr` + 端到端守栏「伪造首段仍须触发 429」 |

验证结果：

- `go test ./internal/api` 全绿；对照实验中旧实现 8 次伪造请求被限流 **0 次**，新实现按 burst=3 正确触发
- `NormalizeQueryConfig` 语义确认：`Limit=0 → 30`、显式大值原样保留
- 修复后 `n` 上限恢复为文档承诺的 50；`weighted` 重新在完整通过集上采样

## 增量: 导入默认路径不依赖 cwd + 首次真实跑通集成测试 (2026-09)

| 路径 | 改动 | 说明 |
|------|------|------|
| `internal/config/config.go` | `CANDIDATE_DATA_DIR` 默认值改为 `../api/database/candidate` | 原默认值 `api/database/candidate` 只有从仓库根执行才成立；README 与 PRD 都要求在 `server/` 下执行，导致照文档操作必然失败 |
| `cmd/import/main.go` | 抽取 `resolveDataDir` / `resolveDataDirFrom` 纯函数 | 按起点向上最多 5 级定位数据目录；绝对路径原样返回，**回退路径的基准与查找基准统一为 `base`**（原回退用 `filepath.Abs`，以进程 cwd 为基准，会给出误导性路径） |
| `cmd/import/main_test.go` | 新增 | 多起点解析一致性 + 默认值语义守栏 + 绝对路径不改写 + 未命中回退，均不依赖进程 cwd（仓库根由源码位置推导） |

验证结果（PostgreSQL 16 容器 + 真实数据）：

- 导入：`chars=7474`、`sources=5`、`candidates=163089`、`name_source_names=223405`、`surnames=386`（全部可用，无需删除），行数对照通过
- **集成测试首次真实执行**（此前因无 PG 全部静默 Skip）：`go test -tags=integration ./...` 全绿
  - `TestQueryNamesFixtureParity` 12 套 fixtures 逐源对照通过（2.2s），验证 Go 与 TS 内核在真实数据上严格一致
  - 限流 / 认证 / health 用例全部实际运行并通过
- 端到端：`n=50` 真实返回 50 条（修复前恒为 30）；`n=100` 被 `maxNum` 正确截到 50

## 增量: 性能优化 —— 全源 586ms → 260ms (2026-09)

| 路径 | 改动 | 说明 |
|------|------|------|
| `internal/core/query.go` | 结果集改 `lightResult` 轻量中间体 | `ScoredCandidate` 实测 704 B / 11 指针字段，直接累积十万级结果导致扩容反复复制重结构体（单次调用分配 900 MB，GC 扫描占 CPU 40%）。改为累积 88 B 轻量体，排序截断后仅为入选的 limit 条构造完整对象 |
| `internal/core/query.go` | 排序键预计算 | 原排序每次比较都 `SplitChars` + 两次 map 查找；改为填充时预计算逐字「拼音+调号+原字符」键，与 `compareZhName` 语义等价 |
| `internal/core/score.go` | 新增 `scoreTotal` + `ScoreCandidateInput.TotalScore` | 一次求和供排序与最终构造复用，避免对同一候选二次累计 |
| `internal/api/handler_random.go`、`deps.go` | 全源合并结果缓存 | 合并 13.8 万条去重实测分配约 32 MB；候选池进程内静态，改构造一次复用（双检锁）。首访 555 ms（含合并），热路径 247-273 ms |

验证（PG 容器 + 真实库）：

- `TestQueryNamesFixtureParity` **12 套 / 2091 条结果逐字段一致** —— 证明排序与截断语义未被改动
- 全量 `-tags=integration ./...` 通过
- 端到端：全源 586→247-273 ms、他山石 234→114 ms、学术 21→16 ms；采集分配 1361 MB→712 MB
- `go vet` 通过；`sortResults` 保留供既有调用方使用

实测推翻的两个假设（记录备查）：

1. 加权采样不是瓶颈（uniform 246 ms vs weighted 238 ms，噪声级差异）
2. 切片扩容不是主因（单做完整预分配仅改善约 9%）；主因是重结构体被反复复制

## 增量: 部署加固 —— CORS / 探活分层 / 访问日志 / 并发上限 (2026-09)

上线前缺口清单的落地。原状态下服务能跑但不可直接对外: 浏览器跨域调用必失败, 探针与业务共用限流桶且不查依赖, 无访问日志, 热查询无并发保护。

| 路径 | 改动 | 说明 |
|------|------|------|
| `internal/api/middleware_cors.go` | 新增 | 预检短路(204, 不进认证/限流) + 简单请求回 `Allow-Origin` 与 `Access-Control-Expose-Headers`(暴露限流头, 否则浏览器读不到); 白名单模式回显 Origin 并声明 `Vary: Origin` |
| `internal/api/middleware_ops.go` | 新增 | 访问日志(状态/耗时/IP/authed/字节, 级别按状态码分层) + `RecoverMiddleware`(panic→500 JSON) + `InflightMiddleware`(在途上限, 满额立即 503 不排队) |
| `internal/api/handler_health.go` | 改 | `HealthHandler` 保持 200 不查依赖(liveness); 新增 `ReadyHandler` 真实 `Ping` PG, 失败 503(readiness, 2s 超时) |
| `internal/api/router.go` | 改 | 链改为 `CORS → RequestLog → Recover → 路由`; 探针只走认证(不限流/不占在途名额), 业务走 `Inflight → Auth → RateLimit`; `BuildMux` 增 `Options` 并返回 `http.Handler` |
| `internal/api/middleware.go` | 改 | `AuthMiddleware` 把认证结果回写到请求级 `reqInfo` 指针, 供外层访问日志读取 |
| `internal/config/config.go` | 改 | 新增 `CORS_ALLOWED_ORIGINS`(默认 `*`) 与 `MAX_INFLIGHT`(默认 16) |
| `docker-compose.yml` | 改 | api 服务加健康检查(探 `/api/ready`)与两个新 env; 新增 `importer` 一次性服务(`--profile import`), 首次导入不再需要手工 `podman run` |
| `.github/workflows/go.yml` | 改 | 新增「集成测试编译检查」(`go vet -tags=integration ./...`, 只编译不运行): 集成测试带 build tag, 默认不参与编译, 改了公开签名却漏改集成测试时 CI 会静默放过 |

验证结果（本机，无 PG）:

- 新增白盒测试: CORS 预检短路/暴露头/白名单与无 Origin 分支/白名单预检、在途上限拒绝与恢复、状态码捕获、日志记录 authed、panic→500
- 新增装配级测试 `router_test.go`: 用真实 `BuildMux` 链路验证「预检在链最外层且不扣桶」「health 连发不限额、help 照常 429」「404 JSON」「匿名 X-Authed-Authed=false」
- `gofmt -l` 无输出; `go vet ./...` 与 `go vet -tags=integration ./...` 通过; `go test -count=1 ./...` 全绿
- 集成测试同步: 429 用例改用 `/api/help`(health 已移出限流链), 新增探针不限额与 `/api/ready` 200 用例

容器环境实测（2026-09-18，宿主 `podman compose`，此前本会话沙箱因只读挂载无法跑容器）：

- `podman compose up -d postgres` → PG 容器 Healthy
- `podman compose --profile import run --rm importer` → 新增的一次性 importer 服务按预期工作：`chars=7474`、`sources=5`、`candidates=163089`、`name_source_names=223405`、`surnames=386`（386/386 全部可用），行数对照通过
- `podman compose up -d --build api` → 镜像构建成功（Dockerfile 复用既有多阶段流程），api 容器启动
- `curl /api/ready` → `{"ok":true}`，readiness 探针在真实 PG 上返回 200
- `go test -tags=integration ./...` **真实执行**（非 Skip）：`internal/api 0.183s`（认证/限流/探针用例）、`internal/core 1.210s`（12 套 fixture 与 TS name-core 逐字段对照）

> NOTE `internal/auth (cached)` 是复用了此前无 PG 时的 Skip 结果；要强制该包真跑加 `-count=1`。

## 已知限制

1. **大源 / 全源 p95 不达标** — 单源 wealth/modern ~270–310ms；全源更重。见 arch §性能.
2. **`localeCompare("zh-Hans-CN")` 不可复刻** — fixture 按同分数组集合比对，不验组内序.
3. **`X-Forwarded-For` 末段必须是可信反代写入的** — 实现只取末段（取首段可被客户端伪造绕过限流）。反代必须覆盖写入该头，不得用追加模式；见 deploy 文档上线检查清单。
4. **compose 不自动 import** — 起 PG 后需跑 `/app/import` 或本机 `go run ./cmd/import`.
5. **单进程限流** — 多副本不共享桶.
6. **成功响应无 RateLimit 头** — 仅 429 带 `X-RateLimit-*` / `Retry-After`.
7. **`/api/help` 计入匿名限流**（`/api/health`、`/api/ready` 已移出限流链，探活不再消耗额度）.
8. **`MAX_INFLIGHT` 满额即 503** — 有意的快速失败，不做排队；上限需按核数压测调整.
9. **`/api/ready` 不限流** — 需在反代层限制其来源网段，否则可被当作廉价的 PG 连通性探测入口.
