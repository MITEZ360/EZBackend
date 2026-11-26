package migrate

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/MITEZ360/tenon/internal/model"
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
		{
			Version: 4,
			Name:    "fix_oauth_binding_unique_indexes",
			// v2 建表时把 user_id 也放进了 uk_oauth_subject，导致同一个
			// (provider, subject) 可以绑定到多个本地用户，与 resolveUser 按该组合
			// 查绑定的逻辑不一致。这里清理历史脏数据并重建为两个唯一约束。
			Up: func(ctx context.Context, db *gorm.DB) error {
				tx := db.WithContext(ctx)
				m := tx.Migrator()
				if !m.HasTable(&model.UserOAuthBinding{}) {
					return nil
				}
				// 物理清理解绑残留的软删除行，它们仍会占用唯一键
				if err := tx.Unscoped().Where("deleted_at IS NOT NULL").
					Delete(&model.UserOAuthBinding{}).Error; err != nil {
					return err
				}
				// 同一外部账号只保留最早一条绑定；派生表写法兼容 MySQL 的
				// "can't specify target table for update in FROM clause" 限制
				if err := tx.Exec(`DELETE FROM sys_user_oauth_binding WHERE id NOT IN (
					SELECT keep_id FROM (
						SELECT MIN(id) AS keep_id FROM sys_user_oauth_binding
						GROUP BY provider, subject
					) t
				)`).Error; err != nil {
					return err
				}
				// 删掉三列旧索引，AutoMigrate 才不会按同名跳过重建
				if m.HasIndex(&model.UserOAuthBinding{}, "uk_oauth_subject") {
					if err := m.DropIndex(&model.UserOAuthBinding{}, "uk_oauth_subject"); err != nil {
						return err
					}
				}
				return tx.AutoMigrate(&model.UserOAuthBinding{})
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				tx := db.WithContext(ctx)
				m := tx.Migrator()
				if !m.HasTable(&model.UserOAuthBinding{}) {
					return nil
				}
				for _, name := range []string{"uk_oauth_subject", "uk_oauth_user_provider"} {
					if m.HasIndex(&model.UserOAuthBinding{}, name) {
						if err := m.DropIndex(&model.UserOAuthBinding{}, name); err != nil {
							return err
						}
					}
				}
				return tx.Exec("CREATE UNIQUE INDEX uk_oauth_subject ON " +
					"sys_user_oauth_binding (user_id, provider, subject)").Error
			},
		},
		{
			Version: 5,
			Name:    "add_user_locale_timezone",
			// 用户级展示偏好：老用户回填为系统默认，新用户由 DDL 默认值补齐。
			// 两列都允许空字符串表示"跟随系统"，因此回填用默认值而非强制覆盖。
			Up: func(ctx context.Context, db *gorm.DB) error {
				tx := db.WithContext(ctx)
				m := tx.Migrator()
				if !m.HasTable(&model.User{}) {
					return nil
				}
				if err := tx.AutoMigrate(&model.User{}); err != nil {
					return err
				}
				if err := tx.Model(&model.User{}).
					Where("locale IS NULL OR locale = ''").
					Update("locale", "zh-CN").Error; err != nil {
					return err
				}
				return tx.Model(&model.User{}).
					Where("timezone IS NULL OR timezone = ''").
					Update("timezone", "Asia/Shanghai").Error
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				tx := db.WithContext(ctx)
				m := tx.Migrator()
				for _, col := range []string{"Locale", "Timezone"} {
					if m.HasColumn(&model.User{}, col) {
						if err := m.DropColumn(&model.User{}, col); err != nil {
							return err
						}
					}
				}
				return nil
			},
		},
		{
			// v6/v7 曾预留给国内能力但未在 main 发布，故 v8 直接接在 v5 之后；
			// 那部分能力现在以 v9 追加，已升级的库不会漏跑。
			Version: 8,
			Name:    "runtime_config_and_cluster",
			// 配置中心与集群协同：运行时配置覆盖、节点心跳、运维指令及其明细。
			// 四张表都只由本程序写入，AutoMigrate 足以覆盖建表与后续加列。
			Up: func(ctx context.Context, db *gorm.DB) error {
				return db.WithContext(ctx).AutoMigrate(
					&model.RuntimeConfig{}, &model.ClusterNode{},
					&model.ClusterCommand{}, &model.ClusterCommandTarget{})
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				return db.WithContext(ctx).Migrator().DropTable(
					&model.ClusterCommandTarget{}, &model.ClusterCommand{},
					&model.ClusterNode{}, &model.RuntimeConfig{})
			},
		},
		{
			Version: 9,
			Name:    "membership_price_col",
			// 会员等级增加充值售价列（单位：分）。
			// 只给"已存在的"会员表补列，不建表：会员表由 membership 模块的
			// Spec.Models 在装配时创建，不含该模块的发行版（如 Base）不该因为
			// 一条历史迁移就多出一张永远为空的 sys_membership_level。
			// 全新库走不到补列分支：模块建表时 Price 列已经在模型里。
			Up: func(ctx context.Context, db *gorm.DB) error {
				m := db.WithContext(ctx).Migrator()
				if !m.HasTable(&model.MembershipLevel{}) {
					return nil
				}
				return db.WithContext(ctx).AutoMigrate(&model.MembershipLevel{})
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				m := db.WithContext(ctx).Migrator()
				if m.HasColumn(&model.MembershipLevel{}, "Price") {
					return m.DropColumn(&model.MembershipLevel{}, "Price")
				}
				return nil
			},
		},
		{
			Version: 10,
			Name:    "workflow_tables",
			// 流程引擎五张表：模板、实例、任务、轨迹、委托。
			// 与 v9 同理，只为已存在的 flow 模块补建；全新库由 Spec.Models 建。
			Up: func(ctx context.Context, db *gorm.DB) error {
				m := db.WithContext(ctx).Migrator()
				if !m.HasTable(&model.Flow{}) {
					return nil
				}
				return db.WithContext(ctx).AutoMigrate(
					&model.Flow{}, &model.FlowInstance{}, &model.FlowTask{},
					&model.FlowRecord{}, &model.FlowDelegate{})
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				return db.WithContext(ctx).Migrator().DropTable(
					&model.FlowDelegate{}, &model.FlowRecord{}, &model.FlowTask{},
					&model.FlowInstance{}, &model.Flow{})
			},
		},
		{
			Version: 11,
			Name:    "flow_timeout_cron",
			// v10 时期 flow.timeout 的默认 cron 写成了 6 段（0 */5 * * * *），
			// 调度器走 cron.ParseStandard 只认 5 段，注册直接失败、任务静默不跑。
			// 只改仍是旧默认值的记录：管理员自己调过的 cron 不覆盖。
			Up: func(ctx context.Context, db *gorm.DB) error {
				if !db.WithContext(ctx).Migrator().HasTable(&model.Job{}) {
					return nil
				}
				return db.WithContext(ctx).Model(&model.Job{}).
					Where("handler = ? AND cron = ?", "flow.timeout", "0 */5 * * * *").
					Update("cron", "*/5 * * * *").Error
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				if !db.WithContext(ctx).Migrator().HasTable(&model.Job{}) {
					return nil
				}
				return db.WithContext(ctx).Model(&model.Job{}).
					Where("handler = ? AND cron = ?", "flow.timeout", "*/5 * * * *").
					Update("cron", "0 */5 * * * *").Error
			},
		},
		{
			Version: 12,
			Name:    "governance_tables",
			// 只在治理模块装进发行版时建表：baseline 已经 AutoMigrate 过一遍
			// 启用模块的 Models，若 gov_service 仍不存在，说明这套发行版没有
			// registry/route/configcenter，凭空建表只会留下空壳。
			Up: func(ctx context.Context, db *gorm.DB) error {
				// 只有当前发行版确实装了治理模块时才建表。
				// 不能拿 gov_service 是否存在做判断：老库升级时 baseline 不会重跑，
				// 那一刻任何 gov_* 表都还不存在，判断会永远为 false。
				installed := false
				for _, m := range models {
					switch m.(type) {
					case *model.GovRevision, *model.GovService, *model.GovRoute, *model.GovConfigNamespace:
						installed = true
					}
				}
				if !installed {
					return nil
				}
				return db.WithContext(ctx).AutoMigrate(
					&model.GovRevision{}, &model.GovService{}, &model.GovInstance{},
					&model.GovRoute{}, &model.GovRelease{},
					&model.GovConfigNamespace{}, &model.GovConfigItem{},
					&model.GovCommand{}, &model.GovCommandTarget{},
				)
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				return db.WithContext(ctx).Migrator().DropTable(
					&model.GovCommandTarget{}, &model.GovCommand{}, &model.GovConfigItem{},
					&model.GovConfigNamespace{}, &model.GovRelease{}, &model.GovRoute{},
					&model.GovInstance{}, &model.GovService{}, &model.GovRevision{},
				)
			},
		},
		{
			Version: 13,
			Name:    "governance_seed",
			// ① 保证全局版本分配器有且只有一行；
			// ② 把老集群节点搬进统一实例表（service_id=tenon-core），
			//    搬迁只做一次，后来的心跳由 registry/cluster 各自维护。
			Up: func(ctx context.Context, db *gorm.DB) error {
				if !db.WithContext(ctx).Migrator().HasTable(&model.GovRevision{}) {
					return nil
				}
				db = db.WithContext(ctx)
				var count int64
				if err := db.Model(&model.GovRevision{}).Where("id = 1").Count(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					if err := db.Create(&model.GovRevision{ID: 1, GlobalRev: 0}).Error; err != nil {
						return err
					}
				}
				if !db.Migrator().HasTable(&model.ClusterNode{}) {
					return nil
				}
				var nodes []model.ClusterNode
				if err := db.Find(&nodes).Error; err != nil {
					return err
				}
				now := time.Now()
				for _, n := range nodes {
					if n.Host == "" || n.Port == 0 {
						continue
					}
					inst := model.GovInstance{
						ServiceID:     "tenon-core",
						InstanceID:    n.ID,
						Name:          n.Name,
						Host:          n.Host,
						Port:          n.Port,
						Version:       n.Version,
						Weight:        100,
						Status:        n.Status,
						ConfigVersion: n.ConfigVersion,
						LastSeen:      n.LastSeen,
						StartedAt:     n.StartedAt,
					}
					if inst.LastSeen.IsZero() {
						inst.LastSeen = now
					}
					// 唯一索引 (service_id, instance_id)：重复搬迁直接跳过。
					if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&inst).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				// 未装配治理模块时 v12 不会建表，撤销也要先确认表存在，
				// 否则"回滚到某个版本再升回来"的测试/运维动作会直接失败。
				if !db.WithContext(ctx).Migrator().HasTable(&model.GovRevision{}) {
					return nil
				}
				return db.WithContext(ctx).Where("id = 1").Delete(&model.GovRevision{}).Error
			},
		},
		{
			Version: 14,
			Name:    "governance_revision",
			// v12 早期的判断依赖 gov_service 是否已存在，老库升级那一刻它必然不存在，
			// 版本分配器就被漏掉了。这里幂等补建治理面的公共表（含指令记录），
			// 不碰任何业务数据。
			Up: func(ctx context.Context, db *gorm.DB) error {
				if !db.WithContext(ctx).Migrator().HasTable(&model.GovService{}) {
					return nil
				}
				if err := db.WithContext(ctx).AutoMigrate(
					&model.GovRevision{}, &model.GovCommand{}, &model.GovCommandTarget{},
				); err != nil {
					return err
				}
				var count int64
				if err := db.WithContext(ctx).Model(&model.GovRevision{}).Where("id = 1").Count(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					return db.WithContext(ctx).Create(&model.GovRevision{ID: 1, GlobalRev: 0}).Error
				}
				return nil
			},
			Down: func(ctx context.Context, db *gorm.DB) error {
				if !db.WithContext(ctx).Migrator().HasTable(&model.GovCommand{}) {
					return nil
				}
				return db.WithContext(ctx).Migrator().DropTable(&model.GovCommandTarget{}, &model.GovCommand{})
			},
		},
	}
}
