package model

// 支付配置记录 channel 取值。global 保存总开关，其余为渠道配置。
const (
	PayConfigGlobal = "global"
)

// PaymentConfig 管理端支付配置。Payload 为加密后的渠道 JSON，凭据不明文落库。
type PaymentConfig struct {
	Base
	Channel string `gorm:"size:16;uniqueIndex;not null" json:"channel"`
	Enabled bool   `gorm:"default:false" json:"enabled"`
	Payload string `gorm:"type:text;not null" json:"-"`
}

// TableName 指定表名。
func (PaymentConfig) TableName() string { return "cn_payment_config" }
