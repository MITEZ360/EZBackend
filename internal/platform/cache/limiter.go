package cache

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter 是限流器抽象。
//
// 未配置 Redis 时使用进程内令牌桶，单机足够；配置 Redis 后自动切换为
// Redis 令牌桶，多实例共享配额——否则每个实例各放行 rps，总量会翻倍。
type Limiter interface {
	Allow(ctx context.Context, key string) (bool, error)
	Kind() string
	// SetRate 运行期调整速率与桶容量，用于管理端热更新限流参数。
	SetRate(rps float64, burst int)
}

// NewLimiter 依据缓存实现创建限流器：Redis 缓存对应分布式限流。
func NewLimiter(c Cache, rps float64, burst int) Limiter {
	if rps <= 0 {
		rps = 100
	}
	if burst <= 0 {
		burst = int(rps * 2)
		if burst < 1 {
			burst = 1
		}
	}
	if rc, ok := c.(*redisCache); ok {
		return &redisLimiter{rc: rc, rps: rps, burst: burst}
	}
	return newLocalLimiter(rps, burst)
}

// ---- Redis 令牌桶 ----

// limiterScript 是原子令牌桶：读取剩余令牌与上次时间，按经过时间补充后再扣减。
// 返回 1 表示放行，0 表示拒绝；同时刷新过期时间，空闲 key 自动回收。
const limiterScript = `
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local data = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then
  tokens = burst
  ts = now
end
local delta = math.max(0, now - ts)
tokens = math.min(burst, tokens + delta / 1000 * rate)
local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end
redis.call('HMSET', KEYS[1], 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', KEYS[1], ttl)
return allowed
`

type redisLimiter struct {
	mu    sync.RWMutex
	rc    *redisCache
	rps   float64
	burst int
}

func (l *redisLimiter) Kind() string { return "redis" }

// SetRate 更新速率。已有令牌桶会在下一次调用时按新参数计算，无需清理。
func (l *redisLimiter) SetRate(rps float64, burst int) {
	if rps <= 0 {
		return
	}
	if burst <= 0 {
		burst = int(rps * 2)
		if burst < 1 {
			burst = 1
		}
	}
	l.mu.Lock()
	l.rps, l.burst = rps, burst
	l.mu.Unlock()
}

func (l *redisLimiter) Allow(ctx context.Context, key string) (bool, error) {
	l.mu.RLock()
	rps, burst := l.rps, l.burst
	l.mu.RUnlock()
	// 桶空闲到足够回满的时间即可回收，再加 1 秒余量
	ttl := int64(float64(burst)/rps*1000) + 1000
	n, err := l.rc.rdb.Eval(ctx, limiterScript, []string{l.rc.key("rl:" + key)},
		rps, burst, time.Now().UnixMilli(), ttl).Int()
	if err != nil {
		// 限流组件故障不应阻断业务：放行并让调用方记录日志
		return true, err
	}
	return n == 1, nil
}

// ---- 进程内令牌桶 ----

type localLimiter struct {
	mu      sync.Mutex
	buckets map[string]*localBucket
	rps     rate.Limit
	burst   int
	lastGC  time.Time
}

type localBucket struct {
	l *rate.Limiter
	t time.Time
}

func newLocalLimiter(rps float64, burst int) *localLimiter {
	return &localLimiter{
		buckets: make(map[string]*localBucket),
		rps:     rate.Limit(rps),
		burst:   burst,
		lastGC:  time.Now(),
	}
}

func (l *localLimiter) Kind() string { return "local" }

// SetRate 更新速率。令牌桶的速率不可变，这里直接重建全部桶；
// 代价是本次调整会重置各 key 的瞬时配额，属于管理端操作的预期行为。
func (l *localLimiter) SetRate(rps float64, burst int) {
	if rps <= 0 {
		return
	}
	if burst <= 0 {
		burst = int(rps * 2)
		if burst < 1 {
			burst = 1
		}
	}
	l.mu.Lock()
	l.rps, l.burst = rate.Limit(rps), burst
	l.buckets = make(map[string]*localBucket)
	l.mu.Unlock()
}

func (l *localLimiter) Allow(_ context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.lastGC) > 5*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.t) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &localBucket{l: rate.NewLimiter(l.rps, l.burst)}
		l.buckets[key] = b
	}
	b.t = now
	return b.l.Allow(), nil
}
