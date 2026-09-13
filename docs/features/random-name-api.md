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
| `internal/api/handler_random.go` | `query.Limit` 由常量 0 改为 `len(candidates)` | 0 会被 `NormalizeQueryConfig` 当"未设置"补成默认 30，导致采样池恒为 30 条：`n>30` 拿不到足量结果，`weighted` 在池内候选同分时退化为 `uniform`。显式上界与前端一致（前端随机排序时传 `candidateDb.length`）。通过率约 0.1%-0.3%，实际池子远小于候选数，不放大后续排序/采样成本 |
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

## 已知限制

1. **大源 / 全源 p95 不达标** — 单源 wealth/modern ~270–310ms；全源更重。见 arch §性能.
2. **`localeCompare("zh-Hans-CN")` 不可复刻** — fixture 按同分数组集合比对，不验组内序.
3. **`X-Forwarded-For` 末段必须是可信反代写入的** — 实现只取末段（取首段可被客户端伪造绕过限流）。反代必须覆盖写入该头，不得用追加模式；见 deploy 文档上线检查清单。
4. **compose 不自动 import** — 起 PG 后需跑 `/app/import` 或本机 `go run ./cmd/import`.
5. **单进程限流** — 多副本不共享桶.
6. **成功响应无 RateLimit 头** — 仅 429 带 `X-RateLimit-*` / `Retry-After`.
7. **help/health 计入匿名限流** — 与业务接口共用桶，探活频繁可能触发 429.
