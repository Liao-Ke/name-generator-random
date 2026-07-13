// Package main: cmd/import — 把 api/database/candidate 下的 JSON 导入 PostgreSQL.
// 用法: go run ./cmd/import
// 环境变量: POSTGRES_DSN, CANDIDATE_DATA_DIR (默认 api/database/candidate)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/namegen/server/internal/config"
	"github.com/namegen/server/internal/db"
)

// JSON 中间结构: 与源数据字段一一对应, 键名保持与文件一致.

type charInfoJSON struct {
	Char           string `json:"char"`
	Pinyin         string `json:"pinyin"`
	Tone           int    `json:"tone"`
	PinyinNoTone   string `json:"pinyinWithoutTone"`
	Initial        string `json:"initial"`
	InitialMethod  string `json:"initialMethod"`
	InitialPlace   string `json:"initialPlace"`
	Vowel          string `json:"vowel"`
	VowelType      string `json:"vowelType"`
	Count          int    `json:"count"`
	IsPolyphone    bool   `json:"isPolyphone"`
}

type sourceEntryJSON struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Priority    int    `json:"priority"`
	Weight      int    `json:"weight"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

type sourceIndexJSON struct {
	DefaultSourceID string                     `json:"defaultSourceId"`
	Sources         map[string]sourceStatsJSON `json:"sources"`
	SourcePriority  []sourceEntryJSON          `json:"sourcePriority"`
}

type sourceStatsJSON struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	CandidateCount   int    `json:"candidateCount"`
	File             string `json:"file"`
	SourceNameFile   string `json:"sourceNameFile"`
}

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.PostgresDSN, 8)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DB 初始化失败: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.ApplySchema(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "Schema 应用失败: %v\n", err)
		os.Exit(1)
	}

	dataDir := cfg.CandidateDataDir
	if !filepath.IsAbs(dataDir) {
		// import 默认相对仓库根执行. 若相对路径, 优先以当前工作目录解析.
		if _, err := os.Stat(dataDir); err != nil {
			abs, aerr := filepath.Abs(dataDir)
			if aerr == nil {
				dataDir = abs
			}
		}
	}

	slog.Info("开始导入", "dataDir", dataDir)

	// 1) chars
	charsPath := filepath.Join(dataDir, "candidate_char_db.json")
	charsCount, err := importChars(ctx, pool, charsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "导入 chars 失败: %v\n", err)
		os.Exit(1)
	}
	slog.Info("chars 导入完成", "count", charsCount)

	// 2) sources (从 source_index 的 sourcePriority 反推)
	idxPath := filepath.Join(dataDir, "source_index.json")
	idx, err := readSourceIndex(idxPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 source_index 失败: %v\n", err)
		os.Exit(1)
	}
	if err := importSources(ctx, pool, idx.SourcePriority); err != nil {
		fmt.Fprintf(os.Stderr, "导入 sources 失败: %v\n", err)
		os.Exit(1)
	}
	slog.Info("sources 导入完成", "count", len(idx.SourcePriority))

	// 3) candidates + name_source_names, 按来源逐个处理
	totalCandidates := 0
	totalSourceNames := 0
	for _, sp := range idx.SourcePriority {
		stats, ok := idx.Sources[sp.ID]
		if !ok {
			slog.Warn("source 无 stats 跳过", "id", sp.ID)
			continue
		}
		srcCandidateFile := filepath.Join(dataDir, stats.File)
		candNames, err := readStringArray(srcCandidateFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取候选名失败 %s: %v\n", srcCandidateFile, err)
			os.Exit(1)
		}
		if len(candNames) != stats.CandidateCount {
			fmt.Fprintf(os.Stderr, "候选名数量对照失败 %s: 文件 %d / index %d\n", sp.ID, len(candNames), stats.CandidateCount)
			os.Exit(1)
		}

		c, err := importCandidates(ctx, pool, sp.ID, candNames)
		if err != nil {
			fmt.Fprintf(os.Stderr, "导入 candidates 失败 %s: %v\n", sp.ID, err)
			os.Exit(1)
		}
		totalCandidates += c
		slog.Info("candidates 导入完成", "source", sp.ID, "count", c)

		// name_source_names 可选
		if stats.SourceNameFile != "" {
			srcNameFile := filepath.Join(dataDir, stats.SourceNameFile)
			nameSources, err := readNameSources(srcNameFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "读取 name_sources 失败 %s: %v\n", srcNameFile, err)
				os.Exit(1)
			}
			n, err := importNameSourceNames(ctx, pool, sp.ID, nameSources)
			if err != nil {
				fmt.Fprintf(os.Stderr, "导入 name_source_names 失败 %s: %v\n", sp.ID, err)
				os.Exit(1)
			}
			totalSourceNames += n
			slog.Info("name_source_names 导入完成", "source", sp.ID, "count", n)
		}
	}

	slog.Info("导入完成",
		"chars", charsCount,
		"sources", len(idx.SourcePriority),
		"candidates", totalCandidates,
		"name_source_names", totalSourceNames)

	// 行数对照: 与 source_index 期望一致则通过
	expectedCandidates := 0
	for _, sp := range idx.SourcePriority {
		if stats, ok := idx.Sources[sp.ID]; ok {
			expectedCandidates += stats.CandidateCount
		}
	}
	if totalCandidates != expectedCandidates {
		fmt.Fprintf(os.Stderr, "候选名总数对照失败: 实际 %d / 期望 %d\n", totalCandidates, expectedCandidates)
		os.Exit(1)
	}
	slog.Info("行数对照通过", "candidates", totalCandidates, "expected", expectedCandidates)
}

func importChars(ctx context.Context, pool *db.Pool, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("读取 %s: %w", path, err)
	}
	var m map[string]charInfoJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return 0, fmt.Errorf("解析 chars JSON: %w", err)
	}

	// 批量插入: 1000 行每批
	const batch = 1000
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// 先清空, 保证可重入
	if _, err := tx.Exec(ctx, "TRUNCATE name_source_names, candidates, sources, chars RESTART IDENTITY CASCADE"); err != nil {
		return 0, fmt.Errorf("TRUNCATE 失败: %w", err)
	}

	rows := make([][]any, 0, batch)
	total := 0
	for _, k := range keys {
		c := m[k]
		rows = append(rows, []any{
			c.Char, c.Pinyin, c.Tone, c.PinyinNoTone,
			c.Initial, c.InitialMethod, c.InitialPlace,
			c.Vowel, c.VowelType, c.Count, c.IsPolyphone,
		})
		if len(rows) >= batch {
			if _, err := tx.CopyFrom(ctx,
				[]string{"chars"},
				[]string{"char", "pinyin", "tone", "pinyin_no_tone",
					"initial", "initial_method", "initial_place",
					"vowel", "vowel_type", "count", "is_polyphone"},
				newRowsSource(rows)); err != nil {
				return 0, fmt.Errorf("CopyFrom chars: %w", err)
			}
			total += len(rows)
			rows = rows[:0]
		}
	}
	if len(rows) > 0 {
		if _, err := tx.CopyFrom(ctx,
			[]string{"chars"},
			[]string{"char", "pinyin", "tone", "pinyin_no_tone",
				"initial", "initial_method", "initial_place",
				"vowel", "vowel_type", "count", "is_polyphone"},
			newRowsSource(rows)); err != nil {
			return 0, fmt.Errorf("CopyFrom chars: %w", err)
		}
		total += len(rows)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return total, nil
}

func importSources(ctx context.Context, pool *db.Pool, srcs []sourceEntryJSON) error {
	_, err := pool.Exec(ctx, "TRUNCATE sources CASCADE")
	if err != nil {
		return fmt.Errorf("TRUNCATE sources: %w", err)
	}
	rows := make([][]any, 0, len(srcs))
	for _, s := range srcs {
		rows = append(rows, []any{s.ID, s.Label, s.Priority, s.Weight, s.Category, s.Description})
	}
	_, err = pool.CopyFrom(ctx,
		[]string{"sources"},
		[]string{"id", "label", "priority", "weight", "category", "description"},
		newRowsSource(rows))
	return err
}

func importCandidates(ctx context.Context, pool *db.Pool, sourceID string, names []string) (int, error) {
	rows := make([][]any, 0, len(names))
	for _, n := range names {
		runes := []rune(n)
		if len(runes) != 2 {
			// 跳过非 2 字名, 与 TS 端 hydration 一致
			continue
		}
		rows = append(rows, []any{n, string(runes[0]), string(runes[1]), sourceID, 0, 0.0})
	}
	if len(rows) == 0 {
		return 0, nil
	}
	n, err := pool.CopyFrom(ctx,
		[]string{"candidates"},
		[]string{"name", "first_char", "second_char", "source_id", "frequency", "confidence"},
		newRowsSource(rows))
	return int(n), err
}

func importNameSourceNames(ctx context.Context, pool *db.Pool, sourceID string, nameSources map[string][]string) (int, error) {
	// 一次查出 (name, candidate_id) 反查表, 避免逐条查
	rows, err := pool.Query(ctx,
		"SELECT id, name FROM candidates WHERE source_id = $1", sourceID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	idByName := make(map[string]int64, 256)
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return 0, err
		}
		idByName[name] = id
	}
	if rows.Err() != nil {
		return 0, rows.Err()
	}

	buf := make([][]any, 0, 1000)
	total := 0
	for name, sns := range nameSources {
		cid, ok := idByName[name]
		if !ok {
			continue
		}
		for _, sn := range sns {
			buf = append(buf, []any{cid, sn})
			if len(buf) >= 1000 {
				if _, err := pool.CopyFrom(ctx,
					[]string{"name_source_names"},
					[]string{"candidate_id", "source_name"},
					newRowsSource(buf)); err != nil {
					return total, err
				}
				total += len(buf)
				buf = buf[:0]
			}
		}
	}
	if len(buf) > 0 {
		if _, err := pool.CopyFrom(ctx,
			[]string{"name_source_names"},
			[]string{"candidate_id", "source_name"},
			newRowsSource(buf)); err != nil {
			return total, err
		}
		total += len(buf)
	}
	return total, nil
}

// --- JSON 读取辅助 ---

func readSourceIndex(path string) (*sourceIndexJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx sourceIndexJSON
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func readStringArray(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func readNameSources(path string) (map[string][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string][]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}