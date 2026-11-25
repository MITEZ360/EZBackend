package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/authn"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

// 本文件只负责底座数据：部门、字典、参数、内置角色与管理员账号。
// 菜单、任务、业务初始数据都来自各模块的声明，加模块不用改这里。

var deptSeeds = []struct{ ID, ParentID, Name, Leader string }{{"d_root", "0", "Tenon 科技", "管理员"},
	{"d_tech", "d_root", "技术中心", "张工"},
	{"d_product", "d_root", "产品中心", "李产品"},
	{"d_ops", "d_root", "运营中心", "王运营"},
}

var dictSeeds = []struct {
	Code, Name string
	Items      [][2]string
}{
	{"user_status", "用户状态", [][2]string{{"启用", "1"}, {"禁用", "0"}}},
	{"gender", "性别", [][2]string{{"未知", "0"}, {"男", "1"}, {"女", "2"}}},
	{"notice_type", "公告类型", [][2]string{{"通知", "notice"}, {"公告", "announcement"}, {"预警", "warning"}}},
	{"job_status", "任务状态", [][2]string{{"运行中", "1"}, {"已停止", "0"}}},
	{"data_scope", "数据范围", [][2]string{{"全部数据", "1"}, {"本部门", "2"}, {"本部门及以下", "3"}, {"仅本人", "4"}}},
}

var configSeeds = []struct {
	Key, Value, Name, Group, Remark string
	Public                          bool
}{
	{"site.name", "Tenon 企业管理平台", "站点名称", "basic", "显示在登录页与侧边栏", true},
	{"site.copyright", "© 2026 Tenon Framework", "版权信息", "basic", "", true},
	{"security.password.min_len", "8", "密码最小长度", "security", "", false},
	{"security.login.max_fail", "5", "最大登录失败次数", "security", "超过后锁定账号", false},
	{"upload.max_size_mb", "20", "上传大小上限(MB)", "storage", "", true},
	{"mail.from_name", "Tenon 通知", "邮件发件人名称", "mail", "", false},
}

// jobSeedsOf 汇总已启用模块声明的定时任务。
func jobSeedsOf(reg *ModuleRegistry) []modulekit.Job {
	var out []modulekit.Job
	for _, spec := range reg.specs {
		out = append(out, spec.Jobs...)
	}
	return out
}

// syncBuiltinMenus 幂等补齐缺失的内置菜单，并给内置角色授权。
//
// 对已存在的内置菜单只同步结构性字段（父节点、路由、权限点、类型），
// 不同步名称、图标、排序、可见性：前者变了说明模块调整了菜单归属或接口，
// 不跟随就会出现"菜单还挂在旧目录下、点进去路径对不上"；后者是运营字段，
// 管理员的调整不该被启动流程打回。
func syncBuiltinMenus(db *gorm.DB, log *zap.Logger, reg *ModuleRegistry) error {
	seeds := modulekit.Menus(reg.asSet())
	created, moved := 0, 0
	err := db.Transaction(func(tx *gorm.DB) error {
		var walk func(nodes []modulekit.MenuNode, parent string) error
		walk = func(nodes []modulekit.MenuNode, parent string) error {
			for i, it := range nodes {
				var cur model.Menu
				err := tx.Select("id", "parent_id", "path", "perm", "type").
					Where("id = ?", it.ID).First(&cur).Error
				switch {
				case errors.Is(err, gorm.ErrRecordNotFound):
					m := model.Menu{
						Name: it.Name, Type: it.Type, Path: it.Path, Icon: it.Icon, Perm: it.Perm,
						Sort: it.Sort, Visible: true, Status: model.StatusEnabled,
					}
					m.ID, m.ParentID = it.ID, parent
					if m.Type == "" {
						m.Type = model.MenuPage
					}
					if m.Sort == 0 {
						m.Sort = i + 1
					}
					if err := tx.Create(&m).Error; err != nil {
						return fmt.Errorf("补齐菜单失败: %w", err)
					}
					if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
						Create(&model.RoleMenu{RoleID: "r_super", MenuID: m.ID}).Error; err != nil {
						return err
					}
					// 模块声明的额外角色（如消息中心授权给普通用户）。
					for _, roleID := range it.Roles {
						var roleExists int64
						if err := tx.Model(&model.Role{}).Where("id = ?", roleID).Count(&roleExists).Error; err != nil {
							return err
						}
						if roleExists == 0 {
							continue
						}
						if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
							Create(&model.RoleMenu{RoleID: roleID, MenuID: m.ID}).Error; err != nil {
							return err
						}
					}
					created++
				case err != nil:
					return err
				default:
					// 只在字段真的变了时写库，避免每次启动刷新 updated_at。
					want := map[string]any{}
					if cur.ParentID != parent {
						want["parent_id"] = parent
					}
					if cur.Path != it.Path {
						want["path"] = it.Path
					}
					if cur.Perm != it.Perm {
						want["perm"] = it.Perm
					}
					if it.Type != "" && cur.Type != it.Type {
						want["type"] = it.Type
					}
					if len(want) == 0 {
						break
					}
					if err := tx.Model(&model.Menu{}).Where("id = ?", it.ID).Updates(want).Error; err != nil {
						return fmt.Errorf("同步内置菜单失败: %w", err)
					}
					moved++
				}
				if err := walk(it.Children, it.ID); err != nil {
					return err
				}
			}
			return nil
		}
		return walk(seeds, "0")
	})
	if err != nil {
		return err
	}
	if created > 0 {
		log.Info("已补齐新增的内置菜单", zap.Int("count", created))
	}
	if moved > 0 {
		log.Info("已同步内置菜单的结构变化", zap.Int("count", moved))
	}
	return nil
}

// builtinMenuPrefix 是内置菜单的 ID 前缀，管理员自建菜单用雪花 ID。
// 两者靠前缀区分：前者跟随模块启停，后者永不被模块开关影响。
const builtinMenuPrefix = "m_"

// syncModuleScopedMenus 让内置菜单的启停跟随模块开关。
//
// 只处理「内置菜单」：管理员自建的菜单不在这里，不受影响。
// 关闭时置为停用而不是删除，重新启用后菜单、角色授权、管理员改过的
// 名称与排序都还在。
//
// 基准取数据库里的内置菜单全集，而不是注册表。这两者在编译期裁剪时不一致：
// 发行版把某个模块的导入删掉后，它的代码与声明都不存在了，但菜单记录还留在
// 库中。若用注册表当基准，这些菜单永远进不了差集，侧边栏会留下一批点进去
// 404 的死入口——这正是"先用全量版、再换精简版"最常见的翻车方式。
func syncModuleScopedMenus(db *gorm.DB, log *zap.Logger, reg *ModuleRegistry) error {
	keep := make(map[string]bool)
	modulekit.WalkMenus(modulekit.Menus(reg.asSet()), func(m modulekit.Menu) {
		keep[m.ID] = true
	})

	var ids []string
	if err := db.Model(&model.Menu{}).Pluck("id", &ids).Error; err != nil {
		return err
	}
	var on, off []string
	for _, id := range ids {
		if !strings.HasPrefix(id, builtinMenuPrefix) {
			continue // 管理员自建菜单
		}
		if keep[id] {
			on = append(on, id)
		} else {
			off = append(off, id)
		}
	}

	if len(off) > 0 {
		if err := db.Model(&model.Menu{}).Where("id IN ?", off).
			Update("status", model.StatusDisabled).Error; err != nil {
			return err
		}
	}
	if len(on) > 0 {
		if err := db.Model(&model.Menu{}).Where("id IN ?", on).
			Update("status", model.StatusEnabled).Error; err != nil {
			return err
		}
	}
	if len(off) > 0 {
		log.Info("已停用不可用的内置菜单",
			zap.Int("disabled", len(off)), zap.Int("enabled", len(on)),
			zap.String("reason", "模块未启用或未编入当前发行版"))
	}
	return nil
}

// syncBuiltinJobs 幂等补齐内置定时任务。按 handler 判重，
// 管理员改过 cron 或停用后不会被重置回默认值。
func syncBuiltinJobs(db *gorm.DB, log *zap.Logger, reg *ModuleRegistry) error {
	// 调度器（job 模块）没装进这套发行版时，sys_job 表不存在。
	// 这不是配置错误，而是"这份发行版本来就没有调度器"：模块照常装配，
	// 只是声明的任务不会落库、不会执行。启动不该因此失败。
	if !db.Migrator().HasTable(&model.Job{}) {
		if n := len(jobSeedsOf(reg)); n > 0 {
			log.Warn("当前发行版未装配定时任务模块，模块声明的任务不会调度",
				zap.Int("skipped", n), zap.String("hint", "需要调度时组装时带上 job 模块"))
		}
		return nil
	}
	created := 0
	for _, j := range jobSeedsOf(reg) {
		var exists int64
		if err := db.Unscoped().Model(&model.Job{}).Where("handler = ?", j.Handler).Count(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		job := model.Job{Name: j.Name, Cron: j.Cron, Handler: j.Handler,
			Params: j.Params, Remark: j.Remark, Status: model.StatusEnabled}
		if err := db.Create(&job).Error; err != nil {
			return fmt.Errorf("补齐内置任务失败: %w", err)
		}
		created++
	}
	if created > 0 {
		log.Info("已补齐内置定时任务", zap.Int("count", created))
	}
	return nil
}

// syncModuleData 执行各模块的幂等数据补齐（内置等级、分类等）。
func syncModuleData(db *gorm.DB, log *zap.Logger, reg *ModuleRegistry) error {
	for _, spec := range reg.specs {
		if spec.Sync == nil {
			continue
		}
		if err := spec.Sync(context.Background(), db); err != nil {
			return fmt.Errorf("模块 %s 数据初始化失败: %w", spec.Name, err)
		}
	}
	return nil
}

// seed 初始化基础数据；已有用户则跳过，保证幂等。
func seed(db *gorm.DB, log *zap.Logger, reg *ModuleRegistry) error {
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		// 已初始化的库也要能拿到后续版本新增的内置菜单，否则升级后功能
		// 有接口却没有入口。
		if err := syncBuiltinMenus(db, log, reg); err != nil {
			return err
		}
		if err := syncModuleScopedMenus(db, log, reg); err != nil {
			return err
		}
		if err := syncBuiltinJobs(db, log, reg); err != nil {
			return err
		}
		return syncModuleData(db, log, reg)
	}
	log.Info("首次启动，开始初始化基础数据")
	now := time.Now()
	// 没有 job 模块就没有 sys_job 表：首次种子也要跳过任务，否则建库直接失败。
	hasJobTable := db.Migrator().HasTable(&model.Job{})
	if !hasJobTable {
		if n := len(jobSeedsOf(reg)); n > 0 {
			log.Warn("当前发行版未装配定时任务模块，模块声明的任务不会调度",
				zap.Int("skipped", n), zap.String("hint", "需要调度时组装时带上 job 模块"))
		}
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		for _, d := range deptSeeds {
			dept := model.Dept{ParentID: d.ParentID, Name: d.Name, Leader: d.Leader, Status: model.StatusEnabled}
			dept.ID, dept.CreatedAt, dept.UpdatedAt = d.ID, now, now
			if err := tx.Create(&dept).Error; err != nil {
				return fmt.Errorf("初始化部门失败: %w", err)
			}
		}

		var allMenuIDs []string
		var insertMenu func(nodes []modulekit.MenuNode, parent string) error
		insertMenu = func(nodes []modulekit.MenuNode, parent string) error {
			for i, it := range nodes {
				m := model.Menu{
					Name: it.Name, Type: it.Type, Path: it.Path, Icon: it.Icon, Perm: it.Perm,
					Sort: it.Sort, Visible: true, Status: model.StatusEnabled,
				}
				m.ID, m.ParentID = it.ID, parent
				if m.Type == "" {
					m.Type = model.MenuPage
				}
				if m.Sort == 0 {
					m.Sort = i + 1
				}
				m.CreatedAt, m.UpdatedAt = now, now
				if err := tx.Create(&m).Error; err != nil {
					return fmt.Errorf("初始化菜单失败: %w", err)
				}
				allMenuIDs = append(allMenuIDs, m.ID)
				if err := insertMenu(it.Children, m.ID); err != nil {
					return err
				}
			}
			return nil
		}
		if err := insertMenu(modulekit.Menus(reg.asSet()), "0"); err != nil {
			return err
		}

		superRole := model.Role{Code: "super", Name: "超级管理员", Description: "拥有全部权限", DataScope: model.DataScopeAll, Status: model.StatusEnabled, Sort: 1}
		superRole.ID = "r_super"
		normalRole := model.Role{Code: "user", Name: "普通用户", Description: "仅可访问仪表盘", DataScope: model.DataScopeSelf, Status: model.StatusEnabled, Sort: 2}
		normalRole.ID = "r_user"
		for _, r := range []*model.Role{&superRole, &normalRole} {
			r.CreatedAt, r.UpdatedAt = now, now
			if err := tx.Create(r).Error; err != nil {
				return fmt.Errorf("初始化角色失败: %w", err)
			}
		}
		for _, mid := range allMenuIDs {
			if err := tx.Create(&model.RoleMenu{RoleID: "r_super", MenuID: mid}).Error; err != nil {
				return err
			}
		}
		// 模块声明里要求授权给普通角色的菜单（首页、收件箱）。
		modulekit.WalkMenus(modulekit.Menus(reg.asSet()), func(m modulekit.Menu) {
			for _, roleID := range m.Roles {
				if roleID == "r_super" {
					continue
				}
				_ = tx.Clauses(clause.OnConflict{DoNothing: true}).
					Create(&model.RoleMenu{RoleID: roleID, MenuID: m.ID}).Error
			}
		})

		adminPwd, err := authn.HashPassword("Admin@123456")
		if err != nil {
			return err
		}
		demoPwd, _ := authn.HashPassword("Demo@123456")
		admin := model.User{
			Username: "admin", Password: adminPwd, Nickname: "系统管理员", RealName: "Admin",
			Email: "admin@tenon.dev", Phone: "13800138000", DeptID: "d_tech",
			Status: model.StatusEnabled, IsSuper: true, Remark: "系统内置超级管理员",
		}
		demo := model.User{
			Username: "demo", Password: demoPwd, Nickname: "演示账号", RealName: "Demo",
			Email: "demo@tenon.dev", Phone: "13900139000", DeptID: "d_product",
			Status: model.StatusEnabled,
		}
		for _, u := range []*model.User{&admin, &demo} {
			u.CreatedAt, u.UpdatedAt = now, now
			if err := tx.Create(u).Error; err != nil {
				return fmt.Errorf("初始化用户失败: %w", err)
			}
		}
		if err := tx.Create(&model.UserRole{UserID: admin.ID, RoleID: "r_super"}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.UserRole{UserID: demo.ID, RoleID: "r_user"}).Error; err != nil {
			return err
		}

		for _, d := range dictSeeds {
			dt := model.DictType{Code: d.Code, Name: d.Name, Status: model.StatusEnabled}
			dt.CreatedAt, dt.UpdatedAt = now, now
			if err := tx.Create(&dt).Error; err != nil {
				return err
			}
			for j, it := range d.Items {
				di := model.DictItem{TypeCode: d.Code, Label: it[0], Value: it[1], Sort: j + 1, Status: model.StatusEnabled}
				di.CreatedAt, di.UpdatedAt = now, now
				if err := tx.Create(&di).Error; err != nil {
					return err
				}
			}
		}

		for _, c := range configSeeds {
			cfg := model.SysConfig{Key: c.Key, Value: c.Value, Name: c.Name, Group: c.Group, Remark: c.Remark, Public: c.Public}
			cfg.CreatedAt, cfg.UpdatedAt = now, now
			if err := tx.Create(&cfg).Error; err != nil {
				return err
			}
		}

		if hasJobTable {
			for _, j := range jobSeedsOf(reg) {
				job := model.Job{Name: j.Name, Cron: j.Cron, Handler: j.Handler,
					Params: j.Params, Remark: j.Remark, Status: model.StatusEnabled}
				job.CreatedAt, job.UpdatedAt = now, now
				if err := tx.Create(&job).Error; err != nil {
					return fmt.Errorf("初始化定时任务失败: %w", err)
				}
			}
		}

		notice := model.Notice{
			Title:   "欢迎使用 Tenon 企业服务端框架",
			Content: "本平台已内置用户、角色、权限、部门、字典、配置、日志、任务、文件等企业常用能力，可直接作为中后台系统的起点。",
			Type:    "notice", Status: model.StatusEnabled, PublishAt: &now,
		}
		notice.CreatedAt, notice.UpdatedAt = now, now
		if err := tx.Create(&notice).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return syncModuleData(db, log, reg)
}
