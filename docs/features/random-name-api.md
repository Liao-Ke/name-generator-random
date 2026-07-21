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

## 已知限制

1. **大源 / 全源 p95 不达标** — 单源 wealth/modern ~270–310ms；全源更重。见 arch §性能.
2. **`localeCompare("zh-Hans-CN")` 不可复刻** — fixture 按同分数组集合比对，不验组内序.
3. **`X-Forwarded-For` 默认信任** — 前置代理须清洗不可信 XFF.
4. **compose 不自动 import** — 起 PG 后需跑 `/app/import` 或本机 `go run ./cmd/import`.
5. **单进程限流** — 多副本不共享桶.
6. **成功响应无 RateLimit 头** — 仅 429 带 `X-RateLimit-*` / `Retry-After`.
7. **help/health 计入匿名限流** — 与业务接口共用桶，探活频繁可能触发 429.
