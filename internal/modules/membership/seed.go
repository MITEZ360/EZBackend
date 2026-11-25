package membership

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
)

// levelSeeds 内置会员等级示例。权益标识是业务侧判断的依据，
// 换成自己的业务时改这里即可；已存在的等级不会被覆盖。
var levelSeeds = []struct {
	ID, Code, Name, Description, Color string
	Rank, DefaultDays                  int
	Benefits                           []model.Benefit
}{
	{ID: "lv_silver", Code: "silver", Name: "白银会员", Description: "基础权益，适合个人用户",
		Color: "#8A94A6", Rank: 10, DefaultDays: 365, Benefits: []model.Benefit{
			{Key: "report.export", Label: "导出报表", Value: "true"},
			{Key: "project.max", Label: "项目数量", Value: "5"},
			{Key: "storage.gb", Label: "存储空间", Value: "5 GB"},
		}},
	{ID: "lv_gold", Code: "gold", Name: "黄金会员", Description: "面向成长型团队，含优先支持",
		Color: "#C08A2E", Rank: 20, DefaultDays: 365, Benefits: []model.Benefit{
			{Key: "report.export", Label: "导出报表", Value: "true"},
			{Key: "project.max", Label: "项目数量", Value: "50"},
			{Key: "storage.gb", Label: "存储空间", Value: "50 GB"},
			{Key: "support.priority", Label: "优先支持", Value: "true"},
		}},
	{ID: "lv_platinum", Code: "platinum", Name: "铂金会员", Description: "不限项目数量，开放更高接口配额",
		Color: "#6D5BD0", Rank: 30, DefaultDays: 365, Benefits: []model.Benefit{
			{Key: "report.export", Label: "导出报表", Value: "true"},
			{Key: "project.max", Label: "项目数量", Value: "-1（不限）"},
			{Key: "storage.gb", Label: "存储空间", Value: "500 GB"},
			{Key: "support.priority", Label: "优先支持", Value: "true"},
			{Key: "api.qps", Label: "接口配额", Value: "200 QPS"},
		}},
}

// syncBuiltinLevels 幂等补齐内置会员等级。已删除的等级不会重建，
// 避免管理员清理示例数据后每次启动又冒出来。
func syncBuiltinLevels(ctx context.Context, db *gorm.DB) error {
	log := zap.L()
	created := 0
	for _, seed := range levelSeeds {
		var exists int64
		if err := db.WithContext(ctx).Unscoped().Model(&model.MembershipLevel{}).
			Where("id = ?", seed.ID).Count(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		lv := model.MembershipLevel{
			Code: seed.Code, Name: seed.Name, Rank: seed.Rank,
			Status: model.StatusEnabled, Description: seed.Description,
			DefaultDays: seed.DefaultDays, Color: seed.Color, Benefits: seed.Benefits,
		}
		lv.ID = seed.ID
		if err := db.WithContext(ctx).Create(&lv).Error; err != nil {
			return fmt.Errorf("初始化会员等级失败: %w", err)
		}
		created++
	}
	if created > 0 {
		log.Info("已补齐内置会员等级", zap.Int("count", created))
	}
	return nil
}

// Sync 模块数据补齐钩子，由框架在每次启动时调用。
func Sync(ctx context.Context, db *gorm.DB) error { return syncBuiltinLevels(ctx, db) }
