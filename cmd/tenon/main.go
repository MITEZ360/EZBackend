// Command tenon 是企业服务端框架的启动入口，支持单体、网关与领域服务三种形态。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/MITEZ360/tenon/internal/bootstrap"
	// 发行版清单：显式列出本二进制包含哪些业务模块，删一行即裁掉一个模块。
	"github.com/MITEZ360/tenon/internal/distribution"
	"github.com/MITEZ360/tenon/pkg/version"
)

var (
	configPath = flag.String("config", "configs/config.yaml", "配置文件路径")
	showVer    = flag.Bool("version", false, "打印版本信息")
	mode       = flag.String("mode", "", "运行形态: mono / gateway / service（覆盖配置文件）")
)

func main() {
	flag.Parse()
	if *showVer {
		fmt.Printf("%s %s %s (%s %s)\n", version.Name, distribution.Name, version.Version, version.Commit, version.BuildTime)
		return
	}
	// 运维子命令只需加载配置与数据库，不启动 HTTP 服务与定时任务
	if args := flag.Args(); len(args) > 0 {
		if _, err := bootstrap.RunCommand(args, *configPath); err != nil {
			fmt.Fprintf(os.Stderr, "命令执行失败: %v\n", err)
			os.Exit(1)
		}
		return
	}
	// -mode 通过环境变量注入，供配置装配阶段读取（装配必须先于运行期覆盖）
	if *mode != "" {
		switch *mode {
		case "mono", "gateway", "service":
			_ = os.Setenv("TENON_MICRO_MODE", *mode)
		default:
			fmt.Fprintf(os.Stderr, "无效的运行形态: %s（可选 mono / gateway / service）\n", *mode)
			os.Exit(2)
		}
	}
	// 发行版名称与模块集都是编译期决定的，启动时先亮明身份，便于核对跑的是哪个版本
	fmt.Printf("%s %s %s（配置文件 %s）\n", version.Name, distribution.Name, version.Version, *configPath)
	app, err := bootstrap.New(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "运行失败: %v\n", err)
		os.Exit(1)
	}
}
