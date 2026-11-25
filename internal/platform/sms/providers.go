package sms

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// errUnknownProvider 返回未知短信厂商错误。
func errUnknownProvider(name string) error {
	return fmt.Errorf("sms: unknown provider %q", name)
}

// mockSender 仅用于本地开发：把验证码写入日志，不真实下发。
type mockSender struct {
	log *zap.Logger
}

func (m *mockSender) Kind() string { return "mock" }

// Send 把手机号与验证码打印到日志。
func (m *mockSender) Send(_ context.Context, phone, code string) error {
	if m.log != nil {
		m.log.Info("sms mock send",
			zap.String("phone", phone),
			zap.String("code", code),
		)
	}
	return nil
}

func (m *mockSender) Health(_ context.Context) error { return nil }

func (m *mockSender) Close() error { return nil }
