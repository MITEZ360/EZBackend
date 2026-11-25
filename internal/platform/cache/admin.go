// 缓存管理能力：给运维中心提供后端信息、键扫描、按命名空间清理。
//
// 这层只做「事实采集与操作」，不做分页、筛选与权限判断——那些属于业务模块。
// 扫描强制带上限，避免大实例上一条管理请求拖垮 Redis 或占满内存。
package cache

import (
	"context"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// DefaultScanLimit 单次扫描最多返回的键数。
const DefaultScanLimit = 5000

// FlushScanLimit 清空操作的扫描上限，比列表扫描宽松得多。
const FlushScanLimit = 100000

// KeyInfo 描述一个缓存键。TTL 单位为秒，-1 表示永不过期，-2 表示键已不存在。
type KeyInfo struct {
	Key       string `json:"key"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	Size      int64  `json:"size"`
	TTL       int64  `json:"ttl"`
}

// Backend 是缓存后端的运行信息，不扫描键。
type Backend struct {
	Kind     string `json:"kind"`
	Prefix   string `json:"prefix"`
	Version  string `json:"version"`
	DBKeys   int64  `json:"dbKeys"`
	UsedMem  int64  `json:"usedMemory"`
	Hits     int64  `json:"hits"`
	Misses   int64  `json:"misses"`
	Capacity int    `json:"capacity"`
}

// BackendInfo 读取后端概况。Redis 的命中数与内存是实例级统计（含其他 DB），
// 运维页需要如实标注，不能当成当前命名空间的数字。
func BackendInfo(ctx context.Context, c Cache) (Backend, error) {
	switch v := c.(type) {
	case *redisCache:
		b := Backend{Kind: "redis", Prefix: v.prefix}
		if n, err := v.rdb.DBSize(ctx).Result(); err == nil {
			b.DBKeys = n
		}
		info, err := v.rdb.Info(ctx, "all").Result()
		if err != nil {
			return b, err
		}
		m := parseInfo(info)
		b.Version = m["redis_version"]
		b.UsedMem, _ = strconv.ParseInt(m["used_memory"], 10, 64)
		b.Hits, _ = strconv.ParseInt(m["keyspace_hits"], 10, 64)
		b.Misses, _ = strconv.ParseInt(m["keyspace_misses"], 10, 64)
		return b, nil
	case *localCache:
		return Backend{
			Kind: "local", DBKeys: int64(v.lru.Len()), Capacity: v.size,
			Hits: v.hits.Load(), Misses: v.misses.Load(),
		}, nil
	}
	return Backend{Kind: c.Kind()}, nil
}

// Scan 按关键字与命名空间扫描缓存键。
//
// keyword 为前缀匹配，支持 * 与 ? 通配；为空表示全部。
// namespace 非空时只在 "namespace:*" 范围内扫描。
func Scan(ctx context.Context, c Cache, keyword, namespace string, limit int) ([]KeyInfo, bool, error) {
	if limit <= 0 {
		limit = DefaultScanLimit
	}
	pattern := buildPattern(keyword, namespace)
	switch v := c.(type) {
	case *redisCache:
		return v.scanKeys(ctx, pattern, limit)
	case *localCache:
		return v.scanKeys(pattern, limit)
	}
	return []KeyInfo{}, false, nil
}

// DeleteKeys 删除指定键，返回实际删除数量。
func DeleteKeys(ctx context.Context, c Cache, keys []string) (int64, error) {
	switch v := c.(type) {
	case *redisCache:
		full := make([]string, 0, len(keys))
		for _, k := range keys {
			if k = strings.TrimSpace(k); k != "" {
				full = append(full, v.key(k))
			}
		}
		if len(full) == 0 {
			return 0, nil
		}
		return v.rdb.Del(ctx, full...).Result()
	case *localCache:
		now := time.Now().UnixMilli()
		var n int64
		for _, k := range keys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if it, ok := v.lru.Peek(k); ok {
				if it.exp == 0 || it.exp > now {
					if v.lru.Remove(k) {
						n++
					}
				}
			}
			v.mu.Lock()
			delete(v.ctr, k)
			v.mu.Unlock()
		}
		return n, nil
	}
	return 0, nil
}

// FlushNamespace 清空某个命名空间（为空表示当前应用命名空间下的全部键），
// 返回删除数量。先扫描完再分批删除，避免边扫边删造成游标漏键。
func FlushNamespace(ctx context.Context, c Cache, namespace string) (int64, error) {
	ns := strings.TrimSpace(namespace)
	pattern := "*"
	if ns != "" {
		pattern = ns + ":*"
	}
	keys, _, err := Scan(ctx, c, pattern, "", FlushScanLimit)
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	var total int64
	const batch = 200
	for start := 0; start < len(keys); start += batch {
		end := start + batch
		if end > len(keys) {
			end = len(keys)
		}
		names := make([]string, 0, end-start)
		for _, k := range keys[start:end] {
			names = append(names, k.Key)
		}
		n, err := DeleteKeys(ctx, c, names)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// ---- Redis 实现 ----

func (r *redisCache) scanKeys(ctx context.Context, pattern string, limit int) ([]KeyInfo, bool, error) {
	full := pattern
	if r.prefix != "" {
		full = r.prefix + ":" + pattern
	}
	out := make([]KeyInfo, 0, limit)
	seen := make(map[string]struct{}, limit)
	var cursor uint64
	truncated := false
	for {
		keys, next, err := r.rdb.Scan(ctx, cursor, full, 500).Result()
		if err != nil {
			return nil, false, err
		}
		for _, k := range keys {
			logical := r.logicalKey(k)
			if _, ok := seen[logical]; ok {
				continue
			}
			seen[logical] = struct{}{}
			out = append(out, KeyInfo{Key: logical, Namespace: namespaceOf(logical)})
			if len(out) >= limit {
				truncated = true
				break
			}
		}
		cursor = next
		if truncated || cursor == 0 {
			break
		}
	}
	if len(out) == 0 {
		return out, truncated, nil
	}

	pipe := r.rdb.Pipeline()
	types := make([]*redis.StatusCmd, len(out))
	ttls := make([]*redis.Cmd, len(out))
	sizes := make([]*redis.IntCmd, len(out))
	for i := range out {
		k := r.key(out[i].Key)
		types[i] = pipe.Type(ctx, k)
		// 用原始 TTL 而非 DurationCmd，才能区分 -1（永不过期）与 -2（键已消失）。
		ttls[i] = pipe.Do(ctx, "TTL", k)
		sizes[i] = pipe.MemoryUsage(ctx, k)
	}
	// 单条命令失败（例如旧版 Redis 不支持 MEMORY USAGE）不影响整体列表。
	_, _ = pipe.Exec(ctx)

	for i := range out {
		if t, err := types[i].Result(); err == nil && t != "" {
			out[i].Type = t
		}
		if n, err := sizes[i].Result(); err == nil {
			out[i].Size = n
		}
		if sec, err := ttls[i].Int64(); err == nil {
			out[i].TTL = sec
		}
	}
	sortKeyInfos(out)
	return out, truncated, nil
}

// ---- 进程内实现 ----

func (l *localCache) scanKeys(pattern string, limit int) ([]KeyInfo, bool, error) {
	now := time.Now().UnixMilli()
	out := make([]KeyInfo, 0, minInt(limit, l.lru.Len()))
	truncated := false
	for _, k := range l.lru.Keys() {
		if ok, err := path.Match(pattern, k); err != nil || !ok {
			continue
		}
		it, ok := l.lru.Peek(k)
		if !ok {
			continue
		}
		if it.exp > 0 && it.exp <= now {
			// 顺手清理过期项，避免管理页出现早已失效的键。
			l.lru.Remove(k)
			continue
		}
		ttl := int64(-1)
		if it.exp > 0 {
			ttl = int64(math.Ceil(float64(it.exp-now) / 1000))
			if ttl < 1 {
				ttl = 1
			}
		}
		out = append(out, KeyInfo{
			Key: k, Namespace: namespaceOf(k), Type: "string",
			Size: int64(len(it.val)), TTL: ttl,
		})
		if len(out) >= limit {
			truncated = true
			break
		}
	}
	sortKeyInfos(out)
	return out, truncated, nil
}

// ---- 辅助 ----

func (r *redisCache) logicalKey(full string) string {
	if r.prefix == "" {
		return full
	}
	return strings.TrimPrefix(full, r.prefix+":")
}

func buildPattern(keyword, namespace string) string {
	kw := strings.TrimSpace(keyword)
	ns := strings.TrimSpace(namespace)
	if kw == "" {
		kw = "*"
	} else if !strings.ContainsAny(kw, "*?[") {
		kw += "*"
	}
	if ns != "" {
		kw = ns + ":" + kw
	}
	return kw
}

func namespaceOf(key string) string {
	if i := strings.Index(key, ":"); i > 0 {
		return key[:i]
	}
	return ""
}

func sortKeyInfos(list []KeyInfo) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Key < list[j].Key })
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseInfo 把 Redis INFO 的 key:value 文本解析为映射。
func parseInfo(text string) map[string]string {
	out := make(map[string]string, 96)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, ":"); i > 0 {
			out[line[:i]] = line[i+1:]
		}
	}
	return out
}
