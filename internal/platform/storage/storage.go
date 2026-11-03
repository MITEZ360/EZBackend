// Package storage 统一对象存储抽象。
//
// 设计目标与缓存保持一致：零配置时落到本地磁盘即可运行，配置了对象存储
// （MinIO / 阿里云 OSS / AWS S3 / 腾讯云 COS）就自动切换，业务代码不感知差异。
// 多实例部署时使用对象存储，上传的文件才对所有实例可见。
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"

	"github.com/keel-framework/keel/internal/platform/config"
)

// Storage 是对象存储的最小接口，只覆盖上传、读取、删除与健康检查。
type Storage interface {
	// Kind 返回实现类型：local / s3。
	Kind() string
	// Put 写入对象并返回可访问 URL。key 由调用方生成，形如 2026/11/03/xxx.png。
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (string, error)
	// Open 读取对象，调用方负责关闭。
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete 删除对象，不存在时应视为成功。
	Delete(ctx context.Context, key string) error
	// Health 检查存储可用性。
	Health(ctx context.Context) error
	Close() error
}

// Manager 持有当前生效的存储，并为历史文件保留其它实现的只读入口。
// 例如从本地磁盘迁到对象存储后，旧记录里 storage=local 的文件仍可下载。
type Manager struct {
	current Storage
	byKind  map[string]Storage
	log     *zap.Logger
}

// New 依据配置创建存储管理器。type=auto 时看 s3.bucket 是否配置。
func New(cfg config.Storage, log *zap.Logger) (*Manager, error) {
	if log == nil {
		log = zap.NewNop()
	}
	m := &Manager{byKind: make(map[string]Storage), log: log}

	switch cfg.Kind() {
	case "local":
		ls, err := newLocal(cfg)
		if err != nil {
			return nil, err
		}
		m.current = ls
		m.byKind["local"] = ls
	case "s3":
		s3s, err := newS3(cfg, log)
		if err != nil {
			return nil, err
		}
		m.current = s3s
		m.byKind["s3"] = s3s
		// 保留本地实现用于读取迁移前的历史文件；失败不影响主存储。
		if ls, err := newLocal(cfg); err == nil {
			m.byKind["local"] = ls
		}
	default:
		return nil, fmt.Errorf("不支持的存储类型: %s", cfg.Type)
	}
	return m, nil
}

// Put 写入当前存储。
func (m *Manager) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (string, error) {
	return m.current.Put(ctx, key, r, size, contentType)
}

// Open 从当前存储读取。
func (m *Manager) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return m.current.Open(ctx, key)
}

// Delete 从当前存储删除。
func (m *Manager) Delete(ctx context.Context, key string) error {
	return m.current.Delete(ctx, key)
}

// Kind 返回当前写入使用的存储类型。
func (m *Manager) Kind() string { return m.current.Kind() }

// Current 返回当前写入使用的存储。
func (m *Manager) Current() Storage { return m.current }

// For 按记录中的 storage 字段选择实现，未知类型回落到当前存储。
func (m *Manager) For(kind string) Storage {
	if s, ok := m.byKind[strings.ToLower(strings.TrimSpace(kind))]; ok {
		return s
	}
	return m.current
}

// Health 检查当前存储可用性。
func (m *Manager) Health(ctx context.Context) error { return m.current.Health(ctx) }

// Close 释放所有实现持有的资源。
func (m *Manager) Close() error {
	var err error
	for _, s := range m.byKind {
		if e := s.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// ---- 本地磁盘 ----

type localStore struct {
	root    string
	baseURL string
}

func newLocal(cfg config.Storage) (*localStore, error) {
	root := cfg.LocalDir
	if root == "" {
		root = "data/uploads"
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("创建上传目录失败: %w", err)
	}
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "/uploads"
	}
	return &localStore{root: root, baseURL: base}, nil
}

func (l *localStore) Kind() string { return "local" }

// path 把对象 key 限制在 root 之下，避免历史数据中的 ../ 逃逸出目录。
// 兼容早期版本在 file.path 中保存绝对路径的情况。
func (l *localStore) path(key string) string {
	clean := filepath.Clean(filepath.FromSlash(key))
	if filepath.IsAbs(clean) {
		if rel, err := filepath.Rel(l.root, clean); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return clean
		}
		return filepath.Join(l.root, filepath.Base(clean))
	}
	return filepath.Join(l.root, filepath.Clean("/"+clean))
}

func (l *localStore) Put(_ context.Context, key string, r io.Reader, size int64, _ string) (string, error) {
	dst := l.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("创建存储目录失败: %w", err)
	}
	f, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	n, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(dst)
		return "", fmt.Errorf("写入文件失败: %w", firstErr(copyErr, closeErr))
	}
	if size >= 0 && n != size {
		_ = os.Remove(dst)
		return "", fmt.Errorf("写入大小不一致: 期望 %d，实际 %d", size, n)
	}
	return l.URL(key), nil
}

// URL 由 key 推导公开访问地址。
func (l *localStore) URL(key string) string {
	return l.baseURL + "/" + strings.TrimPrefix(filepath.ToSlash(key), "/")
}

func (l *localStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(l.path(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return f, nil
}

func (l *localStore) Delete(_ context.Context, key string) error {
	if err := os.Remove(l.path(key)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (l *localStore) Health(_ context.Context) error {
	probe := filepath.Join(l.root, ".keel-health")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("上传目录不可写: %w", err)
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return nil
}

func (l *localStore) Close() error { return nil }

// ---- S3 兼容对象存储 ----

type s3Store struct {
	cli     *minio.Client
	bucket  string
	baseURL string
}

func newS3(cfg config.Storage, log *zap.Logger) (*s3Store, error) {
	endpoint := strings.TrimSpace(cfg.S3.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("storage.type=s3 但 storage.s3.endpoint 为空")
	}
	if cfg.S3.Bucket == "" {
		return nil, fmt.Errorf("storage.type=s3 但 storage.s3.bucket 为空")
	}
	secure := cfg.S3.UseSSL
	switch {
	case strings.HasPrefix(endpoint, "https://"):
		secure, endpoint = true, strings.TrimPrefix(endpoint, "https://")
	case strings.HasPrefix(endpoint, "http://"):
		secure, endpoint = false, strings.TrimPrefix(endpoint, "http://")
	}
	endpoint = strings.TrimSuffix(endpoint, "/")

	var creds *credentials.Credentials
	if cfg.S3.AccessKey == "" && cfg.S3.SecretKey == "" {
		// 云上实例角色 / 匿名访问
		creds = credentials.NewIAM("")
	} else {
		creds = credentials.NewStaticV4(cfg.S3.AccessKey, cfg.S3.SecretKey, "")
	}
	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  creds,
		Secure: secure,
		Region: cfg.S3.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化对象存储失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exists, err := cli.BucketExists(ctx, cfg.S3.Bucket)
	if err != nil {
		return nil, fmt.Errorf("连接对象存储失败（%s）: %w", endpoint, err)
	}
	if !exists {
		if err := cli.MakeBucket(ctx, cfg.S3.Bucket, minio.MakeBucketOptions{Region: cfg.S3.Region}); err != nil {
			return nil, fmt.Errorf("创建存储桶 %s 失败: %w", cfg.S3.Bucket, err)
		}
	}

	base := strings.TrimSuffix(strings.TrimSpace(cfg.S3.PublicURL), "/")
	if base == "" {
		if strings.HasPrefix(strings.TrimSpace(cfg.BaseURL), "http") {
			base = strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
		} else {
			scheme := "http"
			if secure {
				scheme = "https"
			}
			base = fmt.Sprintf("%s://%s/%s", scheme, endpoint, cfg.S3.Bucket)
		}
	}
	log.Info("文件存储已启用对象存储",
		zap.String("endpoint", endpoint), zap.String("bucket", cfg.S3.Bucket), zap.Bool("ssl", secure))
	return &s3Store{cli: cli, bucket: cfg.S3.Bucket, baseURL: base}, nil
}

func (s *s3Store) Kind() string { return "s3" }

func (s *s3Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (string, error) {
	key = strings.TrimPrefix(filepath.ToSlash(key), "/")
	_, err := s.cli.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("上传到对象存储失败: %w", err)
	}
	return s.baseURL + "/" + key, nil
}

func (s *s3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	key = strings.TrimPrefix(filepath.ToSlash(key), "/")
	obj, err := s.cli.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject 是惰性的，Stat 才会真正发起请求并暴露 NoSuchKey
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, err
	}
	return obj, nil
}

func (s *s3Store) Delete(ctx context.Context, key string) error {
	key = strings.TrimPrefix(filepath.ToSlash(key), "/")
	return s.cli.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *s3Store) Health(ctx context.Context) error {
	ok, err := s.cli.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("存储桶不存在: %s", s.bucket)
	}
	return nil
}

func (s *s3Store) Close() error { return nil }

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
