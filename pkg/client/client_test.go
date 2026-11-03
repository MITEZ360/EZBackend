package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Caller-Service") == "" {
			t.Error("缺少调用方标识头")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"name": "keel"}})
	}))
	defer srv.Close()

	c := New(Options{Service: "demo", Direct: srv.URL, Timeout: time.Second})
	var out struct {
		Data map[string]string `json:"data"`
	}
	if err := c.Get(context.Background(), "/ping", &out); err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if out.Data["name"] != "keel" {
		t.Fatalf("响应解析异常: %+v", out)
	}
}

func TestRetryOnServerError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":50000}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(Options{Service: "demo", Direct: srv.URL, Retries: 2, Timeout: time.Second})
	if err := c.Get(context.Background(), "/retry", nil); err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if hits != 3 {
		t.Fatalf("期望请求 3 次，实际 %d", hits)
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":40000,"message":"参数错误"}`))
	}))
	defer srv.Close()

	c := New(Options{Service: "demo", Direct: srv.URL, Retries: 3, Timeout: time.Second})
	err := c.Get(context.Background(), "/bad", nil)
	var ce *Error
	if !errors.As(err, &ce) || ce.Status != http.StatusBadRequest {
		t.Fatalf("应返回 400 业务错误，实际 %v", err)
	}
	if hits != 1 {
		t.Fatalf("4xx 不应重试，实际请求 %d 次", hits)
	}
}

func TestBreakerOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(Options{
		Service: "demo", Direct: srv.URL, Retries: 0, Timeout: 300 * time.Millisecond,
		BreakerEnabled: true, MaxFailed: 3, Interval: 60, BreakerTimeout: 60,
	})
	var lastErr error
	for i := 0; i < 5; i++ {
		lastErr = c.Get(context.Background(), "/boom", nil)
	}
	if !errors.Is(lastErr, http.ErrServerClosed) && !strings.Contains(lastErr.Error(), "circuit breaker") {
		t.Fatalf("熔断后应返回熔断错误，实际 %v", lastErr)
	}
}

func TestResolveFromRegistry(t *testing.T) {
	c := New(Options{Service: "keel-core", Timeout: time.Second})
	if err := c.Get(context.Background(), "/ping", nil); err == nil {
		t.Fatal("未配置注册表与直连地址时应报错")
	}
}

func TestTracePropagation(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Trace-Id")
	}))
	defer srv.Close()

	c := New(Options{Service: "demo", Direct: srv.URL, Timeout: time.Second})
	ctx := WithTraceID(context.Background(), "trace-123")
	if err := c.Get(ctx, "/trace", nil); err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if got != "trace-123" {
		t.Fatalf("链路标识未透传: %q", got)
	}
}
