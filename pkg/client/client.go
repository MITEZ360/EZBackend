// Package client 提供服务间调用的 HTTP 客户端：
// 服务发现寻址、失败重试、熔断、链路标识透传，统一错误模型。
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sony/gobreaker"
	"go.uber.org/zap"

	"github.com/MITEZ360/tenon/pkg/discovery"
)

// Options 客户端参数。
type Options struct {
	Service        string             // 目标服务名
	Registry       discovery.Registry // 注册表，为空时必须提供 Direct
	Direct         string             // 直连地址（单体模式）
	Timeout        time.Duration      // 单次请求超时
	Retries        int                // 失败重试次数（不含首次）
	BreakerEnabled bool
	MaxFailed      int         // 熔断阈值：连续失败次数
	Interval       int         // 熔断统计窗口（秒）
	BreakerTimeout int         // 熔断打开后保持时长（秒）
	Headers        http.Header // 固定请求头，如内部调用令牌
	Logger         *zap.Logger
}

// Client 是服务间调用客户端，并发安全。
type Client struct {
	opts     Options
	hc       *http.Client
	balancer *discovery.Balancer
	breaker  *gobreaker.CircuitBreaker
}

// Error 表示下游返回的非 2xx 响应。
type Error struct {
	Service string
	Status  int
	Body    string
}

// Error 实现 error。
func (e *Error) Error() string {
	body := e.Body
	if len(body) > 200 {
		body = body[:200] + "..."
	}
	return fmt.Sprintf("调用服务 %s 失败: HTTP %d %s", e.Service, e.Status, body)
}

// New 创建客户端。
func New(o Options) *Client {
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.Retries < 0 {
		o.Retries = 0
	}
	if o.MaxFailed <= 0 {
		o.MaxFailed = 10
	}
	if o.Interval <= 0 {
		o.Interval = 30
	}
	if o.BreakerTimeout <= 0 {
		o.BreakerTimeout = 15
	}
	c := &Client{
		opts: o,
		hc: &http.Client{
			Timeout: o.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   32,
				IdleConnTimeout:       90 * time.Second,
				ExpectContinueTimeout: time.Second,
				DisableCompression:    false,
			},
		},
		balancer: discovery.NewBalancer(5 * time.Second),
	}
	if o.BreakerEnabled {
		c.breaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        "client:" + o.Service,
			MaxRequests: 5,
			Interval:    time.Duration(o.Interval) * time.Second,
			Timeout:     time.Duration(o.BreakerTimeout) * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= uint32(o.MaxFailed)
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				if o.Logger != nil {
					o.Logger.Warn("服务调用熔断状态变化",
						zap.String("breaker", name), zap.String("from", from.String()), zap.String("to", to.String()))
				}
			},
		})
	}
	return c
}

// Do 发起 JSON 请求；in 为请求体（可为 nil），out 为响应体目标（可为 nil）。
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var payload []byte
	if in != nil {
		var err error
		payload, err = json.Marshal(in)
		if err != nil {
			return fmt.Errorf("序列化请求体失败: %w", err)
		}
	}
	call := func() error { return c.once(ctx, method, path, payload, out) }
	if c.breaker == nil {
		return c.retry(ctx, call)
	}
	_, err := c.breaker.Execute(func() (any, error) {
		if e := c.retry(ctx, call); e != nil {
			return nil, e
		}
		return nil, nil
	})
	return err
}

// Get 发起 GET 请求。
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Post 发起 POST 请求。
func (c *Client) Post(ctx context.Context, path string, in, out any) error {
	return c.Do(ctx, http.MethodPost, path, in, out)
}

// Put 发起 PUT 请求。
func (c *Client) Put(ctx context.Context, path string, in, out any) error {
	return c.Do(ctx, http.MethodPut, path, in, out)
}

// Delete 发起 DELETE 请求。
func (c *Client) Delete(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodDelete, path, nil, out)
}

// retry 在可重试错误上做指数退避重试。
func (c *Client) retry(ctx context.Context, fn func() error) error {
	attempts := c.opts.Retries + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !retryable(lastErr) || i == attempts-1 {
			return lastErr
		}
		delay := time.Duration(30*(1<<i)) * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

// retryable 仅对网络错误与 5xx 重试，4xx 直接返回。
func retryable(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Status >= 500 || e.Status == http.StatusTooManyRequests
	}
	return true
}

// once 执行一次实际调用。
func (c *Client) once(ctx context.Context, method, path string, payload []byte, out any) error {
	base, instanceID, err := c.pick(ctx)
	if err != nil {
		return err
	}
	url := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range c.opts.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if tid := traceID(ctx); tid != "" {
		req.Header.Set("X-Trace-Id", tid)
	}
	if c.opts.Service != "" {
		req.Header.Set("X-Caller-Service", c.opts.Service)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		c.balancer.Fail(instanceID)
		return fmt.Errorf("请求 %s %s 失败: %w", method, url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		c.balancer.Fail(instanceID)
		return fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 {
			c.balancer.Fail(instanceID)
		}
		return &Error{Service: c.opts.Service, Status: resp.StatusCode, Body: string(data)}
	}
	c.balancer.Success(instanceID)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return nil
}

// pick 解析可用实例并做负载均衡。
func (c *Client) pick(ctx context.Context) (string, string, error) {
	if c.opts.Direct != "" {
		return withScheme(c.opts.Direct), "direct", nil
	}
	if c.opts.Registry == nil {
		return "", "", errors.New("未配置服务发现或直连地址")
	}
	list, err := c.opts.Registry.Resolve(ctx, c.opts.Service)
	if err != nil {
		return "", "", err
	}
	ins, ok := c.balancer.Pick(c.opts.Service, list)
	if !ok {
		return "", "", fmt.Errorf("服务 %s 无可用实例", c.opts.Service)
	}
	return ins.URL("http"), ins.ID, nil
}

func withScheme(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

type traceKey struct{}

// WithTraceID 将链路标识写入上下文，调用下游时自动透传。
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

func traceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey{}).(string); ok {
		return v
	}
	return ""
}
