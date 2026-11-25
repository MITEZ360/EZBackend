// Package role 角色与权限分配。
package role

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/authz"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/utils"
)

// Service 角色业务逻辑。
type Service struct {
	db    *gorm.DB
	authz *authz.Loader
}

// NewService 创建角色服务。
func NewService(db *gorm.DB, al *authz.Loader) *Service { return &Service{db: db, authz: al} }

// List 角色列表。
func (s *Service) List(ctx context.Context, p *query.Params) ([]*model.Role, int64, error) {
	allowed := map[string]string{"code": "code", "name": "name", "status": "status", "created_at": "created_at"}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.Role{}), allowed)
	db = query.Search(db, p.Keyword, "code", "name", "description")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.Role
	err := db.Order(p.Order(map[string]string{"sort": "sort", "created_at": "created_at", "id": "id"}, "sort ASC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	for _, r := range list {
		var n int64
		s.db.WithContext(ctx).Model(&model.UserRole{}).Where("role_id = ?", r.ID).Count(&n)
		r.UserCount = n
		var menuIDs []string
		s.db.WithContext(ctx).Model(&model.RoleMenu{}).Where("role_id = ?", r.ID).Pluck("menu_id", &menuIDs)
		r.MenuIDs = menuIDs
	}
	return list, total, nil
}

// All 返回全部启用角色。
func (s *Service) All(ctx context.Context) ([]*model.Role, error) {
	var list []*model.Role
	err := s.db.WithContext(ctx).Where("status = ?", model.StatusEnabled).Order("sort ASC").Find(&list).Error
	return list, err
}

// Get 查询单个角色。
func (s *Service) Get(ctx context.Context, id string) (*model.Role, error) {
	var r model.Role
	if err := s.db.WithContext(ctx).First(&r, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.NotFound("角色不存在")
		}
		return nil, err
	}
	var menuIDs []string
	s.db.WithContext(ctx).Model(&model.RoleMenu{}).Where("role_id = ?", id).Pluck("menu_id", &menuIDs)
	r.MenuIDs = menuIDs
	return &r, nil
}

// Input 角色创建/更新入参。
type Input struct {
	Code        string `json:"code" binding:"required,max=64" label:"角色编码"`
	Name        string `json:"name" binding:"required,max=64" label:"角色名称"`
	Description string `json:"description" binding:"max=255" label:"描述"`
	DataScope   int8   `json:"dataScope" binding:"oneof=1 2 3 4" label:"数据范围"`
	Sort        int    `json:"sort"`
	Status      *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
}

// Create 创建角色。
func (s *Service) Create(ctx context.Context, in *Input) (*model.Role, error) {
	var n int64
	s.db.WithContext(ctx).Model(&model.Role{}).Where("code = ?", in.Code).Count(&n)
	if n > 0 {
		return nil, errs.Conflict("角色编码已存在")
	}
	r := &model.Role{Code: in.Code, Name: in.Name, Description: in.Description,
		DataScope: in.DataScope, Sort: in.Sort,
		Status: utils.Int8Or(in.Status, model.StatusEnabled)}
	if err := s.db.WithContext(ctx).Select("*").Create(r).Error; err != nil {
		return nil, err
	}
	s.authz.Invalidate(ctx)
	return r, nil
}

// Update 更新角色。
func (s *Service) Update(ctx context.Context, id string, in *Input) error {
	var r model.Role
	if err := s.db.WithContext(ctx).First(&r, "id = ?", id).Error; err != nil {
		return errs.NotFound("角色不存在")
	}
	if r.Code == "super" && in.Code != "super" {
		return errs.BadRequest("内置超级管理员角色编码不可修改")
	}
	var n int64
	s.db.WithContext(ctx).Model(&model.Role{}).Where("code = ? AND id <> ?", in.Code, id).Count(&n)
	if n > 0 {
		return errs.Conflict("角色编码已存在")
	}
	err := s.db.WithContext(ctx).Model(&model.Role{}).Where("id = ?", id).Updates(map[string]any{
		"code": in.Code, "name": in.Name, "description": in.Description,
		"data_scope": in.DataScope, "sort": in.Sort,
		"status": utils.Int8Or(in.Status, model.StatusEnabled),
	}).Error
	if err == nil {
		s.authz.Invalidate(ctx)
	}
	return err
}

// Delete 删除角色。
func (s *Service) Delete(ctx context.Context, id string) error {
	var r model.Role
	if err := s.db.WithContext(ctx).First(&r, "id = ?", id).Error; err != nil {
		return errs.NotFound("角色不存在")
	}
	if r.Code == "super" {
		return errs.Forbidden("内置角色不可删除")
	}
	var n int64
	s.db.WithContext(ctx).Model(&model.UserRole{}).Where("role_id = ?", id).Count(&n)
	if n > 0 {
		return errs.Conflict("该角色下仍有用户，请先解除关联")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Role{}, "id = ?", id).Error
	})
	if err == nil {
		s.authz.Invalidate(ctx)
	}
	return err
}

// Menus 返回角色已分配的菜单 ID。
func (s *Service) Menus(ctx context.Context, id string) ([]string, error) {
	var ids []string
	err := s.db.WithContext(ctx).Model(&model.RoleMenu{}).Where("role_id = ?", id).Pluck("menu_id", &ids).Error
	return ids, err
}

// SetMenus 设置角色菜单权限。
func (s *Service) SetMenus(ctx context.Context, id string, menuIDs []string) error {
	var n int64
	s.db.WithContext(ctx).Model(&model.Role{}).Where("id = ?", id).Count(&n)
	if n == 0 {
		return errs.NotFound("角色不存在")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		for _, mid := range menuIDs {
			if mid == "" {
				continue
			}
			if err := tx.Create(&model.RoleMenu{RoleID: id, MenuID: mid}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.authz.Invalidate(ctx)
	}
	return err
}
