// Package config 读取环境变量配置. 零依赖, 仅 stdlib.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config 服务运行所需配置. import 与 api 共用同一结构, 未设置字段为零值.
type Config struct {
	// PostgresDSN 形如 "postgres://user:pass@host:5432/db?sslmode=disable"
	PostgresDSN string
	// ListenAddr HTTP 监听地址, 如 ":8080"
	ListenAddr string
	// CandidateDataDir 指向仓库根的 api/database/candidate 目录
	CandidateDataDir string
	// RateLimitRPM 匿名限流: 每分钟请求数, 默认 30
	RateLimitRPM int
	// RateLimitBurst 令牌桶容量, 默认等于 RPM
	RateLimitBurst int
}

// FromEnv 按优先级读取环境变量, 必填项缺失时返回错误.
func FromEnv() (Config, error) {
	cfg := Config{
		PostgresDSN:      os.Getenv("POSTGRES_DSN"),
		ListenAddr:       getEnvOrDefault("API_LISTEN_ADDR", ":8080"),
		CandidateDataDir: getEnvOrDefault("CANDIDATE_DATA_DIR", "api/database/candidate"),
		RateLimitRPM:     getEnvIntOrDefault("RATE_LIMIT_RPM", 30),
	}
	cfg.RateLimitBurst = getEnvIntOrDefault("RATE_LIMIT_BURST", cfg.RateLimitRPM)

	if cfg.PostgresDSN == "" {
		return Config{}, fmt.Errorf("POSTGRES_DSN 未设置")
	}
	return cfg, nil
}

func getEnvOrDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvIntOrDefault(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}