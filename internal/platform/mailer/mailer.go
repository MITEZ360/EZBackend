// Package mailer 提供 SMTP 邮件发送能力。
//
// 与缓存、数据库一致：未配置 mail.host 时不创建实例，不占用连接与内存；
// 配置后自动激活，供告警、通知类功能使用。
package mailer

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/keel-framework/keel/internal/platform/config"
)

// Mailer 是邮件发送抽象。
type Mailer interface {
	Kind() string
	Send(ctx context.Context, to []string, subject, body string) error
	Health(ctx context.Context) error
	Close() error
}

// New 依据配置创建邮件客户端；未配置 host 时返回 (nil, nil)。
func New(cfg config.Mail, log *zap.Logger) (Mailer, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if !cfg.UseSMTP() {
		return nil, nil
	}
	port := cfg.Port
	if port <= 0 {
		port = 587
	}
	from := cfg.From
	if from == "" {
		if cfg.Username != "" && strings.Contains(cfg.Username, "@") {
			from = cfg.Username
		} else {
			from = "noreply@" + cfg.Host
		}
	}
	m := &smtpMailer{
		host: cfg.Host, port: port, username: cfg.Username, password: cfg.Password,
		from: from, useTLS: cfg.TLS,
		// 465 是隐式 TLS（SMTPS），587/25 走 STARTTLS
		implicitTLS: cfg.TLS && port == 465,
	}
	log.Info("邮件通知已启用", zap.String("host", cfg.Host), zap.Int("port", port),
		zap.Bool("tls", cfg.TLS), zap.String("from", from))
	return m, nil
}

type smtpMailer struct {
	host, username, password, from string
	port                           int
	useTLS, implicitTLS            bool
}

func (m *smtpMailer) Kind() string { return "smtp" }

func (m *smtpMailer) addr() string { return net.JoinHostPort(m.host, strconv.Itoa(m.port)) }

func (m *smtpMailer) Send(ctx context.Context, to []string, subject, body string) error {
	to = compact(to)
	if len(to) == 0 {
		return fmt.Errorf("收件人为空")
	}
	msg := m.build(to, subject, body)

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if m.implicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", m.addr(), &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", m.addr())
	}
	if err != nil {
		return fmt.Errorf("连接 SMTP 失败: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return fmt.Errorf("初始化 SMTP 会话失败: %w", err)
	}
	defer c.Close()

	if !m.implicitTLS && m.useTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}); err != nil {
				return fmt.Errorf("STARTTLS 失败: %w", err)
			}
		}
	}
	if m.username != "" {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(smtp.PlainAuth("", m.username, m.password, m.host)); err != nil {
				return fmt.Errorf("SMTP 认证失败: %w", err)
			}
		}
	}
	if err := c.Mail(m.from); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("设置收件人 %s 失败: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("准备邮件内容失败: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("提交邮件失败: %w", err)
	}
	return c.Quit()
}

func (m *smtpMailer) build(to []string, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + m.from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded + "\r\n")
	return []byte(b.String())
}

// Health 做一次轻量握手，供就绪探针使用。
func (m *smtpMailer) Health(ctx context.Context) error {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	var conn net.Conn
	var err error
	if m.implicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", m.addr(), &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", m.addr())
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		return err
	}
	return c.Quit()
}

func (m *smtpMailer) Close() error { return nil }

func compact(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
