package bootstrap

import (
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/authn"
)

type menuSeed struct {
	ID       string
	Name     string
	Type     string
	Path     string
	Icon     string
	Perm     string
	Sort     int
	Children []menuSeed
}

// jobSeeds 内置调度示例：均为幂等清理类任务，默认启用。
var jobSeeds = []struct {
	Name    string
	Cron    string
	Handler string
	Params  string
	Remark  string
}{
	{Name: "清理过期日志", Cron: "0 3 * * *", Handler: "log.cleanup", Params: `{"days":30}`,
		Remark: "每天 03:00 清理 30 天前的登录日志与操作日志"},
	{Name: "清理过期会话", Cron: "0 * * * *", Handler: "session.cleanup", Params: `{"days":7}`,
		Remark: "每小时回收已过期的登录会话"},
	{Name: "清理历史消息", Cron: "0 4 * * *", Handler: "message.cleanup", Params: `{"days":180}`,
		Remark: "每天 04:00 清理 180 天前已读的收件记录"},
	{Name: "清理过期资讯", Cron: "15 * * * *", Handler: "news.cleanup", Params: `{}`,
		Remark: "每小时清理超过 news.retention_days 的资讯；任务参数可单独覆盖天数"},
}

var menuSeeds = []menuSeed{
	{ID: "m_dashboard", Name: "仪表盘", Type: model.MenuPage, Path: "/dashboard", Icon: "dashboard", Perm: "dashboard:view", Sort: 1},
	{ID: "m_content", Name: "资讯中心", Type: model.MenuDir, Path: "/content", Icon: "news", Sort: 5, Children: []menuSeed{
		{ID: "m_news", Name: "资讯列表", Type: model.MenuPage, Path: "/content/news", Icon: "file-text", Perm: "content:news:list", Sort: 1, Children: []menuSeed{
			{ID: "m_news_view", Name: "查看资讯", Type: model.MenuButton, Perm: "content:news:view"},
			{ID: "m_news_del", Name: "删除资讯", Type: model.MenuButton, Perm: "content:news:delete"},
			{ID: "m_news_cleanup", Name: "手动清理", Type: model.MenuButton, Perm: "content:news:cleanup"},
		}},
		{ID: "m_news_source", Name: "数据源", Type: model.MenuPage, Path: "/content/source", Icon: "server", Perm: "content:news:list", Sort: 2, Children: []menuSeed{
			{ID: "m_news_source_edit", Name: "维护数据源", Type: model.MenuButton, Perm: "content:news:source"},
		}},
	}},
	{ID: "m_system", Name: "系统管理", Type: model.MenuDir, Path: "/system", Icon: "settings", Sort: 10, Children: []menuSeed{
		{ID: "m_user", Name: "用户管理", Type: model.MenuPage, Path: "/system/user", Icon: "users", Perm: "system:user:list", Sort: 1, Children: []menuSeed{
			{ID: "m_user_add", Name: "新增用户", Type: model.MenuButton, Perm: "system:user:create"},
			{ID: "m_user_edit", Name: "编辑用户", Type: model.MenuButton, Perm: "system:user:update"},
			{ID: "m_user_del", Name: "删除用户", Type: model.MenuButton, Perm: "system:user:delete"},
			{ID: "m_user_reset", Name: "重置密码", Type: model.MenuButton, Perm: "system:user:reset"},
			{ID: "m_user_totp", Name: "重置两步验证", Type: model.MenuButton, Perm: "system:user:reset-totp"},
			{ID: "m_user_member", Name: "设置会员", Type: model.MenuButton, Perm: "system:user:grant-membership"},
			{ID: "m_user_export", Name: "导出用户", Type: model.MenuButton, Perm: "system:user:export"},
		}},
		{ID: "m_role", Name: "角色管理", Type: model.MenuPage, Path: "/system/role", Icon: "shield", Perm: "system:role:list", Sort: 2, Children: []menuSeed{
			{ID: "m_role_add", Name: "新增角色", Type: model.MenuButton, Perm: "system:role:create"},
			{ID: "m_role_edit", Name: "编辑角色", Type: model.MenuButton, Perm: "system:role:update"},
			{ID: "m_role_del", Name: "删除角色", Type: model.MenuButton, Perm: "system:role:delete"},
			{ID: "m_role_perm", Name: "分配权限", Type: model.MenuButton, Perm: "system:role:assign"},
		}},
		{ID: "m_menu", Name: "菜单管理", Type: model.MenuPage, Path: "/system/menu", Icon: "menu", Perm: "system:menu:list", Sort: 3, Children: []menuSeed{
			{ID: "m_menu_add", Name: "新增菜单", Type: model.MenuButton, Perm: "system:menu:create"},
			{ID: "m_menu_edit", Name: "编辑菜单", Type: model.MenuButton, Perm: "system:menu:update"},
			{ID: "m_menu_del", Name: "删除菜单", Type: model.MenuButton, Perm: "system:menu:delete"},
		}},
		{ID: "m_dept", Name: "部门管理", Type: model.MenuPage, Path: "/system/dept", Icon: "org", Perm: "system:dept:list", Sort: 4, Children: []menuSeed{
			{ID: "m_dept_add", Name: "新增部门", Type: model.MenuButton, Perm: "system:dept:create"},
			{ID: "m_dept_edit", Name: "编辑部门", Type: model.MenuButton, Perm: "system:dept:update"},
			{ID: "m_dept_del", Name: "删除部门", Type: model.MenuButton, Perm: "system:dept:delete"},
		}},
		{ID: "m_dict", Name: "字典管理", Type: model.MenuPage, Path: "/system/dict", Icon: "book", Perm: "system:dict:list", Sort: 5, Children: []menuSeed{
			{ID: "m_dict_add", Name: "新增字典", Type: model.MenuButton, Perm: "system:dict:create"},
			{ID: "m_dict_edit", Name: "编辑字典", Type: model.MenuButton, Perm: "system:dict:update"},
			{ID: "m_dict_del", Name: "删除字典", Type: model.MenuButton, Perm: "system:dict:delete"},
		}},
		{ID: "m_config", Name: "参数配置", Type: model.MenuPage, Path: "/system/config", Icon: "sliders", Perm: "system:config:list", Sort: 6, Children: []menuSeed{
			{ID: "m_config_edit", Name: "修改参数", Type: model.MenuButton, Perm: "system:config:update"},
		}},
		{ID: "m_notice", Name: "通知公告", Type: model.MenuPage, Path: "/system/notice", Icon: "bell", Perm: "system:notice:list", Sort: 7, Children: []menuSeed{
			{ID: "m_notice_add", Name: "发布公告", Type: model.MenuButton, Perm: "system:notice:create"},
			{ID: "m_notice_edit", Name: "编辑公告", Type: model.MenuButton, Perm: "system:notice:update"},
			{ID: "m_notice_del", Name: "删除公告", Type: model.MenuButton, Perm: "system:notice:delete"},
		}},
		{ID: "m_message", Name: "消息中心", Type: model.MenuPage, Path: "/system/message", Icon: "message", Sort: 8, Children: []menuSeed{
			{ID: "m_message_send", Name: "发送消息", Type: model.MenuButton, Perm: "system:message:send"},
			{ID: "m_message_sent", Name: "查看发送记录", Type: model.MenuButton, Perm: "system:message:list"},
			{ID: "m_message_del", Name: "撤回消息", Type: model.MenuButton, Perm: "system:message:delete"},
		}},
		{ID: "m_membership", Name: "会员等级", Type: model.MenuPage, Path: "/system/membership", Icon: "trophy", Perm: "system:membership:list", Sort: 9, Children: []menuSeed{
			{ID: "m_membership_add", Name: "新增等级", Type: model.MenuButton, Perm: "system:membership:create"},
			{ID: "m_membership_edit", Name: "编辑等级", Type: model.MenuButton, Perm: "system:membership:update"},
			{ID: "m_membership_del", Name: "删除等级", Type: model.MenuButton, Perm: "system:membership:delete"},
		}},
		{ID: "m_apikey", Name: "API 密钥", Type: model.MenuPage, Path: "/system/apikey", Icon: "key", Perm: "system:apikey:list", Sort: 10, Children: []menuSeed{
			{ID: "m_apikey_add", Name: "签发密钥", Type: model.MenuButton, Perm: "system:apikey:create"},
			{ID: "m_apikey_edit", Name: "编辑密钥", Type: model.MenuButton, Perm: "system:apikey:update"},
			{ID: "m_apikey_rotate", Name: "轮换密钥", Type: model.MenuButton, Perm: "system:apikey:rotate"},
			{ID: "m_apikey_del", Name: "删除密钥", Type: model.MenuButton, Perm: "system:apikey:delete"},
		}},
	}},
	{ID: "m_ops", Name: "运维中心", Type: model.MenuDir, Path: "/ops", Icon: "server", Sort: 20, Children: []menuSeed{
		{ID: "m_job", Name: "定时任务", Type: model.MenuPage, Path: "/ops/job", Icon: "clock", Perm: "ops:job:list", Sort: 1, Children: []menuSeed{
			{ID: "m_job_add", Name: "新增任务", Type: model.MenuButton, Perm: "ops:job:create"},
			{ID: "m_job_edit", Name: "编辑任务", Type: model.MenuButton, Perm: "ops:job:update"},
			{ID: "m_job_del", Name: "删除任务", Type: model.MenuButton, Perm: "ops:job:delete"},
			{ID: "m_job_run", Name: "手动执行", Type: model.MenuButton, Perm: "ops:job:run"},
		}},
		{ID: "m_file", Name: "文件管理", Type: model.MenuPage, Path: "/ops/file", Icon: "folder", Perm: "ops:file:list", Sort: 2, Children: []menuSeed{
			{ID: "m_file_upload", Name: "上传文件", Type: model.MenuButton, Perm: "ops:file:upload"},
			{ID: "m_file_del", Name: "删除文件", Type: model.MenuButton, Perm: "ops:file:delete"},
		}},
		{ID: "m_loginlog", Name: "登录日志", Type: model.MenuPage, Path: "/ops/login-log", Icon: "login", Perm: "ops:loginlog:list", Sort: 3},
		{ID: "m_oplog", Name: "操作日志", Type: model.MenuPage, Path: "/ops/operation-log", Icon: "file-text", Perm: "ops:oplog:list", Sort: 4},
		{ID: "m_online", Name: "在线用户", Type: model.MenuPage, Path: "/ops/online", Icon: "wifi", Perm: "ops:online:list", Sort: 5},
		{ID: "m_monitor", Name: "服务监控", Type: model.MenuPage, Path: "/ops/monitor", Icon: "activity", Perm: "ops:monitor:view", Sort: 6},
	}},
}

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

var deptSeeds = []struct{ ID, ParentID, Name, Leader string }{{"d_root", "0", "Keel 科技", "管理员"},
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
	{"site.name", "Keel 企业管理平台", "站点名称", "basic", "显示在登录页与侧边栏", true},
	{"site.copyright", "© 2026 Keel Framework", "版权信息", "basic", "", true},
	{"security.password.min_len", "8", "密码最小长度", "security", "", false},
	{"security.login.max_fail", "5", "最大登录失败次数", "security", "超过后锁定账号", false},
	{"upload.max_size_mb", "20", "上传大小上限(MB)", "storage", "", true},
	{"mail.from_name", "Keel 通知", "邮件发件人名称", "mail", "", false},
}

// syncBuiltinMenus 幂等补齐缺失的内置菜单，并给内置超管角色授权。
// 只新增、不修改已有节点，管理员对名称、排序、图标的调整不会被打回。
func syncBuiltinMenus(db *gorm.DB, log *zap.Logger) error {
	created := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		var walk func(items []menuSeed, parent string) error
		walk = func(items []menuSeed, parent string) error {
			for i, it := range items {
				var exists int64
				if err := tx.Model(&model.Menu{}).Where("id = ?", it.ID).Count(&exists).Error; err != nil {
					return err
				}
				if exists == 0 {
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
					// 无权限点的页面属于全员能力（如消息中心），同步授权给内置普通角色。
					if m.Type == model.MenuPage && m.Perm == "" {
						var roleExists int64
						if err := tx.Model(&model.Role{}).Where("id = ?", "r_user").Count(&roleExists).Error; err != nil {
							return err
						}
						if roleExists > 0 {
							if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
								Create(&model.RoleMenu{RoleID: "r_user", MenuID: m.ID}).Error; err != nil {
								return err
							}
						}
					}
					created++
				}
				if err := walk(it.Children, it.ID); err != nil {
					return err
				}
			}
			return nil
		}
		return walk(menuSeeds, "0")
	})
	if err != nil {
		return err
	}
	if created > 0 {
		log.Info("已补齐新增的内置菜单", zap.Int("count", created))
	}
	return nil
}

// syncBuiltinJobs 幂等补齐内置定时任务。按 handler 判重，
// 管理员改过 cron 或停用后不会被重置回默认值。
func syncBuiltinJobs(db *gorm.DB, log *zap.Logger) error {
	created := 0
	for _, j := range jobSeeds {
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

// syncBuiltinLevels 幂等补齐内置会员等级。已删除的等级不会重建，
// 避免管理员清理示例数据后每次启动又冒出来。
func syncBuiltinLevels(db *gorm.DB, log *zap.Logger) error {
	created := 0
	for _, seed := range levelSeeds {
		var exists int64
		if err := db.Unscoped().Model(&model.MembershipLevel{}).
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
		if err := db.Create(&lv).Error; err != nil {
			return fmt.Errorf("初始化会员等级失败: %w", err)
		}
		created++
	}
	if created > 0 {
		log.Info("已补齐内置会员等级", zap.Int("count", created))
	}
	return nil
}

// seed 初始化基础数据；已有用户则跳过，保证幂等。
func seed(db *gorm.DB, log *zap.Logger) error {
	var count int64
	if err := db.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		// 已初始化的库也要能拿到后续版本新增的内置菜单，否则升级后功能
		// 有接口却没有入口。
		if err := syncBuiltinMenus(db, log); err != nil {
			return err
		}
		if err := syncBuiltinJobs(db, log); err != nil {
			return err
		}
		return syncBuiltinLevels(db, log)
	}
	log.Info("首次启动，开始初始化基础数据")
	now := time.Now()

	err := db.Transaction(func(tx *gorm.DB) error {
		for _, d := range deptSeeds {
			dept := model.Dept{ParentID: d.ParentID, Name: d.Name, Leader: d.Leader, Status: model.StatusEnabled}
			dept.ID, dept.CreatedAt, dept.UpdatedAt = d.ID, now, now
			if err := tx.Create(&dept).Error; err != nil {
				return fmt.Errorf("初始化部门失败: %w", err)
			}
		}

		var allMenuIDs []string
		var insertMenu func(items []menuSeed, parent string) error
		insertMenu = func(items []menuSeed, parent string) error {
			for i, it := range items {
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
		if err := insertMenu(menuSeeds, "0"); err != nil {
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
		if err := tx.Create(&model.RoleMenu{RoleID: "r_user", MenuID: "m_dashboard"}).Error; err != nil {
			return err
		}
		// 收件箱是全员能力，普通角色也应有入口（管理动作另行按权限点控制）。
		if err := tx.Create(&model.RoleMenu{RoleID: "r_user", MenuID: "m_message"}).Error; err != nil {
			return err
		}

		adminPwd, err := authn.HashPassword("Admin@123456")
		if err != nil {
			return err
		}
		demoPwd, _ := authn.HashPassword("Demo@123456")
		admin := model.User{
			Username: "admin", Password: adminPwd, Nickname: "系统管理员", RealName: "Admin",
			Email: "admin@keel.dev", Phone: "13800138000", DeptID: "d_tech",
			Status: model.StatusEnabled, IsSuper: true, Remark: "系统内置超级管理员",
		}
		demo := model.User{
			Username: "demo", Password: demoPwd, Nickname: "演示账号", RealName: "Demo",
			Email: "demo@keel.dev", Phone: "13900139000", DeptID: "d_product",
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

		for _, j := range jobSeeds {
			job := model.Job{Name: j.Name, Cron: j.Cron, Handler: j.Handler,
				Params: j.Params, Remark: j.Remark, Status: model.StatusEnabled}
			job.CreatedAt, job.UpdatedAt = now, now
			if err := tx.Create(&job).Error; err != nil {
				return fmt.Errorf("初始化定时任务失败: %w", err)
			}
		}

		notice := model.Notice{
			Title:   "欢迎使用 Keel 企业服务端框架",
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
	return syncBuiltinLevels(db, log)
}
