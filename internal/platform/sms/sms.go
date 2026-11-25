// Package sms 提供短信验证码下发网关。支持阿里云、腾讯云与本地 mock。
// 厂商协议用标准库实现，避免引入重型 SDK。
package sms

import (
	"context"

	"go.uber.org/zap"

	"github.com/keel-framework/keel/internal/platform/config"
)

// Sender 短信发送器。
type Sender interface {
	Kind() string
	// Send 向手机号下发验证码。code 为纯数字验证码。
	Send(ctx context.Context, phone, code string) error
	Health(ctx context.Context) error
	Close() error
}

// New 按配置构造短信发送器。未启用时返回 (nil, nil)。
func New(cfg config.SMS, log *zap.Logger) (Sender, error) {
	if !cfg.Use() {
		return nil, nil
	}
	switch cfg.Provider {
	case "mock", "":
		return &mockSender{log: log}, nil
	case "aliyun":
		return newAliyun(cfg, log)
	case "tencent":
		return newTencent(cfg, log)
	default:
		return nil, errUnknownProvider(cfg.Provider)
	}
}
