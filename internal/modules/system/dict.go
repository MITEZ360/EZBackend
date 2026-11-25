// Package system 提供字典、参数配置与通知公告等系统级能力。
package system

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/utils"
)

// DictService 字典业务。
type DictService struct {
	db    *gorm.DB
	cache cache.Cache
}

// NewDictService 创建字典服务。
func NewDictService(db *gorm.DB, c cache.Cache) *DictService {
	return &DictService{db: db, cache: c}
}

// ListTypes 字典类型分页。
func (s *DictService) ListTypes(ctx context.Context, p *query.Params) ([]*model.DictType, int64, error) {
	allowed := map[string]string{"code": "code", "name": "name", "status": "status", "created_at": "created_at"}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.DictType{}), allowed)
	db = query.Search(db, p.Keyword, "code", "name")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.DictType
	err := db.Order(p.Order(map[string]string{"created_at": "created_at", "code": "code"}, "created_at DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	for _, t := range list {
		s.db.WithContext(ctx).Where("type_code = ?", t.Code).Order("sort ASC").Find(&t.Items)
	}
	return list, total, nil
}

// TypeInput 字典类型入参。
type TypeInput struct {
	Code   string `json:"code" binding:"required,max=64" label:"字典编码"`
	Name   string `json:"name" binding:"required,max=64" label:"字典名称"`
	Remark string `json:"remark" binding:"max=255" label:"备注"`
	Status *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
}

// CreateType 新增字典类型。
func (s *DictService) CreateType(ctx context.Context, in *TypeInput) (*model.DictType, error) {
	var n int64
	s.db.WithContext(ctx).Model(&model.DictType{}).Where("code = ?", in.Code).Count(&n)
	if n > 0 {
		return nil, errs.Conflict("字典编码已存在")
	}
	t := &model.DictType{Code: in.Code, Name: in.Name, Remark: in.Remark,
		Status: utils.Int8Or(in.Status, model.StatusEnabled)}
	if err := s.db.WithContext(ctx).Select("*").Create(t).Error; err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateType 更新字典类型。
func (s *DictService) UpdateType(ctx context.Context, id string, in *TypeInput) error {
	var t model.DictType
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return errs.NotFound("字典不存在")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if t.Code != in.Code {
			if err := tx.Model(&model.DictItem{}).Where("type_code = ?", t.Code).Update("type_code", in.Code).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.DictType{}).Where("id = ?", id).Updates(map[string]any{
			"code": in.Code, "name": in.Name, "remark": in.Remark,
			"status": utils.Int8Or(in.Status, model.StatusEnabled),
		}).Error
	})
	if err == nil {
		s.invalidate(ctx, t.Code)
		s.invalidate(ctx, in.Code)
	}
	return err
}

// DeleteType 删除字典类型及其字典项。
func (s *DictService) DeleteType(ctx context.Context, id string) error {
	var t model.DictType
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return errs.NotFound("字典不存在")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("type_code = ?", t.Code).Delete(&model.DictItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.DictType{}, "id = ?", id).Error
	})
	if err == nil {
		s.invalidate(ctx, t.Code)
	}
	return err
}

// Items 返回某字典的全部字典项。
func (s *DictService) Items(ctx context.Context, code string) ([]*model.DictItem, error) {
	var list []*model.DictItem
	err := s.db.WithContext(ctx).Where("type_code = ?", code).Order("sort ASC, created_at ASC").Find(&list).Error
	return list, err
}

// ItemInput 字典项入参。
type ItemInput struct {
	TypeCode string `json:"typeCode" binding:"required,max=64" label:"字典编码"`
	Label    string `json:"label" binding:"required,max=64" label:"显示名称"`
	Value    string `json:"value" binding:"required,max=64" label:"字典值"`
	TagColor string `json:"tagColor" binding:"max=32" label:"标签颜色"`
	Sort     int    `json:"sort"`
	Status   *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
	Remark   string `json:"remark" binding:"max=255" label:"备注"`
}

// CreateItem 新增字典项。
func (s *DictService) CreateItem(ctx context.Context, in *ItemInput) (*model.DictItem, error) {
	var n int64
	s.db.WithContext(ctx).Model(&model.DictType{}).Where("code = ?", in.TypeCode).Count(&n)
	if n == 0 {
		return nil, errs.BadRequest("字典类型不存在")
	}
	it := &model.DictItem{TypeCode: in.TypeCode, Label: in.Label, Value: in.Value,
		TagColor: in.TagColor, Sort: in.Sort, Remark: in.Remark,
		Status: utils.Int8Or(in.Status, model.StatusEnabled)}
	if err := s.db.WithContext(ctx).Select("*").Create(it).Error; err != nil {
		return nil, err
	}
	s.invalidate(ctx, in.TypeCode)
	return it, nil
}

// UpdateItem 更新字典项。
func (s *DictService) UpdateItem(ctx context.Context, id string, in *ItemInput) error {
	var it model.DictItem
	if err := s.db.WithContext(ctx).First(&it, "id = ?", id).Error; err != nil {
		return errs.NotFound("字典项不存在")
	}
	err := s.db.WithContext(ctx).Model(&model.DictItem{}).Where("id = ?", id).Updates(map[string]any{
		"label": in.Label, "value": in.Value, "tag_color": in.TagColor,
		"sort": in.Sort, "status": utils.Int8Or(in.Status, model.StatusEnabled),
		"remark": in.Remark,
	}).Error
	if err == nil {
		s.invalidate(ctx, it.TypeCode)
	}
	return err
}

// DeleteItem 删除字典项。
func (s *DictService) DeleteItem(ctx context.Context, id string) error {
	var it model.DictItem
	if err := s.db.WithContext(ctx).First(&it, "id = ?", id).Error; err != nil {
		return errs.NotFound("字典项不存在")
	}
	if err := s.db.WithContext(ctx).Delete(&model.DictItem{}, "id = ?", id).Error; err != nil {
		return err
	}
	s.invalidate(ctx, it.TypeCode)
	return nil
}

// Data 返回字典项（前端下拉使用），带 10 分钟缓存。
func (s *DictService) Data(ctx context.Context, code string) ([]*model.DictItem, error) {
	key := "dict:" + code
	var cached []*model.DictItem
	if ok, err := cache.GetJSON(ctx, s.cache, key, &cached); err == nil && ok {
		return cached, nil
	}
	list, err := s.Items(ctx, code)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []*model.DictItem{}
	}
	_ = cache.SetJSON(ctx, s.cache, key, list, 10*time.Minute)
	return list, nil
}

func (s *DictService) invalidate(ctx context.Context, code string) {
	if code != "" {
		_ = s.cache.Delete(ctx, "dict:"+code)
	}
}
