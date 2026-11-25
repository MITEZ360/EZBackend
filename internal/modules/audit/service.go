// Package audit 提供登录日志、操作审计日志与在线会话管理，审计写入为异步批量落库。
package audit

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/query"
)

// Service 审计服务，实现 middleware.Auditor。
type Service struct {
	db    *gorm.DB
	cache cache.Cache
	log   *zap.Logger
	ch    chan *model.OperationLog
	done  chan struct{}
}

// NewService 创建审计服务并启动异步写入协程。
func NewService(db *gorm.DB, c cache.Cache, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	s := &Service{db: db, cache: c, log: log, ch: make(chan *model.OperationLog, 1024), done: make(chan struct{})}
	go s.worker()
	return s
}

// Push 非阻塞投递审计日志，队列满时丢弃并告警，避免拖垮业务。
func (s *Service) Push(entry *model.OperationLog) {
	select {
	case s.ch <- entry:
	default:
		s.log.Warn("审计队列已满，丢弃日志", zap.String("path", entry.Path))
	}
}

func (s *Service) worker() {
	batch := make([]*model.OperationLog, 0, 100)
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.db.CreateInBatches(batch, 50).Error; err != nil {
			s.log.Error("写入审计日志失败", zap.Error(err))
		}
		batch = batch[:0]
	}
	for {
		select {
		case <-s.done:
			flush()
			return
		case e := <-s.ch:
			batch = append(batch, e)
			if len(batch) >= 100 {
				flush()
			}
		case <-tk.C:
			flush()
		}
	}
}

// Close 停止接收并落盘剩余日志。
func (s *Service) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// LoginLogs 登录日志分页。
func (s *Service) LoginLogs(ctx context.Context, p *query.Params) ([]*model.LoginLog, int64, error) {
	allowed := map[string]string{"username": "username", "ip": "ip", "status": "status", "created_at": "created_at"}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.LoginLog{}), allowed)
	db = query.Search(db, p.Keyword, "username", "ip", "message")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.LoginLog
	err := db.Order(p.Order(map[string]string{"created_at": "created_at"}, "created_at DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// OperationLogs 操作日志分页。
func (s *Service) OperationLogs(ctx context.Context, p *query.Params) ([]*model.OperationLog, int64, error) {
	allowed := map[string]string{"username": "username", "module": "module", "method": "method",
		"status": "status", "created_at": "created_at", "user_id": "user_id"}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.OperationLog{}), allowed)
	db = query.Search(db, p.Keyword, "username", "path", "module", "action")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.OperationLog
	err := db.Select("id, user_id, username, module, action, method, path, ip, status, cost_ms, trace_id, error, created_at").
		Order(p.Order(map[string]string{"created_at": "created_at"}, "created_at DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// GetOperationLog 操作日志详情。
func (s *Service) GetOperationLog(ctx context.Context, id string) (*model.OperationLog, error) {
	var l model.OperationLog
	if err := s.db.WithContext(ctx).First(&l, "id = ?", id).Error; err != nil {
		return nil, errs.NotFound("日志不存在")
	}
	return &l, nil
}

// ClearLoginLogs 清理指定天数之前的登录日志。
func (s *Service) ClearLoginLogs(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = 30
	}
	before := time.Now().AddDate(0, 0, -days)
	res := s.db.WithContext(ctx).Where("created_at < ?", before).Delete(&model.LoginLog{})
	return res.RowsAffected, res.Error
}

// ClearOperationLogs 清理指定天数之前的操作日志。
func (s *Service) ClearOperationLogs(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = 30
	}
	before := time.Now().AddDate(0, 0, -days)
	res := s.db.WithContext(ctx).Where("created_at < ?", before).Delete(&model.OperationLog{})
	return res.RowsAffected, res.Error
}

// OnlineSession 在线会话视图。
type OnlineSession struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	Username   string    `json:"username"`
	Nickname   string    `json:"nickname"`
	IP         string    `json:"ip"`
	Browser    string    `json:"browser"`
	OS         string    `json:"os"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	LoginAt    time.Time `json:"loginAt"`
}

// Online 在线用户列表。
func (s *Service) Online(ctx context.Context, p *query.Params) ([]*OnlineSession, int64, error) {
	db := s.db.WithContext(ctx).Table("sys_session AS s").
		Joins("JOIN sys_user AS u ON u.id = s.user_id").
		Where("s.revoked = ? AND s.expires_at > ? AND u.deleted_at IS NULL", false, time.Now())
	if kw := p.Keyword; kw != "" {
		db = db.Where("u.username LIKE ? OR u.nickname LIKE ? OR s.ip LIKE ?", "%"+kw+"%", "%"+kw+"%", "%"+kw+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*OnlineSession
	err := db.Select("s.id, s.user_id, u.username, u.nickname, s.ip, s.browser, s.os, s.last_seen_at, s.created_at AS login_at").
		Order("s.last_seen_at DESC").Offset(p.Offset()).Limit(p.PageSize).Scan(&list).Error
	return list, total, err
}

// Kick 强制下线指定会话。
func (s *Service) Kick(ctx context.Context, sessionID string) error {
	res := s.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", sessionID).Update("revoked", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("会话不存在或已下线")
	}
	_ = s.cache.Delete(ctx, "sess:"+sessionID)
	return nil
}

// CleanupSessions 清理过期会话记录。
func (s *Service) CleanupSessions(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = 7
	}
	before := time.Now().AddDate(0, 0, -days)
	res := s.db.WithContext(ctx).Where("expires_at < ?", before).Delete(&model.Session{})
	return res.RowsAffected, res.Error
}
