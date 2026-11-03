package cache

import (
	"context"
	"testing"
	"time"

	"github.com/keel-framework/keel/internal/platform/config"
)

func newLocalCache(t *testing.T) Cache {
	t.Helper()
	disabled := false
	c, err := New(config.Redis{Enabled: &disabled, LocalSize: 100}, nil)
	if err != nil {
		t.Fatalf("创建本地缓存失败: %v", err)
	}
	if c.Kind() != "local" {
		t.Fatalf("未启用 Redis 时应降级为本地缓存，实际 %s", c.Kind())
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestLocalCacheBasic(t *testing.T) {
	ctx := context.Background()
	c := newLocalCache(t)

	if _, err := c.Get(ctx, "missing"); err == nil {
		t.Fatal("不存在的键应返回错误")
	}
	if err := c.Set(ctx, "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	b, err := c.Get(ctx, "k")
	if err != nil || string(b) != "v" {
		t.Fatalf("读取异常: %q %v", b, err)
	}
	ok, err := c.Exists(ctx, "k")
	if err != nil || !ok {
		t.Fatalf("Exists 异常: %v %v", ok, err)
	}
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if ok, _ := c.Exists(ctx, "k"); ok {
		t.Fatal("删除后仍存在")
	}
}

func TestLocalCacheTTL(t *testing.T) {
	ctx := context.Background()
	c := newLocalCache(t)
	_ = c.Set(ctx, "short", []byte("1"), 30*time.Millisecond)
	if ttl, err := c.TTL(ctx, "short"); err != nil || ttl <= 0 {
		t.Fatalf("TTL 异常: %v %v", ttl, err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := c.Get(ctx, "short"); err == nil {
		t.Fatal("过期键应不可读")
	}
}

func TestLocalCacheIncr(t *testing.T) {
	ctx := context.Background()
	c := newLocalCache(t)
	v, err := c.Incr(ctx, "counter", 1)
	if err != nil || v != 1 {
		t.Fatalf("首次自增异常: %d %v", v, err)
	}
	v, _ = c.Incr(ctx, "counter", 2)
	if v != 3 {
		t.Fatalf("自增结果错误: %d", v)
	}
}

func TestJSONHelpers(t *testing.T) {
	ctx := context.Background()
	c := newLocalCache(t)
	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	if err := SetJSON(ctx, c, "user:1", payload{Name: "轻舟", Age: 3}, time.Minute); err != nil {
		t.Fatalf("SetJSON 失败: %v", err)
	}
	var got payload
	ok, err := GetJSON(ctx, c, "user:1", &got)
	if err != nil || !ok || got.Name != "轻舟" || got.Age != 3 {
		t.Fatalf("GetJSON 异常: %v %v %+v", ok, err, got)
	}
	if ok, _ := GetJSON(ctx, c, "user:2", &got); ok {
		t.Fatal("不存在的键应返回 false")
	}
}

func TestRemember(t *testing.T) {
	ctx := context.Background()
	c := newLocalCache(t)
	calls := 0
	load := func() (any, error) {
		calls++
		return "value", nil
	}
	var out string
	if err := Remember(ctx, c, "cfg", time.Minute, &out, load); err != nil {
		t.Fatalf("Remember 失败: %v", err)
	}
	if err := Remember(ctx, c, "cfg", time.Minute, &out, load); err != nil {
		t.Fatalf("Remember 二次失败: %v", err)
	}
	if calls != 1 {
		t.Fatalf("命中缓存后不应重复加载，实际加载 %d 次", calls)
	}
	if out != "value" {
		t.Fatalf("缓存值错误: %q", out)
	}
}

func TestLock(t *testing.T) {
	ctx := context.Background()
	c := newLocalCache(t)
	release, ok := Lock(ctx, c, "job:demo", time.Minute)
	if !ok {
		t.Fatal("首次加锁应成功")
	}
	if _, ok := Lock(ctx, c, "job:demo", time.Minute); ok {
		t.Fatal("重复加锁应失败")
	}
	release()
	if _, ok := Lock(ctx, c, "job:demo", time.Minute); !ok {
		t.Fatal("释放后应可再次加锁")
	}
}
