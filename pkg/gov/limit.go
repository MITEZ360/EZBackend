package gov

import (
	"sync"
	"time"
)

// Limiter 是进程内令牌桶限流器，按 key（路由 / 租户 / 用户）独立计数。
//
// 它只做单机限流：多实例的总量上限 = 单机上限 × 副本数。需要全局精确配额时
// 应改用集中式计数器，但不该把它塞进请求热路径。
type Limiter struct {
	mu      sync.Mutex
	rate    float64 // 每秒补充的令牌数
	burst   float64 // 桶容量
	buckets map[string]*tokenBucket
	sweptAt time.Time
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter 创建限流器；rate<=0 表示不限制，Allow 永远返回 true。
func NewLimiter(rate, burst float64) *Limiter {
	if burst <= 0 {
		burst = rate
	}
	return &Limiter{rate: rate, burst: burst, buckets: map[string]*tokenBucket{}, sweptAt: time.Now()}
}

// Allow 尝试取一个令牌。
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.rate <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[key]
	if b == nil {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		elapsed := now.Sub(b.last).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * l.rate
			if b.tokens > l.burst {
				b.tokens = l.burst
			}
			b.last = now
		}
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	l.sweep(now)
	return true
}

// sweep 定期清理长时间空闲的桶，避免按租户限流时 map 无限增长。
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.sweptAt) < time.Minute {
		return
	}
	l.sweptAt = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > 5*time.Minute {
			delete(l.buckets, k)
		}
	}
}
