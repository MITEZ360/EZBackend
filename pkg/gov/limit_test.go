package gov

import (
	"sync"
	"testing"
	"time"
)

// TestLimiterBurstThenReject 桶容量用尽后拒绝，补充速率决定恢复速度。
func TestLimiterBurstThenReject(t *testing.T) {
	l := NewLimiter(1, 3) // 每秒 1 个，容量 3
	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("第 %d 次应放行（容量内）", i+1)
		}
	}
	if l.Allow("k") {
		t.Fatal("容量耗尽后应拒绝")
	}
}

// TestLimiterRefill 等待后按速率补桶。
func TestLimiterRefill(t *testing.T) {
	l := NewLimiter(1000, 1) // 1ms 补 1 个
	if !l.Allow("k") {
		t.Fatal("首次应放行")
	}
	if l.Allow("k") {
		t.Fatal("容量为 1 时第二次应拒绝")
	}
	time.Sleep(5 * time.Millisecond)
	if !l.Allow("k") {
		t.Fatal("补充后应放行")
	}
}

// TestLimiterKeyIsolation 不同 key 各自计数。
func TestLimiterKeyIsolation(t *testing.T) {
	l := NewLimiter(1, 1)
	if !l.Allow("a") || !l.Allow("b") {
		t.Fatal("不同 key 应各自有初始令牌")
	}
	if l.Allow("a") || l.Allow("b") {
		t.Fatal("各自耗尽后都应拒绝")
	}
}

// TestLimiterDisabled 非正速率表示不限制；nil 限流器也必须放行（未启用限流）。
func TestLimiterDisabled(t *testing.T) {
	l := NewLimiter(0, 0)
	for i := 0; i < 1000; i++ {
		if !l.Allow("k") {
			t.Fatal("rate<=0 应恒放行")
		}
	}
	var nilLimiter *Limiter
	if !nilLimiter.Allow("k") {
		t.Fatal("nil 限流器应放行")
	}
}

// TestLimiterDefaultBurst 只给速率不给容量时，容量等于速率。
func TestLimiterDefaultBurst(t *testing.T) {
	l := NewLimiter(2, 0)
	if !l.Allow("k") || !l.Allow("k") {
		t.Fatal("容量应等于速率 2")
	}
	if l.Allow("k") {
		t.Fatal("超过容量应拒绝")
	}
}

// TestLimiterConcurrent 并发取令牌不会超发：容量 50，100 个并发只有 50 个成功。
func TestLimiterConcurrent(t *testing.T) {
	l := NewLimiter(0.0001, 50) // 补充可忽略，只验证并发扣减的原子性
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		allow int
	)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow("k") {
				mu.Lock()
				allow++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allow != 50 {
		t.Fatalf("容量 50 应恰好放行 50 次，实际 %d", allow)
	}
}

// TestBreakerThreeStates 关闭 → 打开 → 半开探测 → 恢复。
func TestBreakerThreeStates(t *testing.T) {
	b := NewBreaker(3, 30*time.Millisecond)

	// closed：失败未达阈值仍放行。
	for i := 0; i < 2; i++ {
		b.Fail("svc")
		if !b.Allow("svc") {
			t.Fatalf("第 %d 次失败后仍应放行", i+1)
		}
	}
	b.Fail("svc") // 第 3 次达到阈值
	if b.Allow("svc") {
		t.Fatal("达到阈值后应打开熔断")
	}

	// open → half-open：窗口过后只放一个探测。
	time.Sleep(40 * time.Millisecond)
	if !b.Allow("svc") {
		t.Fatal("熔断窗口过后应放行探测请求")
	}
	if b.Allow("svc") {
		t.Fatal("探测期间不应再放行第二个请求")
	}

	// 探测成功 → closed，计数清零。
	b.Success("svc")
	if !b.Allow("svc") {
		t.Fatal("成功后应立即恢复放行")
	}
	b.Fail("svc")
	if !b.Allow("svc") {
		t.Fatal("计数应已清零，一次失败不该重新熔断")
	}
}

// TestBreakerProbeFailureReopens 探测失败后重新计时，不会进入持续的半开放行。
func TestBreakerProbeFailureReopens(t *testing.T) {
	b := NewBreaker(1, 20*time.Millisecond)
	b.Fail("svc")
	if b.Allow("svc") {
		t.Fatal("阈值 1，一次失败即应熔断")
	}
	time.Sleep(30 * time.Millisecond)
	if !b.Allow("svc") {
		t.Fatal("窗口过后应放行探测")
	}
	b.Fail("svc") // 探测失败
	if b.Allow("svc") {
		t.Fatal("探测失败后应重新熔断")
	}
}

// TestBreakerKeyIsolation 熔断按目标隔离，不能互相牵连。
func TestBreakerKeyIsolation(t *testing.T) {
	b := NewBreaker(1, time.Minute)
	b.Fail("a")
	if b.Allow("a") {
		t.Fatal("a 应被熔断")
	}
	if !b.Allow("b") {
		t.Fatal("b 不应受影响")
	}
}

// TestBreakerDefaults nil 与非法参数的安全行为。
func TestBreakerDefaults(t *testing.T) {
	var nilBreaker *Breaker
	if !nilBreaker.Allow("x") {
		t.Fatal("nil 熔断器应放行")
	}
	nilBreaker.Fail("x")    // 不应 panic
	nilBreaker.Success("x") // 不应 panic

	b := NewBreaker(0, 0) // 回落到 5 次 / 10s
	for i := 0; i < 4; i++ {
		b.Fail("x")
		if !b.Allow("x") {
			t.Fatalf("默认阈值 5，第 %d 次失败后仍应放行", i+1)
		}
	}
	b.Fail("x")
	if b.Allow("x") {
		t.Fatal("第 5 次失败后应熔断")
	}
}
