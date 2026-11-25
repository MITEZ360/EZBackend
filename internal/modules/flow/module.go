package flow

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/modules/message"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:    "github.com/MITEZ360/tenon/internal/modules/flow",
		Name:   "flow",
		Label:  "工作流",
		Desc:   "轻量审批流：单线、或签、会签、条件跳过、加减签、回退与委托；关闭后相关页面与接口不可用",
		// 超时提醒依赖消息模块投递站内信；未启用时降级为日志留痕。
		DependsOn: []string{"message"},
		Models: []any{
			&model.Flow{}, &model.FlowInstance{}, &model.FlowTask{},
			&model.FlowRecord{}, &model.FlowDelegate{},
		},
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_flow_design", Parent: "m_system", Name: "流程设计", Type: model.MenuPage,
				Path: "/workflow/design", Icon: "cluster", Perm: "flow:def:list", Sort: 30},
			{ID: "m_flow_def_create", Parent: "m_flow_design", Name: "新建流程", Type: model.MenuButton, Perm: "flow:def:create"},
			{ID: "m_flow_def_edit", Parent: "m_flow_design", Name: "编辑流程", Type: model.MenuButton, Perm: "flow:def:update"},
			{ID: "m_flow_instances", Parent: "m_system", Name: "我发起的", Type: model.MenuPage,
				Path: "/workflow/instances", Icon: "file-text", Perm: "flow:instance:list", Sort: 31},
			{ID: "m_flow_todo", Parent: "m_system", Name: "待办审批", Type: model.MenuPage,
				Path: "/workflow/tasks", Icon: "bell", Perm: "flow:task:list", Sort: 32},
			{ID: "m_flow_delegates", Parent: "m_system", Name: "审批委托", Type: model.MenuPage,
				Path: "/workflow/delegates", Icon: "route", Perm: "flow:delegate:list", Sort: 33},
		},
		Jobs: []modulekit.Job{
			{Name: "流程超时扫描", Cron: "0 */5 * * * *", Handler: "flow.timeout", Params: "{}",
				Remark: "每 5 分钟扫描超时待办，按节点配置提醒或自动处理"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Log)
	h := NewHandler(svc)

	// 超时提醒走站内信：把 {title,content,userId} 交给 message 模块的发布契约；
	// 没有该模块时只留轨迹，不影响主流程。
	svc.Engine().Notify = func(ctx context.Context, instanceID, assigneeID, nodeName string) {
		c.Log.Info("流程节点超时",
			zap.String("instance", instanceID),
			zap.String("assignee", assigneeID),
			zap.String("node", nodeName))
		// 超时提醒走站内信：未启用 message 模块时只留日志。
		if m, ok := c.Lookup("message").(*message.Service); ok {
			_, _ = m.Send(ctx, &message.SendInput{
				Title:   "审批超时提醒",
				Content: fmt.Sprintf("节点「%s」的审批已超时（实例 %s），请及时处理。", nodeName, instanceID),
				Type:    "warning", Audience: "user", UserIDs: []string{assigneeID},
				Link: "/workflow/tasks",
			}, "system", "系统")
		}
	}

	c.Job("flow.timeout", func(ctx context.Context, _ map[string]any) (string, error) {
		n, err := svc.Engine().ScanTimeout(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("扫描完成，处理 %d 条", n), nil
	})

	c.Authed(func(g *gin.RouterGroup) {
		// 流程定义
		g.GET("/flows", middleware.RequirePerm("flow:def:list"), h.ListFlows)
		g.GET("/flows/:id", middleware.RequirePerm("flow:def:list"), h.GetFlow)
		g.POST("/flows", middleware.RequirePerm("flow:def:create"), h.CreateFlow)
		g.PUT("/flows/:id", middleware.RequirePerm("flow:def:update"), h.UpdateFlow)
		g.PUT("/flows/:id/status", middleware.RequirePerm("flow:def:update"), h.SetFlowStatus)
		g.DELETE("/flows/:id", middleware.RequirePerm("flow:def:delete"), h.DeleteFlow)

		// 流程实例
		g.POST("/flow-instances", middleware.RequirePerm("flow:instance:create"), h.Start)
		g.GET("/flow-instances", middleware.RequirePerm("flow:instance:list"), h.ListInstances)
		g.GET("/flow-instances/:id", middleware.RequirePerm("flow:instance:list"), h.InstanceDetail)
		g.POST("/flow-instances/:id/cancel", middleware.RequirePerm("flow:instance:list"), h.CancelInstance)
		g.POST("/flow-instances/:id/rollback", middleware.RequirePerm("flow:instance:list"), h.Rollback)

		// 任务
		g.GET("/flow-tasks", middleware.RequirePerm("flow:task:list"), h.ListTasks)
		g.GET("/flow-tasks/:id", middleware.RequirePerm("flow:task:list"), h.GetTask)
		g.POST("/flow-tasks/:id/approve", middleware.RequirePerm("flow:task:approve"), h.Approve)
		g.POST("/flow-tasks/:id/reject", middleware.RequirePerm("flow:task:approve"), h.Reject)
		g.POST("/flow-tasks/:id/transfer", middleware.RequirePerm("flow:task:approve"), h.Transfer)
		g.POST("/flow-tasks/:id/addsign", middleware.RequirePerm("flow:task:approve"), h.AddSign)
		g.POST("/flow-tasks/:id/removesign", middleware.RequirePerm("flow:task:approve"), h.RemoveSign)
		g.POST("/flow-tasks/timeout-scan", middleware.RequirePerm("flow:task:approve"), h.TimeoutScan)

		// 委托
		g.GET("/flow-delegates", middleware.RequirePerm("flow:delegate:list"), h.ListDelegates)
		g.POST("/flow-delegates", middleware.RequirePerm("flow:delegate:create"), h.CreateDelegate)
		g.PUT("/flow-delegates/:id", middleware.RequirePerm("flow:delegate:update"), h.UpdateDelegate)
		g.DELETE("/flow-delegates/:id", middleware.RequirePerm("flow:delegate:delete"), h.DeleteDelegate)
	})
	return nil
}
