//go:build windows

package bootstrap

import (
	"os"
	"os/exec"
)

// execSelf 在 Windows 上启动新进程后返回，由调用方退出当前进程。
// Windows 无 exec 语义，实际拉起仍需依赖服务管理器或容器编排。
func execSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
