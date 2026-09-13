// Package ratelimit: per-IP token bucket 限流. 仅 stdlib.
// 不引 x/time/rate 的原因: 该库初始 tokens=0 导致冷启动第一次请求必被拒.
// 本实现简单预填 tokens=burst, 更符合"匿名 RPM 立即可用"语义.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Limiter per-IP 限流器.
type Limiter struct {
	rpm     int
	burst   int
	buckets sync.Map // ip -> *bucket
	stopCh  chan struct{}
}

type bucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// Info 响应头回填用.
type Info struct {
	Limit     int
	Remaining int
	ResetUnix int64
}

// New:rpm 匿名限流每分钟 N; burst 桶容量.
func New(rpm, burst int) *Limiter {
	if rpm <= 0 {
		rpm = 30
	}
	if burst <= 0 {
		burst = rpm
	}
	l := &Limiter{rpm: rpm, burst: burst, stopCh: make(chan struct{})}
	go l.gc()
	return l
}

// Close 停止后台扫描 goroutine.
func (l *Limiter) Close() { close(l.stopCh) }

// Allow 返回是否放行, 当前 Info (Limit/Remaining/Reset), 被拒时的 retry-after 秒.
// rate 每 IP 独立: tokens 累积速度为 rpm/60 tokens/sec, 容量 burst.
func (l *Limiter) Allow(ip string) (bool, Info, int) {
	if ip == "" {
		ip = "_local"
	}
	v, _ := l.buckets.LoadOrStore(ip, &bucket{tokens: float64(l.burst), last: time.Now()})
	b := v.(*bucket)

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	if !b.last.IsZero() {
		elapsed := now.Sub(b.last).Seconds()
		if elapsed > 0 {
			ratePerSec := float64(l.rpm) / 60.0
			b.tokens = math.Min(float64(l.burst), b.tokens+elapsed*ratePerSec)
		}
	}
	b.last = now

	if b.tokens >= 1 {
		b.tokens -= 1
		rem := int(b.tokens)
		if rem < 0 {
			rem = 0
		}
		// Reset 设为回到满需多久 (陪同 X-RateLimit-Reset 语义)
		deficit := float64(l.burst) - b.tokens
		ratePerSec := float64(l.rpm) / 60.0
		resetIn := time.Duration(0)
		if deficit > 0 && ratePerSec > 0 {
			resetIn = time.Duration(deficit / ratePerSec * float64(time.Second))
		}
		return true, Info{Limit: l.rpm, Remaining: rem, ResetUnix: now.Add(resetIn).Unix()}, 0
	}

	// 拒绝: deficit = 1 - b.tokens
	deficit := 1 - b.tokens
	ratePerSec := float64(l.rpm) / 60.0
	var delaySec int
	if ratePerSec > 0 {
		delaySec = int(math.Ceil(deficit / ratePerSec))
		if delaySec < 1 {
			delaySec = 1
		}
	}
	return false, Info{
		Limit:     l.rpm,
		Remaining: 0,
		ResetUnix: now.Add(time.Duration(delaySec) * time.Second).Unix(),
	}, delaySec
}

// gc 定期清掉空并发 bucket 节省内存. 10 分钟扫一次.
// ponytail: 仅扫 "last < 30 分钟前" 的桶; 不绑软实时停机, 桶上锁, 不阻塞前台 Allow.
func (l *Limiter) gc() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-t.C:
			cutoff := time.Now().Add(-30 * time.Minute)
			l.buckets.Range(func(k, v any) bool {
				b := v.(*bucket)
				b.mu.Lock()
				idle := b.last.Before(cutoff)
				b.mu.Unlock()
				if idle {
					l.buckets.Delete(k)
				}
				return true
			})
		}
	}
}
