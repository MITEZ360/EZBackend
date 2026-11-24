// Package news 提供资讯聚合能力：批量入库、去重、检索与滚动清理。
//
// 数据是「7 天窗口 + 只追加」的时序数据，因此设计上有几处刻意的取舍：
//   - 不做软删除，过期即物理删除，避免表无限膨胀；
//   - 列表不返回正文，详情单独取，单页响应保持在几十 KB；
//   - 主键与发布时间索引覆盖列表排序，翻页走游标而非深 offset。
package news

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/i18n"
	"github.com/keel-framework/keel/pkg/query"
)

const (
	// MaxBatch 单次推送的条数上限。爬虫应拆批而不是一次灌几十 MB。
	MaxBatch = 200
	// MaxContent 单条正文上限，防止异常数据打爆存储。
	MaxContent = 200 << 10
	// cleanupChunk 清理时的单批删除量，避免长事务锁表。
	cleanupChunk = 2000
	// cleanupRounds 单次清理最多处理的批次数，防止任务长时间占用连接。
	cleanupRounds = 50
)

// Service 资讯业务。
type Service struct {
	db        *gorm.DB
	cache     cache.Cache
	log       *zap.Logger
	retention time.Duration
}

// NewService 创建资讯服务。retentionDays <= 0 时按 7 天处理。
func NewService(db *gorm.DB, c cache.Cache, log *zap.Logger, retentionDays int) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	if retentionDays <= 0 {
		retentionDays = 7
	}
	return &Service{
		db: db, cache: c, log: log,
		retention: time.Duration(retentionDays) * 24 * time.Hour,
	}
}

// Retention 返回保留窗口。
func (s *Service) Retention() time.Duration { return s.retention }

// ---- 导入 ----

// IngestItem 爬虫推送的单条资讯。
type IngestItem struct {
	ExternalID  string     `json:"externalId" binding:"max=160" label:"外部 ID"`
	Title       string     `json:"title" label:"标题"`
	Summary     string     `json:"summary" label:"摘要"`
	Content     string     `json:"content" label:"正文"`
	URL         string     `json:"url" binding:"max=512" label:"原文链接"`
	Author      string     `json:"author" binding:"max=64" label:"作者"`
	Category    string     `json:"category" binding:"max=32" label:"分类"`
	Tags        []string   `json:"tags" label:"标签"`
	CoverURL    string     `json:"coverUrl" binding:"max=512" label:"封面"`
	PublishedAt *time.Time `json:"publishedAt" label:"发布时间"`
}

// IngestRequest 批量推送入参。
type IngestRequest struct {
	Source string       `json:"source" binding:"required,max=32" label:"数据源"`
	Items  []IngestItem `json:"items" binding:"required,min=1,max=200" label:"资讯列表"`
}

// IngestError 单条失败明细，带下标方便爬虫定位。
type IngestError struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// IngestResult 导入结果。爬虫按 accepted/duplicate/invalid 对账。
type IngestResult struct {
	Source    string        `json:"source"`
	Received  int           `json:"received"`
	Accepted  int           `json:"accepted"`
	Duplicate int           `json:"duplicate"`
	Invalid   int           `json:"invalid"`
	Expired   int           `json:"expired"`
	Errors    []IngestError `json:"errors,omitempty"`
}

// Ingest 批量导入。整批在一个事务里写，冲突静默跳过，
// 因此爬虫可以安全重试同一批数据而不会产生重复。
//
// 来源与分类都是白名单：来源不合法整批拒绝（请求级属性，写一半没有意义），
// 分类不合法只把该条计入 invalid（条目级属性，不该拖垮其余数据）。
func (s *Service) Ingest(ctx context.Context, req *IngestRequest) (*IngestResult, error) {
	src, err := s.requireSource(ctx, req.Source)
	if err != nil {
		return nil, err
	}
	cats, err := s.enabledCategories(ctx)
	if err != nil {
		return nil, err
	}
	res := &IngestResult{Source: src.Code, Received: len(req.Items)}
	cutoff := time.Now().UTC().Add(-s.retention)

	now := time.Now().UTC()
	records := make([]*model.NewsArticle, 0, len(req.Items))
	seen := make(map[string]struct{}, len(req.Items))

	for i, it := range req.Items {
		rec, reason := normalizeItem(src, it, now)
		if reason == "" {
			if _, ok := cats[rec.Category]; !ok {
				reason = "分类未配置或已停用：" + rec.Category
			}
		}
		if reason != "" {
			res.Invalid++
			if len(res.Errors) < 20 {
				res.Errors = append(res.Errors, IngestError{Index: i, Reason: reason})
			}
			continue
		}
		// 早于保留窗口的条目直接丢弃：否则刚入库就会被下一轮清理删掉，
		// 白白消耗一次写入，还会让爬虫误以为数据已存档。
		if rec.PublishedAt.Before(cutoff) {
			res.Expired++
			continue
		}
		// 同一批内的重复（爬虫重试时常出现）在这里挡掉，不必交给数据库
		key := rec.SourceID + "|" + rec.ExternalID
		if _, dup := seen[key]; dup {
			res.Duplicate++
			continue
		}
		seen[key] = struct{}{}
		records = append(records, rec)
	}

	if len(records) > 0 {
		tx := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
			CreateInBatches(records, 100)
		if tx.Error != nil {
			return nil, tx.Error
		}
		res.Accepted = int(tx.RowsAffected)
		res.Duplicate += len(records) - res.Accepted
	}

	// 统计只做增量累加，避免每次推送都全表 count。
	if res.Accepted > 0 {
		s.db.WithContext(ctx).Model(&model.NewsSource{}).Where("id = ?", src.ID).
			Updates(map[string]any{
				"last_ingest_at": now,
				"total_ingested": gorm.Expr("total_ingested + ?", res.Accepted),
			})
	}
	s.log.Info("资讯导入完成",
		zap.String("source", src.Code), zap.Int("received", res.Received),
		zap.Int("accepted", res.Accepted), zap.Int("duplicate", res.Duplicate),
		zap.Int("invalid", res.Invalid), zap.Int("expired", res.Expired))
	return res, nil
}

// requireSource 按编码取已启用的数据源。
//
// 这里刻意不做自动注册：来源是运营方预先配置的白名单，
// 若允许爬虫自行造源，后台的图标、名称与统计口径都会被逐步污染。
func (s *Service) requireSource(ctx context.Context, code string) (*model.NewsSource, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return nil, errs.BadRequest("数据源编码不能为空")
	}
	var src model.NewsSource
	err := s.db.WithContext(ctx).Where("code = ?", code).First(&src).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Forbidden(i18n.Tf(ctx, "数据源未配置：{0}，请先在后台「数据源」中创建", code))
		}
		return nil, err
	}
	if src.Status != model.StatusEnabled {
		return nil, errs.Forbidden(i18n.Tf(ctx, "数据源已停用：{0}", src.Code))
	}
	return &src, nil
}

// enabledCategories 返回启用中的分类编码集合，用于逐条校验。
func (s *Service) enabledCategories(ctx context.Context) (map[string]struct{}, error) {
	var codes []string
	if err := s.db.WithContext(ctx).Model(&model.NewsCategory{}).
		Where("status = ?", model.StatusEnabled).Pluck("code", &codes).Error; err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		set[c] = struct{}{}
	}
	return set, nil
}

func normalizeItem(src *model.NewsSource, it IngestItem, now time.Time) (*model.NewsArticle, string) {
	title := strings.TrimSpace(it.Title)
	if title == "" {
		return nil, "标题不能为空"
	}
	if len([]rune(title)) > 300 {
		title = string([]rune(title)[:300])
	}
	if len(it.Content) > MaxContent {
		return nil, "正文超过 200KB"
	}
	content := strings.TrimSpace(it.Content)
	summary := strings.TrimSpace(it.Summary)
	if summary == "" && content != "" {
		summary = firstRunes(content, 120)
	}
	hash := contentHash(title, content)

	ext := strings.TrimSpace(it.ExternalID)
	if ext == "" {
		// 没有外部 ID 时退回内容指纹，保证同一源内重复推送不会重复入库
		ext = "h:" + hash[:32]
	}

	category := strings.ToLower(strings.TrimSpace(it.Category))
	if category == "" {
		// 分类是白名单配置的一部分，不做默认兜底：宁可拒收，
		// 也不要让「没传分类」悄悄堆成一个巨大的 general 桶。
		return nil, "分类不能为空"
	}

	published := now
	if it.PublishedAt != nil && !it.PublishedAt.IsZero() {
		// 统一转 UTC：SQLite 以字符串比较时间，混用偏移会让排序错乱
		published = it.PublishedAt.UTC()
	}
	if published.After(now.Add(24 * time.Hour)) {
		// 允许少量时钟漂移，但明显来自未来的时间视为脏数据
		published = now
	}

	words := len([]rune(content))
	if words == 0 {
		words = len([]rune(summary))
	}

	return &model.NewsArticle{
		SourceID:    src.ID,
		SourceCode:  src.Code,
		SourceName:  src.Name,
		ExternalID:  ext,
		Title:       title,
		Summary:     summary,
		Content:     content,
		URL:         strings.TrimSpace(it.URL),
		Author:      strings.TrimSpace(it.Author),
		Category:    category,
		Tags:        cleanTags(it.Tags),
		CoverURL:    strings.TrimSpace(it.CoverURL),
		ContentHash: hash,
		WordCount:   words,
		PublishedAt: published,
		IngestedAt:  now,
		Status:      model.StatusEnabled,
	}, ""
}

// ---- 查询 ----

// ListOptions 管理端列表筛选。
type ListOptions struct {
	Category string
	SourceID string
	Status   *int8
	From     *time.Time
	To       *time.Time
}

// List 管理端分页。不返回正文，避免单页响应过大。
func (s *Service) List(ctx context.Context, p *query.Params, opt ListOptions) ([]*model.NewsArticle, int64, error) {
	db := p.Filter(s.db.WithContext(ctx).Model(&model.NewsArticle{}), map[string]string{
		"category": "category", "source_id": "source_id", "status": "status",
		"published_at": "published_at",
	})
	db = query.Search(db, p.Keyword, "title", "summary", "author")
	db = s.applyWindow(db, opt)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.NewsArticle
	err := db.Omit("content").
		Order(p.Order(map[string]string{
			"published_at": "published_at", "created_at": "published_at",
		}, "published_at DESC, id DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// CursorQuery 开放接口的游标查询。
// 用「发布时间 + ID」做键集分页，避免深翻页时 offset 越来越慢。
type CursorQuery struct {
	Cursor   string
	Limit    int
	Category string
	Source   string
	Keyword  string
	From     *time.Time
	To       *time.Time
}

// CursorPage 游标分页结果。
type CursorPage struct {
	List       []*model.NewsArticle `json:"list"`
	NextCursor string               `json:"nextCursor,omitempty"`
	HasMore    bool                 `json:"hasMore"`
}

// CursorList 客户端读接口。只返回已发布条目与列表字段。
func (s *Service) CursorList(ctx context.Context, q CursorQuery) (*CursorPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	db := s.db.WithContext(ctx).Model(&model.NewsArticle{}).Where("status = ?", model.StatusEnabled)
	if q.Category != "" {
		db = db.Where("category = ?", strings.ToLower(q.Category))
	}
	if q.Source != "" {
		db = db.Where("source_code = ?", strings.ToLower(q.Source))
	}
	if q.Keyword != "" {
		kw := "%" + escapeLike(strings.TrimSpace(q.Keyword)) + "%"
		db = db.Where("(title LIKE ? ESCAPE '!' OR summary LIKE ? ESCAPE '!')", kw, kw)
	}
	if q.From != nil {
		db = db.Where("published_at >= ?", *q.From)
	}
	if q.To != nil {
		db = db.Where("published_at <= ?", *q.To)
	}
	// 强制保留窗口，即使清理任务延迟也不会读出过期数据
	db = db.Where("published_at >= ?", time.Now().UTC().Add(-s.retention))
	if q.Cursor != "" {
		at, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return nil, errs.BadRequest("游标无效")
		}
		db = db.Where("(published_at < ? OR (published_at = ? AND id < ?))", at, at, id)
	}

	var list []*model.NewsArticle
	if err := db.Omit("content").Order("published_at DESC, id DESC").
		Limit(limit + 1).Find(&list).Error; err != nil {
		return nil, err
	}
	page := &CursorPage{HasMore: len(list) > limit}
	if page.HasMore {
		list = list[:limit]
	}
	page.List = list
	if len(list) == 0 {
		page.List = []*model.NewsArticle{}
	}
	if page.HasMore {
		last := list[len(list)-1]
		page.NextCursor = encodeCursor(last.PublishedAt, last.ID)
	}
	return page, nil
}

// Get 详情。view 为 true 时累加阅读量（异步，不阻塞响应）。
func (s *Service) Get(ctx context.Context, id string, view bool) (*model.NewsArticle, error) {
	var a model.NewsArticle
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.NotFound("资讯不存在或已过期")
		}
		return nil, err
	}
	if view {
		go func() {
			_ = s.db.Model(&model.NewsArticle{}).Where("id = ?", id).
				UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
		}()
	}
	return &a, nil
}

// Delete 删除单条资讯。
func (s *Service) Delete(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.NewsArticle{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("资讯不存在")
	}
	return nil
}

// Stats 概览统计。窗口固定为保留期，用于管理端看板。
type Stats struct {
	Total      int64       `json:"total"`
	Today      int64       `json:"today"`
	Last24h    int64       `json:"last24h"`
	Categories []CountItem `json:"categories"`
	Sources    []CountItem `json:"sources"`
	Oldest     *time.Time  `json:"oldest"`
}

// CountItem 分组计数。
type CountItem struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// Stats 返回概览数据。
func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	out := &Stats{}
	db := s.db.WithContext(ctx).Model(&model.NewsArticle{})
	if err := db.Count(&out.Total).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
	s.db.WithContext(ctx).Model(&model.NewsArticle{}).Where("published_at >= ?", dayStart).Count(&out.Today)
	s.db.WithContext(ctx).Model(&model.NewsArticle{}).Where("published_at >= ?", now.UTC().Add(-24*time.Hour)).Count(&out.Last24h)

	s.groupCount(ctx, "category", &out.Categories)
	// 按来源编码而不是名称分组：两个来源重名时会并成一条，统计就对不上。
	s.groupCount(ctx, "source_code", &out.Sources)

	var oldest model.NewsArticle
	if err := s.db.WithContext(ctx).Order("published_at ASC").Limit(1).Find(&oldest).Error; err == nil && oldest.ID != "" {
		out.Oldest = &oldest.PublishedAt
	}
	return out, nil
}

func (s *Service) groupCount(ctx context.Context, col string, out *[]CountItem) {
	var rows []CountItem
	s.db.WithContext(ctx).Model(&model.NewsArticle{}).
		Select(col + " AS name, COUNT(*) AS count").
		Group(col).Order("count DESC").Limit(12).Scan(&rows)
	*out = rows
}

// Meta 开放端元数据：客户端一次拉全，用于渲染筛选面板与来源标识。
type Meta struct {
	Sources       []MetaSource   `json:"sources"`
	Categories    []MetaCategory `json:"categories"`
	RetentionDays int            `json:"retentionDays"`
	Total         int64          `json:"total"`
}

// MetaSource 来源的对外视图。
type MetaSource struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	Sort  int    `json:"sort"`
}

// MetaCategory 分类的对外视图。
type MetaCategory struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	Sort  int    `json:"sort"`
}

// BuildMeta 组装开放端元数据。只下发启用项，顺序由后台排序字段决定。
func (s *Service) BuildMeta(ctx context.Context) (*Meta, error) {
	out := &Meta{
		Sources:       []MetaSource{},
		Categories:    []MetaCategory{},
		RetentionDays: int(s.retention.Hours() / 24),
	}
	var srcs []*model.NewsSource
	if err := s.db.WithContext(ctx).Where("status = ?", model.StatusEnabled).
		Order("sort ASC, created_at ASC").Find(&srcs).Error; err != nil {
		return nil, err
	}
	for _, v := range srcs {
		out.Sources = append(out.Sources, MetaSource{
			Code: v.Code, Name: v.Name, Kind: v.Kind,
			Icon: v.Icon, Color: v.Color, Sort: v.Sort,
		})
	}
	var cats []*model.NewsCategory
	if err := s.db.WithContext(ctx).Where("status = ?", model.StatusEnabled).
		Order("sort ASC, created_at ASC").Find(&cats).Error; err != nil {
		return nil, err
	}
	for _, v := range cats {
		out.Categories = append(out.Categories, MetaCategory{
			Code: v.Code, Name: v.Name, Icon: v.Icon, Color: v.Color, Sort: v.Sort,
		})
	}
	s.db.WithContext(ctx).Model(&model.NewsArticle{}).
		Where("published_at >= ?", time.Now().UTC().Add(-s.retention)).Count(&out.Total)
	return out, nil
}

// Cleanup 删除超过保留窗口的资讯，返回删除条数。
//
// 分批硬删而不是一条 DELETE：大表一次性删除会长时间持有写锁，
// 在 SQLite 上表现为整个服务卡顿，在 MySQL 上会撑大 undo。
func (s *Service) Cleanup(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = int(s.retention / (24 * time.Hour))
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	var total int64
	for round := 0; round < cleanupRounds; round++ {
		var ids []string
		err := s.db.WithContext(ctx).Model(&model.NewsArticle{}).
			Where("published_at < ?", cutoff).Limit(cleanupChunk).Pluck("id", &ids).Error
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			break
		}
		res := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&model.NewsArticle{})
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if len(ids) < cleanupChunk {
			break
		}
	}
	if total > 0 {
		s.log.Info("资讯清理完成", zap.Int64("deleted", total), zap.String("cutoff", cutoff.Format(time.RFC3339)))
	}
	return total, nil
}

// ---- 数据源 ----

// SourceInput 数据源入参。
type SourceInput struct {
	Code     string `json:"code" binding:"required,max=32" label:"编码"`
	Name     string `json:"name" binding:"required,max=64" label:"名称"`
	Kind     string `json:"kind" binding:"omitempty,oneof=crawler api rss" label:"类型"`
	Icon     string `json:"icon" binding:"max=512" label:"图标"`
	Color    string `json:"color" binding:"max=16" label:"颜色"`
	Sort     int    `json:"sort" label:"排序"`
	Homepage string `json:"homepage" binding:"max=255" label:"主页"`
	Status   *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
	Remark   string `json:"remark" binding:"max=255" label:"备注"`
}

// ListSources 数据源列表。
func (s *Service) ListSources(ctx context.Context, p *query.Params) ([]*model.NewsSource, int64, error) {
	db := p.Filter(s.db.WithContext(ctx).Model(&model.NewsSource{}), map[string]string{
		"code": "code", "name": "name", "status": "status", "kind": "kind",
	})
	db = query.Search(db, p.Keyword, "code", "name")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.NewsSource
	err := db.Order("created_at DESC").Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// CreateSource 新增数据源。
func (s *Service) CreateSource(ctx context.Context, in *SourceInput) (*model.NewsSource, error) {
	code := strings.ToLower(strings.TrimSpace(in.Code))
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.NewsSource{}).Where("code = ?", code).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, errs.Conflict("数据源编码已存在")
	}
	src := &model.NewsSource{
		Code: code, Name: strings.TrimSpace(in.Name), Kind: in.Kind,
		Icon: strings.TrimSpace(in.Icon), Color: strings.TrimSpace(in.Color), Sort: in.Sort,
		Homepage: in.Homepage, Remark: in.Remark, Status: statusOr(in.Status, model.StatusEnabled),
	}
	if src.Kind == "" {
		src.Kind = model.NewsSourceCrawler
	}
	if err := s.db.WithContext(ctx).Create(src).Error; err != nil {
		return nil, err
	}
	return src, nil
}

// UpdateSource 编辑数据源。
func (s *Service) UpdateSource(ctx context.Context, id string, in *SourceInput) (*model.NewsSource, error) {
	var src model.NewsSource
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&src).Error; err != nil {
		return nil, errs.NotFound("数据源不存在")
	}
	updates := map[string]any{
		"name": strings.TrimSpace(in.Name), "homepage": in.Homepage, "remark": in.Remark,
		"icon": strings.TrimSpace(in.Icon), "color": strings.TrimSpace(in.Color), "sort": in.Sort,
	}
	if in.Kind != "" {
		updates["kind"] = in.Kind
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	if err := s.db.WithContext(ctx).Model(&model.NewsSource{}).Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.sourceByID(ctx, id)
}

// DeleteSource 删除数据源。仍有资讯引用时先禁止，避免历史数据失去归属。
func (s *Service) DeleteSource(ctx context.Context, id string) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.NewsArticle{}).Where("source_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errs.Conflict(i18n.Tf(ctx, "该数据源下仍有 {0} 条资讯，请先停用或等待过期", n))
	}
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.NewsSource{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("数据源不存在")
	}
	return nil
}

func (s *Service) sourceByID(ctx context.Context, id string) (*model.NewsSource, error) {
	var src model.NewsSource
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&src).Error; err != nil {
		return nil, errs.NotFound("数据源不存在")
	}
	return &src, nil
}

// ---- 分类 ----

// CategoryInput 分类入参。编码创建后不可修改：
// 资讯表里存的是编码字符串，改了会让历史数据失去归属。
type CategoryInput struct {
	Code   string `json:"code" binding:"required,max=32" label:"编码"`
	Name   string `json:"name" binding:"required,max=64" label:"名称"`
	Icon   string `json:"icon" binding:"max=512" label:"图标"`
	Color  string `json:"color" binding:"max=16" label:"颜色"`
	Sort   int    `json:"sort" label:"排序"`
	Status *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
	Remark string `json:"remark" binding:"max=255" label:"备注"`
}

// ListCategories 分类全量列表，附带资讯条数。
// 分类属于低频配置，数量一般在几十条内，分页反而增加前端拼装成本。
func (s *Service) ListCategories(ctx context.Context) ([]*model.NewsCategory, error) {
	var list []*model.NewsCategory
	if err := s.db.WithContext(ctx).Order("sort ASC, created_at ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	var rows []struct {
		Category string
		Count    int64
	}
	if err := s.db.WithContext(ctx).Model(&model.NewsArticle{}).
		Select("category, COUNT(*) AS count").Group("category").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Category] = r.Count
	}
	for _, c := range list {
		c.ArticleCount = counts[c.Code]
	}
	return list, nil
}

// CreateCategory 新增分类。
func (s *Service) CreateCategory(ctx context.Context, in *CategoryInput) (*model.NewsCategory, error) {
	code := strings.ToLower(strings.TrimSpace(in.Code))
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.NewsCategory{}).
		Where("code = ?", code).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, errs.Conflict("分类编码已存在")
	}
	cat := &model.NewsCategory{
		Code: code, Name: strings.TrimSpace(in.Name), Icon: strings.TrimSpace(in.Icon),
		Color: strings.TrimSpace(in.Color), Sort: in.Sort, Remark: in.Remark,
		Status: statusOr(in.Status, model.StatusEnabled),
	}
	if err := s.db.WithContext(ctx).Create(cat).Error; err != nil {
		return nil, err
	}
	return cat, nil
}

// UpdateCategory 编辑分类。编码不可改，只更新展示与状态。
func (s *Service) UpdateCategory(ctx context.Context, id string, in *CategoryInput) (*model.NewsCategory, error) {
	var cat model.NewsCategory
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&cat).Error; err != nil {
		return nil, errs.NotFound("分类不存在")
	}
	updates := map[string]any{
		"name": strings.TrimSpace(in.Name), "icon": strings.TrimSpace(in.Icon),
		"color": strings.TrimSpace(in.Color), "sort": in.Sort, "remark": in.Remark,
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	if err := s.db.WithContext(ctx).Model(&model.NewsCategory{}).Where("id = ?", id).
		Updates(updates).Error; err != nil {
		return nil, err
	}
	var out model.NewsCategory
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCategory 删除分类。仍有资讯引用时禁止，
// 否则这些资讯会变成筛选项里的孤儿。
func (s *Service) DeleteCategory(ctx context.Context, id string) error {
	var cat model.NewsCategory
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&cat).Error; err != nil {
		return errs.NotFound("分类不存在")
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.NewsArticle{}).
		Where("category = ?", cat.Code).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errs.Conflict(i18n.Tf(ctx, "该分类下仍有 {0} 条资讯，请先停用或等待过期", n))
	}
	res := s.db.WithContext(ctx).Where("id = ?", id).Delete(&model.NewsCategory{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("分类不存在")
	}
	return nil
}

// ---- 内部 ----

func (s *Service) applyWindow(db *gorm.DB, opt ListOptions) *gorm.DB {
	if opt.Category != "" {
		db = db.Where("category = ?", strings.ToLower(opt.Category))
	}
	if opt.SourceID != "" {
		db = db.Where("source_id = ?", opt.SourceID)
	}
	if opt.Status != nil {
		db = db.Where("status = ?", *opt.Status)
	}
	if opt.From != nil {
		db = db.Where("published_at >= ?", *opt.From)
	}
	if opt.To != nil {
		db = db.Where("published_at <= ?", *opt.To)
	}
	return db
}

func contentHash(title, content string) string {
	sum := sha256.Sum256([]byte(title + "\n" + content))
	return hex.EncodeToString(sum[:])
}

func cleanTags(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || len([]rune(t)) > 32 {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) >= 12 {
			break
		}
	}
	sort.Strings(out)
	return out
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func escapeLike(s string) string {
	// 与 query 包一致，用 ! 作为转义符，避免用户输入 % 时全表扫描
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}

func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "|" + id))
}

func decodeCursor(s string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", errors.New("游标格式错误")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", err
	}
	// time.Unix 返回本地时区，必须转 UTC：SQLite 按字符串比较时间，
	// 时区偏移不同会让同一时刻被排成两个位置。
	return time.Unix(0, ns).UTC(), parts[1], nil
}

func statusOr(v *int8, def int8) int8 {
	if v == nil {
		return def
	}
	return *v
}
