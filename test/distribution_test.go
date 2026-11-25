package e2e

// 集成测试直接调用 bootstrap 装配应用，不经过 cmd/keel，因此需要自己触发
// 发行版清单的空白导入，否则 modulekit 注册表为空、装配直接失败。
//
// 这里导入默认发行版：集成测试验证的是"开箱即用"的那套能力。
// 需要资讯中心等可选模块的用例，应另建带 profile 构建标签的用例。
import (
	_ "github.com/keel-framework/keel/internal/distribution"
)
