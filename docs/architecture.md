# 架构说明

## 1. 设计目标与取舍

| 目标 | 具体取舍 |
| --- | --- |
| 成熟优先 | 一律使用社区成熟组件（Gin/GORM/Viper/Zap/…），框架只写装配、约定与领域逻辑 |
| 开箱即用 | 默认 SQLite + 进程内缓存，不装 MySQL/Redis 也能完整跑通 |
| 可演进 | 同一份代码支持单体、网关、领域服务三种形态，无需改业务代码 |
| 可观测 | 结构化日志 + 审计轨迹 + Prometheus 指标 + 健康探针，四件套内置 |
| 低认知成本 | 每个业务模块只有 `service.go` + `handler.go`，没有多余抽象层 |

框架**刻意不做**的事：不做 ORM 之外的查询 DSL、不做自研配置中心、不做自研注册中心、
不做代码生成器。这些东西要么已有成熟方案，要么会显著提高使用门槛。

---

## 2. 分层

```
cmd/keel                进程入口：解析参数 → bootstrap.New → Run
  └─ internal/bootstrap 装配与生命周期（唯一的"上帝对象"，集中管理依赖顺序）
       ├─ internal/router        路由注册（按模块拆分文件，支持按需裁剪）
       │    ├─ internal/middleware   通用中间件
       │    └─ internal/modules/*    业务：handler（协议层）+ service（领域层）
       ├─ internal/platform/*    基础设施：config logger database cache authn authz crypto idgen httpx
       └─ pkg/*                  可复用通用库：errs response query utils client discovery version
```

依赖方向严格单向：`modules → platform → pkg`，`pkg` 不反向依赖任何内部包。
业务模块之间**不互相 import**（唯一例外是 `auth` 复用 `menu` 的导航树、`job` 依赖 `audit` 的清理接口，
均以接口或服务注入方式解耦）。

### 各层职责

| 层 | 职责 | 不该做的事 |
| --- | --- | --- |
| `handler` | 解析参数、调用 service、写响应、标记审计动作 | 写 SQL、写业务规则 |
| `service` | 业务规则、事务、数据权限、缓存 | 直接读写 `gin.Context` |
| `platform` | 与框架无关的基础能力，可在任意项目复用 | 依赖具体业务模型 |
| `pkg` | 零业务语义的工具 | 依赖 `internal` |

---

## 3. 请求生命周期

```
① Recovery        捕获 panic，记录堆栈，返回 500 统一响应
② Trace           复用 X-Trace-Id / traceparent，否则生成 UUID；写入 context 与响应头
③ AccessLog       记录 method/path/route/status/cost/bytes/ip/ua；≥500ms 记 Warn
④ CORS            基于 gin-contrib/cors，白名单可配
⑤ SecurityHeaders X-Content-Type-Options / X-Frame-Options / Referrer-Policy ...
⑥ BodyLimit       超过 max_body_size 直接 413
⑦ RateLimit       按用户（登录后）或 IP 的令牌桶，5 分钟清理一次空闲桶
⑧ Metrics         http_requests_total / duration / inflight
⑨ Auth            解析令牌 → 校验会话（缓存优先，回落 DB）→ 加载权限点（5 分钟缓存）
⑩ RequirePerm     权限点匹配，`*` 或 super 直接放行
⑪ Audit           非 GET 请求记录操作日志：模块/动作/请求体/响应体/耗时/IP，异步批量落库
⑫ Handler         业务处理
```

`Timeout`、`NoCache`、`InternalOnly` 等中间件按需挂载。

---

## 4. 认证与授权

### 4.1 令牌

- **双令牌**：`access`（默认 2h）+ `refresh`（默认 168h），HS256，载荷只放轻量身份（uid/username/roles/sid）。
- **会话表兜底**：每次签发都写入 `sys_session`，中间件用 `sid` 校验会话是否被撤销。
  这样"登出 / 踢下线 / 重置密码"能立即生效，而不必等令牌过期。
- **刷新轮换**：`/auth/refresh` 每次返回新的 refresh，旧的立即失效（防重放）。
- **单点登录开关**：`security.session_single=true` 时，新登录会撤销该用户其它会话。

### 4.2 权限模型

```
用户 ──< 用户角色 >── 角色 ──< 角色菜单 >── 菜单（目录/页面/按钮）
                                              └─ perm: system:user:create
```

- 权限点命名：`模块:资源:动作`，如 `system:user:list`、`ops:job:run`。
- 超级管理员（`role.code = super` 或 `user.is_super`）拥有 `*`，跳过所有权限点校验。
- 权限点在登录后随 `GET /auth/profile` 一次性下发给前端，前端用 `can('system:user:create')` 控制按钮显隐；
  **后端仍然逐接口校验**，前端隐藏只是体验优化。

### 4.3 权限缓存与失效

```
key = authz:user:<uid>:v<authz:version>   ttl = 5min
```

任何角色/菜单/用户角色变更都会 `INCR authz:version`，使**全量**用户权限缓存失效。
这比逐个删除 key 简单可靠，代价是变更后短暂回源（可接受）。

### 4.4 数据权限

角色上的 `data_scope` 决定可见数据范围，用户取所有角色中**最宽**的一档：

| 值 | 含义 | SQL 形态 |
| --- | --- | --- |
| 1 | 全部数据 | 无附加条件 |
| 2 | 本部门 | `dept_id = ?` |
| 3 | 本部门及以下 | `dept_id IN (递归子树)` |
| 4 | 仅本人 | `id = ?` |

实现见 `internal/modules/user/service.go` 的 `ResolveScope` / `deptSubtree`：先取部门全表在内存里递归，
避免递归 CTE 对 SQLite/MySQL 的方言依赖（部门规模通常在千级以内）。

---

## 5. 缓存体系

`cache.Cache` 是统一接口（Get/Set/Delete/Exists/Incr/Expire/TTL/Kind），两种实现：

| 实现 | 触发条件 | 特点 |
| --- | --- | --- |
| `redisCache` | `redis.enabled: true` | 多实例共享，分布式锁用 `SET NX` + Lua 释放 |
| `localCache` | 默认 | 进程内 LRU（10 万条）+ 惰性过期，零依赖 |

上层不关心实现差异：`GetJSON/SetJSON/Remember/Lock` 对两者行为一致。
**注意**：多实例部署（K8s 多副本、微服务拆分）必须启用 Redis，否则会话/权限/字典缓存不共享，
表现为"有时登录状态丢失"。

典型缓存键：

| 键 | TTL | 失效方式 |
| --- | --- | --- |
| `sess:<sid>` | 会话剩余时长 | 登出/踢人时删除 |
| `authz:user:<uid>:v<n>` | 5min | 版本号递增 |
| `dict:data` | 10min | 字典变更时删除 |
| `config:public` | 5min | 参数变更时删除 |
| `login:fail:<username>` | 锁定窗口 | 登录成功清零 |

---

## 6. 错误与响应

```json
{ "code": 0, "message": "success", "data": {}, "traceId": "..." }
```

- 业务码 = HTTP 状态码 × 100 + 序号：`40001` 参数校验、`40101` 令牌过期、`40900` 冲突、`50000` 内部错误。
- 全链路只抛 `*errs.Error`，`response.Fail` 统一渲染；未知错误降级为 500 且不泄露内部细节（详情写日志）。
- 参数校验错误额外带 `data` 字段的字段级明细，前端可直接标红。

---

## 7. 数据模型

| 表 | 说明 |
| --- | --- |
| `sys_user` | 用户（含部门、状态、TOTP 密钥、最后登录信息） |
| `sys_user_recovery_code` | 两步验证恢复码（HMAC 哈希、使用时间；不设软删除） |
| `sys_role` | 角色（编码、数据范围、内置标记） |
| `sys_user_role` | 用户-角色关联 |
| `sys_menu` | 菜单/按钮（类型、路由、权限点、排序、可见性） |
| `sys_role_menu` | 角色-菜单关联 |
| `sys_dept` | 部门树 |
| `sys_session` | 登录会话（设备、IP、过期、撤销） |
| `sys_dict_type` / `sys_dict_item` | 数据字典 |
| `sys_config` | 参数配置（`config_key` / `group_name` 避开 SQL 保留字） |
| `sys_notice` | 通知公告 |
| `sys_login_log` | 登录日志 |
| `sys_operation_log` | 操作日志（含请求/响应快照） |
| `sys_job` / `sys_job_log` | 定时任务与执行日志 |
| `sys_file` | 文件元数据（SHA256、大小、扩展名、路径） |
| `sys_message` | 站内信主体（标题、正文、类型、投递范围、来源引用） |
| `sys_message_recipient` | 收件记录（消息 × 用户，含已读时间；物理删除，不参与软删） |

所有业务表主键为 20 位雪花 ID（`Base.ID` 字符串），统一带 `created_at / updated_at / deleted_at`，
软删除由 GORM 自动处理，索引建在高频查询列（username/phone/status/dept_id 等）。

`sys_message_recipient` 是唯一的例外：它不嵌 `Base`、不做软删除。收件记录是"消息副本"而非业务实体，
用户删除即物理移除，既避免 `(message_id,user_id)` 唯一索引与软删标记冲突，也让收件箱查询只扫一张表。
撤回消息只改 `sys_message.status`，收件记录保留，因此发送记录里的送达/已读统计不会因撤回而归零。

---

## 8. 异步与后台

| 组件 | 实现 | 说明 |
| --- | --- | --- |
| 审计落库 | 内存 channel（容量 2048）+ worker 批量 100 条 / 1s | 队列满时丢弃并计数告警，绝不阻塞请求 |
| 定时任务 | robfig/cron v3 + `SkipIfStillRunning` | 手动执行与定时执行互斥，防重入 |
| 会话清理 | 内置任务 `session.cleanup`（每日 03:30） | 清理过期会话 |
| 日志清理 | 内置任务 `log.cleanup`（每日 03:00） | 保留天数可配 |
| 事件总线 | `pkg/event`：进程内发布订阅，异步 worker + panic 隔离 + 队列满丢弃计数 | 解耦"业务写完"之后的副作用 |
| 停机 | HTTP → Jobs → Audit → 事件总线 → Cache → DB | 依次关闭，等待在途请求 |

---

### 事件总线

`event.Bus` 提供最小的发布订阅能力（`Subscribe` / `SubscribeSync` / `Publish` / `PublishSync`），
支持 `*` 通配订阅、异步 worker 池、panic 隔离、队列满丢弃计数。

当前内置订阅：`audit.operation` → `business_operations_total` 指标。扩展方式：

```go
bus.Subscribe("audit.operation", func(ctx context.Context, e event.Event) {
    op := e.Data.(middleware.OperationEvent)
    if op.Module == "用户管理" && op.Action == "新增用户" {
        // 发通知、同步到外部系统、刷新缓存……
    }
})
```

它只解决单进程解耦；跨进程、需要可靠投递请用 Kafka/RabbitMQ，不要把它当消息队列用。

## 9. 可观测

| 维度 | 手段 |
| --- | --- |
| 日志 | Zap JSON（文件 + 控制台彩色），按大小/天数轮转压缩；字段含 trace_id、route、status、cost_ms |
| 审计 | 登录日志、操作日志、在线用户；管理台可查、可清理、可踢人 |
| 指标 | `http_requests_total`、`http_request_duration_seconds`、`http_inflight_requests`、`business_operations_total` |
| 探针 | `/healthz`（自身+DB+缓存）、`/readyz`（仅就绪依赖） |
| 运行时 | `GET /api/v1/monitor/system`：内存、GC、协程、连接池、缓存类型、路由数 |

---

## 10. 前端架构

控制台是独立的 Vue 3 单页应用，但**交付形态与后端强绑定**：

```
web/src  ──vite build──▶  web/dist  ──go:embed──▶  bin/keel  ──▶  浏览器
   ↑                                                        ↑
 开发期（Node）                                        运行期（无 Node）
```

| 关注点 | 做法 | 原因 |
| --- | --- | --- |
| 交付 | 产物嵌进二进制 | 单文件部署，不用 nginx、不用担心静态资源版本错配 |
| 认证 | accessToken 放 localStorage，401 时单飞刷新 | 并发 401 只发一次 refresh，其余请求排队重放，避免刷新风暴 |
| 路由 | 静态路由表 + 后端菜单驱动导航 | 页面组件与权限解耦：菜单决定"能看到什么"，`v-perm` 决定"能点什么" |
| 权限 | 后端下发 `perms`，前端缓存为 Set | 超管（`*`）短路判断，O(1) 查询 |
| 主题 | CSS 变量（`--brand-*` / `--ink-*`） | 换肤只改变量，不需要重建样式 |
| 分包 | vendor / element / 按页面懒加载 | 首屏只加载登录或仪表盘所需 chunk |

前端**不做**服务端渲染，也不需要：控制台在内网、无 SEO 需求，
SSR 只会把 Node 重新塞回运行时。

## 11. 三种形态的关系

| 形态 | 加载内容 | 典型用途 |
| --- | --- | --- |
| `mono` | 全部模块 + DB + 缓存 + 定时任务 | 中小项目、单机部署、私有化交付 |
| `gateway` | 无 DB、无业务模块，仅转发 + 静态资源 + 指标 | 统一入口、灰度、鉴权前置（可选） |
| `service` | 全部基础设施，仅暴露 `micro.modules` 指定的模块 | 拆分热点模块独立扩容 |

三者共用同一套中间件、错误模型与前端，业务代码零改动。详见 [microservices.md](microservices.md)。
