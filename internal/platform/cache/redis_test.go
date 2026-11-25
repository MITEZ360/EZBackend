package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/MITEZ360/tenon/internal/platform/config"
)

// redisForTest 连接测试用 Redis；不可用时跳过，保证无外部依赖也能跑单测。
func redisForTest(t *testing.T, prefix string) Cache {
	t.Helper()
	addr := os.Getenv("TENON_TEST_REDIS")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	c, err := New(config.Redis{Addr: addr, Prefix: prefix}, zap.NewNop())
	if err != nil {
		t.Skipf("Redis 不可用，跳过: %v", err)
	}
	t.Cleanup(func() {
		if rc, ok := c.(*redisCache); ok {
			keys, _ := rc.rdb.Keys(context.Background(), rc.key("*")).Result()
			if len(keys) > 0 {
				_ = rc.rdb.Del(context.Background(), keys...).Err()
			}
		}
		_ = c.Close()
	})
	return c
}

// 两个"实例"各自建立连接，验证分布式锁互斥。
func TestRedisLockAcrossInstances(t *testing.T) {
	a := redisForTest(t, "tenontest:lock")
	b := redisForTest(t, "tenontest:lock")
	ctx := context.Background()

	release, ok := Lock(ctx, a, "job:1", time.Minute)
	if !ok {
		t.Fatal("第一个实例应拿到锁")
	}
	if _, ok := Lock(ctx, b, "job:1", time.Minute); ok {
		t.Fatal("第二个实例不应拿到锁")
	}
	release()

	release2, ok := Lock(ctx, b, "job:1", time.Minute)
	if !ok {
		t.Fatal("释放后应能重新获取")
	}
	release2()
}

// 两个实例共享同一份限流配额。
func TestRedisLimiterSharedAcrossInstances(t *testing.T) {
	a := redisForTest(t, "tenontest:rl")
	b := redisForTest(t, "tenontest:rl")
	ctx := context.Background()

	la := NewLimiter(a, 1, 1)
	lb := NewLimiter(b, 1, 1)
	if la.Kind() != "redis" || lb.Kind() != "redis" {
		t.Fatalf("应使用 Redis 限流，实际: %s / %s", la.Kind(), lb.Kind())
	}
	if ok, _ := la.Allow(ctx, "u:1"); !ok {
		t.Fatal("首次请求应放行")
	}
	if ok, _ := lb.Allow(ctx, "u:1"); ok {
		t.Fatal("另一实例应被同一配额拒绝")
	}
}

func TestLocalLimiter(t *testing.T) {
	l := NewLimiter(newLocal(100), 1, 1)
	ctx := context.Background()
	if ok, _ := l.Allow(ctx, "k"); !ok {
		t.Fatal("首次请求应放行")
	}
	if ok, _ := l.Allow(ctx, "k"); ok {
		t.Fatal("第二次请求应被限流")
	}
	// 不同 key 独立计数
	if ok, _ := l.Allow(ctx, "other"); !ok {
		t.Fatal("不同 key 不应互相影响")
	}
}
