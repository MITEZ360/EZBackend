package configcenter

import "github.com/keel-framework/keel/internal/platform/modulekit"

// Item 描述一个可在管理端调整的运行时配置项。
//
// 这里的清单是"白名单"：只有出现在清单里的键才会被写入覆盖表并合并进
// platform/config。数据库地址、端口、Redis 地址、密钥这类部署拓扑与
// 自举参数不在清单内，避免管理端把服务改到无法启动。
type Item struct {
	Key     string   `json:"key"`
	Group   string   `json:"group"`
	Name    string   `json:"name"`
	Desc    string   `json:"description"`
	Type    string   `json:"type"` // string / text / bool / int / float / duration / list / secret
	Unit    string   `json:"unit,omitempty"`
	Options []string `json:"options,omitempty"`
	// Choices 用于 list 类型：候选值及说明，前端渲染成多选面板而不是逗号输入框。
	Choices []Choice `json:"choices,omitempty"`
	Default string   `json:"default"`
	// Hot 为 true 表示保存后立即生效；否则需要重启进程（Restart=true）。
	Hot     bool `json:"hot"`
	Restart bool `json:"restart"`
	Secret  bool `json:"secret"`
	// Min / Max 仅对数值类型生效，指针用于区分"未限制"与"限制为 0"。
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// Choice 是 list 类型配置项的一个候选项。
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Desc  string `json:"description,omitempty"`
	// DependsOn 为依赖项，前端勾选时一并选中，与后端解析规则保持一致。
	DependsOn []string `json:"dependsOn,omitempty"`
	// Locked 为常驻项，前端置灰且不可取消。
	Locked bool `json:"locked,omitempty"`
}

// moduleChoices 直接用 modulekit 的声明生成候选项，避免与装配清单漂移。
func moduleChoices() []Choice {
	out := make([]Choice, 0, len(modulekit.Specs))
	for _, s := range modulekit.Specs {
		out = append(out, Choice{
			Value: s.Name, Label: s.Label, Desc: s.Desc,
			DependsOn: s.DependsOn, Locked: s.AlwaysOn,
		})
	}
	return out
}

// Group 是配置项分组。
type Group struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Desc  string `json:"description"`
	Items []Item `json:"items"`
}

func num(v float64) *float64 { return &v }

// items 是全部可管理配置项。顺序即页面展示顺序。
var items = []Item{
	// ---- 基础 ----
	{Key: "app.name", Group: "basic", Name: "服务名称", Type: "string",
		Desc:    "用于日志字段与两步验证发行者；修改后已绑定的验证器需要重新绑定",
		Default: "", Restart: true},
	{Key: "i18n.default_locale", Group: "basic", Name: "默认语言", Type: "string",
		Desc:    "客户端未声明语言时的兜底；改动在重启后对全局中间件生效",
		Default: "zh-CN", Options: []string{"zh-CN", "en-US"}, Restart: true},
	{Key: "app.timezone", Group: "basic", Name: "服务时区", Type: "string",
		Desc:    "影响定时任务、日志轮转与备份命名；重启后生效",
		Default: "Asia/Shanghai", Restart: true},

	// ---- 安全 ----
	{Key: "security.captcha_enabled", Group: "security", Name: "登录图形验证码", Type: "bool",
		Desc: "开启后登录必须提交验证码，可显著降低撞库成功率", Default: "false", Hot: true},
	{Key: "security.max_login_fail", Group: "security", Name: "最大登录失败次数", Type: "int",
		Desc: "达到阈值后锁定账号，0 表示不锁定", Default: "5", Hot: true, Min: num(0), Max: num(100)},
	{Key: "security.lock_duration", Group: "security", Name: "账号锁定时长", Type: "duration",
		Desc: "例如 15m、1h；登录失败达到阈值后的冷却时间", Default: "15m", Hot: true},
	{Key: "security.password_min_len", Group: "security", Name: "密码最小长度", Type: "int",
		Desc: "新密码与改密校验使用", Default: "8", Hot: true, Min: num(6), Max: num(64)},
	{Key: "security.password_strength", Group: "security", Name: "密码强度", Type: "int",
		Desc: "1 仅长度，2 字母+数字，3 大小写+数字+符号", Default: "2", Hot: true,
		Options: []string{"1", "2", "3"}, Min: num(1), Max: num(3)},
	{Key: "security.session_single", Group: "security", Name: "单点登录", Type: "bool",
		Desc: "同一账号仅保留最新会话，其他设备会被踢下线", Default: "false", Hot: true},

	// ---- 限流 ----
	{Key: "rate_limit.enabled", Group: "ratelimit", Name: "启用接口限流", Type: "bool",
		Desc: "关闭后所有接口不再限流，仅建议排障时临时关闭", Default: "true", Hot: true},
	{Key: "rate_limit.rps", Group: "ratelimit", Name: "每秒请求数", Type: "float",
		Desc: "单实例或 Redis 共享令牌桶的补充速率", Default: "200", Hot: true, Min: num(1), Max: num(100000)},
	{Key: "rate_limit.burst", Group: "ratelimit", Name: "突发容量", Type: "int",
		Desc: "允许的瞬时突发请求数，建议为 rps 的 2 倍", Default: "400", Hot: true, Min: num(1)},
	{Key: "rate_limit.by_user", Group: "ratelimit", Name: "按用户限流", Type: "bool",
		Desc: "关闭后登录态接口也按 IP 限流", Default: "true", Hot: true},

	// ---- 日志 ----
	{Key: "log.level", Group: "log", Name: "日志级别", Type: "string",
		Desc:    "debug 会输出 SQL 与请求细节，生产环境建议 info 及以上",
		Default: "info", Hot: true, Options: []string{"debug", "info", "warn", "error"}},

	// ---- 存储 ----
	{Key: "storage.max_size", Group: "storage", Name: "单文件大小上限", Type: "int",
		Desc: "单位字节，例如 20971520 表示 20MB", Default: "20971520", Hot: true, Min: num(1024)},
	{Key: "storage.allow_ext", Group: "storage", Name: "允许的扩展名", Type: "list",
		Desc: "逗号分隔且不带点；留空表示不限制扩展名", Default: "", Hot: true},
	{Key: "storage.type", Group: "storage", Name: "存储类型", Type: "string",
		Desc: "auto 按 S3 配置自动选择；切换后需要重启", Default: "auto",
		Options: []string{"auto", "local", "s3"}, Restart: true},
	{Key: "storage.local_dir", Group: "storage", Name: "本地存储目录", Type: "string",
		Desc: "type=local 时的落盘路径，重启后生效", Default: "data/uploads", Restart: true},
	{Key: "storage.base_url", Group: "storage", Name: "访问前缀", Type: "string",
		Desc: "本地文件的公开访问前缀，默认 /uploads", Default: "/uploads", Restart: true},

	// ---- 令牌 ----
	{Key: "jwt.access_ttl", Group: "jwt", Name: "访问令牌有效期", Type: "duration",
		Desc: "例如 2h；仅影响新签发的令牌", Default: "2h", Hot: true},
	{Key: "jwt.refresh_ttl", Group: "jwt", Name: "刷新令牌有效期", Type: "duration",
		Desc: "例如 168h；仅影响新签发的令牌", Default: "168h", Hot: true},

	// ---- 邮件 ----
	{Key: "mail.host", Group: "mail", Name: "SMTP 主机", Type: "string",
		Desc: "留空则完全关闭邮件发送；修改后需要重启", Default: "", Restart: true},
	{Key: "mail.port", Group: "mail", Name: "SMTP 端口", Type: "int",
		Desc: "常见为 587（STARTTLS）或 465（SSL）", Default: "587", Restart: true, Min: num(1), Max: num(65535)},
	{Key: "mail.username", Group: "mail", Name: "SMTP 账号", Type: "string",
		Desc: "发信账号，通常与发件地址同域", Default: "", Restart: true},
	{Key: "mail.password", Group: "mail", Name: "SMTP 密码", Type: "secret",
		Desc: "授权码或 SMTP 密码，加密存储；提交 ****** 表示不修改", Default: "", Restart: true, Secret: true},
	{Key: "mail.from", Group: "mail", Name: "发件地址", Type: "string",
		Desc: "收件人看到的 From 地址", Default: "noreply@localhost", Restart: true},
	{Key: "mail.tls", Group: "mail", Name: "启用 TLS", Type: "bool",
		Desc: "587 端口通常需要开启 STARTTLS", Default: "true", Restart: true},

	// ---- 资讯 ----
	{Key: "news.retention_days", Group: "news", Name: "资讯保留天数", Type: "int",
		Desc: "超期资讯由清理任务物理删除，0 表示使用任务参数", Default: "7", Restart: true, Min: num(0)},

	// ---- 备份 ----
	{Key: "backup.enabled", Group: "backup", Name: "启用定时备份", Type: "bool",
		Desc: "关闭后 db.backup 任务会跳过执行", Default: "true", Restart: true},
	{Key: "backup.retain", Group: "backup", Name: "备份保留份数", Type: "int",
		Desc: "未启用分档保留时按最近 N 份清理", Default: "7", Restart: true, Min: num(1)},

	// ---- 集群 ----
	{Key: "cluster.enabled", Group: "cluster", Name: "启用集群协同", Type: "bool",
		Desc: "节点通过共享数据库登记心跳并领取运维指令；SQLite 部署自动退化为单机", Default: "true", Restart: true},
	{Key: "cluster.advertise_addr", Group: "cluster", Name: "节点对外地址", Type: "string",
		Desc: "如 10.0.0.12:9000，用于在节点列表里区分机器；留空时显示 hostname:port", Default: "", Restart: true},
	{Key: "cluster.heartbeat", Group: "cluster", Name: "心跳间隔", Type: "duration",
		Desc: "节点上报存活状态的间隔，离线判定为其 3 倍", Default: "15s", Restart: true},

	// ---- 业务模块 ----
	// 模块开关决定装配范围（路由、建表、菜单、定时任务），只能在启动时生效，
	// 因此标为需重启。清单来自 modulekit，与 bootstrap 解析用的是同一份数据。
	{Key: "modules.enabled", Group: "modules", Name: "启用的模块", Type: "list",
		Desc:    "白名单：只启用勾选的模块，其依赖会自动补齐；标记为常驻的基础模块不可取消。改动需重启才生效，重启后未勾选的模块不注册路由、不建表、不挂任务",
		Default: "", Restart: true, Choices: moduleChoices()},
}

// groupMeta 定义分组顺序与标题。
var groupMeta = []Group{
	{Name: "basic", Label: "基础信息", Desc: "服务标识与默认语言"},
	{Name: "security", Label: "登录与安全", Desc: "验证码、锁定策略与密码强度"},
	{Name: "ratelimit", Label: "接口限流", Desc: "防止突发流量与暴力破解"},
	{Name: "log", Label: "日志", Desc: "运行日志级别"},
	{Name: "storage", Label: "文件存储", Desc: "上传限制与存储后端"},
	{Name: "jwt", Label: "令牌", Desc: "访问令牌与刷新令牌有效期"},
	{Name: "mail", Label: "邮件通知", Desc: "SMTP 发信参数"},
	{Name: "news", Label: "资讯", Desc: "资讯保留窗口"},
	{Name: "backup", Label: "备份", Desc: "定时备份开关与保留策略"},
	{Name: "cluster", Label: "集群", Desc: "多节点协同与心跳"},
	{Name: "modules", Label: "业务模块", Desc: "按需启用业务能力；关闭的模块不注册路由、不建表、不挂定时任务"},
}

// itemIndex 按 key 建索引，保存时用于白名单校验。
var itemIndex = func() map[string]Item {
	m := make(map[string]Item, len(items))
	for _, it := range items {
		m[it.Key] = it
	}
	return m
}()

// DeployItem 是只读的部署参数，仅用于在页面上说明"这些项需要改配置文件"。
type DeployItem struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Desc  string `json:"description"`
}
