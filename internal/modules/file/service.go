// Package file 文件上传与管理：本地磁盘存储、类型与大小校验、哈希去重与下载。
package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/idgen"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/utils"
)

var defaultAllowExt = []string{
	".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".bmp",
	".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	".txt", ".csv", ".md", ".json", ".xml",
	".zip", ".rar", ".7z", ".tar", ".gz",
	".mp4", ".mp3", ".wav",
}

// Service 文件业务。
type Service struct {
	db  *gorm.DB
	cfg *config.Config
	log *zap.Logger
}

// NewService 创建文件服务。
func NewService(db *gorm.DB, cfg *config.Config, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, cfg: cfg, log: log}
}

// Upload 保存上传文件并写入元数据。
func (s *Service) Upload(ctx context.Context, fh *multipart.FileHeader, uploaderID string) (*model.File, error) {
	maxSize := s.cfg.Storage.MaxSize
	if maxSize <= 0 {
		maxSize = 20 << 20
	}
	if fh.Size > maxSize {
		return nil, errs.BadRequest(fmt.Sprintf("文件大小超过限制（最大 %s）", utils.FormatBytes(maxSize)))
	}
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	allow := s.cfg.Storage.AllowExt
	if len(allow) == 0 {
		allow = defaultAllowExt
	}
	ok := false
	for _, e := range allow {
		if strings.EqualFold(e, ext) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, errs.BadRequest("不支持的文件类型: " + ext)
	}

	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	now := time.Now()
	relDir := filepath.Join(now.Format("2006"), now.Format("01"), now.Format("02"))
	dir := filepath.Join(s.cfg.Storage.LocalDir, relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errs.Internal("创建存储目录失败")
	}
	name := idgen.Next() + ext
	dstPath := filepath.Join(dir, name)

	dst, err := os.Create(dstPath)
	if err != nil {
		return nil, errs.Internal("写入文件失败")
	}
	h := sha256.New()
	written, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(src, maxSize+1))
	closeErr := dst.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(dstPath)
		return nil, errs.Internal("写入文件失败")
	}
	if written > maxSize {
		_ = os.Remove(dstPath)
		return nil, errs.BadRequest("文件大小超过限制")
	}

	mimeType := fh.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(ext)
	}
	url := strings.TrimSuffix(s.cfg.Storage.BaseURL, "/") + "/" + filepath.ToSlash(filepath.Join(relDir, name))
	f := &model.File{
		Name: utils.SafeFilename(fh.Filename), Path: dstPath, URL: url,
		Mime: mimeType, Ext: ext, Size: written,
		Hash: hex.EncodeToString(h.Sum(nil)), UploaderID: uploaderID, Storage: "local",
	}
	if err := s.db.WithContext(ctx).Create(f).Error; err != nil {
		_ = os.Remove(dstPath)
		return nil, err
	}
	return f, nil
}

// List 文件分页。
func (s *Service) List(ctx context.Context, p *query.Params) ([]*model.File, int64, error) {
	allowed := map[string]string{"name": "name", "ext": "ext", "uploader_id": "uploader_id", "created_at": "created_at"}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.File{}), allowed)
	db = query.Search(db, p.Keyword, "name", "ext")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.File
	err := db.Order(p.Order(map[string]string{"created_at": "created_at", "size": "size"}, "created_at DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// Get 文件详情。
func (s *Service) Get(ctx context.Context, id string) (*model.File, error) {
	var f model.File
	if err := s.db.WithContext(ctx).First(&f, "id = ?", id).Error; err != nil {
		return nil, errs.NotFound("文件不存在")
	}
	return &f, nil
}

// Delete 删除文件（软删除元数据并移除磁盘文件）。
func (s *Service) Delete(ctx context.Context, id string) error {
	f, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if f.Storage == "local" {
		if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
			s.log.Warn("删除磁盘文件失败", zap.String("path", f.Path), zap.Error(err))
		}
	}
	return s.db.WithContext(ctx).Delete(&model.File{}, "id = ?", id).Error
}

// Open 打开文件用于下载。
func (s *Service) Open(ctx context.Context, id string) (*os.File, *model.File, error) {
	f, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	fh, err := os.Open(f.Path)
	if err != nil {
		return nil, nil, errs.NotFound("文件已丢失")
	}
	return fh, f, nil
}
