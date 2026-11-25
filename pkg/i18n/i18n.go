// Package i18n 提供请求级语言协商与消息目录。
//
// 设计约定：
//   - 中文原文即消息 key，未翻译的消息原样返回（渐进翻译，不破坏存量文案）；
//   - 语言来源优先级：X-Locale 显式头 > Accept-Language > 默认语言；
//   - 只影响对外响应文案，不改动审计日志与内部错误信息。
package i18n

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// DefaultLocale 是未协商到语言时的兜底，与项目历史文案一致。
	DefaultLocale = "zh-CN"
	// LocaleKey 是 gin.Context 上存放协商结果的键。
	LocaleKey = "tenon.locale"
	// LocaleHeader 允许客户端显式指定语言，优先于 Accept-Language。
	LocaleHeader = "X-Locale"
)

// catalog 按语言存放「中文原文 -> 译文」。新增语言只需加一张表；
// 表里没有的条目自动降级为中文原文，因此可以先上线骨架再逐步补齐翻译。
var catalog = map[string]map[string]string{
	"en": {
		// errs 包兜底与通用响应
		"参数校验失败":  "Invalid parameters",
		"服务内部错误":  "Internal server error",
		"success": "Success",
		"created": "Created",
		// 参数解析
		"请求体不能为空":  "Request body is empty",
		"请求参数格式错误": "Malformed request parameters",
		"参数校验错误":   "Validation failed",
		"{0}格式不正确": "Invalid {0}",
		"{0}需以字母开头，仅含字母、数字、下划线，长度 3-32": "{0} must start with a letter and contain only letters, digits or underscores (3-32 characters)",
		"{0}长度至少 8 位": "{0} must be at least 8 characters",
		// 认证与权限
		"用户名或密码错误": "Incorrect username or password",
		"密码错误":     "Incorrect password",
		"账号已被锁定":   "Account is locked",
		"登录已过期":    "Session expired",
		"登录状态无效":   "Invalid session",
		"无权访问":     "Permission denied",
		"资源不存在":    "Resource not found",
		"用户名已存在":   "Username already exists",
		"手机号已被使用":  "Phone number already in use",
		"邮箱已被使用":   "Email already in use",
		"用户不存在":    "User not found",
		"时区无效":     "Invalid timezone",
		"操作成功":     "Operation completed",
		"删除成功":     "Deleted successfully",
		"创建成功":     "Created successfully",
		"更新成功":     "Updated successfully",
		// 校验字段名（compiler 的 label 标签仍为中文，按 key 翻译）
		"用户名": "Username",
		"密码":  "Password",
		"昵称":  "Nickname",
		"姓名":  "Full name",
		"邮箱":  "Email",
		"手机号": "Phone",
		"部门":  "Department",
		"角色":  "Role",
		"性别":  "Gender",
		"状态":  "Status",
		"备注":  "Remarks",
		"头像":  "Avatar",

		// 认证、会话与第三方登录
		"请先登录":               "Please sign in first",
		"登录已过期，请重新登录":        "Session expired. Please sign in again",
		"登录凭证无效":             "Invalid credentials",
		"登录状态不匹配":            "Sign-in state mismatch",
		"登录状态保存失败":           "Failed to save sign-in state",
		"登录状态已失效，请重新登录":      "Sign-in state expired. Please sign in again",
		"登录状态已失效，请重新登录。":     "Sign-in state expired. Please sign in again.",
		"登录状态已失效，请重新发起登录":    "Sign-in state expired. Please start again",
		"登录已超时，请重新发起":        "Sign-in timed out. Please try again",
		"会话校验失败":             "Session validation failed",
		"会话不存在":              "Session not found",
		"会话不存在或已下线":          "Session not found or signed out",
		"登录状态已失效":            "Sign-in state expired",
		"缺少刷新令牌":             "Refresh token is required",
		"刷新令牌不能为空":           "Refresh token is required",
		"无权访问该资源":            "Access denied to this resource",
		"仅超级管理员可操作":          "Only a super admin can perform this action",
		"禁止访问":               "Forbidden",
		"账号已被禁用":             "Account is disabled",
		"账号已被禁用，请联系管理员":      "Account is disabled. Contact the administrator",
		"两步验证已开启":            "Two-factor authentication is enabled",
		"两步验证已开启，请先关闭后再重新绑定": "Disable two-factor authentication before rebinding",
		"请先开启两步验证":           "Enable two-factor authentication first",
		"该用户未开启两步验证":         "The user has not enabled two-factor authentication",
		"动态验证码错误":            "Incorrect verification code",
		"动态验证码错误，请确认认证器中的验证码与手机时间":       "Incorrect verification code. Check your authenticator and device time",
		"请输入动态验证码或恢复码":                   "Enter your verification code or recovery code",
		"不能重置超级管理员的两步验证":                 "Cannot reset two-factor authentication for a super admin",
		"请在个人中心关闭自己的两步验证":                "Disable your own two-factor authentication in Profile",
		"恢复码生成失败":                        "Failed to generate recovery codes",
		"动态口令生成失败":                       "Failed to generate one-time password",
		"二维码生成失败":                        "Failed to generate QR code",
		"密钥解析失败":                         "Failed to parse secret",
		"未启用该第三方登录渠道":                    "This sign-in provider is not enabled",
		"第三方授权失败，请重试":                    "Third-party authorization failed. Please try again",
		"第三方用户信息不是合法 JSON":               "Third-party user info is not valid JSON",
		"第三方用户信息缺少唯一标识，请检查 field_map.id": "Third-party user info is missing a unique ID. Check field_map.id",
		"读取第三方用户信息失败":                    "Failed to read third-party user info",
		"读取第三方用户信息失败，请重试":                "Failed to read third-party user info. Please try again",
		"构造用户信息请求失败":                     "Failed to build user info request",
		"绑定会话已失效":                        "Binding session expired",
		"绑定账号不存在，请联系管理员":                 "Bound account not found. Contact the administrator",
		"未找到绑定记录":                        "No binding record found",
		"读取绑定关系失败":                       "Failed to read bindings",
		"该渠道已绑定其它账号，请先解绑":                "This provider is already linked to another account. Unlink it first",
		"该第三方账号尚未绑定本地账号，请联系管理员或改用账号密码登录": "This third-party account is not linked. Contact the administrator or sign in with your password",
		"该第三方账号已绑定其它本地账号，请先解绑":           "This third-party account is already linked to another local account. Unlink it first",
		"该第三方账号已被绑定，请刷新后重试":              "This third-party account is already linked. Refresh and try again",
		"待绑定账号不存在":     "Account to bind not found",
		"缺少授权码或 state": "Missing authorization code or state",
		"验证码生成失败":      "Failed to generate captcha",
		"验证码错误或已过期":    "Captcha is incorrect or expired",

		// 资源、业务校验
		"接口不存在":                  "Endpoint not found",
		"资源不存在或已被删除":             "Resource not found or deleted",
		"服务不可用: ":                "Service unavailable: ",
		"服务内部错误，请联系管理员":          "Internal server error. Contact the administrator",
		"操作失败，请检查输入或联系管理员":       "Operation failed. Check your input or contact the administrator",
		"请求处理超时":                 "Request timed out",
		"请求过于频繁，请稍后再试":           "Too many requests. Please try again later",
		"来源 IP 不在白名单内":           "Source IP is not in the allowlist",
		"当前未启用版本化迁移":             "Versioned migrations are not enabled",
		"保存文件失败":                 "Failed to save file",
		"文件不存在":                  "File not found",
		"文件已丢失":                  "File is missing",
		"文件读取失败":                 "Failed to read file",
		"读取上传文件失败":               "Failed to read the uploaded file",
		"请选择要上传的文件":              "Select a file to upload",
		"文件大小超过限制":               "File size exceeds the limit",
		"创建临时文件失败":               "Failed to create temporary file",
		"不能删除当前登录账号":             "Cannot delete the current signed-in account",
		"不能禁用当前登录账号":             "Cannot disable the current signed-in account",
		"内置管理员不可删除":              "The built-in administrator cannot be deleted",
		"内置角色不可删除":               "The built-in role cannot be deleted",
		"内置超级管理员角色编码不可修改":        "The built-in super admin role code cannot be changed",
		"原密码不正确":                 "Incorrect current password",
		"新密码不能与原密码相同":            "New password must differ from the current password",
		"密码需同时包含字母与数字":           "Password must contain both letters and digits",
		"密码需包含大写字母、小写字母、数字与特殊符号": "Password must contain uppercase and lowercase letters, digits and a special character",
		"参数必须是合法 JSON":           "Parameter must be valid JSON",
		"导入文件不能超过 10MB":          "Import file cannot exceed 10 MB",
		"请选择要导入的 Excel 文件":       "Select an Excel file to import",
		"无法解析文件，请使用 .xlsx 格式的模板": "Unable to parse the file. Use the .xlsx template",
		"工作表读取失败":                "Failed to read worksheet",
		"表头不匹配，请下载最新模板后填写":       "Headers do not match. Download the latest template",
		"生成模板失败":                 "Failed to generate template",
		"导出失败":                   "Export failed",
		"游标无效":                   "Invalid cursor",
		"状态值非法":                  "Invalid status",
		"状态仅支持「启用」或「禁用」":         "Status must be enabled or disabled",
		"到期时间必须晚于当前时间":           "Expiration must be later than the current time",
		"到期时间格式不正确":              "Invalid expiration time format",
		"接收范围不合法":                "Invalid recipient scope",
		"请选择接收用户":                "Select recipients",
		"请选择接收角色":                "Select recipient roles",
		"请选择用户":                  "Select a user",
		"请选择要删除的消息":              "Select messages to delete",
		"单次最多删除 200 条消息":         "At most 200 messages can be deleted at once",
		"没有可投递的接收人，请确认所选用户或角色已启用": "No deliverable recipient. Ensure the selected users or roles are enabled",
		"消息不存在":           "Message not found",
		"消息不存在或已删除":       "Message not found or deleted",
		"标题不能为空":          "Title is required",
		"公告不存在":           "Notice not found",
		"分类不存在":           "Category not found",
		"分类编码已存在":         "Category code already exists",
		"数据源不存在":          "Data source not found",
		"数据源已停用":          "Data source is disabled",
		"数据源编码不能为空":       "Data source code is required",
		"数据源编码已存在":        "Data source code already exists",
		"资讯不存在":           "News not found",
		"资讯不存在或已过期":       "News not found or expired",
		"字典不存在":           "Dictionary not found",
		"字典类型不存在":         "Dictionary type not found",
		"字典编码已存在":         "Dictionary code already exists",
		"字典项不存在":          "Dictionary item not found",
		"参数配置项不存在":        "Configuration item not found",
		"配置项不存在":          "Configuration item not found",
		"配置键已存在":          "Configuration key already exists",
		"角色不存在":           "Role not found",
		"角色编码已存在":         "Role code already exists",
		"权限标识已存在":         "Permission identifier already exists",
		"菜单不存在":           "Menu not found",
		"上级菜单不能是自身":       "Parent menu cannot be itself",
		"请先删除子菜单":         "Delete child menus first",
		"不能移动到自身的子菜单下":    "Cannot move under its own child menu",
		"部门不存在":           "Department not found",
		"部门查询失败":          "Failed to query departments",
		"上级部门不存在":         "Parent department does not exist",
		"上级部门不能是自身":       "Parent department cannot be itself",
		"请先删除下级部门":        "Delete subordinate departments first",
		"不能移动到自身的下级部门":    "Cannot move to a subordinate department",
		"该角色下仍有用户，请先解除关联": "The role still has users. Unlink them first",
		"该部门下仍有用户，无法删除":   "The department still has users and cannot be deleted",
		"任务不存在":           "Job not found",
		"cron 表达式不合法，需为标准 5 段格式（分 时 日 月 周），如 */5 * * * *": "Invalid cron expression. Use the standard 5-field format (minute hour day month weekday), e.g. */5 * * * *",
		"执行器不存在":  "Handler not found",
		"日志不存在":   "Log not found",
		"等级不存在":   "Level not found",
		"会员等级不存在": "Membership level not found",
		"等级编码需以小写字母开头，仅含小写字母、数字与下划线（2-32 位）": "Level code must start with a lowercase letter and contain only lowercase letters, digits or underscores (2-32 characters)",
		"标识色需为 #RRGGBB 格式":     "Badge color must be in #RRGGBB format",
		"权益标识不能为空":             "Benefit key is required",
		"至少需要一个权限范围":           "At least one scope is required",
		"权限范围格式不正确":            "Invalid scope format",
		"API 密钥无效":             "Invalid API key",
		"API 密钥已过期":            "API key has expired",
		"API 密钥已停用":            "API key is disabled",
		"缺少 API 密钥":            "API key is required",
		"缺少机器身份":               "Machine identity is required",
		"该密钥无权访问此接口":           "This key is not allowed to access the endpoint",
		"调用配额已用完，请稍后再试":        "API quota exhausted. Please try again later",
		"凭证不存在":                "Credential not found",
		"请先获取密钥":               "Get a key first",
		"跳转地址必须是站内路径，以单个 / 开头": "Redirect URL must be an internal path starting with a single /",
		"跳转地址不能包含空白或控制字符":      "Redirect URL must not contain whitespace or control characters",

		// 校验器字段标签（binding label / form field label）
		"编码":     "Code",
		"名称":     "Name",
		"配置键":    "Config key",
		"配置名称":   "Config name",
		"字典名称":   "Dictionary name",
		"字典编码":   "Dictionary code",
		"显示名称":   "Display name",
		"数据值":    "Data value",
		"菜单名称":   "Menu name",
		"路由路径":   "Route path",
		"权限标识":   "Permission",
		"菜单图标":   "Menu icon",
		"排序":     "Sort order",
		"文件":     "File",
		"文件大小":   "File size",
		"文件大小上限": "File size limit",
		"任务名称":   "Job name",
		"执行器":    "Handler",
		"任务参数":   "Job parameters",
		"等级名称":   "Level name",
		"等级编码":   "Level code",
		"权益":     "Benefit",
		"权益标识":   "Benefit key",
		"权益值":    "Benefit value",
		"消息标题":   "Message title",
		"消息内容":   "Message content",
		"接收用户":   "Recipients",
		"接收角色":   "Recipient roles",
		"公告标题":   "Notice title",
		"公告内容":   "Notice content",
		"密钥名称":   "Key name",
		"权限范围":   "Scopes",
		"关键词":    "Keyword",
		"开始时间":   "Start time",
		"结束时间":   "End time",
		"设备":     "Device",
		"浏览器":    "Browser",
		"创建时间":   "Created at",
		"更新时间":   "Updated at",
		"过期时间":   "Expires at",
		"描述":     "Description",
		"真实姓名":   "Full name",
		"头像地址":   "Avatar URL",
		"部门名称":   "Department name",
		"上级部门":   "Parent department",
		"角色名称":   "Role name",
		"角色编码":   "Role code",
		"数据范围":   "Data scope",
		"旧密码":    "Current password",
		"新密码":    "New password",
		"确认密码":   "Confirm password",
		"动态验证码":  "Verification code",
		"恢复码":    "Recovery code",
		"客户端 ID": "Client ID",
		"客户端密钥":  "Client secret",
		"回调地址":   "Redirect URI",
		"来源":     "Source",
		"数据源":    "Data source",
		"分类":     "Category",
		"资讯":     "News",
		"标题":     "Title",
		"内容":     "Content",

		// 动态消息模板：service 层用 Tf(ctx, ...) 渲染，占位符为 {0} {1} ...
		"文件大小超过限制（最大 {0}）":          "File size exceeds the limit (max {0})",
		"不支持的文件类型: {0}":             "Unsupported file type: {0}",
		"账号已锁定，请 {0} 分钟后再试":         "Account locked. Try again in {0} minutes",
		"密码长度至少 {0} 位":              "Password must be at least {0} characters",
		"第三方用户信息接口返回 {0}":           "Third-party user info endpoint returned HTTP {0}",
		"第 {0} 行读取失败":               "Failed to read row {0}",
		"单次最多导入 {0} 行，请拆分后重试":       "At most {0} rows can be imported at once. Split the file and retry",
		"部门不存在：{0}":                 "Department not found: {0}",
		"存在多个同名部门，无法确定：{0}":         "Multiple departments are named {0}. Cannot determine which one to use",
		"角色不存在：{0}":                 "Role not found: {0}",
		"权限范围格式不正确：{0}":             "Invalid scope format: {0}",
		"不能授予自己不具备的权限：{0}":          "Cannot grant a permission you do not have: {0}",
		"权限范围最多 {0} 个":              "At most {0} scopes are allowed",
		"IP 或网段格式不正确：{0}":           "Invalid IP address or CIDR: {0}",
		"数据源未配置：{0}，请先在后台「数据源」中创建":  "Data source {0} is not configured. Create it in Data sources first",
		"数据源已停用：{0}":                "Data source {0} is disabled",
		"该数据源下仍有 {0} 条资讯，请先停用或等待过期": "There are still {0} articles under this data source. Disable them or wait for expiry",
		"该分类下仍有 {0} 条资讯，请先停用或等待过期":  "There are still {0} articles in this category. Disable them or wait for expiry",
		"执行器不存在：{0}":                "Handler not found: {0}",
		"权益标识「{0}」不合法，需为小写字母开头的字母、数字、下划线或点": "Benefit key {0} is invalid. It must start with a lowercase letter and contain only letters, digits, underscores or dots",
		"权益标识「{0}」重复":   "Duplicate benefit key {0}",
		"等级编码「{0}」已被使用": "Level code {0} is already in use",
		"仍有 {0} 个用户属于该等级，请先调整他们的等级或将等级停用":     "There are still {0} users in this level. Reassign them or disable the level first",
		"等级「{0}」已停用，不能开通":                     "Level {0} is disabled and cannot be granted",
		"该操作需要会员权益：{0}":                       "This action requires membership benefit: {0}",
		"清理登录日志 {0} 条、操作日志 {1} 条（保留 {2} 天）":   "Cleared {0} login logs and {1} audit logs (kept {2} days)",
		"清理过期会话 {0} 条":                        "Cleared {0} expired sessions",
		"清理历史收件记录 {0} 条":                      "Cleared {0} old messages",
		"可读，{0} 字节":                           "Readable, {0} bytes",
		"{0} 个文件哈希匹配":                         "{0} file hashes matched",
		"当前数据库 {0}（{1} 字节）将被覆盖":               "Current database {0} ({1} bytes) will be overwritten",
		"通过 {0} 导入 SQL 转储（覆盖当前库对象）":           "Import SQL dump via {0} (overwrites current database objects)",
		"还原文件资产到 {0}（临时目录 + 原子替换）":            "Restore file assets to {0} (staging directory + atomic swap)",
		"注意：备份含 {0} 个文件资产，但当前非本地存储模式，将跳过文件还原": "Note: the backup has {0} file assets, but file restore is skipped in non-local storage mode",
		"备份时间 {0}，数据库 {1}，表 {2} 张":            "Backup time {0}, database {1}, {2} tables",
		"恢复后表 {0} 张，用户 {1} 人":                 "After restore: {0} tables, {1} users",
		"表清单：备份 {0} 张 / 实际 {1} 张":             "Table count: {0} in backup / {1} actual",
		"用户数：备份 {0} / 实际 {1}":                 "User count: {0} in backup / {1} actual",
		"前端资源未打包":                             "Frontend assets are not bundled",

		// 成功响应 data.message（前端常直接展示）
		"已保存":   "Saved",
		"已删除":   "Deleted",
		"已更新":   "Updated",
		"状态已更新": "Status updated",
		"权限已更新": "Permissions updated",
		"已退出登录": "Signed out",
		"资料已更新": "Profile updated",
		"偏好已更新": "Preferences updated",
		"密码已修改，其它设备已退出登录": "Password changed. Other devices have been signed out",
		"密码已重置":      "Password reset",
		"两步验证已关闭":    "Two-factor authentication disabled",
		"两步验证已重置":    "Two-factor authentication reset",
		"已注销该设备":     "Device revoked",
		"已解绑":        "Unlinked",
		"已读":         "Marked as read",
		"已撤回":        "Recalled",
		"角色已更新":      "Role updated",
		"已强制下线":      "Session terminated",
		"备份已删除":      "Backup deleted",
		"已取消会员":      "Membership cancelled",
		"今日发送次数已达上限": "Daily send limit reached",
		"发送过于频繁，请 60 秒后再试": "Too many requests, please try again in 60 seconds",
		"回调缺少订单号":          "Callback is missing the order number",
		"回调金额与订单不一致":       "Callback amount does not match the order",
		"场景":               "Scene",
		"待支付订单不存在":         "Pending order not found",
		"手机号格式不正确":         "Invalid phone number",
		"支付下单失败，请稍后重试":     "Payment request failed, please try again later",
		"支付功能未开放":          "Payments are not enabled",
		"支付方式":             "Payment method",
		"支付渠道与订单不匹配":       "Payment channel does not match the order",
		"注册未开放":            "Registration is not open",
		"用户":               "User",
		"用户名需以字母开头，仅含字母、数字、下划线或连字符，长度 3-32": "Username must be 3-32 chars, start with a letter and contain only letters, digits, underscores or hyphens",
		"短信发送失败，请稍后重试":                      "Failed to send SMS, please try again later",
		"短信登录未开放":                           "SMS sign-in is not enabled",
		"短信网关未配置":                           "SMS gateway is not configured",
		"短信验证码":                             "SMS code",
		"订单不存在":                             "Order not found",
		"订单状态不允许支付":                         "Order status does not allow payment",
		"该等级未配置售价，请联系管理员":                   "This level has no price configured, please contact the administrator",
		"账号不存在":                             "Account does not exist",
		"账号不存在，请先注册":                        "Account does not exist, please register first",
		"验证码场景不合法":                          "Invalid verification code scene",
		"会员等级":                              "Membership level",
		"订单已关闭":                             "Order closed",
	},
}

// Normalize 把各种语言标签归一化为受支持的值；未识别时返回空串。
func Normalize(tag string) string {
	s := strings.TrimSpace(strings.ReplaceAll(tag, "_", "-"))
	if s == "" {
		return ""
	}
	base := strings.ToLower(strings.SplitN(s, "-", 2)[0])
	switch base {
	case "zh":
		return "zh-CN"
	case "en":
		return "en"
	}
	return ""
}

// FromAcceptLanguage 从 Accept-Language 中取第一个受支持的语言。
// 不处理 q 值排序：浏览器与 API 客户端几乎都把首选语言放在首位，
// 完整 q 排序的收益远低于实现复杂度。
func FromAcceptLanguage(header string) string {
	for _, part := range strings.Split(header, ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if loc := Normalize(tag); loc != "" {
			return loc
		}
	}
	return ""
}

// Locale 读取当前请求协商出的语言；没有中间件或值非法时返回默认语言。
func Locale(c *gin.Context) string {
	if c == nil {
		return DefaultLocale
	}
	if v, ok := c.Get(LocaleKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return DefaultLocale
}

// Translate 按显式语言翻译，供校验器等没有 gin.Context 的层使用。
func Translate(locale, msg string) string {
	if locale == DefaultLocale || msg == "" {
		return msg
	}
	table := catalog[locale]
	if table == nil {
		return msg
	}
	if translated, ok := table[msg]; ok && translated != "" {
		return translated
	}
	return msg
}

// T 翻译消息。未配置译文、或本身就是非中文文案时原样返回。
func T(c *gin.Context, msg string) string {
	return Translate(Locale(c), msg)
}

type contextKey struct{}

// WithLocale 把协商出的语言写入 context.Context，供 service 层使用。
// Gin 中间件的 c.Set 只在 handler 层可见，service 只能通过标准 context 传递。
func WithLocale(ctx context.Context, locale string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, locale)
}

// FromContext 读取 context 中的语言；没有值时返回默认语言。
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return DefaultLocale
	}
	if v, ok := ctx.Value(contextKey{}).(string); ok {
		return Normalize(v)
	}
	return DefaultLocale
}

// Tf 按 context 语言翻译带 {0} {1} 占位符的消息模板。
// service 层构造的动态错误文案统一走这里，避免中文片段散落在拼接字符串里。
func Tf(ctx context.Context, template string, args ...any) string {
	msg := Translate(FromContext(ctx), template)
	for i, arg := range args {
		msg = strings.ReplaceAll(msg, fmt.Sprintf("{%d}", i), fmt.Sprint(arg))
	}
	return msg
}

// Middleware 解析请求语言并写入上下文，供响应层与校验层共用。
func Middleware(defaultLocale string) gin.HandlerFunc {
	if Normalize(defaultLocale) == "" {
		defaultLocale = DefaultLocale
	} else {
		defaultLocale = Normalize(defaultLocale)
	}
	return func(c *gin.Context) {
		loc := Normalize(c.GetHeader(LocaleHeader))
		if loc == "" {
			loc = FromAcceptLanguage(c.GetHeader("Accept-Language"))
		}
		if loc == "" {
			loc = defaultLocale
		}
		c.Set(LocaleKey, loc)
		c.Request = c.Request.WithContext(WithLocale(c.Request.Context(), loc))
		c.Next()
	}
}
