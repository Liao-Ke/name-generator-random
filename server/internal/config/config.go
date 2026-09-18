// Package config 读取环境变量配置. 零依赖, 仅 stdlib.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config 服务运行所需配置. import 与 api 共用同一结构, 未设置字段为零值.
type Config struct {
	// PostgresDSN 形如 "postgres://user:pass@host:5432/db?sslmode=disable"
	PostgresDSN string
	// ListenAddr HTTP 监听地址, 如 ":8080"
	ListenAddr string
	// CandidateDataDir 候选数据目录. 默认相对 server 模块根 (即仓库根下的 api/database/candidate),
	// 仅 cmd/import 使用; cmd/import 会按 cwd 向上查找定位, 不要求特定执行目录.
	CandidateDataDir string
	// RateLimitRPM 匿名限流: 每分钟请求数, 默认 30
	RateLimitRPM int
	// RateLimitBurst 令牌桶容量, 默认等于 RPM
	RateLimitBurst int
	// CORSAllowedOrigins 浏览器跨域允许来源白名单, 逗号分隔; 默认 ["*"]. 见 api.CORS.
	CORSAllowedOrigins []string
	// MaxInflight 业务端点同时执行的请求数上限, 默认 16; <=0 = 不限.
	// 依据: /api/random 全源路径单请求约 260ms 纯 CPU, 上限过高等于没有保护, 过低会
	// 在正常高峰误伤 —— 按实际核数与压测结果调整.
	MaxInflight int
}

// FromEnv 按优先级读取环境变量, 必填项缺失时返回错误.
func FromEnv() (Config, error) {
	cfg := Config{
		PostgresDSN:        os.Getenv("POSTGRES_DSN"),
		ListenAddr:         getEnvOrDefault("API_LISTEN_ADDR", ":8080"),
		CandidateDataDir:   getEnvOrDefault("CANDIDATE_DATA_DIR", "../api/database/candidate"),
		RateLimitRPM:       getEnvIntOrDefault("RATE_LIMIT_RPM", 30),
		CORSAllowedOrigins: splitCS(getEnvOrDefault("CORS_ALLOWED_ORIGINS", "*")),
		MaxInflight:        getEnvIntOrDefault("MAX_INFLIGHT", 16),
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

// splitCS 按逗号切分并去掉空白段; 空串返回 nil (调用方按语义决定空值含义).
func splitCS(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
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
