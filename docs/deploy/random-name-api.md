# 随机名字 API — 部署方案

## 环境要求

- 容器运行时: Podman 5+ (或 Docker 24+)
- Go 1.26+ (本地构建/开发)
- 磁盘: ≥ 500 MB (PG data + 候选 JSON 数据)
- 端口: 5433 (PG, 仅本地); 8080 (api)

## 首次部署 (本地开发)

### 1. 起 PG + 导入数据

```bash
podman compose up -d postgres

# 等 healthy
for i in $(seq 1 15); do
  s=$(podman inspect -f '{{.State.Health.Status}}' namegen-pg 2>/dev/null)
  [ "$s" = "healthy" ] && break
  sleep 1
done

# 一次性导入 JSON → PG（含 surnames）
POSTGRES_DSN="postgres://namegen:namegen@localhost:5433/namegen?sslmode=disable" \
  CANDIDATE_DATA_DIR="$PWD/api/database/candidate" \
  go run ./cmd/import
```

在 `server/` 目录执行时把 `CANDIDATE_DATA_DIR` 指到仓库根下的 candidate 目录。

预期日志含：`chars` / `sources` / 各源 `candidates` / `name_source_names` / `surnames` / `行数对照通过`。

### 2. 启动 API

```bash
# 在 server/ 下
POSTGRES_DSN="postgres://namegen:namegen@localhost:5433/namegen?sslmode=disable" \
  API_LISTEN_ADDR=":8080" \
  RATE_LIMIT_RPM=30 \
  RATE_LIMIT_BURST=30 \
  go run ./cmd/api
```

或：

```bash
go build -o ./bin/api ./cmd/api
./bin/api
```

### 3. 冒烟验证

```bash
curl http://localhost:8080/api/health
curl http://localhost:8080/api/help
curl "http://localhost:8080/api/random?n=3"                    # 随机姓 + 全源 + uniform
curl "http://localhost:8080/api/random?surname=姚&n=3&source=academic&strategy=weighted"
curl "http://localhost:8080/api/name/姚悟移?source=academic"
```

### 4. 发放 API key

```bash
POSTGRES_DSN="..." go run ./cmd/keymgmt issue --label "租户A"
POSTGRES_DSN="..." go run ./cmd/keymgmt list
POSTGRES_DSN="..." go run ./cmd/keymgmt revoke --label "租户A"
# 或 revoke --key k_...
```

## 容器化部署

### 镜像产物

`server/Dockerfile` 多阶段构建，最终镜像含：

| 路径 | 用途 |
|------|------|
| `/app/api` | HTTP 服务（默认 ENTRYPOINT） |
| `/app/import` | 一次性数据导入 |
| `/app/keymgmt` | key 运维 CLI |

### 配置 env

| env | 必填 | 默认 | 说明 |
|-----|------|------|------|
| `POSTGRES_DSN` | 是 | — | 如 `postgres://u:p@host:5432/db?sslmode=disable` |
| `API_LISTEN_ADDR` | 否 | `:8080` | 监听地址 |
| `CANDIDATE_DATA_DIR` | 否 | `api/database/candidate` | **仅 import** |
| `RATE_LIMIT_RPM` | 否 | `30` | 匿名每分钟请求数 |
| `RATE_LIMIT_BURST` | 否 | `=RPM` | 令牌桶容量 |

### 完整栈

```bash
podman compose up -d --build

# 挂载候选 JSON 后一次性导入（compose 默认 api 服务未挂数据卷，需按需加 volume 或本机 import）
podman run --rm --network container:namegen-pg \
  -e POSTGRES_DSN="postgres://namegen:namegen@127.0.0.1:5432/namegen?sslmode=disable" \
  -e CANDIDATE_DATA_DIR=/data/candidate \
  -v "$PWD/api/database/candidate:/data/candidate:Z" \
  $(podman build -q ./server) \
  /app/import

# key
podman exec namegen-api /app/keymgmt issue --label "prod"
```

可选：在 `docker-compose.yml` 增加 one-shot `importer` 服务，`command: ["/app/import"]`，挂载 `./api/database/candidate`，`depends_on: postgres healthy`。

## 回滚

### 仅代码回滚 (DB schema 不变)

```bash
git revert <commit>
podman compose up -d --build api
```

### DB 回滚 (危险, 清数据)

```bash
psql "$POSTGRES_DSN" -f server/internal/db/schema.down.sql   # 6 表全删
# 再起 api 或 import 会 ApplySchema 建空表
go run ./cmd/import   # 重灌
```

### PG 卷回滚

数据在 `./.data/pg`：

```bash
tar czf pg-backup.tgz ./.data/pg
# 回滚:
podman compose down
rm -rf ./.data/pg
tar xzf pg-backup.tgz
podman compose up -d
```

## 监控 / 健康检查

- `GET /api/health` 探活（**计入匿名限流**；高频探活请带 key 或调高 RPM）
- 性能见 `docs/arch/random-name-api.md` §性能现状

## 生产上线检查清单

- [ ] `POSTGRES_DSN` SSL 按环境收紧
- [ ] API 在反代后；反代**覆盖写入** `X-Forwarded-For`（nginx: `proxy_set_header X-Forwarded-For $remote_addr;`），**不要**用 `$proxy_add_x_forwarded_for` 追加
  - 限流按 XFF 末段判客户端 IP（首段客户端可伪造）；追加模式下末段仍是客户端可控值，匿名限流会被绕过
  - 验证方式：分别发 `X-Forwarded-For: 1.1.1.1` 与 `X-Forwarded-For: 2.2.2.2` 的请求，应共用同一个限流桶（超过 rpm 后两者都被 429）
- [ ] `RATE_LIMIT_RPM / BURST` 按流量调
- [ ] 已 import（含 surnames）
- [ ] 至少一个有效 api_keys；验证 `X-Authed-Authed: true`
- [ ] `.data/pg` 或外部卷持久化
- [ ] slog stderr 接入日志收集
