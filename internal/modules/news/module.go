package news

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:       "github.com/keel-framework/keel/internal/modules/news",
		Name:      "news",
		Label:     "资讯聚合",
		Desc:      "演示业务：资讯源、分类、文章抓取与清理",
		DependsOn: []string{"apikey", "job"},
		Models:    []any{&model.NewsSource{}, &model.NewsCategory{}, &model.NewsArticle{}},
		Menus: []modulekit.Menu{
			// 资讯中心是模块私有目录：模块被裁掉后目录与页面一起消失。
			{ID: "m_content", Name: "资讯中心", Type: model.MenuDir, Path: "/content", Icon: "news", Sort: 5},
			{ID: "m_news", Parent: "m_content", Name: "资讯列表", Type: model.MenuPage, Path: "/content/news",
				Icon: "file-text", Perm: "content:news:list", Sort: 1},
			{ID: "m_news_view", Parent: "m_news", Name: "查看资讯", Type: model.MenuButton, Perm: "content:news:view"},
			{ID: "m_news_del", Parent: "m_news", Name: "删除资讯", Type: model.MenuButton, Perm: "content:news:delete"},
			{ID: "m_news_cleanup", Parent: "m_news", Name: "手动清理", Type: model.MenuButton, Perm: "content:news:cleanup"},
			{ID: "m_news_source", Parent: "m_content", Name: "数据源", Type: model.MenuPage, Path: "/content/source",
				Icon: "server", Perm: "content:news:list", Sort: 2},
			{ID: "m_news_source_edit", Parent: "m_news_source", Name: "维护数据源", Type: model.MenuButton, Perm: "content:news:source"},
			{ID: "m_news_category", Parent: "m_content", Name: "资讯分类", Type: model.MenuPage, Path: "/content/category",
				Icon: "grid", Perm: "content:news:list", Sort: 3},
			{ID: "m_news_category_edit", Parent: "m_news_category", Name: "维护分类", Type: model.MenuButton, Perm: "content:news:category"},
		},
		Jobs: []modulekit.Job{
			{Name: "清理过期资讯", Cron: "15 * * * *", Handler: "news.cleanup", Params: `{}`,
				Remark: "每小时清理超过 news.retention_days 的资讯；任务参数可单独覆盖天数"},
		},
		Setup: setup,
		Sync:  Sync,
	})
}

// setup 资讯中心管理端接口。
//
// 数据源单独挂在 /news-sources，避免与 /news/:id 的路径参数混在一起。
// 开放接口走 API 密钥认证，这里只声明路径，认证中间件由框架统一挂载。
func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Cache, c.Log, c.Config.News.RetentionDays)

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/news/stats", middleware.RequirePerm("content:news:list"), h.Stats)
		g.GET("/news", middleware.RequirePerm("content:news:list"), h.List)
		g.GET("/news/:id", middleware.RequirePerm("content:news:view"), h.Detail)
		g.DELETE("/news/:id", middleware.RequirePerm("content:news:delete"), h.Delete)
		g.POST("/news/cleanup", middleware.RequirePerm("content:news:cleanup"), h.Cleanup)

		g.GET("/news-sources", middleware.RequirePerm("content:news:list"), h.Sources)
		g.POST("/news-sources", middleware.RequirePerm("content:news:source"), h.CreateSource)
		g.PUT("/news-sources/:id", middleware.RequirePerm("content:news:source"), h.UpdateSource)
		g.DELETE("/news-sources/:id", middleware.RequirePerm("content:news:delete"), h.DeleteSource)

		g.GET("/news-categories", middleware.RequirePerm("content:news:list"), h.Categories)
		g.POST("/news-categories", middleware.RequirePerm("content:news:category"), h.CreateCategory)
		g.PUT("/news-categories/:id", middleware.RequirePerm("content:news:category"), h.UpdateCategory)
		g.DELETE("/news-categories/:id", middleware.RequirePerm("content:news:delete"), h.DeleteCategory)
	})

	open := NewOpenHandler(svc)
	c.Open(func(g *gin.RouterGroup) {
		g.POST("/news/batch", middleware.RequirePerm("news:ingest"), open.Ingest)
		g.GET("/news/meta", middleware.RequirePerm("news:read"), open.Meta)
		g.GET("/news", middleware.RequirePerm("news:read"), open.List)
		g.GET("/news/:id", middleware.RequirePerm("news:read"), open.Detail)
	})

	// 资讯是滚动窗口数据，清理任务与业务同生命周期，不依赖外部调度。
	c.Job("news.cleanup", func(ctx context.Context, p map[string]any) (string, error) {
		days := 0
		if v, ok := p["days"].(float64); ok {
			days = int(v)
		}
		n, err := svc.Cleanup(ctx, days)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已清理 %d 条过期资讯", n), nil
	})
	return nil
}
