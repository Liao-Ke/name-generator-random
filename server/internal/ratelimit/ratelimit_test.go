// Package ratelimit: token bucket 单元测试 (不依赖外部).
package ratelimit

import (
	"testing"
	"time"
)

func TestAllowBurstFull(t *testing.T) {
	l := New(30, 30)
	defer l.Close()
	// 首次应放行, 因为 burst 预填
	for i := 0; i < 30; i++ {
		ok, _, _ := l.Allow("ip1")
		if !ok {
			t.Fatalf("attempt %d: 预填 burst 应允许, got 拒绝", i)
		}
	}
	// 第 31 个 exceed (ratePlusBurst 计完)
	ok, _, retryAfter := l.Allow("ip1")
	if ok {
		t.Fatalf("第 31 次 should 429")
	}
	if retryAfter < 1 {
		t.Fatalf("retryAfter 应 ≥1, got %d", retryAfter)
	}
}

func TestPerIPIsolation(t *testing.T) {
	l := New(30, 1)
	defer l.Close()
	if ok, _, _ := l.Allow("ip1"); !ok {
		t.Fatalf("ip1 first should pass (burst 1)")
	}
	if ok, _, _ := l.Allow("ip1"); ok {
		t.Fatalf("ip1 second should 429 (burst 1)")
	}
	// ip2 桶独立, 应放行
	if ok, _, _ := l.Allow("ip2"); !ok {
		t.Fatalf("ip2 first should pass")
	}
}

func TestRefill(t *testing.T) {
	// rpm=600, burst=1 → 10 tokens/sec; 排空后 200ms 应可再放行
	l := New(600, 1)
	defer l.Close()
	if ok, _, _ := l.Allow("ip1"); !ok {
		t.Fatalf("first should pass")
	}
	time.Sleep(250 * time.Millisecond)
	if ok, _, _ := l.Allow("ip1"); !ok {
		t.Fatalf("after 250ms should refill at least 1 token (rate 10/sec)")
	}
}