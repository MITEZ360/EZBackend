package news

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/pkg/query"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "news.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.NewsSource{}, &model.NewsArticle{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	c, err := cache.New(config.Redis{}, zap.NewNop())
	if err != nil {
		t.Fatalf("创建缓存失败: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return NewService(db, c, zap.NewNop(), 7), db
}

func item(title string, published time.Time) IngestItem {
	return IngestItem{
		Title:       title,
		Summary:     "摘要：" + title,
		Content:     "正文内容：" + title,
		URL:         "https://example.com/" + title,
		Category:    "tech",
		PublishedAt: &published,
	}
}

func TestIngestDedup(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	now := time.Now()

	// 同批内重复 + 重复推送
	it := item("AI 芯片发布", now)
	req := &IngestRequest{Source: "tech-daily", Items: []IngestItem{it, it, it}}
	res, err := svc.Ingest(ctx, req)
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.Accepted != 1 || res.Duplicate != 2 {
		t.Fatalf("同批去重不正确: accepted=%d duplicate=%d", res.Accepted, res.Duplicate)
	}
	if !res.SourceCreated {
		t.Fatal("首次推送应自动注册数据源")
	}

	res, err = svc.Ingest(ctx, &IngestRequest{Source: "tech-daily", Items: []IngestItem{it}})
	if err != nil {
		t.Fatalf("重复导入失败: %v", err)
	}
	if res.Accepted != 0 || res.Duplicate != 1 || res.SourceCreated {
		t.Fatalf("重复推送应全部跳过: %+v", res)
	}

	var n int64
	db.Model(&model.NewsArticle{}).Count(&n)
	if n != 1 {
		t.Fatalf("库中应只有 1 条，实际 %d", n)
	}

	var src model.NewsSource
	db.Where("code = ?", "tech-daily").First(&src)
	if src.TotalIngested != 1 {
		t.Fatalf("累计导入数不正确: %d", src.TotalIngested)
	}
}

func TestIngestValidation(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	now := time.Now()
	old := now.AddDate(0, 0, -10)

	res, err := svc.Ingest(ctx, &IngestRequest{Source: "src", Items: []IngestItem{
		{Title: "  ", PublishedAt: &now}, // 空标题
		item("过期新闻", old),                // 早于保留窗口
		item("正常新闻", now),                // 正常
		{Title: "无时间", Content: "内容"},    // 时间缺失按当前时间
	}})
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if res.Invalid != 1 || res.Expired != 1 || res.Accepted != 2 {
		t.Fatalf("分类计数不正确: %+v", res)
	}
	if len(res.Errors) != 1 || res.Errors[0].Index != 0 {
		t.Fatalf("错误明细不正确: %+v", res.Errors)
	}

	var n int64
	db.Model(&model.NewsArticle{}).Count(&n)
	if n != 2 {
		t.Fatalf("应入库 2 条，实际 %d", n)
	}
}

func TestContentHashDedup(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	now := time.Now()

	a := item("同一篇新闻", now)
	a.ExternalID = ""
	b := item("同一篇新闻", now)
	b.ExternalID = ""

	res, _ := svc.Ingest(ctx, &IngestRequest{Source: "s1", Items: []IngestItem{a, b}})
	if res.Accepted != 1 || res.Duplicate != 1 {
		t.Fatalf("无外部 ID 时应按内容指纹去重: %+v", res)
	}
	// 不同来源的同一篇允许共存
	res, _ = svc.Ingest(ctx, &IngestRequest{Source: "s2", Items: []IngestItem{a}})
	if res.Accepted != 1 {
		t.Fatalf("跨来源不应互相去重: %+v", res)
	}
}

func TestCursorPagination(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	items := make([]IngestItem, 0, 25)
	for i := 0; i < 25; i++ {
		it := item(fmt.Sprintf("新闻-%02d", i), base.Add(time.Duration(i)*time.Minute))
		it.ExternalID = fmt.Sprintf("e%d", i)
		items = append(items, it)
	}
	if _, err := svc.Ingest(ctx, &IngestRequest{Source: "s", Items: items}); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page, err := svc.CursorList(ctx, CursorQuery{Cursor: cursor, Limit: 10})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		for _, a := range page.List {
			if seen[a.ID] {
				t.Fatalf("游标翻页出现重复: %s", a.ID)
			}
			seen[a.ID] = true
		}
		pages++
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
		if pages > 5 {
			t.Fatal("翻页未终止")
		}
	}
	if len(seen) != 25 || pages != 3 {
		t.Fatalf("应取到 25 条 / 3 页，实际 %d 条 / %d 页", len(seen), pages)
	}

	// 第一页应是最新的三条
	first, _ := svc.CursorList(ctx, CursorQuery{Limit: 3})
	if first.List[0].Title != "新闻-24" {
		t.Fatalf("排序不正确，首条为 %s", first.List[0].Title)
	}

	if _, err := svc.CursorList(ctx, CursorQuery{Cursor: "not-a-cursor"}); err == nil {
		t.Fatal("非法游标应报错")
	}
}

func TestSearchAndFilter(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	now := time.Now()

	a := item("量子计算突破", now)
	a.Category = "science"
	a.Tags = []string{"量子", "计算"}
	b := item("足球联赛开幕", now)
	b.Category = "sports"
	if _, err := svc.Ingest(ctx, &IngestRequest{Source: "s", Items: []IngestItem{a, b}}); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	res, err := svc.Ingest(ctx, &IngestRequest{Source: "s2", Items: []IngestItem{item("50% 增长", now)}})
	if err != nil || res.Accepted != 1 {
		t.Fatalf("含百分号的标题应能正常入库: %v %+v", err, res)
	}

	page, _ := svc.CursorList(ctx, CursorQuery{Keyword: "量子"})
	if len(page.List) != 1 || page.List[0].Title != "量子计算突破" {
		t.Fatalf("关键词检索不正确: %+v", page.List)
	}
	page, _ = svc.CursorList(ctx, CursorQuery{Keyword: "50%"})
	if len(page.List) != 1 {
		t.Fatalf("百分号应被转义为字面量，实际 %d 条", len(page.List))
	}
	page, _ = svc.CursorList(ctx, CursorQuery{Category: "sports"})
	if len(page.List) != 1 || page.List[0].Title != "足球联赛开幕" {
		t.Fatalf("分类筛选不正确: %+v", page.List)
	}

	// 列表不返回正文
	if page.List[0].Content != "" {
		t.Fatal("列表不应返回正文")
	}
}

func TestCleanup(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	now := time.Now()

	if _, err := svc.Ingest(ctx, &IngestRequest{Source: "s", Items: []IngestItem{
		item("新", now), item("六天前", now.AddDate(0, 0, -6)),
	}}); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	// 直接插一条 8 天前的数据（导入层会拒绝过期条目）
	old := item("八天前", now.AddDate(0, 0, -8))
	src := &model.NewsSource{Code: "s"}
	src.ID = "s1"
	rec, _ := normalizeItem(src, old, now)
	rec.PublishedAt = now.AddDate(0, 0, -8)
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("插入历史数据失败: %v", err)
	}

	n, err := svc.Cleanup(ctx, 7)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应清理 1 条，实际 %d", n)
	}
	var left int64
	db.Model(&model.NewsArticle{}).Count(&left)
	if left != 2 {
		t.Fatalf("应剩 2 条，实际 %d", left)
	}
}

func TestSourceLifecycle(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	src, err := svc.CreateSource(ctx, &SourceInput{Code: "Tech", Name: "科技日报", Kind: "rss"})
	if err != nil {
		t.Fatalf("新建数据源失败: %v", err)
	}
	if src.Code != "tech" {
		t.Fatalf("编码应统一小写: %s", src.Code)
	}
	if _, err := svc.CreateSource(ctx, &SourceInput{Code: "tech", Name: "重复"}); err == nil {
		t.Fatal("重复编码应报错")
	}

	if _, err := svc.Ingest(ctx, &IngestRequest{Source: "tech", Items: []IngestItem{item("一条", time.Now())}}); err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	if err := svc.DeleteSource(ctx, src.ID); err == nil {
		t.Fatal("仍有资讯的数据源不应被删除")
	}

	// 停用后拒绝写入
	off := model.StatusDisabled
	if _, err := svc.UpdateSource(ctx, src.ID, &SourceInput{Name: "科技日报", Status: &off}); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if _, err := svc.Ingest(ctx, &IngestRequest{Source: "tech", Items: []IngestItem{item("两条", time.Now())}}); err == nil {
		t.Fatal("停用的数据源应拒绝写入")
	}
}

func TestListPaging(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	now := time.Now()
	items := make([]IngestItem, 0, 15)
	for i := 0; i < 15; i++ {
		it := item(fmt.Sprintf("列表-%02d", i), now.Add(time.Duration(i)*time.Second))
		it.ExternalID = fmt.Sprintf("l%d", i)
		items = append(items, it)
	}
	if _, err := svc.Ingest(ctx, &IngestRequest{Source: "s", Items: items}); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	p := &query.Params{Page: 2, PageSize: 10, Filters: map[string]string{}}
	list, total, err := svc.List(ctx, p, ListOptions{})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 15 || len(list) != 5 {
		t.Fatalf("分页不正确: total=%d len=%d", total, len(list))
	}

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.Total != 15 || stats.Today != 15 {
		t.Fatalf("统计不正确: %+v", stats)
	}
	if len(stats.Categories) == 0 || stats.Categories[0].Name != "tech" {
		t.Fatalf("分类分布不正确: %+v", stats.Categories)
	}
}
