// Package dashboard 提供管理后台首页所需的统计数据。
package dashboard

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/gin-gonic/gin"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/pkg/response"
	"github.com/MITEZ360/tenon/pkg/utils"
)

// Service 仪表盘业务。
type Service struct {
	db    *gorm.DB
	cache cache.Cache
}

// NewService 创建仪表盘服务。
func NewService(db *gorm.DB, c cache.Cache) *Service { return &Service{db: db, cache: c} }

// Stats 概览指标。
type Stats struct {
	UserCount     int64 `json:"userCount"`
	ActiveUsers   int64 `json:"activeUsers"`
	RoleCount     int64 `json:"roleCount"`
	DeptCount     int64 `json:"deptCount"`
	FileCount     int64 `json:"fileCount"`
	TodayLogins   int64 `json:"todayLogins"`
	TodayOps      int64 `json:"todayOps"`
	OnlineCount   int64 `json:"onlineCount"`
	LoginFailures int64 `json:"loginFailures"`
}

// TrendPoint 趋势点。
type TrendPoint struct {
	Date   string `json:"date"`
	Logins int64  `json:"logins"`
	Ops    int64  `json:"ops"`
}

// Overview 仪表盘数据。
type Overview struct {
	Stats   Stats             `json:"stats"`
	Trend   []TrendPoint      `json:"trend"`
	Recent  []*model.LoginLog `json:"recent"`
	Notices []*model.Notice   `json:"notices"`
	Dist    []map[string]any  `json:"userDeptDist"`
}

// Overview 汇总仪表盘数据。
func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	now := time.Now()
	start, end := utils.TodayRange(now)
	o := &Overview{}

	s.db.WithContext(ctx).Model(&model.User{}).Count(&o.Stats.UserCount)
	s.db.WithContext(ctx).Model(&model.User{}).Where("status = 1").Count(&o.Stats.ActiveUsers)
	s.db.WithContext(ctx).Model(&model.Role{}).Count(&o.Stats.RoleCount)
	s.db.WithContext(ctx).Model(&model.Dept{}).Count(&o.Stats.DeptCount)
	s.db.WithContext(ctx).Model(&model.File{}).Count(&o.Stats.FileCount)
	s.db.WithContext(ctx).Model(&model.LoginLog{}).Where("created_at >= ? AND created_at < ? AND status = 1", start, end).Count(&o.Stats.TodayLogins)
	s.db.WithContext(ctx).Model(&model.LoginLog{}).Where("created_at >= ? AND created_at < ? AND status = 0", start, end).Count(&o.Stats.LoginFailures)
	s.db.WithContext(ctx).Model(&model.OperationLog{}).Where("created_at >= ? AND created_at < ?", start, end).Count(&o.Stats.TodayOps)
	s.db.WithContext(ctx).Model(&model.Session{}).Where("revoked = ? AND expires_at > ?", false, now).Count(&o.Stats.OnlineCount)

	o.Trend = s.trend(ctx, now)
	s.db.WithContext(ctx).Order("created_at DESC").Limit(8).Find(&o.Recent)
	s.db.WithContext(ctx).Where("status = 1").Order("created_at DESC").Limit(5).Find(&o.Notices)

	type row struct {
		Name string
		N    int64
	}
	var rows []row
	s.db.WithContext(ctx).Table("sys_user AS u").
		Select("d.name AS name, count(*) AS n").
		Joins("JOIN sys_dept AS d ON d.id = u.dept_id").
		Where("u.deleted_at IS NULL").
		Group("d.name").Order("n DESC").Limit(6).Scan(&rows)
	o.Dist = make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		o.Dist = append(o.Dist, map[string]any{"name": r.Name, "value": r.N})
	}
	return o, nil
}

func (s *Service) trend(ctx context.Context, now time.Time) []TrendPoint {
	days := 7
	from := utils.DaysAgo(now, days-1)
	type row struct {
		CreatedAt time.Time
	}
	var logins, ops []row
	s.db.WithContext(ctx).Model(&model.LoginLog{}).Select("created_at").
		Where("created_at >= ? AND status = 1", from).Find(&logins)
	s.db.WithContext(ctx).Model(&model.OperationLog{}).Select("created_at").
		Where("created_at >= ?", from).Find(&ops)

	lm := map[string]int64{}
	for _, r := range logins {
		lm[r.CreatedAt.Format("2006-01-02")]++
	}
	om := map[string]int64{}
	for _, r := range ops {
		om[r.CreatedAt.Format("2006-01-02")]++
	}
	out := make([]TrendPoint, 0, days)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, TrendPoint{Date: d, Logins: lm[d], Ops: om[d]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// Handler 仪表盘接口。
type Handler struct{ svc *Service }

// NewHandler 创建仪表盘处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Overview 返回首页数据。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Overview
// @Tags 运维
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/dashboard/overview [get]
// KE:SWAGGER-END
func (h *Handler) Overview(c *gin.Context) {
	o, err := h.svc.Overview(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, o)
}
