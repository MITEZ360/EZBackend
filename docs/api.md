# API 约定与接口清单

所有接口以 `/api/v1` 为前缀，请求与响应均为 `application/json; charset=utf-8`（文件下载除外）。

---

## 1. 统一响应

```json
{
  "code": 0,
  "message": "success",
  "data": {},
  "traceId": "0f3a1c9e-6d0f-4a58-bf0e-7a1f2a1c9e11"
}
```

| 字段 | 说明 |
| --- | --- |
| `code` | 0 表示成功；非 0 为业务错误码 |
| `message` | 可直接展示给用户的文案（中文） |
| `data` | 业务数据；分页时包含 `list / total / page / pageSize` |
| `traceId` | 本次请求的链路标识，与日志中的 `trace_id` 一致，排障时提供给后端即可定位 |

- 成功统一返回 200/201，失败按语义返回 4xx/5xx，**同时**给出业务码。
- 业务码规则：`HTTP 状态码 × 100 + 序号`。

### 错误码

| 业务码 | HTTP | 含义 |
| --- | --- | --- |
| `0` | 200 | 成功 |
| `40000` | 400 | 请求参数错误 |
| `40001` | 400 | 参数校验失败（`data` 为字段级明细） |
| `40100` | 401 | 未登录 |
| `40101` | 401 | 令牌过期（前端应触发刷新） |
| `40102` | 401 | 令牌无效 / 会话已失效 |
| `40103` | 401 | 账号被锁定 |
| `40104` | 401 | 账号已开启两步验证，需要补充动态码 |
| `40105` | 401 | 动态验证码错误 |
| `40106` | 401 | 恢复码无效或已被使用 |
| `40107` | 401 | API 密钥缺失、无效、已停用或已过期 |
| `40300` | 403 | 无权限 |
| `40400` | 404 | 资源不存在 |
| `40500` | 405 | 方法不允许 |
| `40900` | 409 | 资源冲突（如用户名已存在） |
| `42900` | 429 | 触发限流 |
| `50000` | 500 | 服务器内部错误 |
| `50100` | 501 | 未实现 |
| `50300` | 503 | 服务不可用（依赖故障 / 网关后端全挂） |

校验失败的 `data` 形如：

```json
{ "username": "用户名需以字母开头，仅含字母、数字、下划线，长度 3-32", "password": "密码长度至少 8 位" }
```

---

## 2. 认证

```http
POST /api/v1/auth/login
Content-Type: application/json

{ "username": "admin", "password": "Admin@123456", "captchaId": "", "captchaCode": "", "totpCode": "" }
```

```json
{
  "code": 0,
  "data": {
    "token": {
      "accessToken": "eyJhbGciOi...",
      "refreshToken": "F83-3kxNLnzo2...",
      "tokenType": "Bearer",
      "expiresIn": 7199
    },
    "user": { "id": "…", "username": "admin", "nickname": "系统管理员", "roles": ["super"], "perms": ["*"] }
  }
}
```

- 后续请求携带 `Authorization: Bearer <accessToken>`（也支持 `?token=` 与 `keel_token` Cookie，便于下载/预览场景）。
- `POST /auth/refresh` 请求体 `{"refreshToken": "..."}`，返回同上（`refreshToken` 每次轮换）。
- 验证码流程：`GET /auth/captcha` 返回 `{captchaId, imageBase64}`，登录时带上 `captchaId/captchaCode`。
- 两步验证流程：账号开启 TOTP 后，首次提交不携带第二因子会返回 `401 + code 40104`，
  前端据此进入第二步，再带 `totpCode`（6 位动态码）或 `recoveryCode`（10 位恢复码）重新提交。
  两次提交可复用同一个图形验证码（有效期 5 分钟），失败一次即作废。

### 机器身份（API 密钥）

`/api/v1/*` 面向后台用户，用登录令牌；`/api/open/v1/*` 面向程序调用方，用 API 密钥。
两套凭证互不通用——拿着用户令牌无法调用开放接口，反之亦然。

```http
GET /api/open/v1/whoami
X-API-Key: ak_s_8fQ2mN...
```

- 密钥仅在签发与轮换时返回一次，库中只存 SHA-256 哈希与前 12 位前缀。
- 失败统一返回 `401 + code 40107`，不区分「不存在」与「已停用」，避免被用来枚举凭证。
- 每次调用按凭证独立的速率与日配额限流；缓存故障时放行，不把缓存问题放大成业务不可用。
- `kind=service` 的服务端密钥可当作秘密保管；`kind=client` 随安装包分发，只能视为来源标识。
- 可配置 IP / CIDR 白名单，命中失败的请求返回 `403`。

---

## 3. 列表查询通用参数

| 参数 | 说明 |
| --- | --- |
| `page` | 页码，从 1 开始，默认 1 |
| `page_size` | 每页条数，默认 20，上限 500 |
| `keyword` / `q` | 关键字，作用于接口预设的若干列（`LIKE`，通配符已转义） |
| `sort` | 排序，`-` 前缀表示倒序，逗号分隔多列，如 `sort=-created_at,username` |
| 业务字段 | 等值过滤，如 `status=1&deptId=d_tech` |

字段后缀可表达更复杂的比较（均需在接口白名单内）：

| 后缀 | 含义 | 示例 |
| --- | --- | --- |
| 无 | 等值 | `status=1` |
| `_ne` | 不等于 | `status_ne=0` |
| `_like` | 模糊 | `username_like=li` |
| `_prefix` | 前缀 | `phone_prefix=138` |
| `_in` / `_nin` | 集合内 / 不在集合 | `status_in=0,1` |
| `_gt` / `_gte` | 大于 / 大于等于 | `createdAt_gte=2026-01-01` |
| `_lt` / `_lte` | 小于 / 小于等于 | `createdAt_lt=2026-12-31` |

**排序与过滤字段一律走白名单**，未登记的参数被忽略，避免注入与全表扫描。

过滤参数同时接受 `camelCase` 与 `snake_case`：`deptId=d_tech` 与 `dept_id=d_tech` 等价，后端统一归一化为列名后匹配白名单。排序字段仍以白名单中登记的列名为准。

分页响应：

```json
{ "code": 0, "data": { "list": [], "total": 42, "page": 1, "pageSize": 20 } }
```

---

## 4. 接口清单

> 标注「权限点」的接口在未授权时返回 403；超级管理员 `*` 全部放行。

### 4.1 认证与个人中心

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/auth/captcha` | 获取图形验证码 | 公开 |
| POST | `/auth/login` | 登录 | 公开 |
| POST | `/auth/refresh` | 刷新令牌 | 公开 |
| POST | `/auth/logout` | 登出（撤销当前会话） | 登录 |
| GET | `/auth/profile` | 个人资料 + 菜单树 + 权限点 | 登录 |
| PUT | `/auth/profile` | 修改个人资料 | 登录 |
| PUT | `/auth/password` | 修改密码（其他设备下线） | 登录 |
| POST | `/auth/totp/setup` | 生成 TOTP 密钥、`otpauth` 链接与二维码（PNG data URI） | 登录 |
| POST | `/auth/totp/enable` | 校验动态码并启用，返回 10 个一次性恢复码 | 登录 |
| POST | `/auth/totp/disable` | 关闭（接受动态码或恢复码） | 登录 |
| POST | `/auth/totp/recovery` | 重新生成恢复码（旧码立即失效） | 登录 |
| GET | `/auth/sessions` | 我的登录设备 | 登录 |
| DELETE | `/auth/sessions/:id` | 下线指定设备 | 登录 |

### 4.2 工作台

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/dashboard/overview` | 统计卡片 + 7 日趋势 + 最近登录 + 公告 + 部门分布 | `dashboard:view` |

### 4.3 用户

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/users` | 列表（手机号脱敏、数据权限过滤） | `system:user:list` |
| POST | `/users` | 新增 | `system:user:create` |
| GET | `/users/:id` | 详情 | `system:user:list` |
| PUT | `/users/:id` | 修改 | `system:user:update` |
| DELETE | `/users/:id` | 删除（软删） | `system:user:delete` |
| PUT | `/users/:id/status` | 启用/禁用 | `system:user:update` |
| PUT | `/users/:id/password` | 重置密码（强制下线） | `system:user:reset` |
| DELETE | `/users/:id/totp` | 重置两步验证（清密钥与恢复码，强制下线） | `system:user:reset-totp` |
| PUT | `/users/:id/roles` | 分配角色 | `system:user:update` |
| GET | `/options/users` | 下拉选项 | `system:user:list` |
| GET | `/exports/users` | 导出 Excel（xlsx，遵循当前筛选条件） | `system:user:export` |
| POST | `/users/batch-delete` | 批量删除，返回 `deleted` 与被跳过的 `skipped` | `system:user:delete` |
| GET | `/imports/users/template` | 下载导入模板（含填写说明工作表） | `system:user:create` |
| POST | `/imports/users` | Excel 导入，逐行校验并返回失败明细 | `system:user:create` |

导入接口为 `multipart/form-data`，字段名 `file`，单次上限 1000 行、10MB。返回结构：

```json
{
  "total": 120,
  "success": 117,
  "failed": 3,
  "truncated": false,
  "errors": [{ "row": 8, "username": "zhang3", "message": "部门不存在：研发部" }]
}
```

- `row` 为 Excel 中的真实行号，便于定位修改。
- 单行失败不影响其余行；`errors` 最多返回 100 条，超出时 `truncated=true`。
- 用户名、手机号、密码策略与普通创建接口一致，重复用户名直接判为失败而非覆盖。

两步验证相关字段约定：

```json
// POST /auth/totp/setup
{ "secret": "JBSWY3DPEHPK3PXP", "url": "otpauth://totp/...", "qrCode": "data:image/png;base64,..." }

// POST /auth/totp/enable
{ "message": "两步验证已开启", "recoveryCodes": ["7K3M9-QP2XR", "..."] }
```

- `qrCode` 由服务端生成，可直接作为 `<img src>`，不依赖外网二维码服务。
- 参数固定 30 秒 / 6 位 / SHA1，兼容 Google Authenticator、Microsoft Authenticator、
  Duo Mobile、Bitwarden、1Password、Authy 等。
- 恢复码仅在启用时明文返回一次，服务端只保存 HMAC 哈希；每个恢复码只能成功使用一次。
- 管理员重置不能作用于自己与超级管理员：前者应走个人中心，后者是兜底通道。

### 4.3.1 会员体系

角色决定"能进哪些后台页面"，会员等级决定"能享受哪些业务权益"，两者互不替代。

- **非会员**：`membershipLevelId` 为空。新用户默认即非会员。
- **有效会员**：等级启用、且未到期（`expireAt` 为空表示长期有效）。
- **到期/停用**：到期不清理数据，读取时按当前时间判定，状态变为 `expired`；
  等级被停用后，该等级用户状态为 `disabled`。两种情况均按非会员处理，权益立即失效。
- **续费**：同等级且未到期时，在原到期日上顺延；切换等级则从当前时间重新计算。
- **权益**：等级上的 `benefits` 是 `{key, label, value}` 数组。业务侧按 `key` 判断，
  值为空、`false`、`0`、`off`、`no` 时视为未开通。框架提供
  `membership.Service.RequireBenefit("report.export")` 中间件用于保护衍生业务接口。
- **内置示例**：首次启动（`db.seed=true`）会补齐白银/黄金/铂金三个示例等级，
  已存在或已删除的等级不会被重建。

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/membership/levels` | 等级分页列表，附各等级当前有效会员数 | `system:membership:list` |
| POST | `/membership/levels` | 新增等级 | `system:membership:create` |
| PUT | `/membership/levels/:id` | 修改等级（编码可改，需保持唯一） | `system:membership:update` |
| DELETE | `/membership/levels/:id` | 删除等级；仍有用户引用时返回 409，请改用停用 | `system:membership:delete` |
| GET | `/membership/catalog` | 启用中的等级目录（登录即可读） | - |
| GET | `/membership/me` | 当前用户的会员状态、权益与等级目录（登录即可读） | - |
| PUT | `/users/:id/membership` | 开通 / 续费会员 | `system:user:grant-membership` |
| DELETE | `/users/:id/membership` | 取消会员（幂等） | `system:user:grant-membership` |

开通入参的优先级为 `expireAt` > `days` > `permanent` > 等级 `defaultDays`：

```json
// PUT /users/:id/membership
{ "levelId": "lv_gold", "days": 365 }
{ "levelId": "lv_gold", "expireAt": "2027-12-31" }   // 只给日期时按当天 23:59:59 截止
{ "levelId": "lv_gold", "permanent": true }
```

`GET /membership/me` 返回：

```json
{
  "member": true,
  "state": "active",
  "level": { "id": "lv_gold", "code": "gold", "name": "黄金会员", "rank": 20, "color": "#C08A2E", "benefits": [] },
  "levelId": "lv_gold",
  "startAt": "2026-11-03T10:00:00+08:00",
  "expireAt": "2027-11-03T10:00:00+08:00",
  "daysLeft": 365,
  "benefits": { "report.export": "true", "project.max": "50" },
  "levels": [{ "code": "platinum", "name": "铂金会员" }]
}
```

- `state` 取值：`none` / `active` / `expired` / `disabled`。
- `daysLeft` 为 `-1` 表示长期有效。
- 列表接口 `/users` 额外支持 `membershipState=member|none|expired|disabled` 与
  `membershipLevelId`，其中 `member + none` 覆盖全量用户。

### 4.4 角色

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/roles` | 列表（含成员数） | `system:role:list` |
| POST | `/roles` | 新增 | `system:role:create` |
| GET | `/roles/:id` | 详情 | `system:role:list` |
| PUT | `/roles/:id` | 修改 | `system:role:update` |
| DELETE | `/roles/:id` | 删除（内置 `super` 受保护） | `system:role:delete` |
| GET | `/roles/:id/menus` | 已授权菜单 ID | `system:role:list` |
| PUT | `/roles/:id/menus` | 保存菜单授权 | `system:role:assign` |
| GET | `/options/roles` | 全部角色（下拉用） | 登录 |

### 4.5 菜单

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/menus` | 扁平列表 | `system:menu:list` |
| GET | `/menus/tree` | 树形列表 | `system:menu:list` |
| POST | `/menus` | 新增 | `system:menu:create` |
| PUT | `/menus/:id` | 修改（防环） | `system:menu:update` |
| DELETE | `/menus/:id` | 删除（有子节点拒绝） | `system:menu:delete` |

### 4.6 部门

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/depts/tree` | 部门树（含成员数） | `system:dept:list` |
| POST | `/depts` | 新增 | `system:dept:create` |
| PUT | `/depts/:id` | 修改（防环） | `system:dept:update` |
| DELETE | `/depts/:id` | 删除（有下级或成员拒绝） | `system:dept:delete` |
| GET | `/options/depts` | 部门树（下拉用） | 登录 |

### 4.7 字典

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/dicts/types` | 字典类型列表 | `system:dict:list` |
| POST | `/dicts/types` | 新增类型 | `system:dict:create` |
| PUT | `/dicts/types/:id` | 修改类型 | `system:dict:update` |
| DELETE | `/dicts/types/:id` | 删除类型（连带字典项） | `system:dict:delete` |
| GET | `/dicts/types/:code/items` | 字典项列表 | `system:dict:list` |
| POST | `/dicts/items` | 新增字典项 | `system:dict:create` |
| PUT | `/dicts/items/:id` | 修改字典项 | `system:dict:update` |
| DELETE | `/dicts/items/:id` | 删除字典项 | `system:dict:delete` |

### 4.8 参数

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/configs` | 列表（敏感值展示 `******`） | `system:config:list` |
| POST | `/configs` | 新增 | `system:config:update` |
| PUT | `/configs/:id` | 修改 | `system:config:update` |
| DELETE | `/configs/:id` | 删除 | `system:config:update` |

### 4.9 公告

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/notices` | 列表 | `system:notice:list` |
| GET | `/notices/:id` | 详情 | `system:notice:list` |
| POST | `/notices` | 发布 | `system:notice:create` |
| PUT | `/notices/:id` | 修改 | `system:notice:update` |
| DELETE | `/notices/:id` | 删除 | `system:notice:delete` |

### 4.10 消息中心

收件箱相关接口对**所有登录用户**开放，不挂权限点；发送与撤回属于管理动作。

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/messages` | 我的消息分页（`status=unread/read`、`type`、`keyword`） | 登录即可 |
| GET | `/messages/unread-count` | 未读数量（铃铛轮询） | 登录即可 |
| GET | `/messages/recent` | 最近 N 条（`limit`，默认 5） | 登录即可 |
| GET | `/messages/:id` | 消息详情（`id` 为消息 ID，非收件记录 ID） | 登录即可 |
| PUT | `/messages/:id/read` | 标记单条已读（幂等） | 登录即可 |
| PUT | `/messages/read-all` | 全部已读，可带 `?type=notice` | 登录即可 |
| DELETE | `/messages/:id` | 删除自己的一条消息 | 登录即可 |
| POST | `/messages/batch-delete` | 批量删除，body `{"ids":["..."]}` | 登录即可 |
| DELETE | `/messages/read` | 清空已读 | 登录即可 |
| POST | `/messages/send` | 发送消息 | `system:message:send` |
| GET | `/messages/sent` | 发送记录（含送达/已读统计） | `system:message:list` |
| DELETE | `/messages/sent/:id` | 撤回消息 | `system:message:delete` |

发送请求体：

```json
{
  "title": "系统维护通知",
  "content": "今晚 22:00 起升级数据库，预计 30 分钟。",
  "type": "notice",
  "link": "/system/message",
  "audience": "all",
  "roleIds": [],
  "userIds": []
}
```

`audience` 取 `all` / `role` / `user`；`link` 只接受以单个 `/` 开头的站内路径，
用于防止消息被用作跳转外站的诱饵。禁用账号不会被投递，接收人为空时返回 400。

**公告联动**：公告发布（`status=1`）时自动同步一条全员站内信，编辑正文会更新同一条消息
而不重复打扰，转为草稿或删除公告会同步撤回；重新发布时原地恢复。

### 4.11 审计与在线用户

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/logs/login` | 登录日志 | `ops:loginlog:list` |
| DELETE | `/logs/login` | 清理 N 天前 | `ops:loginlog:list` |
| GET | `/logs/operation` | 操作日志 | `ops:oplog:list` |
| GET | `/logs/operation/:id` | 详情（含请求/响应快照） | `ops:oplog:list` |
| DELETE | `/logs/operation` | 清理 N 天前 | `ops:oplog:list` |
| GET | `/online/sessions` | 在线用户 | `ops:online:list` |
| DELETE | `/online/sessions/:id` | 强制下线 | `ops:online:list` |

### 4.12 定时任务

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/jobs` | 任务列表（含成功/失败次数、下次执行） | `ops:job:list` |
| GET | `/jobs/handlers` | 已注册的处理器（可选） | `ops:job:list` |
| POST | `/jobs` | 新增 | `ops:job:create` |
| PUT | `/jobs/:id` | 修改 | `ops:job:update` |
| DELETE | `/jobs/:id` | 删除 | `ops:job:delete` |
| PUT | `/jobs/:id/status` | 启用/停止 | `ops:job:update` |
| POST | `/jobs/:id/run` | 立即执行一次 | `ops:job:run` |
| GET | `/jobs/logs` | 执行日志 | `ops:job:list` |

### 4.13 文件

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| POST | `/files` | 上传（multipart，字段名 `file`） | `ops:file:upload` |
| GET | `/files` | 列表 | `ops:file:list` |
| GET | `/files/:id/download` | 下载（支持 `?inline=1` 预览） | `ops:file:list` |
| DELETE | `/files/:id` | 删除（同时删物理文件） | `ops:file:delete` |

### 4.14 监控与缓存管理

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/monitor/system` | 运行时/内存/GC/连接池/缓存/路由数 | `ops:monitor:view` |
| GET | `/monitor/routes` | 全部注册路由 | `ops:monitor:view` |
| GET | `/routes` | 路由目录：运行时路由 + 作用/备注/权限/请求示例，支持分页与筛选 | `ops:route:list` |
| POST | `/monitor/restart` | 触发服务原地重启（高危操作） | `ops:monitor:restart` |
| GET | `/cache/overview` | 缓存后端、键分布、内存与命中率 | `ops:cache:list` |
| GET | `/cache/keys` | 缓存键分页：前缀（支持 `*`）+ 命名空间筛选 | `ops:cache:list` |
| DELETE | `/cache/keys` | 删除指定键，请求体 `{"keys":["dict:notice_type"]}` | `ops:cache:delete` |
| POST | `/cache/flush` | 按命名空间清空，`namespace` 为空表示当前应用前缀下全部；需 `confirm: true` | `ops:cache:flush` |
| GET | `/ops/config/items` | 配置中心：分组配置项、当前值/来源、热生效与需重启标记、只读部署参数 | `ops:config:view` |
| PUT | `/ops/config/items` | 保存配置，热项立即生效，需重启项在 `needRestart` 中返回 | `ops:config:update` |
| POST | `/ops/config/reload` | 从覆盖表重新加载并应用，用于多节点手工拉齐 | `ops:config:reload` |
| GET | `/ops/cluster/nodes` | 节点列表：在线状态、版本、配置版本、最后心跳 | `ops:cluster:view` |
| POST | `/ops/cluster/dispatch` | 下发 `restart` / `reload-config`，`scope=all` 表示全部在线节点 | `ops:cluster:operate` |
| GET | `/ops/cluster/commands` | 最近运维指令及各节点执行明细 | `ops:cluster:view` |

### 4.15 公开接口（无需登录）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/public/config` | 站点名称、版权、备案号、是否开启验证码等 |
| GET | `/public/configs` | 所有 `public=true` 的参数 |
| GET | `/public/dicts/:code` | 指定字典的字典项 |
| GET | `/public/notices` | 最新公告 |

### 4.16 运维端点（非 `/api` 前缀）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 存活检查：自身 + 数据库 + 缓存 |
| GET | `/readyz` | 就绪检查：数据库连通 |
| GET | `/metrics` | Prometheus 指标（可加 token 保护） |

### 4.17 API 密钥（后台管理）

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/api-keys` | 分页；支持 `keyword`、`kind`、`status`，返回时附当日用量 | `system:apikey:list` |
| GET | `/api-keys/scopes` | 可授予的权限点清单（取自菜单） | `system:apikey:list` |
| POST | `/api-keys` | 签发，返回 `{key, plain}`，`plain` 仅此一次 | `system:apikey:create` |
| PUT | `/api-keys/:id` | 编辑名称/范围/白名单/配额/有效期；类型不可改 | `system:apikey:update` |
| POST | `/api-keys/:id/rotate` | 轮换，旧密钥立即失效 | `system:apikey:rotate` |
| PATCH | `/api-keys/:id/status` | `{"status": 0\|1}` 启用或停用 | `system:apikey:update` |
| DELETE | `/api-keys/:id` | 软删除，保留审计轨迹 | `system:apikey:delete` |

签发入参：

```json
{
  "name": "资讯爬虫-生产",
  "kind": "service",
  "scopes": ["news:ingest"],
  "ipAllow": ["10.0.0.0/8", "203.0.113.7"],
  "rateRps": 50,
  "rateBurst": 100,
  "dailyQuota": 200000,
  "expireAt": "2027-01-01T00:00:00+08:00",
  "remark": "生产环境采集"
}
```

- `rateRps`/`rateBurst` 为 0 时取默认值（服务端 60 rps，客户端 10 rps）。
- `dailyQuota` 为 0 表示不限额；用量按自然日统计，跨实例共享。
- 非超管只能授予自己已拥有的权限点，`*` 仅超管可签发。

### 4.18 开放接口（`/api/open/v1`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/whoami` | 返回凭证身份、scope、日配额与当日用量，用于接入自检 |

请求头二选一：`X-API-Key: <plain>` 或 `Authorization: ApiKey <plain>`。

### 4.19 资讯中心（后台管理）

> 资讯中心是**可选模块**，默认发行版不含这些接口（`make dist MODULES="user news"`
> 组装后才存在）。默认构建下它们统一返回 404，侧边栏也不会出现资讯中心。

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/news` | 分页；`keyword`、`category`、`sourceId`、`from`、`to`、`status` | `content:news:list` |
| GET | `/news/stats` | 总量、今日、近 24 小时、分类与来源分布 | `content:news:list` |
| GET | `/news/:id` | 详情（含正文） | `content:news:view` |
| DELETE | `/news/:id` | 删除单条 | `content:news:delete` |
| POST | `/news/cleanup` | 立即清理过期数据，返回 `{deleted}` | `content:news:cleanup` |
| GET | `/news-sources` | 数据源分页 | `content:news:list` |
| POST | `/news-sources` | 新增数据源；`icon`、`color`、`sort` 用于客户端展示 | `content:news:source` |
| PUT | `/news-sources/:id` | 编辑；编码不可改 | `content:news:source` |
| DELETE | `/news-sources/:id` | 删除；仍有资讯引用时返回 `409` | `content:news:delete` |
| GET | `/news-categories` | 分类全量列表（含 `articleCount`） | `content:news:list` |
| POST | `/news-categories` | 新增分类 | `content:news:category` |
| PUT | `/news-categories/:id` | 编辑；编码不可改 | `content:news:category` |
| DELETE | `/news-categories/:id` | 删除；仍有资讯引用时返回 `409` | `content:news:delete` |

列表接口不返回 `content`，正文只在详情里给，避免单页响应过大。

来源与分类都是**运营预置的白名单**，不是爬虫自由填写的字段：未配置或已停用的编码会在推送时被拒。
两者的 `icon` 存可公开访问的图片 URL（本地上传为 `/uploads/...`，对象存储为外链），
`color` 作为图标缺失时的兜底主色，客户端不需要各自维护图标映射表。

### 4.20 资讯开放接口（`/api/open/v1`，API 密钥）

| 方法 | 路径 | scope | 说明 |
| --- | --- | --- | --- |
| POST | `/news/batch` | `news:ingest` | 批量推送，单次最多 200 条 |
| GET | `/news` | `news:read` | 游标分页，`limit` 最大 100 |
| GET | `/news/:id` | `news:read` | 详情，异步累加阅读量 |
| GET | `/news/meta` | `news:read` | 全量下发来源与分类（含图标、颜色、排序）、保留天数 |

推送：

```http
POST /api/open/v1/news/batch
X-API-Key: ak_s_...
Content-Type: application/json

{
  "source": "tech-daily",
  "items": [
    {
      "externalId": "a1b2c3",
      "title": "标题",
      "summary": "摘要，可省略，缺省时截取正文前 120 字",
      "content": "正文",
      "url": "https://example.com/a1b2c3",
      "author": "作者",
      "category": "tech",
      "tags": ["AI", "芯片"],
      "coverUrl": "https://example.com/cover.jpg",
      "publishedAt": "2026-11-03T10:00:00+08:00"
    }
  ]
}
```

```json
{
  "code": 0,
  "data": {
    "source": "tech-daily",
    "received": 200,
    "accepted": 197,
    "duplicate": 2,
    "invalid": 1,
    "expired": 0,
    "errors": [{ "index": 12, "reason": "分类未配置或已停用：gossip" }]
  }
}
```

元数据：

```http
GET /api/open/v1/news/meta
X-API-Key: ak_c_...
```

```json
{
  "code": 0,
  "data": {
    "sources": [
      { "code": "tech-daily", "name": "科技日报", "kind": "crawler",
        "icon": "https://cdn.example.com/icons/tech.png", "color": "#1E6FFF", "sort": 10 }
    ],
    "categories": [
      { "code": "tech", "name": "科技", "icon": "/uploads/2026/11/04/tech.png",
        "color": "#1E6FFF", "sort": 10 }
    ],
    "retentionDays": 7,
    "total": 1280
  }
}
```

`meta` 只返回启用项，按 `sort` 升序。来源和分类数量是运营配置级别（几十条），
一次全量下发 + 客户端缓存即可，不再返回「按名称分组的 Top 12 计数」——
那种结构既不能直接用于筛选，还会因截断漏项。

- **幂等**：唯一键是 `(source, externalId)`；未传 `externalId` 时用「标题+正文」的内容指纹兜底。
  重复推送不会报错，直接计入 `duplicate`，因此爬虫可以放心重试整批。
- **过期丢弃**：`publishedAt` 早于保留窗口的条目计入 `expired` 且不入库，
  避免刚写入就被清理、同时让爬虫知道这批数据没有存档价值。
- **来源白名单**：`source` 必须是后台已创建且启用的数据源，否则整批 `403`、不写入任何一条。
  来源是请求级属性，配置错了应该让爬虫立刻感知，而不是写一半。
- **分类白名单**：`category` 必填且必须命中已启用分类，否则该条计入 `invalid`，其余正常入库。
  分类是条目级属性，一条脏数据不该拖垮整批。缺失分类不再兜底到 `general`。
- **校验**：单条正文上限 200KB；标题为空、正文超限、分类非法均计入 `invalid`，`errors` 最多返回 20 条明细。
- **游标分页**：`GET /news?limit=20&cursor=...`，按 `published_at DESC, id DESC` 排序，
  深翻页不会变慢；`hasMore=false` 时结束。响应中的列表不含正文。

```json
{ "code": 0, "data": { "list": [], "nextCursor": "MTc5...", "hasMore": true } }
```

---

### 4.21 第三方登录（配置化 OAuth2 / OIDC）

新增渠道只需在 `oauth.providers` 里加一段配置，代码无需改动；完整示例见
`configs/config.oauth.example.yaml`。凭据建议用环境变量
`KEEL_OAUTH_<NAME>_CLIENT_ID` / `_CLIENT_SECRET` 注入。

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/oauth/providers` | 公开 | 返回启用渠道 `[{name,label,icon}]`，不含凭据 |
| GET | `/oauth/authorize/:name` | 公开 | 返回 `{url, state}`；`?redirect=1` 直接 302 |
| GET | `/oauth/callback/:name` | 公开 | 回调，成功后 302 到 `oauth.frontend_path#access_token=...` |
| POST | `/oauth/bind/:name` | Bearer | 已登录用户发起绑定，返回 `{url, state}` |
| GET | `/oauth/bindings` | Bearer | 我的绑定列表 |
| DELETE | `/oauth/bindings/:name` | Bearer | 解绑 |

回调地址固定为 `{callback_base 或请求 Host}/api/v1/oauth/callback/{name}`，
需在身份提供商后台登记一致。令牌放在 URL **fragment**，不会进入服务端日志与 Referer。

账号落库顺序：已绑定关系 → 按已验证邮箱/用户名关联已有账号（`link_existing`）
→ 自动注册（`auto_register`）→ 拒绝。自动注册的账号密码为随机串，需管理员重置。
`state` 一次性、默认 10 分钟有效，重放会被拒绝并跳回登录页。

- `field_map`：外部用户信息字段 → 系统字段，支持点路径、数组下标和逗号分隔的多候选路径。
  不配置时使用内置候选表，已覆盖 `sub/id`、`preferred_username/login`、`name`、
  `email`、`picture/avatar_url`、`email_verified`。
- `email_url`：GitHub 一类把邮箱放在独立接口时使用，支持数组（优先 primary + verified）与对象。
- `auto_register` + `default_roles`：控制是否自动建号及分配的角色编码。
- `auth_style`：`auto` / `in_header` / `in_params`，对应 `oauth2.AuthStyle`。

### 4.22 备份与迁移（运维中心）

| 方法 | 路径 | 权限点 | 说明 |
| --- | --- | --- | --- |
| GET | `/maintenance/backups` | `ops:backup:list` | 备份文件列表 |
| POST | `/maintenance/backups` | `ops:backup:create` | 立即备份，返回元信息 |
| GET | `/maintenance/backups/:name/meta` | `ops:backup:list` | 备份元信息 |
| GET | `/maintenance/backups/:name/download` | `ops:backup:list` | 下载备份文件 |
| POST | `/maintenance/backups/:name/verify` | `ops:backup:list` | 校验和 + 完整性 + 表清单 + 文件资产哈希 |
| POST | `/maintenance/backups/:name/restore` | `ops:backup:restore` | 恢复；带 `?dry_run=true` 时只返回计划不改文件 |
| GET | `/maintenance/prune-plan` | `ops:backup:list` | GFS 保留策略演练（保留/删除集合、各档槽位数） |
| DELETE | `/maintenance/backups/:name` | `ops:backup:delete` | 删除备份及元信息 |
| GET | `/maintenance/migrations` | `ops:backup:list` | 版本状态（驱动、当前/最新版本、清单） |
| POST | `/maintenance/migrations/up` | `ops:backup:restore` | 升级到最新 |
| POST | `/maintenance/migrations/down` | `ops:backup:restore` | 回滚最近一个版本 |

备份产物是 **tar 容器**（`keel-<驱动>-<时间>.tar[.gz]`），内含 `database.db`（或 `database.sql`）、
`manifest.json` 与 `files/` 本地文件资产目录；下载一份即拿到数据库 + 图标/附件。旧格式
`*.db[.gz]` / `*.sql[.gz]` 仍可正常校验与恢复。对象存储模式下跳过文件资产并在元信息标注 `storageMode: s3`。

`VerifyResult.fileCheck` 报告文件资产哈希比对结论；`RestoreResult` 在演练时返回 `plan`，
真实恢复后返回 `postCheck`（逐表行数比对）与 `filesRestored`。

备份定时执行器 `db.backup` 默认每天 02:00 运行，保留策略默认 GFS 分档
（`retain_hourly=24` / `retain_daily=14` / `retain_weekly=8` / `retain_monthly=12`），
四档全为 0 时退回 `retain` 保留最近 N 份，**最新一份永不删除**。
SQLite 恢复需停机执行 `keel -config ... restore [--dry-run] <文件>`；MySQL / PostgreSQL
可在开启 `backup.allow_restore` 后在线导入。CLI 子命令见 `docs/deploy.md` 7.1 / 7.2。

### 4.23 自动接口文档

`docs.enabled` 未配置时，开发环境自动开启、生产环境自动关闭，路径由 `docs.path`
指定（默认 `/docs`）。

| 路径 | 说明 |
| --- | --- |
| `GET /docs/` | 跳转 Swagger UI |
| `GET /docs/index.html` | Swagger UI |
| `GET /docs/doc.json` | OpenAPI 2.0 描述（随二进制嵌入） |

文档由 `swaggo/swag` 从代码注解生成，注解由 `scripts/gen_swagger_annotations.py`
按路由表自动生成，保证与真实路由一致。重新生成：`make docs`（需先 `make swag-install`）。
两种鉴权在文档里分别定义为 `BearerAuth` 与 `ApiKeyAuth`。

---

## 5. 调用示例

```bash
# 登录并保存令牌
TOKEN=$(curl -s -X POST http://localhost:9000/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin@123456"}' | jq -r '.data.token.accessToken')

# 查询用户（关键字 + 状态过滤 + 倒序）
curl -s "http://localhost:9000/api/v1/users?page=1&page_size=10&keyword=zhang&status=1&sort=-createdAt" \
  -H "Authorization: Bearer $TOKEN" | jq

# 新增用户
curl -s -X POST http://localhost:9000/api/v1/users \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"username":"lisi","nickname":"李四","password":"Lisi@123456","deptId":"d_tech","roleIds":["r_user"],"status":1}'

# 上传文件
curl -s -X POST http://localhost:9000/api/v1/files \
  -H "Authorization: Bearer $TOKEN" -F "file=@./report.pdf"
```

---

## 6. 前端接入要点

1. `401` + `code=40101` → 用 `refreshToken` 调 `/auth/refresh`，成功后重放原请求；`40102` → 跳登录页。
2. 每个响应都带 `traceId`，前端可在错误提示中展示后 8 位，方便对日志。
3. 按钮级权限：`profile` 返回的 `perms` 数组，`perms.includes('*')` 或有对应权限点才渲染按钮。
4. 下载/预览二进制接口同样支持 `?token=`，方便 `<a>`、`<img>` 直接引用。
