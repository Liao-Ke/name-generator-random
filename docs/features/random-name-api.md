# 随机名字 API — 功能记录

## 实施范围

把 `packages/name-core` (TS) 的中文评分能力移植到 Go, 在 PG 中存储候选名 + 字库,
构建 HTTP API 提供随机起名 + 单名详情. 与原 Taro 前端共存, 前端改动零.

### 改动文件 / 新增目录

| 路径 | 类型 | 说明 |
|------|------|------|
| `docker-compose.yml`                                | 新增 | Postgres 16 + api 两服务 |
| `server/go.mod` `server/go.sum`                      | 新增 | module `github.com/namegen/server` |
| `server/Dockerfile`                                  | 新增 | 多阶段构建占位, 等 cmd 产物落地 |
| `server/internal/db/`                                | 新增 | `pg.go` (连接池 + schema up/down) / `schema.sql` / `schema.down.sql` |
| `server/internal/config/config.go`                  | 新增 | env 读取 |
| `server/cmd/import/`                                 | 新增 | JSON → PG 一次性导入 (char_db / source_index / candidates / name_source_names) |
| `server/internal/core/`                              | 新增 | 9 文件镜像 `packages/name-core/src` |
| `server/internal/core/PORTING.md`                    | 新增 | TS↔Go 文件映射 + 已知差异 |
| `server/internal/core/core_test.go`                  | 新增 | 集成 (`+build integration`), 12 套 fixture 对照 |
| `server/testdata/fixtures/*.json`                    | 新增 | 12 套 fixtures (limit=200) |
| `scripts/generateFixtures.ts`                        | 新增 | 一次性 tsx 脚本生成 fixtures |
| `server/internal/sampler/sampler.go` + `_test.go`   | 新增 | uniform / weighted 采样 |
| `server/internal/api/`                               | 新增 | router / handlers / middleware / deps / respond |
| `server/internal/auth/`                              | 新增 | api_keys PG 校验 + 30s 缓存 + ConstantTimeCompare |
| `server/internal/ratelimit/`                          | 新增 | 自写 token bucket, 不引 x/time/rate |
| `server/cmd/api/main.go`                             | 新增 | 启动入口, 优雅停机 |
| `server/cmd/keymgmt/main.go`                          | 新增 | key 运维 CLI: issue / list / revoke |
| `docs/prd/random-name-api.md`                        | 新增 | PRD |
| `docs/arch/random-name-api.md`                       | 新增 | 架构描述 |
| `docs/api/random-name.md`                            | 新增 | 接口文档 |
| `docs/features/random-name-api.md`                  | 本文件 | 功能记录 |

仅修改: 根 `.gitignore` (新增 `.data/` 忽略 PG 数据卷).

## 验证方式

| 验证项 | 命令 | 通过标准 |
|--------|------|----------|
| Schema up/down 幂等 | `podman compose up -d postgres`; `psql < schema.sql && psql < schema.down.sql` | 各 5 表建/清   |
| JSON → PG 导入        | `go run ./cmd/import`                                          | chars=7474 / sources=5 / candidates=163089 / name_source_names=223405 |
| core 与 TS 对照       | `go test -tags=integration ./internal/core`                    | 12 fixture / 30 score group PASS (评分字段严格集对齐) |
| health / random / name | `curl localhost:18080/api/...`                                | 见接口文档示例 |
| 限流匿名 429 突刺     | `go test -tags=integration ./internal/api`                    | +`TestRateLimit_*` PASS |
| key 全链             | `keymgmt issue/list/revoke`; 用 key 调 `health` 见 `X-Authed-Authed: true` | 通过 |
| 部署                  | `podman compose up`                                            | 起双服务, `curl /api/health` 200 |

## 已知限制

1. **大源 p95 不达标** — wealth 57k 起 modern 60k 起, QueryNames 缓存命中后仍 ~270-310 ms,
   高于 PRD 100ms 目标. 缓解策略见 `docs/arch/random-name-api.md` §性能现状. 本期接受.
2. **`localeCompare("zh-Hans-CN")` 同音符 Go 端不可复刻** — fixture 测试按同分数组集合严格比对
   (单字段全等), 不验证同分内部排序次序. random/handler 语义不依赖该排序.
3. **`X-Forwarded-For` 默认信任** — 部署在 nginx 之后时, 必须由前置 nginx 设定 trusted proxies 并清
   掉不可信 XFF, 否则匿名限流可被伪造头绕过.
4. **PG 容器初始化未自动跑 schema** — 当前 compose 不挂 schema.sql 到 docker-entrypoint-initdb.d.
   部署文档要求: 起容器后, 运行 `cmd/import` (会 ApplySchema + 灌数据) 或
   `cmd/api` (会 ApplySchema 后空表启动). 由生产 SOP 落地.
5. **单进程限流** — 不支持多副本共享限流; 横向扩展时需升 Redis.