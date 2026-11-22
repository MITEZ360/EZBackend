# 更新日志

遵循 [语义化版本](https://semver.org/lang/zh-CN/)：`MAJOR.MINOR.PATCH`。
重大的架构取舍记录见 [docs/decisions.md](docs/decisions.md)。

## [Unreleased]

（暂无）

## [1.1.0] - 2026-11-22

上线保障与工程成熟度集中补齐，全部配置驱动。

### 新增
- **会员体系**：多等级配置、权益键值对、开通/续费/取消、到期自动降级、权益中间件
- **API 密钥**：服务端/客户端两类机器身份，scope、IP 白名单、独立配额、轮换
- **资讯聚合**：爬虫批量推送、来源/分类白名单、双去重、游标分页、7 天滚动清理、开放接口与图标下发
- **版本化数据库迁移**：goose 版本账本，支持 up/down/up-to/down-to 与 CLI 子命令
- **备份与恢复**：tar 容器归档（数据库 + `files/` 文件资产 + 清单）、SHA-256、完整性校验、dry-run、恢复后逐表比对
- **GFS 保留策略**：按小时/天/周/月分档保留，最新一份永不删除
- **配置化 OAuth2/OIDC**：`oauth.providers` 新增渠道不改代码，字段映射、邮箱补全、自动注册与绑定
- **自动接口文档**：swag 生成 OpenAPI 2.0，Swagger UI 随二进制嵌入
- **架构约束自动化**：`make check-arch`（依赖边界 / 方言白名单 / `pkg` 签名快照）
- **启动自检**：关键依赖（数据库/缓存/存储）不可用即 fail-fast，报告挂 `app.SelfCheck`
- **跨方言契约测试**：`make test-dialect` 在真实 MySQL/PG 上验证时间/JSON/布尔/唯一索引/LIKE
- **模块可拔插**：`modules.enabled` 白名单，依赖自动补齐、基础模块常驻
- **接口回归脚本入库**：`make test-regression` 一键起停服务跑 136 项断言
- **CI / 治理**：GitHub Actions（后端/前端/文档幂等）、Dependabot、issue/PR 模板、MIT 许可

### 修复
- OAuth 绑定唯一约束与冲突处理（v4 迁移）：一个外部账号只能属于一个本地用户，冲突返回 409
- 资讯游标时间 UTC 统一、图标 `/uploads` 静态路由、Swagger 路由冲突等历史问题

## [1.0.0] - 2026-11-03

基础框架基线：工程骨架、HTTP 内核与中间件、存储/ORM/缓存、认证与 RBAC、
组织/字典/参数/公告/消息/审计/任务/文件/监控模块、Vue3 + Element Plus 管理控制台、
两步验证、依赖自动激活与无状态多实例部署。

[Unreleased]: https://github.com/MITEZ360/EZBackend/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/MITEZ360/EZBackend/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/MITEZ360/EZBackend/releases/tag/v1.0.0
