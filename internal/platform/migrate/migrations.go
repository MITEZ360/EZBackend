package migrate

import (
	"context"

	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
)

type definition struct {
	Version int64
	Name    string
	Up      func(ctx context.Context, db *gorm.DB) error
	Down    func(ctx context.Context, db *gorm.DB) error
}

// definitions 是全部迁移版本，按版本号升序，只允许在末尾追加。
//
// 追加新版本的示例（改列 / 回填 / 建表都写在 Go 里，方言交给 GORM）：
//
//	{Version: 4, Name: "add_user_timezone", Up: func(ctx, db) error {
//	    return db.WithContext(ctx).Migrator().AddColumn(&model.User{}, "Timezone")
//	}, Down: func(ctx, db) error {
//	    return db.WithContext(ctx).Migrator().DropColumn(&model.User{}, "Timezone")
//	}},
//
// 已发布的版本不要修改内容：版本表记录了已应用状态，改动不会重新执行，
// 反而会让新库与老库结构不一致。需要修正时一律追加新版本。
func (r *Runner) definitions() []definition {
	models := r.models
	return []definition{
		{
			Version: 1,
			Name:    "baseline",
			// baseline 等价于历史上的 AutoMigrate：把全量模型同步到最新结构。
			// 已有库重复执行是幂等的，因此从旧版升级时会直接记录版本而不改结构。
			Up: func(ctx context.Context, db *gorm.DB) error {
				return db.WithContext(ctx).AutoMigrate(models...)
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				// 倒序删除以避开外键顺序；生产环境请勿回滚 baseline。
				for i := len(models) - 1; i >= 0; i-- {
					if err := db.WithContext(ctx).Migrator().DropTable(models[i]); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			Version: 2,
			Name:    "oauth_bindings",
			Up: func(ctx context.Context, db *gorm.DB) error {
				return db.WithContext(ctx).AutoMigrate(&model.UserOAuthBinding{})
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				return db.WithContext(ctx).Migrator().DropTable(&model.UserOAuthBinding{})
			},
		},
		{
			Version: 3,
			Name:    "backfill_user_nickname",
			// 数据回填示例：把历史用户里空昵称补成账号名，避免前端显示空白。
			// 这类「改数据不改结构」的变更是 AutoMigrate 无法表达的。
			Up: func(ctx context.Context, db *gorm.DB) error {
				return db.WithContext(ctx).Model(&model.User{}).
					Where("nickname IS NULL OR nickname = ''").
					Update("nickname", gorm.Expr("username")).Error
			},
			// 回填不可逆（无法区分哪些昵称是回填产生的），回滚只撤销版本记录。
			Down: func(context.Context, *gorm.DB) error { return nil },
		},
	}
}
