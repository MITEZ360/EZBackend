# 配置说明

配置加载顺序（后者覆盖前者）：

```
内置默认值  →  configs/config.yaml  →  环境变量（KEEL_ 前缀）  →  运行期覆盖（-mode）
```

环境变量映射规则：配置键的 `.` 替换为 `_`，加 `KEEL_` 前缀。
例如 `server.port` → `KEEL_SERVER_PORT`，`db.conn_max_lifetime` → `KEEL_DB_CONN_MAX_LIFETIME`。

---

## 0. 依赖自动装配

所有外部依赖遵循同一条规则：**没配置就不用，配置了就自动激活**，不需要额外的
开关。启动日志会打印实际生效的组合（`依赖组件已装配`），`/healthz` 同时返回各
组件状态。

| 依赖 | 判定方式 | 未配置时 | 配置后 |
| --- | --- | --- | --- |
| 数据库 | `db.driver`，留空按 `db.dsn` 推断 | SQLite 本地文件 | MySQL / PostgreSQL |
| 缓存 | `redis.addr` 非空 | 进程内 LRU | Redis |
| 限流 | 跟随缓存实现 | 进程内令牌桶 | Redis 令牌桶（跨实例共享配额） |
| 任务锁 | 跟随缓存实现 | 进程内锁 | Redis 分布式锁（多实例只跑一份） |
| 文件存储 | `storage.type=auto` 时看 `storage.s3.bucket` 与 `endpoint` | 本地磁盘 | MinIO / OSS / S3 / COS |
| 邮件 | `mail.host` 非空 | 不创建实例 | SMTP |
| 服务发现 | `micro.registry.type` | 静态列表 | DNS（K8s Headless） |

显式关闭：`redis.enabled: false`、`mail.enabled: false`、`storage.type: local`。
`enabled` 不写时按上述规则自动判断，写 `true` 也不会跳过连接检查。

### runtime

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `stateless` | `auto` | 多实例无状态校验：`auto` 仅告警 / `strict` 拒绝启动 / `off` 不检查 |
| `instances` | `1` | 计划实例数；大于 1 或 `micro.mode=service` 时触发校验 |
| `instance_id` | 空 | 实例标识，留空自动生成 `hostname-pid`，写入启动日志 |

校验项：进程内缓存、进程内限流、本地磁盘、SQLite。`strict` 适合 CI 的部署清单检查：

```bash
KEEL_RUNTIME_STATELESS=strict go run ./cmd/keel -config configs/config.service.yaml
```

## 1. app

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `app.name` | `Keel 企业服务端框架` | 服务名，写入日志与网关调用方标识 |
| `app.env` | `dev` | `dev` / `test` / `prod`；`prod` 会启用额外的安全校验 |
| `app.version` | `1.0.0` | 版本号，健康检查返回 |
| `app.timezone` | `Asia/Shanghai` | 时区 |

## 2. server

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `host` / `port` | `0.0.0.0` / `9000` | 监听地址 |
| `mode` | `release` | gin 模式，`debug` / `release` |
| `read_timeout` | `30s` | 读超时 |
| `write_timeout` | `60s` | 写超时（大文件下载可调大） |
| `idle_timeout` | `120s` | 空闲连接保持 |
| `shutdown_timeout` | `15s` | 优雅停机等待时长 |
| `max_body_size` | `33554432`（32MB） | 请求体上限，超过返回 413 |
| `trust_proxy` | `false` | 位于 Nginx/网关之后时置 `true`，用于取真实 IP |
| `tls_cert` / `tls_key` | 空 | 同时配置则启用 HTTPS |
| `web_dir` | `web` | 前端资源目录（已内嵌，仅调试用） |

## 3. log

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `level` | `info` | `debug` / `info` / `warn` / `error` |
| `format` | `json` | `json`（便于采集）/ `console`（人读） |
| `dir` / `filename` | `logs` / `keel.log` | 落盘位置 |
| `max_size_mb` | `64` | 单文件大小上限，超出轮转 |
| `max_age_days` | `14` | 保留天数 |
| `max_backups` | `30` | 最多保留文件数 |
| `compress` | `true` | 轮转后 gzip |
| `console` | `true` | 是否同时输出到控制台（容器内建议关） |
| `caller` | `true` | 是否记录调用位置 |

日志默认带 `Sampler`（每秒 100 条/每个重复消息后限流），避免异常风暴打满磁盘。

## 4. db

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `driver` | 空 | `sqlite` / `mysql` / `postgres`；留空按 `dsn` 自动推断 |
| `dsn` | `data/keel.db` | 见下方示例 |
| `max_open_conns` | `50` | 连接池上限（实际最小值 10） |
| `max_idle_conns` | `10` | 空闲连接（实际最小值 5） |
| `conn_max_lifetime` | `1h` | 连接最大存活 |
| `conn_max_idle_time` | `10m` | 空闲连接回收 |
| `slow_threshold` | `200ms` | 慢 SQL 阈值，超过记 Warn |
| `log_level` | `warn` | GORM 日志级别，`info` 会打印全部 SQL |
| `auto_migrate` | `true` | 启动自动建表/加列（生产建议关掉，用脚本迁移） |
| `seed` | `true` | 首次启动写入内置菜单/部门/字典/账号（有用户则跳过） |

DSN 示例（`driver` 均可省略）：

```yaml
# SQLite：框架自动附加 busy_timeout / WAL / synchronous=NORMAL
dsn: data/keel.db

# MySQL：DSN 含 @tcp( 或 @unix( 即识别
dsn: keel:Keel@123456@tcp(127.0.0.1:3306)/keel?charset=utf8mb4&parseTime=True&loc=Asia%2FShanghai

# PostgreSQL：以 postgres:// 开头，或含 host= 与 dbname=
dsn: postgres://keel:Keel@123456@127.0.0.1:5432/keel?sslmode=disable
# 也支持 key=value 形式
dsn: host=127.0.0.1 user=keel password=Keel@123456 dbname=keel port=5432 sslmode=disable TimeZone=Asia/Shanghai
```

推断规则：`mysql://`、`mysql:`、含 `@tcp(` / `@unix(` → MySQL；`postgres://`、
`postgresql://`、含 `host=` 且含 `dbname=` → PostgreSQL；其余按 SQLite 处理。

## 5. redis

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `addr` | 空 | **填写地址即启用**；留空使用进程内 LRU 缓存 |
| `enabled` | 空（自动） | 可选，显式 `false` 关闭；`true` 时 `addr` 留空则用 `127.0.0.1:6379` |
| `optional` | `false` | 连接失败时是否降级为进程内缓存；默认直接启动失败 |
| `password` / `db` | 空 / `0` | 认证与库号 |
| `pool_size` | `32` | 连接池（实际最小值 16） |
| `prefix` | `keel` | 键前缀，便于多环境共用实例 |
| `dial_timeout` / `read_timeout` | `3s` | 超时 |
| `local_size` | `100000` | 本地缓存容量（未启用 Redis 时生效） |

> 配置了 `addr` 却连不上时默认**启动失败**，而不是悄悄改用本地缓存——否则多实例
> 下验证码、登录失败计数、限流、任务锁会各自为政。确实需要容错时显式设
> `optional: true`，并在日志中确认降级。
>
> 启用 Redis 后，限流与定时任务锁自动切换为分布式实现，无需额外配置。

## 6. jwt

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `secret` | 内置开发密钥 | **生产必须替换**，≥32 位随机串 |
| `issuer` | `keel` | 签发者，解析时校验 |
| `access_ttl` | `2h` | 访问令牌有效期 |
| `refresh_ttl` | `168h` | 刷新令牌有效期 |

`app.env=prod` 时若仍使用内置密钥，启动直接失败（网关形态除外，因为网关不签发令牌）。

## 7. security

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `captcha_enabled` | `false` | 登录图形验证码（生产建议 `true`） |
| `max_login_fail` | `5` | 连续失败次数上限 |
| `lock_duration` | `15m` | 锁定时长 |
| `password_min_len` | `8` | 密码最小长度 |
| `password_strength` | `2` | 1 仅长度 / 2 字母+数字 / 3 大小写+数字+符号 |
| `data_encrypt_key` | 空 | AES-256-GCM 密钥，用于 TOTP 密钥与敏感参数；空则复用 `jwt.secret` |
| `session_single` | `false` | 是否只允许一处登录 |
| `internal_token` | 内置默认值 | 服务间调用令牌（`X-Internal-Token`），**生产必须替换** |

## 8. storage

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `type` | `auto` | `auto` / `local` / `s3`；`auto` 时看 `s3.endpoint` 与 `s3.bucket` |
| `local_dir` | `data/uploads` | 本地存储根目录，按 `yyyy/MM/dd` 分目录 |
| `base_url` | `/uploads` | 本地文件对外前缀 |
| `max_size` | `20971520`（20MB） | 单文件上限 |
| `allow_ext` | 图片/文档/压缩包/音视频常见后缀 | 扩展名白名单 |

对象存储（`s3`）参数，兼容 MinIO、阿里云 OSS、AWS S3、腾讯云 COS：

| 键 | 说明 |
| --- | --- |
| `endpoint` | `host:port`，也可写 `https://host`，会解析出 TLS 开关 |
| `region` | 区域，MinIO 可留空，AWS 必填 |
| `bucket` | 桶名，不存在时启动自动创建 |
| `access_key` / `secret_key` | 访问密钥；都留空时使用云实例角色（IAM） |
| `use_ssl` | 是否使用 HTTPS（`endpoint` 带 scheme 时以 scheme 为准） |
| `public_url` | 对外访问前缀，如 CDN 域名；留空按 `endpoint/bucket` 拼接 |

```yaml
storage:
  type: auto            # 填了 endpoint 与 bucket 就自动走对象存储
  s3:
    endpoint: "minio.internal:9000"
    bucket: "keel"
    access_key: "..."
    secret_key: "..."
    use_ssl: false
```

> 文件下载始终走 `GET /api/v1/files/:id/download`（带权限校验），`url` 字段仅用于
> 展示；如果桶是私有的，直接访问该 URL 会 403。
>
> 多实例部署请使用对象存储：本地磁盘只有当前实例可见，`runtime.stateless` 会给出告警。

### 8.1 mail

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `host` | 空 | **填写即启用**；留空不创建实例 |
| `enabled` | 空（自动） | 可选，显式 `false` 关闭 |
| `port` | `587` | 465 走隐式 TLS（SMTPS），其它端口按服务端能力 STARTTLS |
| `username` / `password` | 空 | 留空表示不认证（内网中继） |
| `from` | `noreply@host` | 发件人；账号是邮箱时默认取账号 |
| `tls` | `true` | 是否启用 TLS |

## 9. cors

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `allow_origins` | `["*"]` | 生产建议收敛为具体域名 |
| `allow_methods` | GET/POST/PUT/PATCH/DELETE/OPTIONS | |
| `allow_headers` | Origin/Content-Type/Authorization/X-Requested-With/X-Trace-Id | |
| `expose_headers` | X-Trace-Id / Content-Disposition | 让前端能读到 traceId 与下载文件名 |
| `allow_credentials` | `false` | 与 `*` 源互斥，需要 Cookie 时改具体域名 |
| `max_age` | `86400` | 预检缓存秒数 |

## 10. rate_limit

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `enabled` | `true` | 令牌桶限流 |
| `rps` / `burst` | `200` / `400` | 每秒补充速率与桶容量 |
| `by_user` | `true` | 登录后按用户限流，否则按 IP |

## 11. metrics

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `enabled` | `true` | 暴露 Prometheus 指标 |
| `path` | `/metrics` | 指标路径 |
| `token` | 空 | 非空时需 `Authorization: Bearer <token>` 才能抓取 |

### 11.1 news

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `retention_days` | `7` | 资讯保留天数。超期由 `news.cleanup` 任务物理删除，查询侧也会强制加上该时间窗 |

调整保留期后无需重启：清理任务与查询侧都实时读取该值。若某个任务需要不同口径，
可在定时任务的参数里显式传 `{"days": N}` 覆盖。

## 12. micro

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `mode` | `mono` | `mono` / `gateway` / `service` |
| `modules` | `[]` | service 形态下启用的模块，空表示全部 |
| `timeout` | `5s` | 网关转发与客户端超时 |
| `retries` | `2` | 失败重试次数 |
| `registry.type` | `static` | `static`（配置）/ `dns`（K8s Headless） |
| `registry.endpoints` | `[]` | 形如 `keel-core=10.0.0.1:9000,10.0.0.2:9000` |
| `registry.dns_suffix` | 空 | 形如 `keel.svc.cluster.local:9000` |
| `registry.interval` | `30s` | DNS 解析缓存时长 |
| `routes` | `[]` | 网关转发规则：`prefix` / `service` / `targets` / `strip_prefix` |
| `breaker.*` | `enabled=true, max_failed=10, interval=30, timeout=15` | 熔断参数 |

网关路由也可以用环境变量简化：

```bash
KEEL_GATEWAY_ROUTES="/api/v1=keel-core,/uploads=keel-file"
```

---

## 12.1 modules（业务模块启停，P1-2）

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `modules.enabled` | `[]` | 空表示全部启用；非空时只挂载列出的模块 |

- **依赖自动补齐**：启用某模块时会自动启用它依赖的模块（如 `news` → `apikey`，`user` → `role` → `menu`）。
- **基础模块常驻**：`auth` / `config` / `dict` / `notice` 因公开路由依赖，始终挂载。
- **未知模块 fail-fast**：列出不存在的模块名会在装配阶段直接拒绝启动，并打印可用清单。
- **与 `micro.modules` 的关系**：`micro.modules` 仅在 `micro.mode=service` 生效；`modules.enabled` 在 mono 模式也能裁剪，单调优先生效。
- **可观测**：启动日志 `业务模块已解析` 打印 `source` / `count` / `modules` 清单；自检报告附在 `app.SelfCheck`。

```yaml
modules:
  enabled: [dashboard, user, news]   # 仅挂载这些 + 依赖 + 基础模块
```

> 这是声明式模块契约的一部分（`Module.Name/DependsOn/AlwaysOn`），服务与处理器仍由
> `bootstrap/app.go` 统一构造，路由按注册表结果挂载。详见
> [工程实践借鉴与改进路线](engineering-practices.md) P1-2。

---

## 13. 配置热更新

`bootstrap` 启动时会调用 `config.Watch()`，监听配置文件变更：

1. 重新解析 → 2. 校验（失败则忽略本次变更，保留旧配置）→ 3. 替换全局配置 → 4. 触发 `OnChange` 回调。

当前已支持热更新的项：日志级别、限流参数、CORS。
**不支持**热更新的项（需要重启）：监听端口、数据库/Redis 连接、文件存储、邮件、
JWT 密钥、微服务形态。依赖实例在启动时装配，运行期不会因为改配置而重建连接。

---

## 14. 环境变量速查

```bash
# 数据库
KEEL_DB_DRIVER=mysql
KEEL_DB_DSN="keel:pass@tcp(mysql:3306)/keel?charset=utf8mb4&parseTime=True"
# 缓存（填地址即启用，无需 KEEL_REDIS_ENABLED）
KEEL_REDIS_ADDR=redis:6379
# 文件存储（填了 endpoint 与 bucket 即启用）
KEEL_STORAGE_TYPE=auto
KEEL_STORAGE_S3_ENDPOINT=minio:9000
KEEL_STORAGE_S3_BUCKET=keel
KEEL_STORAGE_S3_ACCESS_KEY=...
KEEL_STORAGE_S3_SECRET_KEY=...
# 邮件（填 host 即启用）
KEEL_MAIL_HOST=smtp.example.com
KEEL_MAIL_USERNAME=...
KEEL_MAIL_PASSWORD=...
# 多实例无状态校验
KEEL_RUNTIME_STATELESS=strict
KEEL_RUNTIME_INSTANCES=3
# 安全
KEEL_JWT_SECRET="$(openssl rand -base64 48)"
KEEL_SECURITY_INTERNAL_TOKEN="$(openssl rand -hex 24)"
# 形态
KEEL_MICRO_MODE=service
KEEL_MICRO_MODULES=user,role,dept
KEEL_GATEWAY_ROUTES="/api/v1=keel-core"
# 注册发现
KEEL_MICRO_REGISTRY_TYPE=dns
KEEL_MICRO_REGISTRY_DNS_SUFFIX=keel.svc.cluster.local:9000
```

> 切片类型（`micro.modules`、`micro.registry.endpoints`、`cors.allow_origins`、`storage.allow_ext`）
> 已显式绑定环境变量，用英文逗号分隔即可。

补充：

```bash
# 迁移与备份
KEEL_MIGRATION_MODE=versioned
KEEL_MIGRATION_TABLE_NAME=keel_schema_migrations
KEEL_BACKUP_ENABLED=true
KEEL_BACKUP_DIR=/var/backups/keel
KEEL_BACKUP_RETAIN=14
KEEL_BACKUP_ALLOW_RESTORE=false
# 接口文档（不配置时 dev 开 / prod 关）
KEEL_DOCS_ENABLED=true
KEEL_DOCS_PATH=/docs
# OAuth 凭据（名称转大写、非字母数字转下划线）
KEEL_OAUTH_GITHUB_CLIENT_ID=...
KEEL_OAUTH_GITHUB_CLIENT_SECRET=...
KEEL_OAUTH_ENABLED=true
```

---

## 15. 数据库迁移、备份、第三方登录与接口文档

### 15.1 migration

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `migration.mode` | `versioned` | `versioned` 按版本执行 / `auto` 沿用 AutoMigrate / `off` 不迁移 |
| `migration.table_name` | `keel_schema_migrations` | 版本记录表 |

`db.auto_migrate`、`db.seed` 仅影响 `auto` 模式与种子数据。版本清单、回滚与 CLI
用法见 `docs/deploy.md` 7.1。

### 15.2 backup

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `backup.enabled` | `true` | `db.backup` 定时任务是否真正执行 |
| `backup.dir` | `data/backups` | 产物目录 |
| `backup.retain` | `7` | 兜底：保留最近 N 份（GFS 四档全为 0 时生效） |
| `backup.retain_hourly` | `24` | GFS：保留最近 N 个小时槽位（同槽位只留最新一份） |
| `backup.retain_daily` | `14` | GFS：保留最近 N 个天槽位 |
| `backup.retain_weekly` | `8` | GFS：保留最近 N 个周槽位（ISO 周） |
| `backup.retain_monthly` | `12` | GFS：保留最近 N 个月槽位 |
| `backup.compress` | `true` | gzip 压缩（备份为 tar 容器，含数据库 + `files/` 文件资产） |
| `backup.allow_restore` | `false` | 是否允许在线恢复（高危） |

> **GFS 保留**：任一分档 >0 即启用；按槽位挑选而非时间窗，避免断档时误删窗口内唯一备份，
> **最新一份永不删除**。演练命令 `keel -config ... backup-prune --dry-run`。
> 备份会一并纳入本地存储目录（`storage.local_dir`）的文件资产；对象存储模式跳过并在元信息标注。
| `backup.mysql_dump` / `mysql_restore` | `mysqldump` / `mysql` | 外部工具路径 |
| `backup.pg_dump` / `pg_restore` | `pg_dump` / `psql` | 外部工具路径 |

### 15.3 oauth

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `oauth.enabled` | `false` | 总开关 |
| `oauth.callback_base` | 空 | 回调基址，网关后必填；留空按请求 Host 推断 |
| `oauth.frontend_path` | `/oauth/callback` | 回调成功后前端接收页 |
| `oauth.state_ttl` | `10m` | state 有效期 |
| `oauth.providers` | `[]` | 渠道列表，见下表 |

单个 provider 的关键字段：

| 字段 | 说明 |
| --- | --- |
| `name` / `label` / `icon` / `enabled` | 唯一标识、按钮文案、图标 URL、开关 |
| `client_id` / `client_secret` | 凭据，可用 `KEEL_OAUTH_<NAME>_CLIENT_*` 覆盖 |
| `auth_url` / `token_url` / `user_info_url` / `email_url` | 端点 |
| `scopes` | 授权范围 |
| `field_map` | 用户信息字段映射，支持点路径、数组下标、逗号分隔多候选 |
| `link_existing` / `match_by` | 是否按已验证邮箱或用户名关联已有账号 |
| `auto_register` / `default_roles` | 是否自动建号及其默认角色编码 |
| `auth_style` | `auto` / `in_header` / `in_params` |
| `extra_auth` | 附加到授权 URL 的自定义参数 |
| `user_info_raw` | `true` 时用 POST 请求用户信息接口 |

完整可复制示例：`configs/config.oauth.example.yaml`。

### 15.4 docs

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `docs.enabled` | 未配置 | 三态：未配置时 dev 开 / prod 关；显式 `true`/`false` 覆盖 |
| `docs.path` | `/docs` | Swagger UI 与 `doc.json` 挂载前缀 |
