// Package main: cmd/api - HTTP 服务入口.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/namegen/server/internal/api"
	"github.com/namegen/server/internal/auth"
	"github.com/namegen/server/internal/config"
	"github.com/namegen/server/internal/db"
	"github.com/namegen/server/internal/ratelimit"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("config invalid", "err", err)
		os.Exit(2)
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	pool, err := db.NewPool(rootCtx, cfg.PostgresDSN, 8)
	if err != nil {
		slog.Error("DB 初始化失败", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.ApplySchema(rootCtx, pool); err != nil {
		slog.Error("Schema 应用失败", "err", err)
		os.Exit(1)
	}

	deps := api.NewDeps(pool)
	authn := auth.New(pool)
	limiter := ratelimit.New(cfg.RateLimitRPM, cfg.RateLimitBurst)
	defer limiter.Close()

	handler := api.BuildMux(deps, authn, limiter, api.Options{
		AllowedOrigins: cfg.CORSAllowedOrigins,
		MaxInflight:    cfg.MaxInflight,
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("服务监听",
			"addr", cfg.ListenAddr,
			"rate_limit_rpm", cfg.RateLimitRPM,
			"cors_origins", cfg.CORSAllowedOrigins,
			"max_inflight", cfg.MaxInflight)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("ListenAndServe 失败", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("接收到停止信号, 优雅退出中...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("优雅停机失败", "err", err)
	}
	slog.Info("已退出")
}
