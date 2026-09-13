// Package main: cmd/keymgmt — api_keys 发放 / 吊销 / 列示 CLI.
// 不暴露 HTTP 端点 (YAGNI), 仅运维通过 SSH/容器内执行.
//
// 用法:
//
//	go run ./cmd/keymgmt issue  --label "租户 X"     # 发放新 key
//	go run ./cmd/keymgmt list                     # 列所有有效 key
//	go run ./cmd/keymgmt revoke --key KEY         # 吊销某 key
//	go run ./cmd/keymgmt revoke --label "租户 X"  # 按 label 吊销
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/namegen/server/internal/config"
	"github.com/namegen/server/internal/db"
)

const keyBytes = 24 // 24 字节 = 48 位 hex. 适合 API key 长度.

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	label := fs.String("label", "", "调用方标签 (issue / revoke 按此匹配)")
	key := fs.String("key", "", "目标 key (revoke 用)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置: %v\n", err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.PostgresDSN, 2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DB: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := db.ApplySchema(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "schema: %v\n", err)
		os.Exit(1)
	}

	switch cmd {
	case "issue":
		if *label == "" {
			fmt.Fprintln(os.Stderr, "--label 必填")
			os.Exit(2)
		}
		k := newKey()
		if _, err := pool.Exec(ctx,
			`INSERT INTO api_keys (key, label) VALUES ($1, $2)`, k, *label); err != nil {
			fmt.Fprintf(os.Stderr, "insert: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("issued key=%s  label=%s\n", k, *label)
	case "revoke":
		if *key == "" && *label == "" {
			fmt.Fprintln(os.Stderr, "需 --key 或 --label 之一")
			os.Exit(2)
		}
		switch {
		case *key != "":
			tag, err := pool.Exec(ctx,
				`UPDATE api_keys SET revoked_at = now() WHERE key = $1 AND revoked_at IS NULL`, *key)
			if err != nil {
				fmt.Fprintf(os.Stderr, "revoke: %v\n", err)
				os.Exit(1)
			}
			if tag.RowsAffected() == 0 {
				fmt.Println("no rows affected (key 不存在或已吊销)")
				os.Exit(1)
			}
			fmt.Printf("revoked key=%s\n", *key)
		default:
			tag, err := pool.Exec(ctx,
				`UPDATE api_keys SET revoked_at = now() WHERE label = $1 AND revoked_at IS NULL`, *label)
			if err != nil {
				fmt.Fprintf(os.Stderr, "revoke: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("revoked by label=%s rows=%d\n", *label, tag.RowsAffected())
		}
	case "list":
		rows, err := pool.Query(ctx,
			`SELECT key, label, created_at, revoked_at FROM api_keys ORDER BY created_at DESC`)
		if err != nil {
			fmt.Fprintf(os.Stderr, "list: %v\n", err)
			os.Exit(1)
		}
		defer rows.Close()
		fmt.Printf("%-48s  %-20s  %-22s  %s\n", "key", "label", "created_at", "revoked_at")
		fmt.Println(strings.Repeat("-", 100))
		for rows.Next() {
			var k, lbl string
			var created time.Time
			var revoked *time.Time
			if err := rows.Scan(&k, &lbl, &created, &revoked); err != nil {
				fmt.Fprintf(os.Stderr, "scan: %v\n", err)
				os.Exit(1)
			}
			r := ""
			if revoked != nil {
				r = revoked.Local().Format(time.RFC3339)
			}
			fmt.Printf("%-48s  %-20s  %-22s  %s\n", k, lbl, created.Local().Format(time.RFC3339), r)
		}
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	_ = slog.Default()
}

func usage() {
	fmt.Fprint(os.Stderr, `keymgmt — 随机名字 API 的 key 运维 CLI

用法:
  keymgmt issue  --label "<说明>"
  keymgmt revoke --key <KEY>
  keymgmt revoke --label "<说明>"
  keymgmt list

不暴露 HTTP 端点, 仅运维通过容器内或 SSH 执行.
`)
}

// newKey 生成 24 字节随机 hex 编码 (48 字符).
func newKey() string {
	b := make([]byte, keyBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "k_" + hex.EncodeToString(b)
}
