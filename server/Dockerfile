# 随机名字 API 服务. 阶段 1: 仅运行 schema, 不打代码
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 当前阶段尚未有 main; 仅作占位. cmd/*/main.go 落地后改为 go build -o /out/api ./cmd/api
RUN test -f cmd/api/main.go || echo "stage1 placeholder: cmd/api/main.go 尚未落地"

FROM alpine:3.20
# tini 用于 1 号进程, 处理信号
RUN apk add --no-cache tini ca-certificates tzdata
WORKDIR /app
# 占位: 后续阶段拷贝编译产物
# COPY --from=build /out/api /app/api
# CMD ["/sbin/tini","--","/app/api"]
CMD ["echo","server 骨架: 等待 cmd/api/main.go 落地"]