// Package menu 菜单与权限点管理。
package menu

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/utils"
)

// Service 菜单业务逻辑。
type Service struct {
	db *gorm.DB
}

// NewService 创建菜单服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// List 返回扁平菜单列表（含按钮）。
func (s *Service) List(ctx context.Context) ([]*model.Menu, error) {
	var list []*model.Menu
	err := s.db.WithContext(ctx).Order("sort ASC, created_at ASC").Find(&list).Error
	return list, err
}

// Tree 返回菜单树。
func (s *Service) Tree(ctx context.Context) ([]*model.Menu, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	return buildTree(list), nil
}

func buildTree(list []*model.Menu) []*model.Menu {
	byID := make(map[string]*model.Menu, len(list))
	for _, m := range list {
		m.Children = nil
		byID[m.ID] = m
	}
	roots := make([]*model.Menu, 0, 8)
	for _, m := range list {
		if m.ParentID == "" || m.ParentID == "0" {
			roots = append(roots, m)
			continue
		}
		if p, ok := byID[m.ParentID]; ok {
			p.Children = append(p.Children, m)
		} else {
			roots = append(roots, m)
		}
	}
	return roots
}

// CreateInput 创建菜单入参。
type CreateInput struct {
	ParentID  string `json:"parentId"`
	Name      string `json:"name" binding:"required,max=64"`
	Type      string `json:"type" binding:"required,oneof=dir menu button"`
	Path      string `json:"path" binding:"max=128"`
	Component string `json:"component" binding:"max=128"`
	Icon      string `json:"icon" binding:"max=64"`
	Perm      string `json:"perm" binding:"max=96"`
	Sort      int    `json:"sort"`
	Visible   *bool  `json:"visible"`
	KeepAlive bool   `json:"keepAlive"`
	Status    int8   `json:"status"`
}

// Create 新增菜单。
func (s *Service) Create(ctx context.Context, in *CreateInput) (*model.Menu, error) {
	if in.Perm != "" {
		var n int64
		s.db.WithContext(ctx).Model(&model.Menu{}).Where("perm = ?", in.Perm).Count(&n)
		if n > 0 {
			return nil, errs.Conflict("权限标识已存在")
		}
	}
	m := &model.Menu{
		ParentID: in.ParentID, Name: in.Name, Type: in.Type, Path: in.Path,
		Component: in.Component, Icon: in.Icon, Perm: in.Perm, Sort: in.Sort,
		KeepAlive: in.KeepAlive, Status: in.Status, Visible: true,
	}
	if m.ParentID == "" {
		m.ParentID = "0"
	}
	if in.Visible != nil {
		m.Visible = *in.Visible
	}
	if m.Status == 0 {
		m.Status = model.StatusEnabled
	}
	if err := s.db.WithContext(ctx).Select("*").Create(m).Error; err != nil {
		return nil, err
	}
	return m, nil
}

// Update 更新菜单。
func (s *Service) Update(ctx context.Context, id string, in *CreateInput) error {
	var m model.Menu
	if err := s.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return errs.NotFound("菜单不存在")
	}
	if in.ParentID == id {
		return errs.BadRequest("上级菜单不能是自身")
	}
	if in.ParentID != "" {
		// 防止形成环
		parent := in.ParentID
		for parent != "" && parent != "0" {
			if parent == id {
				return errs.BadRequest("不能移动到自身的子菜单下")
			}
			var p model.Menu
			if err := s.db.WithContext(ctx).Select("id", "parent_id").First(&p, "id = ?", parent).Error; err != nil {
				break
			}
			parent = p.ParentID
		}
	}
	updates := map[string]any{
		"parent_id": in.ParentID, "name": in.Name, "type": in.Type, "path": in.Path,
		"component": in.Component, "icon": in.Icon, "perm": in.Perm, "sort": in.Sort,
		"keep_alive": in.KeepAlive, "status": in.Status,
	}
	if in.Visible != nil {
		updates["visible"] = *in.Visible
	}
	return s.db.WithContext(ctx).Model(&model.Menu{}).Where("id = ?", id).Updates(updates).Error
}

// Delete 删除菜单，存在子节点时禁止删除。
func (s *Service) Delete(ctx context.Context, id string) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.Menu{}).Where("parent_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errs.Conflict("请先删除子菜单")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("menu_id = ?", id).Delete(&model.RoleMenu{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Menu{}, "id = ?", id).Error
	})
}

// ForUser 返回用户可见的导航菜单（不含按钮）与全部权限点。
func (s *Service) ForUser(ctx context.Context, userID string, super bool) ([]*model.Menu, []string, error) {
	var list []*model.Menu
	q := s.db.WithContext(ctx).Where("status = ?", model.StatusEnabled)
	if !super {
		q = q.Where("id IN (?)",
			s.db.Table("sys_role_menu AS rm").
				Select("rm.menu_id").
				Joins("JOIN sys_user_role AS ur ON ur.role_id = rm.role_id").
				Where("ur.user_id = ?", userID))
	}
	if err := q.Order("sort ASC").Find(&list).Error; err != nil {
		return nil, nil, err
	}
	perms := make([]string, 0, len(list))
	nav := make([]*model.Menu, 0, len(list))
	for _, m := range list {
		if m.Perm != "" {
			perms = append(perms, m.Perm)
		}
		if m.Type != model.MenuButton && m.Visible {
			nav = append(nav, m)
		}
	}
	return buildTree(nav), utils.UniqueStrings(perms), nil
}

// RoutePerms 从注册路由推导出权限点建议值（用于菜单管理页辅助填写）。
func RoutePerms(routes [][2]string) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r[1])
	}
	return utils.UniqueStrings(out)
}

var _ = errors.Is
var _ = strings.TrimSpace
