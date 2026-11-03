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

### 4.14 监控

| 方法 | 路径 | 说明 | 权限点 |
| --- | --- | --- | --- |
| GET | `/monitor/system` | 运行时/内存/GC/连接池/缓存/路由数 | `ops:monitor:view` |
| GET | `/monitor/routes` | 全部注册路由 | `ops:monitor:view` |

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
