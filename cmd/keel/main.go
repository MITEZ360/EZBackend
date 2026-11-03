// Command keel 是企业服务端框架的启动入口，支持单体、网关与领域服务三种形态。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/keel-framework/keel/internal/bootstrap"
	"github.com/keel-framework/keel/pkg/version"
)

var (
	configPath = flag.String("config", "configs/config.yaml", "配置文件路径")
	showVer    = flag.Bool("version", false, "打印版本信息")
	mode       = flag.String("mode", "", "运行形态: mono / gateway / service（覆盖配置文件）")
)

func main() {
	flag.Parse()
	if *showVer {
		fmt.Printf("%s %s (%s %s)\n", version.Name, version.Version, version.Commit, version.BuildTime)
		return
	}
	// -mode 通过环境变量注入，供配置装配阶段读取（装配必须先于运行期覆盖）
	if *mode != "" {
		switch *mode {
		case "mono", "gateway", "service":
			_ = os.Setenv("KEEL_MICRO_MODE", *mode)
		default:
			fmt.Fprintf(os.Stderr, "无效的运行形态: %s（可选 mono / gateway / service）\n", *mode)
			os.Exit(2)
		}
	}
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
