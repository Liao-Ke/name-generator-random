// Package api 跨 handler 共享的依赖: PG 连接池 + 内存缓存 charDb/candidates.
package api

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/namegen/server/internal/core"
	"github.com/namegen/server/internal/db"
)

// Deps 共享依赖容器. 生命周期 = 服务进程.
type Deps struct {
	Pool *db.Pool

	charDbOnce sync.Once
	charDb     core.CharDb
	charDbOK   bool

	candidateMu sync.RWMutex
	candidates  map[string][]core.CandidateName

	// allMu/allCandidates: source 缺省时用的"全源合并去重"结果, 构造一次后复用.
	// 见 handler_random.go 的 loadAllCandidates.
	allMu         sync.RWMutex
	allCandidates []core.CandidateName

	surnamesOnce sync.Once
	surnames     []string

	rngPool sync.Pool // *rand.Rand with source from 系统时间, 仅匿名 Go map per-call
	// 单 global rng 仅生成种子; per-request 复用避免每次 rand.NewSource 开销
	globalRngMu sync.Mutex
	globalRng   *rand.Rand
}

// NewDeps 构造依赖容器. 需要已建好的 PG 连接池.
func NewDeps(pool *db.Pool) *Deps {
	return &Deps{
		Pool:       pool,
		candidates: make(map[string][]core.CandidateName, 5),
		globalRng:  rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// GetCharDb 进程级单例加载字库 (一次性 ~3ms).
func (d *Deps) GetCharDb(ctx context.Context) (core.CharDb, bool) {
	d.charDbOnce.Do(func() {
		db, ok := loadCharDbFromPG(ctx, d.Pool)
		if !ok {
			slog.Error("加载字库失败")
			return
		}
		d.charDb = db
		d.charDbOK = true
		slog.Info("charDb 已缓存", "size", len(db))
	})
	return d.charDb, d.charDbOK
}

// GetSurnames 进程级单例加载百家姓候选单字姓氏列表 (仅含在 chars 中存在的字).
// /api/random 缺省 surname 时从本表随机抽一个.
func (d *Deps) GetSurnames(ctx context.Context) []string {
	d.surnamesOnce.Do(func() {
		rows, err := d.Pool.Query(ctx, "SELECT char FROM surnames ORDER BY char")
		if err != nil {
			slog.Error("加载 surnames 失败", "err", err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				slog.Error("scan surname", "err", err)
				return
			}
			d.surnames = append(d.surnames, s)
		}
		slog.Info("surnames 已缓存", "size", len(d.surnames))
	})
	return d.surnames
}

// GetCandidateDb 拉指定 source 的候选名; 进程级缓存, 首次访问 hydrate.
func (d *Deps) GetCandidateDb(ctx context.Context, sourceID string) []core.CandidateName {
	d.candidateMu.RLock()
	if cached, ok := d.candidates[sourceID]; ok {
		d.candidateMu.RUnlock()
		return cached
	}
	d.candidateMu.RUnlock()

	charDb, ok := d.GetCharDb(ctx)
	if !ok {
		return nil
	}
	cd := loadCandidatesFromPG(ctx, d.Pool, sourceID, charDb)
	if len(cd) == 0 {
		return nil
	}
	d.candidateMu.Lock()
	d.candidates[sourceID] = cd
	d.candidateMu.Unlock()
	slog.Info("candidateDb 已缓存", "source", sourceID, "size", len(cd))
	return cd
}

// NewRNG 给每次请求一个具种子的随机源. seed=0 用 globalRng 提供的随机数.
func (d *Deps) NewRNG(seed int64) *rand.Rand {
	if seed != 0 {
		return rand.New(rand.NewSource(seed))
	}
	d.globalRngMu.Lock()
	defer d.globalRngMu.Unlock()
	// 用 global 随机种子产生这次的源, 避免每次固定种子的并发冲突.
	seed2 := d.globalRng.Int63()
	return rand.New(rand.NewSource(seed2))
}

// --- PG 读取辅助 ---

func loadCharDbFromPG(ctx context.Context, pool *db.Pool) (core.CharDb, bool) {
	rows, err := pool.Query(ctx, `
		SELECT char, pinyin, tone, pinyin_no_tone,
			initial, initial_method, initial_place,
			vowel, vowel_type, count, is_polyphone
		FROM chars`)
	if err != nil {
		slog.Error("query chars", "err", err)
		return nil, false
	}
	defer rows.Close()
	m := make(core.CharDb, 8000)
	for rows.Next() {
		var c core.CharInfo
		if err := rows.Scan(&c.Char, &c.Pinyin, &c.Tone, &c.PinyinNoTone,
			&c.Initial, &c.InitialMethod, &c.InitialPlace,
			&c.Vowel, &c.VowelType, &c.Count, &c.IsPolyphone); err != nil {
			slog.Error("scan char", "err", err)
			return nil, false
		}
		m[c.Char] = c
	}
	if rows.Err() != nil {
		slog.Error("rows err", "err", rows.Err())
		return nil, false
	}
	return m, len(m) > 0
}

func loadCandidatesFromPG(ctx context.Context, pool *db.Pool, sourceID string, charDb core.CharDb) []core.CandidateName {
	// 主体: 候选名 + chars
	crows, err := pool.Query(ctx, `
		SELECT name FROM candidates WHERE source_id = $1`, sourceID)
	if err != nil {
		slog.Error("query candidates", "err", err, "source", sourceID)
		return nil
	}
	compact := make([]string, 0, 64)
	for crows.Next() {
		var n string
		if err := crows.Scan(&n); err != nil {
			crows.Close()
			return nil
		}
		compact = append(compact, n)
	}
	crows.Close()

	// name_source_names 按 candidate_id 原序取回 (preserve 导入顺序 = 源 JSON 顺序)
	srows, err := pool.Query(ctx, `
		SELECT c.name, n.source_name
		FROM candidates c
		JOIN name_source_names n ON n.candidate_id = c.id
		WHERE c.source_id = $1`, sourceID)
	if err != nil {
		slog.Error("query name_source_names", "err", err, "source", sourceID)
		return nil
	}
	defer srows.Close()
	nameToSrc := make(map[string][]string)
	for srows.Next() {
		var name, sn string
		if err := srows.Scan(&name, &sn); err != nil {
			return nil
		}
		nameToSrc[name] = append(nameToSrc[name], sn)
	}

	return core.HydrateCandidateDb(core.HydrateInput{
		Data:              compact,
		SourceID:          sourceID,
		CharDb:            charDb,
		SourceNamesByName: nameToSrc,
	})
}
