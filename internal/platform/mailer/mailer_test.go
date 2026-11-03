package mailer

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/keel-framework/keel/internal/platform/config"
)

func TestDisabledWhenNoHost(t *testing.T) {
	m, err := New(config.Mail{}, zap.NewNop())
	if err != nil {
		t.Fatalf("未配置 host 不应报错: %v", err)
	}
	if m != nil {
		t.Fatal("未配置 host 时应返回 nil，不创建实例")
	}
	// 显式关闭
	off := false
	m, _ = New(config.Mail{Enabled: &off, Host: "smtp.example.com"}, zap.NewNop())
	if m != nil {
		t.Fatal("enabled=false 时应关闭")
	}
}

func TestSendToLocalSMTP(t *testing.T) {
	addr := os.Getenv("KEEL_TEST_SMTP")
	if addr == "" {
		addr = "127.0.0.1:1025"
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Skipf("本地 SMTP 不可用，跳过: %v", err)
	}
	_ = conn.Close()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("地址格式错误: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	m, err := New(config.Mail{Host: host, Port: port, From: "keel@test.local", TLS: false}, zap.NewNop())
	if err != nil {
		t.Fatalf("创建邮件客户端失败: %v", err)
	}
	if m == nil {
		t.Fatal("配置 host 后应自动启用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Send(ctx, []string{"ops@test.local"}, "Keel 邮件通道自检", "hello keel\n第二行中文"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
}
