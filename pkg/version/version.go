// Package version 保存构建期注入的版本信息。
package version

// 构建时可通过 -ldflags 注入：
// -X github.com/MITEZ360/tenon/pkg/version.Version=1.0.0
var (
	Name      = "Tenon"
	Version   = "1.0.0"
	Commit    = "dev"
	BuildTime = "unknown"
)
