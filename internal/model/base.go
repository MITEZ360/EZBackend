// Package model 定义数据模型。所有主键使用雪花 ID 的字符串形式，避免前端精度丢失。
package model

import (
	"time"

	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/platform/idgen"
)

// Base 提供通用主键与审计字段（含软删除）。
type Base struct {
	ID        string         `gorm:"primaryKey;size:32" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate 自动生成主键。
func (b *Base) BeforeCreate(*gorm.DB) error {
	if b.ID == "" {
		b.ID = idgen.Next()
	}
	return nil
}

// 通用状态常量。
const (
	StatusDisabled int8 = 0
	StatusEnabled  int8 = 1
)

// 菜单类型。
const (
	MenuDir    = "dir"
	MenuPage   = "menu"
	MenuButton = "button"
)

// 数据范围。
const (
	DataScopeAll       int8 = 1 // 全部数据
	DataScopeDept      int8 = 2 // 本部门
	DataScopeDeptChild int8 = 3 // 本部门及下级
	DataScopeSelf      int8 = 4 // 仅本人
)
