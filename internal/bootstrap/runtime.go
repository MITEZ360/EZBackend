package bootstrap

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// logRuntime 打印实际生效的依赖组合。
// 部署时最怕"以为连的是 Redis，其实是进程内缓存"，这里把结果明确写进日志。
func (a *App) logRuntime() {
	fields := []zap.Field{
		zap.String("mode", a.Mode()),
		zap.String("instance", a.Instance),
		zap.String("db", a.Cfg.DB.ResolveDriver()),
	}
	if a.Cache != nil {
		fields = append(fields, zap.String("cache", a.Cache.Kind()))
	} else {
		fields = append(fields, zap.String("cache", "disabled"))
	}
	if a.Limiter != nil {
		fields = append(fields, zap.String("ratelimit", a.Limiter.Kind()))
	} else {
		fields = append(fields, zap.String("ratelimit", "disabled"))
	}
	if a.Storage != nil {
		fields = append(fields, zap.String("storage", a.Storage.Kind()))
	} else {
		fields = append(fields, zap.String("storage", "disabled"))
	}
	if a.Mailer != nil {
		fields = append(fields, zap.String("mailer", a.Mailer.Kind()))
	} else {
		fields = append(fields, zap.String("mailer", "disabled"))
	}
	if a.Cfg.MultiInstance() {
		fields = append(fields, zap.Int("instances", maxInt(a.Cfg.Runtime.Instances, 1)))
	}
	a.Log.Info("依赖组件已装配", fields...)
}

// checkStateless 在多实例部署下检查仍然留在单机上的状态。
//
// 判定为多实例的条件：runtime.instances > 1 或 micro.mode=service。
// 策略：auto 仅告警、strict 直接拒绝启动、off 跳过检查。
// 单实例 mono 部署不做限制——本地缓存与本地磁盘本来就是最省资源的默认值。
func (a *App) checkStateless() error {
	mode := a.Cfg.StatelessMode()
	if mode == "off" || !a.Cfg.MultiInstance() {
		return nil
	}

	var issues []string
	if a.Cache != nil && a.Cache.Kind() == "local" {
		issues = append(issues,
			"缓存为进程内实现：验证码、登录失败计数、权限缓存、任务锁均不跨实例，请配置 redis.addr")
	}
	if a.Limiter != nil && a.Limiter.Kind() == "local" {
		issues = append(issues, "限流为进程内令牌桶：每个实例各放行一份配额，实际总量是配置值的 N 倍")
	}
	if a.Storage != nil && a.Storage.Kind() == "local" {
		issues = append(issues, "文件存储在本地磁盘：其它实例无法读取上传的文件，请配置 storage.s3")
	}
	if drv := a.Cfg.DB.ResolveDriver(); drv == "sqlite" {
		issues = append(issues, "数据库为 SQLite：多实例并发写会锁库，请改用 MySQL 或 PostgreSQL")
	}

	if len(issues) == 0 {
		a.Log.Info("无状态校验通过", zap.Int("instances", maxInt(a.Cfg.Runtime.Instances, 1)))
		return nil
	}
	if mode == "strict" {
		return fmt.Errorf("无状态校验未通过（runtime.stateless=strict）：%s", strings.Join(issues, "；"))
	}
	a.Log.Warn("多实例部署下存在本地状态，可能导致请求落到不同实例时行为不一致",
		zap.Strings("issues", issues),
		zap.String("hint", "配置 Redis / 对象存储 / 共享数据库，或显式设置 runtime.stateless=off"))
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
