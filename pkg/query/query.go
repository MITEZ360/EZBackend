// Package query 负责把 HTTP 查询参数解析为分页、排序与过滤条件，并生成 GORM Scope。
// 所有字段均经过白名单映射，避免任意列被查询或排序。
package query

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Params 是解析后的列表查询参数。
type Params struct {
	Page     int
	PageSize int
	Sort     string
	Keyword  string
	Filters  map[string]string
}

// Parse 解析 page / page_size / sort / keyword 及白名单内的过滤参数。
func Parse(c *gin.Context) *Params {
	p := &Params{Filters: map[string]string{}}
	p.Page = atoi(c.Query("page"), 1)
	if p.Page < 1 {
		p.Page = 1
	}
	p.PageSize = atoi(c.Query("page_size"), 20)
	if p.PageSize < 1 {
		p.PageSize = 20
	}
	if p.PageSize > 500 {
		p.PageSize = 500
	}
	p.Sort = c.Query("sort")
	p.Keyword = strings.TrimSpace(c.Query("keyword"))
	if p.Keyword == "" {
		p.Keyword = strings.TrimSpace(c.Query("q"))
	}
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 && v[0] != "" {
			p.Filters[k] = v[0]
		}
	}
	return p
}

// Offset 计算偏移量。
func (p *Params) Offset() int { return (p.Page - 1) * p.PageSize }

// Order 依据白名单生成 ORDER BY 子句，未知字段回退到默认排序。
func (p *Params) Order(allowed map[string]string, def string) string {
	if p.Sort == "" {
		return def
	}
	parts := strings.Split(p.Sort, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		desc := false
		if strings.HasPrefix(part, "-") {
			desc = true
			part = part[1:]
		} else if strings.HasPrefix(part, "+") {
			part = part[1:]
		}
		col, ok := allowed[part]
		if !ok {
			continue
		}
		if desc {
			out = append(out, col+" DESC")
		} else {
			out = append(out, col+" ASC")
		}
	}
	if len(out) == 0 {
		return def
	}
	return strings.Join(out, ", ")
}

// Filter 依据白名单追加等值/模糊/区间/集合过滤，支持后缀：
// _like、_prefix、_in、_nin、_gte、_lte、_gt、_lt、_ne。
func (p *Params) Filter(db *gorm.DB, allowed map[string]string) *gorm.DB {
	for key, val := range p.Filters {
		switch key {
		case "page", "page_size", "sort", "keyword", "q":
			continue
		}
		field, suffix := splitSuffix(key)
		col, ok := allowed[field]
		if !ok {
			// 前端习惯用 camelCase（deptId / uploaderId），白名单以列名为准；
			// 归一化后再查一次，避免参数被静默忽略导致筛选失效。
			if normalized := camelToSnake(field); normalized != field {
				col, ok = allowed[normalized]
			}
		}
		if !ok {
			continue
		}
		switch suffix {
		case "":
			db = db.Where(col+" = ?", val)
		case "ne":
			db = db.Where(col+" <> ?", val)
		case "like":
			db = db.Where(col+" LIKE ?", "%"+val+"%")
		case "prefix":
			db = db.Where(col+" LIKE ?", val+"%")
		case "in":
			db = db.Where(col+" IN ?", splitList(val))
		case "nin":
			db = db.Where(col+" NOT IN ?", splitList(val))
		case "gte":
			db = db.Where(col+" >= ?", val)
		case "lte":
			db = db.Where(col+" <= ?", val)
		case "gt":
			db = db.Where(col+" > ?", val)
		case "lt":
			db = db.Where(col+" < ?", val)
		}
	}
	return db
}

// Search 对多个列做关键字模糊匹配（OR 关系）。
// 关键字中的 % _ \ 会被转义，避免用户输入被当成通配符。
func Search(db *gorm.DB, keyword string, columns ...string) *gorm.DB {
	if keyword == "" || len(columns) == 0 {
		return db
	}
	like := "%" + escapeLike(keyword) + "%"
	cond := db.Session(&gorm.Session{NewDB: true})
	for i, col := range columns {
		if i == 0 {
			cond = cond.Where(col+" LIKE ? ESCAPE '\\'", like)
			continue
		}
		cond = cond.Or(col+" LIKE ? ESCAPE '\\'", like)
	}
	return db.Where(cond)
}

// escapeLike 转义 LIKE 通配符。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

// camelToSnake 把 camelCase 转为 snake_case，用于兼容前端参数命名：
// deptId → dept_id、uploaderId → uploader_id。已是 snake_case 时原样返回。
func camelToSnake(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'A' && ch <= 'Z' {
			if i > 0 && s[i-1] != '_' {
				b.WriteByte('_')
			}
			b.WriteByte(ch + ('a' - 'A'))
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func splitSuffix(key string) (string, string) {
	for _, s := range []string{"_like", "_prefix", "_in", "_nin", "_gte", "_lte", "_gt", "_lt", "_ne"} {
		if strings.HasSuffix(key, s) {
			return strings.TrimSuffix(key, s), strings.TrimPrefix(s, "_")
		}
	}
	return key, ""
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoi(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
