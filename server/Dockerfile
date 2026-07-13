# 随机名字 API 服务. 多阶段构建, 编出 api 二进制.
# build context: ./server (见根 docker-compose.yml)
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/import ./cmd/import
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/keymgmt ./cmd/keymgmt

FROM alpine:3.20
RUN apk add --no-cache tini ca-certificates tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=build /out/api      /app/api
COPY --from=build /out/import   /app/import
COPY --from=build /out/keymgmt   /app/keymgmt
EXPOSE 8080
ENTRYPOINT ["/sbin/tini","--","/app/api"]