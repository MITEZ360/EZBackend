package model

import "time"

// 充值订单状态。
const (
	OrderPending  = "pending"  // 待支付
	OrderPaid     = "paid"     // 已支付并已发放会员
	OrderClosed   = "closed"   // 已关闭/超时
	OrderRefunded = "refunded" // 已退款
)

// 支付渠道。
const (
	PayWechat = "wechat"
	PayAlipay = "alipay"
)

// 短信验证码场景。
const (
	SMSCodeRegister = "register"
	SMSCodeLogin    = "login"
	SMSCodeReset    = "reset"
)

// RechargeOrder 会员充值订单。金额统一以「分」存储，避免浮点误差。
// OrderNo 是业务单号（外部支付渠道的 out_trade_no），TradeNo 是渠道交易号。
type RechargeOrder struct {
	Base
	OrderNo   string     `gorm:"size:64;uniqueIndex;not null" json:"orderNo"`
	UserID    string     `gorm:"size:32;index;not null" json:"userId"`
	LevelID   string     `gorm:"size:32;index;not null" json:"levelId"`
	LevelName string     `gorm:"size:64" json:"levelName,omitempty"`
	Days      int        `gorm:"default:0" json:"days"`  // 0 表示按等级默认时长
	Amount    int64      `gorm:"not null" json:"amount"` // 单位：分
	Currency  string     `gorm:"size:8;default:CNY" json:"currency"`
	PayType   string     `gorm:"size:16;index;not null" json:"payType"`
	Status    string     `gorm:"size:16;index;not null" json:"status"`
	TradeNo   string     `gorm:"size:128;uniqueIndex" json:"tradeNo,omitempty"`
	PaidAt    *time.Time `json:"paidAt,omitempty"`
	RawNotify string     `gorm:"type:text" json:"-"`
	ClientIP  string     `gorm:"size:64" json:"-"`
	Remark    string     `gorm:"size:255" json:"remark,omitempty"`
}

// TableName 指定表名。
func (RechargeOrder) TableName() string { return "cn_recharge_order" }

// IsFinal 判断订单是否已进入终态。
func (o *RechargeOrder) IsFinal() bool {
	return o != nil && (o.Status == OrderPaid || o.Status == OrderClosed || o.Status == OrderRefunded)
}

// SMSCode 短信验证码记录。只存哈希，不存明文；验证成功后置 UsedAt。
type SMSCode struct {
	Base
	Phone     string     `gorm:"size:32;index;not null" json:"-"`
	CodeHash  string     `gorm:"size:128;not null" json:"-"`
	Scene     string     `gorm:"size:16;index;not null" json:"-"`
	ExpiresAt time.Time  `gorm:"index;not null" json:"-"`
	UsedAt    *time.Time `json:"-"`
	FailCount int        `gorm:"default:0" json:"-"`
	IP        string     `gorm:"size:64" json:"-"`
}

// TableName 指定表名。
func (SMSCode) TableName() string { return "cn_sms_code" }
