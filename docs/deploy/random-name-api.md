# 随机名字 API — 部署方案

## 环境要求

- 容器运行时: Podman 5+ (或 Docker 24+)
- Go 1.26+ (本地构建/开发)
- 磁盘: ≥ 500 MB (PG data + 候选 JSON 数据)
- 端口: 5433 (PG, 仅本地); 8080 (api)

## 首次部署 (本地开发)

### 1. 起 PG + 导入数据

```bash
# 起 PG 容器 (5433 端口)
podman compose up -d postgres

# 等 healthy
for i in $(seq 1 15); do
  s=$(podman inspect -f '{{.State.Health.Status}}' namegen-pg 2>/dev/null)
  [ "$s" = "healthy" ] && break
  sleep 1
done

# 一次性导入 JSON → PG
POSTGRES_DSN="postgres://namegen:namegen@localhost:5433/namegen?sslmode=disable" \
  CANDIDATE_DATA_DIR="$PWD/api/database/candidate" \
  go run ./cmd/import
```

预期输出:

```
INFO 开始导入 dataDir=api/database/candidate
INFO chars 导入完成 count=7474
INFO sources 导入完成 count=5
INFO candidates 导入完成 source=wealth count=57327
INFO name_source_names 导入完成 source=wealth count=159345
... (5 个来源逐个完成)
INFO 导入完成 chars=7474 sources=5 candidates=163089 name_source_names=223405
INFO 行数对照通过 candidates=163089 expected=163089
```

### 2. 启动 API

```bash
POSTGRES_DSN="postgres://namegen:namegen@localhost:5433/namegen?sslmode=disable" \
  API_LISTEN_ADDR=":8080" \
  RATE_LIMIT_RPM=30 \
  RATE_LIMIT_BURST=30 \
  go run ./cmd/api
```

或编译为二进制:

```bash
go build -o ./bin/api ./cmd/api
./bin/api
```

### 3. 冒烟验证

```bash
curl http://localhost:8080/api/health
curl "http://localhost:8080/api/random?surname=姚&n=3&source=academic&strategy=weighted"
curl "http://localhost:8080/api/name/姚悟移?source=academic"
```

### 4. 发放 API key

```bash
POSTGRES_DSN="..." go run ./cmd/keymgmt issue --label "租户A"
# 输出: issued key=k_abcdef...  label=租户A

POSTGRES_DSN="..." go run ./cmd/keymgmt list
POSTGRES_DSN="..." go run ./cmd/keymgmt revoke --label "租户A"
```

## 容器化部署 (生产)

### Dockerfile 多阶段构建

`server/Dockerfile` 把 api 编译为静态二进制, 在 alpine 上运行:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache tini ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/api /app/api
EXPOSE 8080
ENTRYPOINT ["/sbin/tini","--","/app/api"]
```

### 配置 env

| env                  | 必填 | 默认 | 说明 |
|----------------------|------|------|------|
| `POSTGRES_DSN`         | 是  | —   | apgxxxxx dsn, 如 `postgres://u:p@host:5432/db?sslmode=disable` |
| `API_LISTEN_ADDR`       | 否  | `:8080` | 监听地址 |
| `CANDIDATE_DATA_DIR`     | 否  | `api/database/candidate` | 仅 import 脚本用 |
| `RATE_LIMIT_RPM`         | 否  | `30`     | 匿名每分钟请求数 |
| `RATE_LIMIT_BURST`       | 否  | `=RPM`   | 令牌桶容量 |

### 完整栈起

```bash
podman compose up -d
# 期望: postgres + api 两容器, api 容器需先有数据 → 进 api 容器跑一次 import
podman exec namegen-api /app/api-import # (需把 import 也打入镜像或单独一InitContainer)
```

当前 Dockerfile 仅编译 api. 想做 init import, 建议在 compose 加一个 `importer` one-shot 服务:

```yaml
importer:
  build: ./server
  command: ["sh","-c","/out/import"]
  depends_on:
    postgres: { condition: service_healthy }
  environment:
    POSTGRES_DSN: "..."
    CANDIDATE_DATA_DIR: "/data/candidate"
  volumes:
    - ./api/database/candidate:/data/candidate:Z
```

(本仓库 Dockerfile 暂未走多 binary 模式, 待生产 SOP 落地时补.)

## 回滚

### 仅代码回滚 (DB schema 不变)

```bash
git revert <commit>
podman compose restart api
```

### DB 回滚 (危险, 会清数据)

```bash
psql -f server/internal/db/schema.down.sql  # 清五表 + api_keys
psql -f server/internal/db/schema.sql        # 重建空表
go run ./cmd/import                          # 重新导入
```

### PG 卷回滚

由于 PG 数据放在 `./.data/pg` 卷, 可备份该目录:

```bash
tar czf pg-backup.tgz ./.data/pg
# 回滚:
podman compose down
rm -rf ./.data/pg
tar xzf pg-backup.tgz
podman compose up -d
```

## 监控 / 健康检查

- `GET /api/health` 探活
- `/api/random` 的 p95 见 `docs/arch/random-name-api.md` §性能现状

## 生产上线检查清单

- [ ] `POSTGRES_DSN` SSL mode 改 `require` 或 `verify-full`, sslmode= 协议层加密
- [ ] API 容器端口暴露在 nginx 后, nginx 设定 `X-Forwarded-For` 并丢弃不可信头
- [ ] `RATE_LIMIT_RPM / BURST` 按预期流量调
- [ ] 发放至少一个有效 api_keys 行, 验证带 key 时 `X-Authed-Authed: true`
- [ ] `podman volume` 持久化路径正确, 重启不丢数据
- [ ] 日志 (`slog` text to stderr) 接到集中日志方案