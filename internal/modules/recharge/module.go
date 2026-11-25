package recharge

import (
	"context"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/modules/membership"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:       "github.com/MITEZ360/tenon/internal/modules/recharge",
		Name:      "recharge",
		Label:     "会员充值",
		Desc:      "会员等级售价、充值下单、微信/支付宝支付回调与订单管理",
		DependsOn: []string{"membership"},
		Models:    []any{&model.RechargeOrder{}, &model.PaymentConfig{}},
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_recharge", Parent: "m_system", Name: "充值订单", Type: model.MenuPage, Path: "/recharge",
				Icon: "wallet", Perm: "system:recharge:list", Sort: 11},
			{ID: "m_recharge_pay_config", Parent: "m_recharge", Name: "支付配置", Type: model.MenuButton,
				Perm: "system:recharge:config"},
		},
		Setup: setup,
		Sync:  syncDefaultPrices,
	})
}

// setup 会员充值的公开回调与登录后接口。
//
// 支付回调必须公开（网关直接 POST），验签在 handler 内部完成；
// 查询与下单只需登录，订单管理与支付配置走独立权限点。
func setup(c *modulekit.Context) error {
	memSvc, err := modulekit.Need[*membership.Service](c, "membership")
	if err != nil {
		return err
	}
	svc := NewService(c.DB, c.Config, c.Cache, memSvc, c.Log)
	h := NewHandler(svc, c.Config.Server.TrustProxy)

	c.Public(func(g *gin.RouterGroup) {
		g.POST("/pay/notify/wechat", h.WechatNotify)
		g.POST("/pay/notify/alipay", h.AlipayNotify)
	})

	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/recharge/channels", h.Available)
		g.POST("/recharge/orders", h.Create)
		g.GET("/recharge/orders", h.List)
		g.GET("/recharge/orders/:orderNo", h.Query)
		g.POST("/recharge/orders/:orderNo/close", h.Close)

		g.GET("/recharge/admin/orders", middleware.RequirePerm("system:recharge:list"), h.AdminList)
		g.GET("/recharge/admin/payment-config", middleware.RequirePerm("system:recharge:config"), h.PaymentConfig)
		g.PUT("/recharge/admin/payment-config", middleware.RequirePerm("system:recharge:config"), h.SavePaymentConfig)
	})
	return nil
}

// syncDefaultPrices 给内置会员等级补一个示例售价。
//
// 只在价格为 0 时写入，管理员改过的价格不会被覆盖；默认发行版没有
// recharge 模块，Sync 不执行，等级价格保持 0，会员页面不会出现买不了的价签。
func syncDefaultPrices(ctx context.Context, db *gorm.DB) error {
	prices := map[string]int64{"silver": 9900, "gold": 29900, "platinum": 99900}
	for code, price := range prices {
		if err := db.WithContext(ctx).Model(&model.MembershipLevel{}).
			Where("code = ? AND price = 0", code).Update("price", price).Error; err != nil {
			return err
		}
	}
	return nil
}
