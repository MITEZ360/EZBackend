// Package web 以嵌入方式打包管理控制台静态资源，保证单二进制部署。
//
// 构建流程：web/ 下执行 pnpm build 产出 web/dist，再由 go build 一并嵌入。
// 开发阶段用 pnpm dev 起 Vite，通过 /api 代理直连本地 Go 服务，无需重新编译。
package web

import "embed"

//go:embed all:dist
var FS embed.FS
