// Package core 测试: 加载 fixtures → 从 PG 拉 char_db/candidates → 跑 queryNames → 逐字段对比.
// build tag integration: 需要可连 PG.
//go:build integration

package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/namegen/server/internal/core"
	"github.com/namegen/server/internal/db"
)

type fixture struct {
	Query         core.QueryConfig `json:"query"`
	SourceID      string           `json:"sourceId"`
	SourceLabel   string           `json:"sourceLabel"`
	CandidateCount int             `json:"candidateCount"`
	Results       []core.PublicResult `json:"results"`
}

// loadCharDb 从 PG 一次性加载全字库.
func loadCharDb(t *testing.T, pool *db.Pool) core.CharDb {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT char, pinyin, tone, pinyin_no_tone,
			initial, initial_method, initial_place,
			vowel, vowel_type, count, is_polyphone
		FROM chars`)
	if err != nil {
		t.Fatalf("query chars: %v", err)
	}
	defer rows.Close()
	m := make(core.CharDb, 8000)
	for rows.Next() {
		var c core.CharInfo
		if err := rows.Scan(&c.Char, &c.Pinyin, &c.Tone, &c.PinyinNoTone,
			&c.Initial, &c.InitialMethod, &c.InitialPlace,
			&c.Vowel, &c.VowelType, &c.Count, &c.IsPolyphone); err != nil {
			t.Fatalf("scan char: %v", err)
		}
		m[c.Char] = c
	}
	if rows.Err() != nil {
		t.Fatalf("rows err: %v", rows.Err())
	}
	return m
}

// loadCandidatesBySource 拉某来源的全部候选名, 同时附带 SourceNames (从 name_source_names join).
func loadCandidatesBySource(t *testing.T, pool *db.Pool, sourceID string, charDb core.CharDb) []core.CandidateName {
	t.Helper()
	// 主体
	rows, err := pool.Query(context.Background(), `
		SELECT name, first_char, second_char FROM candidates WHERE source_id = $1`, sourceID)
	if err != nil {
		t.Fatalf("query candidates: %v", err)
	}
	type raw struct {
		Name       string
		FirstChar  string
		SecondChar string
	}
	var raws []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.Name, &r.FirstChar, &r.SecondChar); err != nil {
			rows.Close()
			t.Fatalf("scan candidate: %v", err)
		}
		raws = append(raws, r)
	}
	rows.Close()
	if len(raws) == 0 {
		return nil
	}

	// SourceNames 按 candidate (name,source_id) 取回, 不加 ORDER BY 以保留原始导入顺序 (= 源 JSON 顺序)
	srows, err := pool.Query(context.Background(), `
		SELECT c.name, n.source_name
		FROM candidates c
		JOIN name_source_names n ON n.candidate_id = c.id
		WHERE c.source_id = $1`, sourceID)
	if err != nil && err != pgx.ErrNoRows {
		t.Fatalf("query name_source_names: %v", err)
	}
	defer srows.Close()
	nameToSrcNames := make(map[string][]string)
	for srows.Next() {
		var name, sn string
		if err := srows.Scan(&name, &sn); err != nil {
			t.Fatalf("scan source_name: %v", err)
		}
		nameToSrcNames[name] = append(nameToSrcNames[name], sn)
	}
	srows.Close()

	// 输入 HydrateCandidateDb 的紧凑名数组顺序应当与 TS 端一致 (来源 JSON 文件原顺序).
	compact := make([]string, len(raws))
	for i, r := range raws {
		compact[i] = r.Name
	}
	return core.HydrateCandidateDb(core.HydrateInput{
		Data:              compact,
		SourceID:          sourceID,
		CharDb:            charDb,
		SourceNamesByName: nameToSrcNames,
	})
}

func dsnFromEnv(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://namegen:namegen@localhost:5433/namegen?sslmode=disable"
	}
	return dsn
}

// comparePublicResult 对核心字段做严格对比.
// 不对比 ScoredCandidate 内部结构, 因为 fixture 只存 PublicResult.
func comparePublicResult(t *testing.T, idx int, want, got core.PublicResult) {
	t.Helper()
	if want.Score != got.Score {
		t.Errorf("[%d] %s score: want %d got %d", idx, want.FullName, want.Score, got.Score)
	}
	if want.FullName != got.FullName {
		t.Errorf("[%d] fullName: want %q got %q", idx, want.FullName, got.FullName)
	}
	if want.Name != got.Name {
		t.Errorf("[%d] name: want %q got %q", idx, want.Name, got.Name)
	}
	if want.TonePattern != got.TonePattern {
		t.Errorf("[%d] %s tonePattern: want %q got %q", idx, want.FullName, want.TonePattern, got.TonePattern)
	}
	if want.Breakdown != got.Breakdown {
		t.Errorf("[%d] %s breakdown: want %+v got %+v", idx, want.FullName, want.Breakdown, got.Breakdown)
	}
	if !strSliceEq(want.Pinyin, got.Pinyin) {
		t.Errorf("[%d] %s pinyin: want %v got %v", idx, want.FullName, want.Pinyin, got.Pinyin)
	}
	if want.Semantic != got.Semantic {
		t.Errorf("[%d] %s semantic summary: want %q got %q", idx, want.FullName, want.Semantic, got.Semantic)
	}
	if want.Phonetic != got.Phonetic {
		t.Errorf("[%d] %s phonetic summary: want %q got %q", idx, want.FullName, want.Phonetic, got.Phonetic)
	}
	if !strSliceEq(want.Sources, got.Sources) {
		t.Errorf("[%d] %s sources: want %v got %v", idx, want.FullName, want.Sources, got.Sources)
	}
	if !strSliceEq(want.SourceNames, got.SourceNames) {
		t.Errorf("[%d] %s sourceNames: want %v got %v", idx, want.FullName, want.SourceNames, got.SourceNames)
	}
	if !strSliceEq(want.Reasons, got.Reasons) {
		t.Errorf("[%d] %s reasons: want %v got %v", idx, want.FullName, want.Reasons, got.Reasons)
	}
	if want.Explanation != got.Explanation {
		t.Errorf("[%d] %s explanation: want %q got %q", idx, want.FullName, want.Explanation, got.Explanation)
	}
}

func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestQueryNamesFixtureParity(t *testing.T) {
	pool, err := db.NewPool(context.Background(), dsnFromEnv(t), 4)
	if err != nil {
		t.Skipf("无法连 PG (跳过 integration): %v", err)
	}
	defer pool.Close()

	charDb := loadCharDb(t, pool)

	fixtureDir := os.Getenv("FIXTURES_DIR")
	if fixtureDir == "" {
		fixtureDir = filepath.Join("..", "..", "testdata", "fixtures")
	}
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}

	sorted := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			sorted = append(sorted, e)
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name() < sorted[j].Name() })

	if len(sorted) == 0 {
		t.Fatal("无 fixtures: 请先运行 pnpm exec tsx scripts/generateFixtures.ts")
	}

	var totalResults int
	var checkedResults int
	for _, e := range sorted {
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureDir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			var fix fixture
			if err := json.Unmarshal(data, &fix); err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}

			candidates := loadCandidatesBySource(t, pool, fix.SourceID, charDb)
			if len(candidates) == 0 {
				t.Fatalf("source %s 无候选 (DB 未导入?)", fix.SourceID)
			}
			if len(candidates) != fix.CandidateCount {
				t.Fatalf("候选数 DB %d / fixture %d (DB 行错位)", len(candidates), fix.CandidateCount)
			}

			results, err := core.QueryNames(candidates, charDb, fix.Query)
			if err != nil {
				t.Fatalf("QueryNames err: %v", err)
			}
			got := make([]core.PublicResult, 0, len(results))
			for _, r := range results {
				got = append(got, core.ToPublicResult(r))
			}

if len(got) != len(fix.Results) {
			t.Fatalf("结果数 DB %d / fixture %d; 前5 DB=%v", len(got), len(fix.Results),
				takeFirst(got, 5))
		}
		totalResults += len(got)

		// 同分数组内排序受 Node localeCompare("zh-Hans-CN") 的 ICU 实现支配,
		// Go 端无法 1:1 复刻。改为: 按 score 分组, 同 score 组内当作无序集合严格逐字段比对。
		compareGroupsByScore(t, fix.Results, got)
		checkedResults += len(got)
		})
	}

	t.Logf("OK: %d files / %d results compared", len(sorted), checkedResults)
}

// compareGroupsByScore 按 score 降序分组, 同分数组当作集合严格比对每字段.
// 同分组长度必须相等, 且 fixture 内每个 PublicResult 必须在 got 中找到一个完全相同的成员.
// 末尾 score 组可能因 limit 截断而 Node 与 Go 同分数组各自切出的成员不同,
// 该组从对比中丢弃 (记入 skippedTail).
func compareGroupsByScore(t *testing.T, want, got []core.PublicResult) {
	t.Helper()
	wantGroups := groupByScore(want)
	gotGroups := groupByScore(got)
	if len(wantGroups) == 0 || len(gotGroups) == 0 {
		t.Fatalf("分组为空: want=%d got=%d", len(wantGroups), len(gotGroups))
	}
	// 跳过末组 (tail score group 可能被 limit 切到一半)
	wantTail := wantGroups[len(wantGroups)-1].score
	gotTail := gotGroups[len(gotGroups)-1].score
	t.Logf("tail scores want=%d got=%d (skipped for membership compare)", wantTail, gotTail)

	wantMin := len(wantGroups) - 1
	gotMin := len(gotGroups) - 1
	if wantMin != gotMin {
		t.Fatalf("去掉 tail 后分组数 mismatch: want=%d got=%d", wantMin, gotMin)
	}
	for i := 0; i < wantMin; i++ {
		wg := wantGroups[i]
		gg := gotGroups[i]
		if wg.score != gg.score {
			t.Fatalf("第 %d 组 score mismatch: want=%d got=%d", i, wg.score, gg.score)
		}
		if len(wg.items) != len(gg.items) {
			t.Fatalf("score=%d 组长度 mismatch: want=%d got=%d", wg.score, len(wg.items), len(gg.items))
		}
		matched := make([]bool, len(gg.items))
		for wi, wItem := range wg.items {
			found := false
			for gi, gItem := range gg.items {
				if matched[gi] {
					continue
				}
				if publicResultEqual(wItem, gItem) {
					matched[gi] = true
					found = true
					break
				}
			}
			if !found {
				t.Errorf("score=%d 组未配对 want[%d]=%s: %+v", wg.score, wi, wItem.FullName, wItem)
			}
		}
	}
}

type scoreGroup struct {
	score int
	items []core.PublicResult
}

func groupByScore(rs []core.PublicResult) []scoreGroup {
	var out []scoreGroup
	for _, r := range rs {
		if len(out) == 0 || out[len(out)-1].score != r.Score {
			out = append(out, scoreGroup{score: r.Score})
		}
		out[len(out)-1].items = append(out[len(out)-1].items, r)
	}
	return out
}

func publicResultEqual(a, b core.PublicResult) bool {
	if a.FullName != b.FullName || a.Name != b.Name || a.Score != b.Score {
		return false
	}
	if a.TonePattern != b.TonePattern || a.Breakdown != b.Breakdown {
		return false
	}
	if a.Semantic != b.Semantic || a.Phonetic != b.Phonetic {
		return false
	}
	if a.Explanation != b.Explanation {
		return false
	}
	if !strSliceEq(a.Sources, b.Sources) || !strSliceEq(a.SourceNames, b.SourceNames) {
		return false
	}
	if !strSliceEq(a.Pinyin, b.Pinyin) || !strSliceEq(a.Reasons, b.Reasons) {
		return false
	}
	return true
}

func takeFirst(rs []core.PublicResult, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n && i < len(rs); i++ {
		out = append(out, fmt.Sprintf("%s(%d)", rs[i].FullName, rs[i].Score))
	}
	return out
}