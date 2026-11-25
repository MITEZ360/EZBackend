package news

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
)

// categorySeeds 内置资讯分类。分类是推送白名单，空库会导致所有推送被拒，
// 因此预置一组常见分类；管理员改名、停用或删除后不会被重建。
var categorySeeds = []struct {
	ID, Code, Name, Color string
	Sort                  int
}{
	{ID: "cat_tech", Code: "tech", Name: "科技", Color: "#409EFF", Sort: 10},
	{ID: "cat_finance", Code: "finance", Name: "财经", Color: "#E6A23C", Sort: 20},
	{ID: "cat_society", Code: "society", Name: "社会", Color: "#67C23A", Sort: 30},
	{ID: "cat_world", Code: "world", Name: "国际", Color: "#909399", Sort: 40},
	{ID: "cat_sports", Code: "sports", Name: "体育", Color: "#F56C6C", Sort: 50},
	{ID: "cat_ent", Code: "entertainment", Name: "娱乐", Color: "#B37FEB", Sort: 60},
	{ID: "cat_general", Code: "general", Name: "综合", Color: "#606266", Sort: 70},
}

// Sync 幂等补齐内置分类，并把历史资讯里出现过、但配置表中不存在的分类
// 回填成记录。
//
// 回填只针对「已有文章」使用的编码：新推送仍然必须先有分类，
// 因此不会让爬虫绕过白名单，只是保证老数据不会变成筛不出来的孤儿。
func Sync(ctx context.Context, db *gorm.DB) error {
	log := zap.L()
	db = db.WithContext(ctx)
	created := 0
	for _, s := range categorySeeds {
		var exists int64
		if err := db.Unscoped().Model(&model.NewsCategory{}).
			Where("id = ?", s.ID).Count(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		cat := model.NewsCategory{
			Code: s.Code, Name: s.Name, Color: s.Color, Sort: s.Sort,
			Status: model.StatusEnabled,
		}
		cat.ID = s.ID
		if err := db.Create(&cat).Error; err != nil {
			return fmt.Errorf("初始化资讯分类失败: %w", err)
		}
		created++
	}
	var legacy []string
	if err := db.Model(&model.NewsArticle{}).Distinct().Pluck("category", &legacy).Error; err != nil {
		return err
	}
	for _, code := range legacy {
		if code == "" {
			continue
		}
		var exists int64
		if err := db.Unscoped().Model(&model.NewsCategory{}).
			Where("code = ?", code).Count(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		cat := model.NewsCategory{
			Code: code, Name: code, Sort: 900, Status: model.StatusEnabled,
		}
		if err := db.Create(&cat).Error; err != nil {
			return fmt.Errorf("回填历史资讯分类失败: %w", err)
		}
		created++
	}
	if created > 0 {
		log.Info("已补齐资讯分类", zap.Int("count", created))
	}
	return nil
}
