// Package file 文件上传与管理：本地磁盘存储、类型与大小校验、哈希去重与下载。
package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/idgen"
	"github.com/MITEZ360/tenon/internal/platform/storage"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/i18n"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/utils"
)

var defaultAllowExt = []string{
	// 不接受 SVG：它是可执行脚本的文档类型，放在同源静态目录会形成存储型 XSS。
	".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp",
	".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	".txt", ".csv", ".md", ".json", ".xml",
	".zip", ".rar", ".7z", ".tar", ".gz",
	".mp4", ".mp3", ".wav",
}

// Service 文件业务。
type Service struct {
	db    *gorm.DB
	cfg   *config.Config
	store *storage.Manager
	log   *zap.Logger
}

// NewService 创建文件服务。
func NewService(db *gorm.DB, cfg *config.Config, store *storage.Manager, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, cfg: cfg, store: store, log: log}
}

// Upload 保存上传文件并写入元数据。
// 先落到临时文件，拿到准确大小与摘要后再交给存储实现，避免对象存储分片上传
// 必须预先知道长度的问题，也不会把大文件整个读进内存。
func (s *Service) Upload(ctx context.Context, fh *multipart.FileHeader, uploaderID string) (*model.File, error) {
	maxSize := s.cfg.Storage.MaxSize
	if maxSize <= 0 {
		maxSize = 20 << 20
	}
	if fh.Size > maxSize {
		return nil, errs.BadRequest(i18n.Tf(ctx, "文件大小超过限制（最大 {0}）", utils.FormatBytes(maxSize)))
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
		return nil, errs.BadRequest(i18n.Tf(ctx, "不支持的文件类型: {0}", ext))
	}

	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	tmp, err := os.CreateTemp("", "tenon-upload-*")
	if err != nil {
		return nil, errs.Internal("创建临时文件失败")
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	h := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(src, maxSize+1))
	if err != nil {
		return nil, errs.Internal("读取上传文件失败")
	}
	if written > maxSize {
		return nil, errs.BadRequest("文件大小超过限制")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, errs.Internal("读取上传文件失败")
	}

	mimeType := fh.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(ext)
	}

	now := time.Now()
	relDir := filepath.ToSlash(filepath.Join(now.Format("2006"), now.Format("01"), now.Format("02")))
	name := idgen.Next() + ext
	key := relDir + "/" + name

	url, err := s.store.Put(ctx, key, tmp, written, mimeType)
	if err != nil {
		s.log.Error("写入存储失败", zap.String("key", key), zap.Error(err))
		return nil, errs.Internal("保存文件失败")
	}

	f := &model.File{
		Name: utils.SafeFilename(fh.Filename), Path: key, URL: url,
		Mime: mimeType, Ext: ext, Size: written,
		Hash: hex.EncodeToString(h.Sum(nil)), UploaderID: uploaderID, Storage: s.store.Kind(),
	}
	if err := s.db.WithContext(ctx).Create(f).Error; err != nil {
		_ = s.store.For(f.Storage).Delete(context.Background(), key)
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

// Delete 删除文件（软删除元数据并移除存储对象）。
func (s *Service) Delete(ctx context.Context, id string) error {
	f, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	// 历史记录可能是本地磁盘写入的，按记录中的 storage 选择实现
	if err := s.store.For(f.Storage).Delete(ctx, f.Path); err != nil {
		s.log.Warn("删除存储对象失败", zap.String("path", f.Path), zap.String("storage", f.Storage), zap.Error(err))
	}
	return s.db.WithContext(ctx).Delete(&model.File{}, "id = ?", id).Error
}

// Open 打开文件用于下载。
func (s *Service) Open(ctx context.Context, id string) (io.ReadCloser, *model.File, error) {
	f, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	fh, err := s.store.For(f.Storage).Open(ctx, f.Path)
	if err != nil {
		return nil, nil, errs.NotFound("文件已丢失")
	}
	return fh, f, nil
}
