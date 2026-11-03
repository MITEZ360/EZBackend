// Package cache 统一缓存接口：生产使用 Redis，未启用时自动降级为进程内 LRU，
// 保证单机部署零外部依赖即可运行。
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/keel-framework/keel/internal/platform/config"
)

// ErrMiss 表示缓存未命中。
var ErrMiss = errors.New("cache: 未命中")

// Cache 是缓存抽象，值统一为字节切片以兼容任意序列化格式。
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, key string) (bool, error)
	Incr(ctx context.Context, key string, delta int64) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	TTL(ctx context.Context, key string) (time.Duration, error)
	Kind() string
	Close() error
}

// New 依据配置创建缓存实例。
//
// 启用规则：显式 enabled 优先，未配置时 addr 非空即启用。
// 连接失败默认直接返回错误（配置了却连不上，静默降级会让多实例状态分裂）；
// 只有 optional: true 时才退回进程内缓存。
func New(cfg config.Redis, log *zap.Logger) (Cache, error) {
	if log == nil {
		log = zap.NewNop()
	}
	size := cfg.LocalSize
	if size <= 0 {
		size = 100000
	}
	if !cfg.UseRedis() {
		log.Info("缓存使用进程内 LRU（未配置 Redis）", zap.Int("capacity", size))
		return newLocal(size), nil
	}
	{
		addr := cfg.EffectiveAddr()
		rdb := redis.NewClient(&redis.Options{
			Addr:         addr,
			Username:     cfg.Username,
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     atLeast(cfg.PoolSize, 16),
			DialTimeout:  orDefaultDuration(cfg.DialTimeout, 3*time.Second),
			ReadTimeout:  orDefaultDuration(cfg.ReadTimeout, 3*time.Second),
			WriteTimeout: orDefaultDuration(cfg.ReadTimeout, 3*time.Second),
		})
		ctx, cancel := context.WithTimeout(context.Background(), orDefaultDuration(cfg.DialTimeout, 3*time.Second))
		defer cancel()
		if err := rdb.Ping(ctx).Err(); err != nil {
			_ = rdb.Close()
			if cfg.Optional {
				log.Warn("Redis 连接失败，按 optional 配置降级为进程内缓存",
					zap.String("addr", addr), zap.Error(err))
				return newLocal(size), nil
			}
			return nil, fmt.Errorf("连接 Redis 失败（%s）: %w", addr, err)
		}
		log.Info("缓存已启用 Redis", zap.String("addr", addr), zap.Int("db", cfg.DB), zap.String("prefix", cfg.Prefix))
		return &redisCache{rdb: rdb, prefix: cfg.Prefix}, nil
	}
}

func atLeast(v, min int) int {
	if v < min {
		return min
	}
	return v
}

func orDefaultDuration(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// RedisClient 返回底层 Redis 客户端；当前为进程内缓存时返回 false。
// 限流、分布式锁等组件复用同一个连接池，避免重复建连。
func RedisClient(c Cache) (*redis.Client, bool) {
	if rc, ok := c.(*redisCache); ok {
		return rc.rdb, true
	}
	return nil, false
}

// ---- Redis 实现 ----

type redisCache struct {
	rdb    *redis.Client
	prefix string
}

func (r *redisCache) key(k string) string {
	if r.prefix == "" {
		return k
	}
	return r.prefix + ":" + k
}

func (r *redisCache) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := r.rdb.Get(ctx, r.key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrMiss
	}
	return b, err
}

func (r *redisCache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return r.rdb.Set(ctx, r.key(key), val, ttl).Err()
}

func (r *redisCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = r.key(k)
	}
	return r.rdb.Del(ctx, full...).Err()
}

func (r *redisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.rdb.Exists(ctx, r.key(key)).Result()
	return n > 0, err
}

func (r *redisCache) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	return r.rdb.IncrBy(ctx, r.key(key), delta).Result()
}

func (r *redisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.rdb.Expire(ctx, r.key(key), ttl).Err()
}

func (r *redisCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.rdb.TTL(ctx, r.key(key)).Result()
}

func (r *redisCache) Kind() string { return "redis" }

func (r *redisCache) Close() error { return r.rdb.Close() }

// ---- 进程内 LRU 实现 ----

// localItem 保存值与绝对过期时间，避免为每个键创建定时器。
type localItem struct {
	val []byte
	exp int64 // 毫秒时间戳，0 表示永不过期
}

type localCache struct {
	lru *lru.LRU[string, localItem]
	mu  sync.Mutex
	ctr map[string]int64
}

func newLocal(size int) *localCache {
	return &localCache{
		lru: lru.NewLRU[string, localItem](size, nil, 0),
		ctr: make(map[string]int64),
	}
}

func (l *localCache) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := l.lru.Get(key)
	if !ok {
		return nil, ErrMiss
	}
	if v.exp > 0 && v.exp <= time.Now().UnixMilli() {
		l.lru.Remove(key)
		return nil, ErrMiss
	}
	return v.val, nil
}

func (l *localCache) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	it := localItem{val: val}
	if ttl > 0 {
		it.exp = time.Now().Add(ttl).UnixMilli()
	}
	l.lru.Add(key, it)
	return nil
}

func (l *localCache) Delete(_ context.Context, keys ...string) error {
	for _, k := range keys {
		l.lru.Remove(k)
		l.mu.Lock()
		delete(l.ctr, k)
		l.mu.Unlock()
	}
	return nil
}

func (l *localCache) Exists(ctx context.Context, key string) (bool, error) {
	_, err := l.Get(ctx, key)
	if errors.Is(err, ErrMiss) {
		return false, nil
	}
	return err == nil, err
}

func (l *localCache) Incr(_ context.Context, key string, delta int64) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ctr[key] += delta
	b, _ := json.Marshal(l.ctr[key])
	it := localItem{val: b}
	if old, ok := l.lru.Get(key); ok && old.exp > 0 {
		it.exp = old.exp
	}
	l.lru.Add(key, it)
	return l.ctr[key], nil
}

func (l *localCache) Expire(_ context.Context, key string, ttl time.Duration) error {
	v, ok := l.lru.Get(key)
	if !ok || ttl <= 0 {
		return nil
	}
	v.exp = time.Now().Add(ttl).UnixMilli()
	l.lru.Add(key, v)
	return nil
}

func (l *localCache) TTL(_ context.Context, key string) (time.Duration, error) {
	v, ok := l.lru.Get(key)
	if !ok {
		return 0, ErrMiss
	}
	if v.exp == 0 {
		return -1, nil
	}
	return time.Until(time.UnixMilli(v.exp)), nil
}

func (l *localCache) Kind() string { return "local" }

func (l *localCache) Close() error {
	l.lru.Purge()
	return nil
}

// ---- 便捷方法 ----

// GetJSON 读取并反序列化，未命中返回 (false, nil)。
func GetJSON(ctx context.Context, c Cache, key string, dst any) (bool, error) {
	b, err := c.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrMiss) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return false, err
	}
	return true, nil
}

// SetJSON 序列化并写入。
func SetJSON(ctx context.Context, c Cache, key string, val any, ttl time.Duration) error {
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return c.Set(ctx, key, b, ttl)
}

// Remember 缓存读取模式：命中直接返回，未命中调用 loader 并回填。
func Remember(ctx context.Context, c Cache, key string, ttl time.Duration, dst any, loader func() (any, error)) error {
	if ok, err := GetJSON(ctx, c, key, dst); err == nil && ok {
		return nil
	}
	val, err := loader()
	if err != nil {
		return err
	}
	if err := SetJSON(ctx, c, key, val, ttl); err != nil {
		return err
	}
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// Lock 获取分布式锁（Redis SET NX），返回释放函数。
// 未启用 Redis 时退化为进程内互斥锁，语义保持一致，便于单机部署。
func Lock(ctx context.Context, c Cache, key string, ttl time.Duration) (func(), bool) {
	if rc, ok := c.(*redisCache); ok {
		token := fmt.Sprintf("%d", time.Now().UnixNano())
		okSet, err := rc.rdb.SetNX(ctx, rc.key("lock:"+key), token, ttl).Result()
		if err != nil || !okSet {
			return func() {}, false
		}
		release := func() {
			script := `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
			_ = rc.rdb.Eval(context.Background(), script, []string{rc.key("lock:" + key)}, token).Err()
		}
		return release, true
	}
	return lockLocal(key, ttl)
}

// localLock 是进程内锁的持有记录。
type localLock struct {
	token string
	exp   time.Time
}

var localLocks sync.Map // key -> localLock

// lockLocal 在单进程内模拟 SET NX 语义，过期后可被重新抢占。
func lockLocal(key string, ttl time.Duration) (func(), bool) {
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now()
	mine := localLock{token: fmt.Sprintf("%d", time.Now().UnixNano()), exp: now.Add(ttl)}
	for {
		actual, loaded := localLocks.LoadOrStore(key, mine)
		if !loaded {
			return func() { localLocks.CompareAndDelete(key, mine) }, true
		}
		cur, ok := actual.(localLock)
		if !ok || now.After(cur.exp) {
			if localLocks.CompareAndSwap(key, actual, mine) {
				return func() { localLocks.CompareAndDelete(key, mine) }, true
			}
			continue
		}
		return func() {}, false
	}
}
