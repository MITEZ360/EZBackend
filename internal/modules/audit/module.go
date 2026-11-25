package audit

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
		Pkg:    "github.com/keel-framework/keel/internal/modules/audit",
		Name:   "audit",
		Label:  "审计日志",
		Desc:   "操作日志与登录日志查询",
		Models: []any{&model.OperationLog{}},
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_loginlog", Parent: "m_ops", Name: "登录日志", Type: model.MenuPage, Path: "/ops/login-log",
				Icon: "login", Perm: "ops:loginlog:list", Sort: 3},
			{ID: "m_oplog", Parent: "m_ops", Name: "操作日志", Type: model.MenuPage, Path: "/ops/operation-log",
				Icon: "file-text", Perm: "ops:oplog:list", Sort: 4},
			// 在线用户的实际接口在审计模块（/online/sessions），菜单跟着路由走，
			// 不按"会话属于认证"来归类，否则关掉审计会留下死菜单。
			{ID: "m_online", Parent: "m_ops", Name: "在线用户", Type: model.MenuPage, Path: "/ops/online",
				Icon: "wifi", Perm: "ops:online:list", Sort: 5},
		},
		Jobs: []modulekit.Job{
			{Name: "清理过期日志", Cron: "0 3 * * *", Handler: "log.cleanup", Params: `{"days":30}`,
				Remark: "每天 03:00 清理 30 天前的登录日志与操作日志"},
			{Name: "清理过期会话", Cron: "0 * * * *", Handler: "session.cleanup", Params: `{"days":7}`,
				Remark: "每小时回收已过期的登录会话"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Cache, c.Log)
	// 审计中间件通过这个实例异步落库。
	c.Auditor = svc

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/logs/login", middleware.RequirePerm("ops:loginlog:list"), h.LoginLogs)
		g.DELETE("/logs/login", middleware.RequirePerm("ops:loginlog:list"), h.ClearLoginLogs)
		g.GET("/logs/operation", middleware.RequirePerm("ops:oplog:list"), h.OperationLogs)
		g.GET("/logs/operation/:id", middleware.RequirePerm("ops:oplog:list"), h.OperationLogDetail)
		g.DELETE("/logs/operation", middleware.RequirePerm("ops:oplog:list"), h.ClearOperationLogs)
		g.GET("/online/sessions", middleware.RequirePerm("ops:online:list"), h.Online)
		g.DELETE("/online/sessions/:id", middleware.RequirePerm("ops:online:list"), h.Kick)
	})

	// 清理任务的实际实现；声明在 Spec.Jobs，两边按 handler 名对应。
	c.Job("log.cleanup", func(ctx context.Context, p map[string]any) (string, error) {
		days := intParam(p, "days", 30)
		n1, err := svc.ClearLoginLogs(ctx, days)
		if err != nil {
			return "", err
		}
		n2, err := svc.ClearOperationLogs(ctx, days)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("清理登录日志 %d 条、操作日志 %d 条（保留 %d 天）", n1, n2, days), nil
	})
	c.Job("session.cleanup", func(ctx context.Context, p map[string]any) (string, error) {
		n, err := svc.CleanupSessions(ctx, intParam(p, "days", 7))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("清理过期会话 %d 条", n), nil
	})
	return nil
}

// intParam 从任务参数里取整数，兼容 JSON 解析出的 float64 与手工传入的字符串。
func intParam(p map[string]any, key string, def int) int {
	switch v := p[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}
