// Package apidocs 存放由 swag 从代码注解生成的 OpenAPI 描述。
//
// 生成命令见 Makefile 的 docs 目标：swag init -g internal/apidocs/general.go ...
// 生成物（docs.go / swagger.json / swagger.yaml）不要手工编辑。
//
// @title                       Keel 企业服务端框架 API
// @version                     1.0.0
// @description                 统一响应体为 {code, message, data, traceId}；code=0 表示成功。登录后接口使用 Bearer 令牌，开放接口使用 API Key。
// @contact.name                Keel Framework
// @license.name                MIT
// @BasePath                    /
// @schemes                     http https
// @securityDefinitions.apikey BearerAuth
// @in                          header
// @name                        Authorization
// @description                 格式：Bearer <accessToken>
// @securityDefinitions.apikey ApiKeyAuth
// @in                          header
// @name                        X-API-Key
// @description                 开放接口 /api/open/v1 使用 X-API-Key（或 Authorization: ApiKey <明文>）
// @tag.name                    认证
// @tag.description             登录、会话、个人中心、两步验证与第三方登录
// @tag.name                    用户与权限
// @tag.description             用户、角色、菜单、部门、会员等级
// @tag.name                    内容
// @tag.description             资讯、数据源与分类
// @tag.name                    运维
// @tag.description             定时任务、日志、文件、监控、备份与迁移
// @tag.name                    配置
// @tag.description             字典、参数、公告、消息
// @tag.name                    开放接口
// @tag.description             面向第三方程序，使用 API Key 鉴权
package apidocs
