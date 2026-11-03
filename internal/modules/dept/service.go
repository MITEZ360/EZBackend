// Package dept 部门管理（树形组织架构）。
package dept

import (
	"context"

	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/utils"
)

// Service 部门业务逻辑。
type Service struct{ db *gorm.DB }

// NewService 创建部门服务。
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// Tree 返回部门树。
func (s *Service) Tree(ctx context.Context) ([]*model.Dept, error) {
	var list []*model.Dept
	if err := s.db.WithContext(ctx).Order("sort ASC, created_at ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	type countRow struct {
		DeptID string
		N      int64
	}
	var rows []countRow
	s.db.WithContext(ctx).Model(&model.User{}).Select("dept_id, count(*) AS n").Group("dept_id").Scan(&rows)
	counts := make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.DeptID] = r.N
	}
	for _, d := range list {
		d.UserCount = counts[d.ID]
	}
	return utils.BuildTree(list,
		func(d *model.Dept) string { return d.ID },
		func(d *model.Dept) string { return d.ParentID },
		func(d *model.Dept, children []*model.Dept) { d.Children = children },
		utils.DefaultIsRoot), nil
}

// Input 部门入参。
type Input struct {
	ParentID string `json:"parentId"`
	Name     string `json:"name" binding:"required,max=64" label:"部门名称"`
	Leader   string `json:"leader" binding:"max=64" label:"负责人"`
	Phone    string `json:"phone" binding:"max=32" label:"联系电话"`
	Email    string `json:"email" binding:"omitempty,email" label:"邮箱"`
	Sort     int    `json:"sort"`
	Status   *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
}

// Create 新增部门。
func (s *Service) Create(ctx context.Context, in *Input) (*model.Dept, error) {
	d := &model.Dept{ParentID: in.ParentID, Name: in.Name, Leader: in.Leader,
		Phone: in.Phone, Email: in.Email, Sort: in.Sort,
		Status: utils.Int8Or(in.Status, model.StatusEnabled)}
	if d.ParentID == "" {
		d.ParentID = "0"
	}
	if d.ParentID != "0" {
		var n int64
		s.db.WithContext(ctx).Model(&model.Dept{}).Where("id = ?", d.ParentID).Count(&n)
		if n == 0 {
			return nil, errs.BadRequest("上级部门不存在")
		}
	}
	if err := s.db.WithContext(ctx).Select("*").Create(d).Error; err != nil {
		return nil, err
	}
	return d, nil
}

// Update 更新部门。
func (s *Service) Update(ctx context.Context, id string, in *Input) error {
	if in.ParentID == id {
		return errs.BadRequest("上级部门不能是自身")
	}
	var d model.Dept
	if err := s.db.WithContext(ctx).First(&d, "id = ?", id).Error; err != nil {
		return errs.NotFound("部门不存在")
	}
	// 防止环
	parent := in.ParentID
	for parent != "" && parent != "0" {
		if parent == id {
			return errs.BadRequest("不能移动到自身的下级部门")
		}
		var p model.Dept
		if err := s.db.WithContext(ctx).Select("id", "parent_id").First(&p, "id = ?", parent).Error; err != nil {
			break
		}
		parent = p.ParentID
	}
	return s.db.WithContext(ctx).Model(&model.Dept{}).Where("id = ?", id).Updates(map[string]any{
		"parent_id": in.ParentID, "name": in.Name, "leader": in.Leader,
		"phone": in.Phone, "email": in.Email, "sort": in.Sort,
		"status": utils.Int8Or(in.Status, model.StatusEnabled),
	}).Error
}

// Delete 删除部门，存在下级或成员时禁止删除。
func (s *Service) Delete(ctx context.Context, id string) error {
	var n int64
	s.db.WithContext(ctx).Model(&model.Dept{}).Where("parent_id = ?", id).Count(&n)
	if n > 0 {
		return errs.Conflict("请先删除下级部门")
	}
	s.db.WithContext(ctx).Model(&model.User{}).Where("dept_id = ?", id).Count(&n)
	if n > 0 {
		return errs.Conflict("该部门下仍有用户，无法删除")
	}
	return s.db.WithContext(ctx).Delete(&model.Dept{}, "id = ?", id).Error
}
