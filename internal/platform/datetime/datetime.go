// Package datetime 提供进程级时区装配。
//
// 框架里大量时间语义依赖 time.Local（GORM NowFunc、cron 调度、DSN loc、
// 备份文件名、会员到期解析等）。逐一改造既易遗漏又会破坏方言一致性，
// 因此在启动时一次性把 time.Local 指向配置时区，所有隐含依赖自然生效。
// 未配置或配置非法时保持进程默认时区，不影响已有部署。
package datetime

import (
	"fmt"
	"time"
)

// Configure 按 IANA 时区名设置进程的 time.Local。
// 空字符串表示沿用系统默认，不做改动。
//
// 只在值真的不同时才写：time.Local 是进程级全局变量，运行时改写与并发调用
// time.Now() 本身就是竞争（标准库只保证启动期一次性赋值的用法）。同一进程里
// 装配多个 App、或测试里反复装配时，第二次起都是空操作，不会再去碰全局状态。
func Configure(name string) error {
	if name == "" {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return fmt.Errorf("无效的时区 %q: %w", name, err)
	}
	if time.Local.String() == loc.String() {
		return nil
	}
	time.Local = loc
	return nil
}

// Name 返回当前进程时区名，供健康检查与自检报告展示。
func Name() string { return time.Local.String() }
