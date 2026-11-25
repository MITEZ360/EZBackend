# 配置说明

配置加载顺序（后者覆盖前者）：

```
内置默认值  →  configs/config.yaml  →  环境变量（TENON_ 前缀）  →  运行期覆盖（-mode）
```

环境变量映射规则：配置键的 `.` 替换为 `_`，加 `TENON_` 前缀。
例如 `server.port` → `TENON_SERVER_PORT`，`db.conn_max_lifetime` → `TENON_DB_CONN_MAX_LIFETIME`。

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
TENON_RUNTIME_STATELESS=strict go run ./cmd/tenon -config configs/config.service.yaml
```

## 1. app

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `app.name` | `Tenon 企业服务端框架` | 服务名，写入日志与网关调用方标识 |
| `app.env` | `dev` | `dev` / `test` / `prod`；`prod` 会启用额外的安全校验 |
| `app.version` | `1.0.0` | 版本号，健康检查返回 |
| `app.timezone` | `Asia/Shanghai` | IANA 时区名。启动时设为进程 `time.Local`，统一影响 GORM 时间戳、cron 调度、备份文件名、会员到期解析等；非法值启动失败 |

## i18n（语言协商）

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `i18n.default_locale` | `zh-CN` | 客户端未声明语言时的兜底；目前支持 `zh-CN` / `en` |

请求按 `X-Locale` > `Accept-Language` > 默认语言协商。错误文案、字段级校验提示均按此翻译；未收录译文的文案自动降级为中文原文，逐步补齐语言包即可覆盖更多接口。用户可在个人中心保存自己的语言与时区偏好（`PUT /api/v1/auth/preferences`），登录后会覆盖本地选择。

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
| `dir` / `filename` | `logs` / `tenon.log` | 落盘位置 |
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
| `dsn` | `data/tenon.db` | 见下方示例 |
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
dsn: data/tenon.db

# MySQL：DSN 含 @tcp( 或 @unix( 即识别
dsn: tenon:Tenon@123456@tcp(127.0.0.1:3306)/tenon?charset=utf8mb4&parseTime=True&loc=Asia%2FShanghai

# PostgreSQL：以 postgres:// 开头，或含 host= 与 dbname=
dsn: postgres://tenon:Tenon@123456@127.0.0.1:5432/tenon?sslmode=disable
# 也支持 key=value 形式
dsn: host=127.0.0.1 user=tenon password=Tenon@123456 dbname=tenon port=5432 sslmode=disable TimeZone=Asia/Shanghai
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
| `prefix` | `tenon` | 键前缀，便于多环境共用实例 |
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
| `issuer` | `tenon` | 签发者，解析时校验 |
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
    bucket: "tenon"
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
| `registry.endpoints` | `[]` | 形如 `tenon-core=10.0.0.1:9000,10.0.0.2:9000` |
| `registry.dns_suffix` | 空 | 形如 `tenon.svc.cluster.local:9000` |
| `registry.interval` | `30s` | DNS 解析缓存时长 |
| `routes` | `[]` | 网关转发规则：`prefix` / `service` / `targets` / `strip_prefix` |
| `breaker.*` | `enabled=true, max_failed=10, interval=30, timeout=15` | 熔断参数 |

网关路由也可以用环境变量简化：

```bash
TENON_GATEWAY_ROUTES="/api/v1=tenon-core,/uploads=tenon-file"
```

---

## 12.1 modules（业务模块启停）

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `modules.enabled` | `[]` | 空表示全部启用；非空时作为白名单，只启用列出的模块。也可在管理端「配置中心 → 业务模块」勾选 |

- **依赖自动补齐**：启用某模块时会自动启用它依赖的模块（`news` → `apikey` + `job`，
  `user` → `role` → `menu`，`cluster` → `configcenter`）。
- **基础模块常驻**：`auth` / `config` / `dict` / `notice` 因公开路由依赖，始终启用。
- **未知模块 fail-fast**：列出不存在的模块名会在装配阶段直接拒绝启动，并打印可用清单。
- **与 `micro.modules` 的关系**：`micro.modules` 仅在 `micro.mode=service` 生效；
  `modules.enabled` 在 mono 模式也能裁剪，两者同时配置时以 `micro.modules` 为准。

### 关闭一个模块会发生什么

模块开关是完整闭环，不只是隐藏路由：

| 影响面 | 关闭后 | 重新启用后 |
| --- | --- | --- |
| API 路由 | 不挂载，访问返回 404 | 恢复 |
| 侧边栏菜单 | 内置菜单置为停用（不删除） | 恢复原状态，管理员改过的名称与排序不丢 |
| 数据表 | 新库不建表；已存在的表保留 | 缺失的表自动补建 |
| 定时任务 | 不再补种该模块的清理任务 | 恢复补种 |
| 后台循环 | 调度器、集群心跳等不启动 | 恢复启动 |
| 前端页面 | 页面仍在包里，但菜单守卫会拦到 403；登录后自动落到菜单里第一个可访问页面 | 恢复可达 |

之所以「停用菜单」而不是删除：重新启用后角色授权、管理员自定义的名称与排序都还在。
已建的表同理，关闭模块不会删数据。

版本化迁移是一次性的，模块先关后开时 baseline 不会回头执行，因此启动时会对
「已启用但表不存在」的可选模块补一次幂等建表，不修改已有表结构。

### 在管理端调整

`运维中心 → 配置中心 → 业务模块` 提供逐个模块的勾选开关，不需要改文件：

- **优先级**：管理端保存的值写进覆盖表，优先级高于配置文件（见第 13 节）。
  想让配置文件重新生效，点该区域的「恢复配置文件值」删掉覆盖行。
- **需重启**：模块开关决定装配范围，只能在启动时生效，因此标记为「需重启」。
  保存后接口返回 `needRestart`，页面会提示并可直接下发「重启全部节点」。
- **依赖与常驻**：勾选时前端按 `dependsOn` 自动补依赖，取消时级联移除依赖它的模块，
  与后端解析规则一致；基础模块置灰不可取消。
- **非法值当场拒绝**：保存时会逐项校验模块名，避免写错一个名字导致下次启动 fail-fast 起不来。
- **当前运行**：页面展示的是实际启用集合（依赖补齐后），与勾选不一致说明改动还没重启。

装配顺序为此做了调整：先打开数据库、读取覆盖表，再解析模块白名单。因此
`modules.enabled` 存在 `sys_runtime_config` 里也能在启动阶段生效。

> 注意：把 `configcenter` 自己取消勾选并重启后，配置页会消失，只能改配置文件或
> 直接改覆盖表。覆盖表本身在底座表里，不会跟随模块关闭而不建，所以白名单不会
> 因为「关掉配置中心」而丢失，重启后模块不会静默恢复成全开。

### 发行版裁剪示例

只保留后台管理与运维控制台，去掉资讯、消息、会员、API 密钥与工作台：

```yaml
modules:
  enabled: [auth, config, dict, notice, user, role, menu, dept,
            job, file, audit, monitor, routemanage, cacheadmin,
            maintenance, configcenter, cluster]
```

实测效果：路由 172 → 126，内置菜单停用 23 条（侧边栏 30 → 22 项），
定时任务 5 → 3，被裁模块的接口全部 404。表与数据保留，重新启用后原样恢复。

### 两层裁剪：运行期开关 vs 编译期裁剪

Go 里裁剪有两条独立路径，别混在一起：

| | 运行期开关（`modules.enabled`） | 编译期裁剪（发行版导入清单） |
| --- | --- | --- |
| 作用 | 同一份二进制按配置关掉模块 | 不导入的模块不进入二进制 |
| 生效 | 保存后重启 | 重新编译 |
| 效果 | 路由、菜单、建表、任务、循环都不生效 | 代码根本不存在，体积与攻击面都变小 |
| 适合 | 同一发行版交付不同客户，按需开功能 | 做精简发行版、把演示业务整块摘掉 |

编译期裁剪改的是发行版清单：`internal/distribution/default.go` 只有空白导入，
删一行就少一个模块的全部代码——路由、表、菜单、任务、后台循环一并消失。
默认发行版只收通用后台能力（登录、RBAC、组织、运维控制台、消息/会员/API 密钥），
演示业务（资讯中心）与国内能力（自助注册/短信登录、会员充值）不在其中，
它们都是独立的可选模块。

> 生成器要回答"要不要资讯中心"，就得看到完整的模块图，而默认清单里没有它。
> `internal/distribution/catalog` 汇总「默认清单 + 可选模块」，只被 `cmd/distgen`
> 与测试引用，不进入应用二进制。

### 组装一个只含指定功能的系统

不想手动删导入时，用生成器按模块名组装：

```bash
make dist-list                                    # 看有哪些模块、谁依赖谁；+ 为默认不装
make dist-base                                    # Base：只要后台管理基础功能
make dist MODULES="user configcenter cluster"     # 组装并编译
make dist MODULES="user news"                     # 带资讯中心的系统
make dist MODULES=all                             # 全量（含默认不装的模块）
make dist-default                                 # 删掉定制清单，回到默认发行版
```

`make dist-base` 是预置组合，模块清单写在 Makefile 的 `BASE_MODULES` 里，
产物为 `bin/tenon-base`，发行版名 `Base`（`internal/distribution/profile.go`
里的 `const Name`）。它不含会员、资讯、充值、自助注册、开放密钥、集群协同，
前端也以 `VITE_EDITION=base` 构建，可选模块页面不生成 chunk。

发行版名不只是一个标签：启动横幅、`-version` 输出都会带上它，便于在机器上
区分同时部署的多个版本：

```bash
$ ./bin/tenon-base -version
Tenon Base 1.0.0 (b864f62 2026-11-25T13:40:14Z)
```

生成器做三件事：补齐依赖、强制带上常驻模块、把模块名翻译成包导入清单，
写入 `internal/distribution/profile.go`（带 `profile` 构建标签，与 `default.go`
的 `!profile` 互斥）。所以同一份源码能产出多个发行版，互不干扰。

### 资讯中心：默认不装的独立模块

`news` 的全部代码仍在 `internal/modules/news`（`service.go` + `handler.go` +
`module.go`），只是默认发行版不导入它：

| 影响面 | 默认发行版 |
| --- | --- |
| 路由 | `/api/v1/news*`、`/api/v1/news-sources`、`/api/open/v1/news*` 全部 404 |
| 数据表 | 新库不建 `news_source` / `news_category` / `news_article` |
| 菜单 | 资讯中心目录与其页面不出现；旧库里这些内置菜单被自动停用 |
| 配置中心 | 「资讯」分组整组隐藏（配置项带 `Module` 归属，模块不在发行版就隐藏） |
| 定时任务 | `news.cleanup` 不补种 |
| 模块开关 | 配置中心的候选清单里没有 `news`（代码都不在，勾了也没用） |

需要时显式组装，生成器会自动补齐 `apikey`、`job` 与常驻模块：

```bash
make dist MODULES="user news"
# 显式选择：user news
# 最终装配 11 个：menu auth config dict apikey job news notice role dept user
```

实测该组合 115 条路由（默认发行版 155，全量 172）：三张资讯表、清理任务、
管理端接口与开放接口都齐全，`/api/v1/news` 返回 200。

实测「用户管理 + 配置中心 + 集群管理」：

```
显式选择：user configcenter cluster
最终装配 10 个：menu auth configcenter cluster config dict notice role dept user
自动补齐 7 个：  menu auth config dict notice role dept
```

启动后 88 条路由（全量 172），侧边栏只剩系统管理与运维中心两组，
`/api/v1/news`、`/api/v1/messages`、`/api/v1/dashboard/overview` 全部 404。

> 包是裁剪的最小单位。`system` 包同时注册了 `dict` / `config` / `notice`
> 三个模块，导入它就会把三个都带进来——它们本来就是常驻模块，没有实际影响。
> 将来若某个包内含多个可选模块，包级裁剪后可用 `modules.enabled` 再做模块级筛选。

### 国内能力：两个独立模块

中国大陆常用的注册/短信与支付/充值做成两个模块，默认都不装：

| 模块 | 依赖 | 内容 |
| --- | --- | --- |
| `cnauth` | `auth` | 手机号自助注册、短信验证码登录、找回密码 |
| `recharge` | `membership` | 等级售价、充值下单、微信/支付宝回调、订单管理 |

```bash
make dist MODULES="user cnauth recharge"
# 最终装配 11 个：menu auth cnauth config dict notice role dept user membership recharge
```

体量与边界和资讯中心一致：

| 影响面 | 默认发行版 |
| --- | --- |
| 路由 | `/api/v1/auth/register`、`/api/v1/auth/sms/code`、`/api/v1/auth/login/phone`、`/api/v1/pay/notify/*`、`/api/v1/recharge/*` 全部 404 |
| 数据表 | 新库不建 `cn_sms_code` / `cn_recharge_order` / `cn_payment_config` |
| 菜单 | 充值订单与支付配置按钮不出现 |
| 配置中心 | 「注册与短信」「支付」两组整组隐藏 |
| 登录页 | `public/cn-config` 404，注册入口保持隐藏 |

两点实现细节：

- 平台库 `internal/platform/sms`、`internal/platform/pay` 不算模块，只在被模块
  引用时进二进制；会员等级表的 `price` 列由迁移 v9 无条件加上（列在默认版里恒为 0，
  不装配 `recharge` 时没有读取方）。
- 微信/QQ 的 OAuth 协议适配留在 `auth` 模块内部，由 `oauth.providers[].protocol`
  驱动；不填 `protocol` 时走标准 OAuth2，行为与之前一致。因此“第三方登录”不需要
  额外模块，只要配置。

### 遗留菜单会被自动停用

先用全量版跑过、再换成精简版时，数据库里还留着被裁模块的菜单记录。
启动时会把这些「不在当前发行版内」的内置菜单置为停用（`status=0`），
不是删除，换回全量版后原样恢复。

判据是菜单 ID 的 `m_` 前缀：内置菜单一律以 `m_` 开头，管理员自建菜单用
雪花 ID，脚本按模块停用内置菜单时不会误伤手工建的菜单。**这也是内置菜单
必须保留 `m_` 前缀的原因**——换成别的 ID 规范就会被当成自建菜单而漏掉。

> 数据库里两条历史遗留菜单（`m_recharge`、`m_recharge_pay_config`）不属于
> 任何模块，会在每次启动时被停用。这是预期行为，不是误伤。


### 模块如何自描述

每个模块包里有一个 `module.go`，在 `init()` 里声明自己的一切：

```go
func init() {
    modulekit.Register(modulekit.Spec{
        Name:      "news",
        Label:     "资讯聚合",
        DependsOn: []string{"apikey"},
        Models:    []any{&model.NewsArticle{}, /* … */},
        Menus:     []modulekit.Menu{ /* 扁平声明，Parent 指向目录 */ },
        Jobs:      []modulekit.Job{{Handler: "news.cleanup", Cron: "15 * * * *"}},
        Setup:     setup,  // 构造服务、挂路由、注册任务实现
        Sync:      Sync,   // 每次启动的幂等数据补齐
    })
}
```

`Setup` 拿到的 `modulekit.Context` 只暴露平台能力（DB、Cache、Config、Log、
Storage、JWT、Authz、事件总线、路由快照、重启入口）与服务查找：

- `c.Authed/Public/Open(fn)` 声明路由，中间件由框架按组挂好；
- `c.Provide(name, svc)` 供其它模块取用，`modulekit.Need[T](c, name)` 取；
- `c.Job(handler, fn)` 注册任务实现；
- `c.OnStart/OnStop` 声明后台循环的生命周期；
- `c.After(fn)` 在全部模块装配完成后做跨模块注入。

模块之间**不允许互相 import**。需要别的模块的能力时，在自己的包里定义
一个小接口（如 `user.MembershipFiller`、`system.NoticeNotifier`），对方结构
匹配即可，框架在装配末尾注入。这样任一模块都能被单独裁掉。

### 新增一个模块

1. 新建 `internal/modules/<name>/`，实现 service 与 handler；
2. 写 `module.go`：`init()` 里 `modulekit.Register`（`Pkg` 填本包导入路径），`setup` 里挂路由；
3. 加进发行版：通用能力写进 `internal/distribution/default.go`；独立模块写进
   `internal/distribution/catalog/catalog.go`（同时把名字加进 `Optional`），
   或用 `make dist MODULES="<name> …"` 组装；
4. 写完。`bootstrap`、`router`、`seed` 都不用改。

配置项仍集中在 `internal/modules/configcenter/schema.go`，每一项带 `Module` 归属：
模块没编进发行版时，该项连同空分组一起隐藏，不会留下"改了不生效"的输入框。
这是下一步要下沉的部分：模块应能自报 `Schema() []Item`，否则模块搬走了、
它的配置项还得回主工程改。


---

## 13. 配置分层与热更新

优先级从低到高：**内置默认值 < 配置文件 < 环境变量 < 管理端覆盖**。
管理端覆盖存放在 `sys_runtime_config`（与业务参数 `sys_config` 分表），
键名就是本文档里的点分路径。

两条热更新路径：

1. **文件监听**：`config.Watch()` 监听配置文件，变更后重新解析、校验，
   原地覆盖全局配置并触发 `OnChange` 回调；管理端覆盖仍然优先。
2. **配置中心**：`运维中心 → 配置中心` 修改后调用 `config.Apply`，
   热项立即生效，不需要重启。

热生效项：日志级别、登录安全策略（验证码、失败锁定、密码强度、单点登录）、
限流参数、上传大小与扩展名、JWT 有效期（只影响新签发的令牌）。

需重启项：监听端口、数据库/Redis、存储后端与本地路径、邮件、JWT 密钥、
服务时区、备份策略、集群参数、业务模块开关。保存后接口返回 `needRestart`，页面会提示
并可直接跳到「危险操作」重启全部节点。

**多节点同步**：每次保存都会把覆盖表版本号加一，节点每 10 秒比对
`MAX(version)`，发现变化后自动重新加载，因此不必逐台操作；也可以在
「集群管理」页手动下发 `reload-config` 立即拉齐。

**最小部署配置**：`configs/config.min.yaml` 只保留端口、数据库、Redis、
JWT 密钥与节点地址，其余参数启动后在管理端设置。

---

## 14. 环境变量速查

```bash
# 数据库
TENON_DB_DRIVER=mysql
TENON_DB_DSN="tenon:pass@tcp(mysql:3306)/tenon?charset=utf8mb4&parseTime=True"
# 缓存（填地址即启用，无需 TENON_REDIS_ENABLED）
TENON_REDIS_ADDR=redis:6379
# 文件存储（填了 endpoint 与 bucket 即启用）
TENON_STORAGE_TYPE=auto
TENON_STORAGE_S3_ENDPOINT=minio:9000
TENON_STORAGE_S3_BUCKET=tenon
TENON_STORAGE_S3_ACCESS_KEY=...
TENON_STORAGE_S3_SECRET_KEY=...
# 邮件（填 host 即启用）
TENON_MAIL_HOST=smtp.example.com
TENON_MAIL_USERNAME=...
TENON_MAIL_PASSWORD=...
# 多实例无状态校验
TENON_RUNTIME_STATELESS=strict
TENON_RUNTIME_INSTANCES=3
# 安全
TENON_JWT_SECRET="$(openssl rand -base64 48)"
TENON_SECURITY_INTERNAL_TOKEN="$(openssl rand -hex 24)"
# 形态
TENON_MICRO_MODE=service
TENON_MICRO_MODULES=user,role,dept
TENON_GATEWAY_ROUTES="/api/v1=tenon-core"
# 注册发现
TENON_MICRO_REGISTRY_TYPE=dns
TENON_MICRO_REGISTRY_DNS_SUFFIX=tenon.svc.cluster.local:9000
```

> 切片类型（`micro.modules`、`micro.registry.endpoints`、`cors.allow_origins`、`storage.allow_ext`）
> 已显式绑定环境变量，用英文逗号分隔即可。

补充：

```bash
# 迁移与备份
TENON_MIGRATION_MODE=versioned
TENON_MIGRATION_TABLE_NAME=tenon_schema_migrations
TENON_BACKUP_ENABLED=true
TENON_BACKUP_DIR=/var/backups/tenon
TENON_BACKUP_RETAIN=14
TENON_BACKUP_ALLOW_RESTORE=false
# 接口文档（不配置时 dev 开 / prod 关）
TENON_DOCS_ENABLED=true
TENON_DOCS_PATH=/docs
# OAuth 凭据（名称转大写、非字母数字转下划线）
TENON_OAUTH_GITHUB_CLIENT_ID=...
TENON_OAUTH_GITHUB_CLIENT_SECRET=...
TENON_OAUTH_ENABLED=true
```

---

## 15. 数据库迁移、备份、第三方登录与接口文档

### 15.1 migration

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `migration.mode` | `versioned` | `versioned` 按版本执行 / `auto` 沿用 AutoMigrate / `off` 不迁移 |
| `migration.table_name` | `tenon_schema_migrations` | 版本记录表 |

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
> **最新一份永不删除**。演练命令 `tenon -config ... backup-prune --dry-run`。
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
| `client_id` / `client_secret` | 凭据，可用 `TENON_OAUTH_<NAME>_CLIENT_*` 覆盖 |
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
