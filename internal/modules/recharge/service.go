// Package recharge 会员充值：下单、支付回调、订单查询与会员发放。
// 金额统一以「分」存储；回调通过「分布式锁 + 条件更新 + 事务发放」保证幂等。
package recharge

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/modules/membership"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/idgen"
	"github.com/MITEZ360/tenon/internal/platform/pay"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/i18n"
	"github.com/MITEZ360/tenon/pkg/query"
)

// Service 充值业务。
type Service struct {
	db         *gorm.DB
	cfg        *config.Config
	payCfg     *PaymentConfigStore
	cache      cache.Cache
	log        *zap.Logger
	membership *membership.Service
	// newGateway 可替换，便于单测注入 mock 网关；生产走 pay.New。
	newGateway func(payType string) (pay.Gateway, error)
}

// NewService 构造服务。
func NewService(db *gorm.DB, cfg *config.Config, c cache.Cache, memSvc *membership.Service, log *zap.Logger) *Service {
	payCfg := newPaymentConfigStore(db, cfg)
	return &Service{
		db: db, cfg: cfg, payCfg: payCfg, cache: c, log: log, membership: memSvc,
		newGateway: func(payType string) (pay.Gateway, error) {
			// 每次下单/回调都取当前配置，管理端保存后立即生效。
			p, err := payCfg.Snapshot(context.Background())
			if err != nil {
				return nil, err
			}
			return pay.New(payType, p, "")
		},
	}
}

// CreateInput 创建订单入参。
type CreateInput struct {
	LevelID string `json:"levelId" binding:"required" label:"会员等级"`
	PayType string `json:"payType" binding:"required,oneof=wechat alipay" label:"支付方式"`
}

// OrderVO 订单与支付凭证。
type OrderVO struct {
	*model.RechargeOrder
	PayURL string `json:"payUrl,omitempty"`
	QRCode string `json:"qrCode,omitempty"`
}

// CreateOrder 创建待支付订单并调用支付网关下单。
func (s *Service) CreateOrder(ctx context.Context, userID string, in *CreateInput, clientIP string) (*OrderVO, error) {
	payCfg, err := s.payCfg.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if !payCfg.Enabled {
		return nil, errs.Forbidden(i18n.Tf(ctx, "支付功能未开放"))
	}
	var lv model.MembershipLevel
	if err := s.db.WithContext(ctx).First(&lv, "id = ?", in.LevelID).Error; err != nil {
		return nil, errs.NotFound(i18n.Tf(ctx, "会员等级不存在"))
	}
	if !lv.Active() {
		return nil, errs.BadRequest(i18n.Tf(ctx, "等级「{0}」已停用，不能开通", lv.Name))
	}
	if lv.Price <= 0 {
		return nil, errs.BadRequest(i18n.Tf(ctx, "该等级未配置售价，请联系管理员"))
	}
	order := &model.RechargeOrder{
		OrderNo: idgen.OrderNo("RC"), UserID: userID, LevelID: lv.ID, LevelName: lv.Name,
		Days: lv.DefaultDays, Amount: lv.Price, Currency: "CNY",
		PayType: in.PayType, Status: model.OrderPending, ClientIP: clientIP,
	}
	if err := s.db.WithContext(ctx).Create(order).Error; err != nil {
		return nil, err
	}
	gw, err := s.newGateway(in.PayType)
	if err != nil {
		s.closeOrder(ctx, order)
		return nil, errs.BadRequest(err.Error())
	}
	result, err := gw.Charge(ctx, pay.Charge{
		OrderNo: order.OrderNo, AmountCents: order.Amount,
		Subject: "会员充值 - " + lv.Name, Body: "会员等级 " + lv.Code,
		ClientIP: clientIP,
	})
	if err != nil {
		s.closeOrder(ctx, order)
		return nil, errs.Internal(i18n.Tf(ctx, "支付下单失败，请稍后重试"))
	}
	return &OrderVO{RechargeOrder: order, PayURL: result.PayURL, QRCode: result.QRCode}, nil
}

func (s *Service) closeOrder(ctx context.Context, order *model.RechargeOrder) {
	_ = s.db.WithContext(ctx).Model(order).
		Where("status = ?", model.OrderPending).
		Update("status", model.OrderClosed).Error
}

// Query 查询本人订单。
func (s *Service) Query(ctx context.Context, userID, orderNo string) (*model.RechargeOrder, error) {
	var order model.RechargeOrder
	if err := s.db.WithContext(ctx).Where("order_no = ? AND user_id = ?", orderNo, userID).First(&order).Error; err != nil {
		return nil, errs.NotFound(i18n.Tf(ctx, "订单不存在"))
	}
	return &order, nil
}

// List 分页列出本人订单。
func (s *Service) List(ctx context.Context, userID string, p *query.Params) ([]*model.RechargeOrder, int64, error) {
	var list []*model.RechargeOrder
	db := s.db.WithContext(ctx).Model(&model.RechargeOrder{}).Where("user_id = ?", userID)
	if v := p.Filters["status"]; v != "" {
		db = db.Where("status = ?", v)
	}
	if p.Keyword != "" {
		db = db.Where("order_no LIKE ?", "%"+p.Keyword+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC").Offset((p.Page - 1) * p.PageSize).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// AdminList 管理端全量订单。
func (s *Service) AdminList(ctx context.Context, p *query.Params) ([]*model.RechargeOrder, int64, error) {
	var list []*model.RechargeOrder
	db := s.db.WithContext(ctx).Model(&model.RechargeOrder{})
	if v := p.Filters["status"]; v != "" {
		db = db.Where("status = ?", v)
	}
	if v := p.Filters["pay_type"]; v != "" {
		db = db.Where("pay_type = ?", v)
	}
	if p.Keyword != "" {
		db = db.Where("order_no LIKE ? OR trade_no LIKE ?", "%"+p.Keyword+"%", "%"+p.Keyword+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Order("created_at DESC").Offset((p.Page - 1) * p.PageSize).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// Close 关闭本人待支付订单。
func (s *Service) Close(ctx context.Context, userID, orderNo string) error {
	res := s.db.WithContext(ctx).Model(&model.RechargeOrder{}).
		Where("order_no = ? AND user_id = ? AND status = ?", orderNo, userID, model.OrderPending).
		Update("status", model.OrderClosed)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound(i18n.Tf(ctx, "待支付订单不存在"))
	}
	return nil
}

// Notify 处理支付回调。返回给渠道的 content-type 与 body。
func (s *Service) Notify(ctx context.Context, payType string, body []byte, form map[string]string) (string, string, error) {
	gw, err := s.newGateway(payType)
	if err != nil {
		return "", "", err
	}
	n, err := gw.VerifyNotify(body, form)
	if err != nil {
		return "", "", err
	}
	if n.OrderNo == "" {
		return "", "", errs.BadRequest(i18n.Tf(ctx, "回调缺少订单号"))
	}

	release, ok := cache.Lock(ctx, s.cache, "pay:notify:"+payType+":"+n.OrderNo, 30*time.Second)
	if !ok {
		// 另一实例正在处理，直接应答成功，避免渠道重复推送。
		ct, resp := gw.SuccessResponse()
		return ct, resp, nil
	}
	defer release()

	var order model.RechargeOrder
	if err := s.db.WithContext(ctx).Where("order_no = ?", n.OrderNo).First(&order).Error; err != nil {
		return "", "", errs.NotFound(i18n.Tf(ctx, "订单不存在"))
	}
	if order.PayType != payType {
		return "", "", errs.BadRequest(i18n.Tf(ctx, "支付渠道与订单不匹配"))
	}
	if order.Status == model.OrderPaid {
		ct, resp := gw.SuccessResponse()
		return ct, resp, nil
	}
	if order.Status != model.OrderPending {
		return "", "", errs.BadRequest(i18n.Tf(ctx, "订单状态不允许支付"))
	}
	if n.Amount > 0 && n.Amount != order.Amount {
		return "", "", errs.BadRequest(i18n.Tf(ctx, "回调金额与订单不一致"))
	}

	paidAt := n.PaidAt
	if paidAt.IsZero() {
		paidAt = time.Now()
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.RechargeOrder{}).
			Where("id = ? AND status = ?", order.ID, model.OrderPending).
			Updates(map[string]any{
				"status": model.OrderPaid, "trade_no": n.TradeNo,
				"paid_at": paidAt, "raw_notify": string(body),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 并发下已被另一请求落地
		}
		_, e := s.membership.GrantTx(ctx, tx, order.UserID, &membership.GrantInput{
			LevelID: order.LevelID, Days: order.Days,
		})
		return e
	})
	if err != nil {
		s.log.Error("充值回调处理失败", zap.String("order", order.OrderNo), zap.Error(err))
		return "", "", err
	}
	s.log.Info("充值到账", zap.String("order", order.OrderNo), zap.Int64("amount", order.Amount))
	ct, resp := gw.SuccessResponse()
	return ct, resp, nil
}

// Available 返回已启用且配置完整的支付渠道，供下单页展示。
func (s *Service) Available() []string {
	out := []string{}
	p, err := s.payCfg.Snapshot(context.Background())
	if err != nil || !p.Enabled {
		return out
	}
	if p.Wechat.Active() {
		out = append(out, model.PayWechat)
	}
	if p.Alipay.Active() {
		out = append(out, model.PayAlipay)
	}
	return out
}

// PaymentConfig 返回管理端支付配置视图。
func (s *Service) PaymentConfig(ctx context.Context) (*PaymentConfigView, error) {
	return s.payCfg.View(ctx)
}

// SavePaymentConfig 保存管理端支付配置，保存后下一次下单/回调立即生效。
func (s *Service) SavePaymentConfig(ctx context.Context, in *PaymentConfigView) error {
	return s.payCfg.Save(ctx, in)
}
