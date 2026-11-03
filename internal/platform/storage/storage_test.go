package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keel-framework/keel/internal/platform/config"
)

func newTestStore(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := New(config.Storage{Type: "local", LocalDir: dir, BaseURL: "/uploads"}, nil)
	if err != nil {
		t.Fatalf("创建本地存储失败: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestLocalPutOpenDelete(t *testing.T) {
	m := newTestStore(t)
	ctx := context.Background()

	url, err := m.Put(ctx, "2026/11/03/abc.txt", strings.NewReader("hello keel"), 10, "text/plain")
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if url != "/uploads/2026/11/03/abc.txt" {
		t.Fatalf("URL 不符合预期: %s", url)
	}

	rc, err := m.Open(ctx, "2026/11/03/abc.txt")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(body) != "hello keel" {
		t.Fatalf("内容不一致: %q", body)
	}

	if err := m.Delete(ctx, "2026/11/03/abc.txt"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := m.Open(ctx, "2026/11/03/abc.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("删除后应不存在，实际: %v", err)
	}
	// 重复删除应视为成功
	if err := m.Delete(ctx, "2026/11/03/abc.txt"); err != nil {
		t.Fatalf("重复删除不应报错: %v", err)
	}
}

func TestLocalPathTraversal(t *testing.T) {
	m := newTestStore(t)
	ctx := context.Background()
	// key 里带 ../ 只能写到存储根目录内
	if _, err := m.Put(ctx, "../../../tmp/evil.txt", strings.NewReader("x"), 1, "text/plain"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	root := m.current.(*localStore).root
	if _, err := os.Stat(filepath.Join(root, "tmp", "evil.txt")); err != nil {
		t.Fatalf("文件应被限制在存储根目录内: %v", err)
	}
	if _, err := os.Stat("/tmp/evil.txt"); err == nil {
		t.Fatal("文件逃逸到了存储目录之外")
	}
}

func TestLocalHealth(t *testing.T) {
	m := newTestStore(t)
	if err := m.Health(context.Background()); err != nil {
		t.Fatalf("健康检查失败: %v", err)
	}
}

func TestAutoSelect(t *testing.T) {
	// 未配置对象存储时落到本地
	if got := (config.Storage{Type: "auto"}).Kind(); got != "local" {
		t.Fatalf("期望 local，实际 %s", got)
	}
	// 配置 endpoint 与 bucket 后自动使用对象存储
	cfg := config.Storage{Type: "auto", S3: config.S3{Endpoint: "minio:9000", Bucket: "keel"}}
	if got := cfg.Kind(); got != "s3" {
		t.Fatalf("期望 s3，实际 %s", got)
	}
}

func TestS3RequiresEndpoint(t *testing.T) {
	_, err := New(config.Storage{Type: "s3", S3: config.S3{Bucket: "keel"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("缺少 endpoint 应报错，实际: %v", err)
	}
}
