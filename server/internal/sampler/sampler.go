// Package sampler: 在 queryNames 输出中按策略取 N 个.
//   - uniform: 无放回均匀采样 (Fisher-Yates 洗前 N)
//   - weighted: 无放回加权采样, 权重 ∝ exp(α·score/T), 高分易被选中但不排斥低分
package sampler

import (
	"math"
	"math/rand"
	"sort"

	"github.com/namegen/server/internal/core"
)

// SampleInput 通用入参.
// Alpha 控制 weighted 模式的锐度, 默认 0.15 (top 20% 拿约 50% 概率).
type SampleInput struct {
	Results  []core.ScoredCandidate
	N        int
	Strategy string  // "weighted" | "uniform"
	Alpha    float64 // weighted 用的锐度, <=0 时取默认 0.15
	Seed     int64   // 随机种子, 0 表示每次随机
}

// Sample 返回采样后的 ScoredCandidate 切片, 长度 min(N, len(results)).
// 不修改输入切片 (内部 copy 一份).
func Sample(in SampleInput, r *rand.Rand) []core.ScoredCandidate {
	src := make([]core.ScoredCandidate, len(in.Results))
	copy(src, in.Results)

	n := in.N
	if n > len(src) {
		n = len(src)
	}
	if n <= 0 {
		return nil
	}

	strategy := in.Strategy
	if strategy == "" {
		strategy = "weighted"
	}
	switch strategy {
	case "uniform":
		return sampleUniform(src, n, r)
	default: // weighted
		alpha := in.Alpha
		if alpha <= 0 {
			alpha = 0.15
		}
		return sampleWeighted(src, n, alpha, r)
	}
}

// sampleUniform Fisher-Yates 洗前 n. 输入不会被修改二次洗乱 (前 n 已排列正确).
func sampleUniform(src []core.ScoredCandidate, n int, r *rand.Rand) []core.ScoredCandidate {
	for i := 0; i < n; i++ {
		j := i + r.Intn(len(src)-i)
		src[i], src[j] = src[j], src[i]
	}
	return src[:n]
}

// sampleWeighted 按 softmax(α·score/T) 无放回采样.
// T = 当前剩余池的均分; 每抽一个重新归一化权重再抽下一个.
// O(N·K), K 是采样数, N 是池大小. 适合 N=50, K≤50.
func sampleWeighted(src []core.ScoredCandidate, n int, alpha float64, r *rand.Rand) []core.ScoredCandidate {
	// 对负 weight (score 但都是正), 不需截尾; score 区间 ~50-100, 适合 α=0.15
	// 选定后从池中删除该 index, 避免重复.
	out := make([]core.ScoredCandidate, 0, n)
	pool := make([]core.ScoredCandidate, len(src))
	copy(pool, src)

	weights := make([]float64, len(pool))
	for i, c := range pool {
		weights[i] = math.Exp(alpha * float64(c.Score))
	}

	for len(out) < n && len(pool) > 0 {
		var sum float64
		for _, w := range weights {
			sum += w
		}
		// 兜底: 全 NaN/0 退化为均匀
		if !(sum > 0) {
			j := r.Intn(len(pool))
			out = append(out, pool[j])
			pool = append(pool[:j], pool[j+1:]...)
			weights = append(weights[:j], weights[j+1:]...)
			continue
		}
		// 轮盘
		t := r.Float64() * sum
		acc := 0.0
		picked := -1
		for i, w := range weights {
			acc += w
			if t <= acc {
				picked = i
				break
			}
		}
		if picked < 0 {
			picked = len(pool) - 1
		}
		out = append(out, pool[picked])
		pool = append(pool[:picked], pool[picked+1:]...)
		weights = append(weights[:picked], weights[picked+1:]...)
	}

	// 排序保持与端点响应期望的稳定呈现: score 降序.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
