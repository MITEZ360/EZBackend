//go:build !windows

package bootstrap

import (
	"os"
	"syscall"
)

// execSelf 原地重新执行当前可执行文件，保持 PID 不变。
// 调用方必须先完成优雅停机，exec 成功后本函数不会返回。
func execSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}
