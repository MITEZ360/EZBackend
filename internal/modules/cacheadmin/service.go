// Package cacheadmin 运维中心「缓存管理」。
//
// 提供缓存后端概况、键分布与清理能力。列表按运行时真实键扫描，
// 不依赖业务模块上报，因此新增缓存键无需改动本模块。
package cacheadmin

import (
	"context"
	"sort"
	"strings"

	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/pkg/errs"
)

// Service 缓存管理业务。
type Service struct{ c cache.Cache }

// NewService 创建服务。
func NewService(c cache.Cache) *Service { return &Service{c: c} }

// NamespaceStat 命名空间键数量，供前端筛选项与快捷清理使用。
type NamespaceStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Overview 缓存概况。
type Overview struct {
	Kind       string          `json:"kind"`
	Prefix     string          `json:"prefix"`
	Version    string          `json:"version"`
	DBKeys     int64           `json:"dbKeys"`
	UsedMemory int64           `json:"usedMemory"`
	Hits       int64           `json:"hits"`
	Misses     int64           `json:"misses"`
	Capacity   int             `json:"capacity"`
	Keys       int             `json:"keys"`
	Expiring   int             `json:"expiring"`
	Persistent int             `json:"persistent"`
	KeyBytes   int64           `json:"keyBytes"`
	Truncated  bool            `json:"truncated"`
	Namespaces []NamespaceStat `json:"namespaces"`
}

// Query 键列表筛选条件。
type Query struct {
	Page      int
	PageSize  int
	Keyword   string
	Namespace string
}

// Page 键列表分页结果。
type Page struct {
	List      []cache.KeyInfo `json:"list"`
	Total     int             `json:"total"`
	Page      int             `json:"page"`
	PageSize  int             `json:"pageSize"`
	Truncated bool            `json:"truncated"`
}

// Overview 汇总后端信息与扫描结果。
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	be, err := cache.BackendInfo(ctx, s.c)
	if err != nil {
		return Overview{}, err
	}
	keys, truncated, err := cache.Scan(ctx, s.c, "", "", cache.DefaultScanLimit)
	if err != nil {
		return Overview{}, err
	}
	out := Overview{
		Kind: be.Kind, Prefix: be.Prefix, Version: be.Version, DBKeys: be.DBKeys,
		UsedMemory: be.UsedMem, Hits: be.Hits, Misses: be.Misses, Capacity: be.Capacity,
		Keys: len(keys), Truncated: truncated,
	}
	counts := map[string]int{}
	for _, k := range keys {
		out.KeyBytes += k.Size
		if k.TTL < 0 {
			out.Persistent++
		} else {
			out.Expiring++
		}
		counts[k.Namespace]++
	}
	out.Namespaces = make([]NamespaceStat, 0, len(counts))
	for name, n := range counts {
		out.Namespaces = append(out.Namespaces, NamespaceStat{Name: name, Count: n})
	}
	sort.SliceStable(out.Namespaces, func(i, j int) bool {
		if out.Namespaces[i].Count != out.Namespaces[j].Count {
			return out.Namespaces[i].Count > out.Namespaces[j].Count
		}
		return out.Namespaces[i].Name < out.Namespaces[j].Name
	})
	return out, nil
}

// Keys 分页返回键列表。
func (s *Service) Keys(ctx context.Context, q Query) (Page, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	keys, truncated, err := cache.Scan(ctx, s.c, q.Keyword, q.Namespace, cache.DefaultScanLimit)
	if err != nil {
		return Page{}, err
	}
	sort.SliceStable(keys, func(i, j int) bool { return keys[i].Key < keys[j].Key })

	total := len(keys)
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	return Page{
		List: keys[start:end], Total: total, Page: q.Page, PageSize: q.PageSize, Truncated: truncated,
	}, nil
}

// Delete 删除指定键。单次上限与列表扫描上限一致，避免误传全量键。
func (s *Service) Delete(ctx context.Context, keys []string) (int64, error) {
	cleaned := make([]string, 0, len(keys))
	for _, k := range keys {
		if k = strings.TrimSpace(k); k != "" {
			cleaned = append(cleaned, k)
		}
	}
	if len(cleaned) == 0 {
		return 0, errs.BadRequest("请选择要删除的缓存键")
	}
	if len(cleaned) > cache.DefaultScanLimit {
		return 0, errs.BadRequest("单次删除的键数量过多")
	}
	return cache.DeleteKeys(ctx, s.c, cleaned)
}

// Flush 清空命名空间；namespace 为空表示清空当前应用前缀下的全部键。
func (s *Service) Flush(ctx context.Context, namespace string) (int64, error) {
	ns := strings.TrimSpace(namespace)
	if !validNamespace(ns) {
		return 0, errs.BadRequest("命名空间只能包含字母、数字、下划线与短横线")
	}
	return cache.FlushNamespace(ctx, s.c, ns)
}

func validNamespace(ns string) bool {
	if len(ns) > 64 {
		return false
	}
	for _, r := range ns {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
