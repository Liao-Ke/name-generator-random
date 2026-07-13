// Package db 封装 PostgreSQL 连接与 schema 管理. 仅依赖 pgx.
package db

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var SchemaSQL string

//go:embed schema.down.sql
var SchemaDownSQL string

// Pool 是全服务共享的 pgx 连接池. 调用方用 (*Pool).Acquire(ctx) 取连接.
type Pool = pgxpool.Pool

// NewPool 按给定 DSN 创建连接池, 最多 connMax 并发连接.
func NewPool(ctx context.Context, dsn string, connMax int32) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 DSN 失败: %w", err)
	}
	cfg.MaxConns = connMax
	cfg.HealthCheckPeriod = time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接 PG 失败: %w", err)
	}
	return pool, nil
}

// ApplySchema 执行 schema.sql 全部语句, 幂等.
func ApplySchema(ctx context.Context, pool *Pool) error {
	_, err := pool.Exec(ctx, SchemaSQL)
	return err
}

// DropSchema 执行 schema.down.sql, 用于回滚验证.
func DropSchema(ctx context.Context, pool *Pool) error {
	_, err := pool.Exec(ctx, SchemaDownSQL)
	return err
}