# 配置说明

配置加载顺序（后者覆盖前者）：

```
内置默认值  →  configs/config.yaml  →  环境变量（KEEL_ 前缀）  →  运行期覆盖（-mode）
```

环境变量映射规则：配置键的 `.` 替换为 `_`，加 `KEEL_` 前缀。
例如 `server.port` → `KEEL_SERVER_PORT`，`db.conn_max_lifetime` → `KEEL_DB_CONN_MAX_LIFETIME`。

---

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
| `driver` | `sqlite` | `sqlite` / `mysql` / `postgres` |
| `dsn` | `data/keel.db` | 见下方示例 |
| `max_open_conns` | `50` | 连接池上限（实际最小值 10） |
| `max_idle_conns` | `10` | 空闲连接（实际最小值 5） |
| `conn_max_lifetime` | `1h` | 连接最大存活 |
| `conn_max_idle_time` | `10m` | 空闲连接回收 |
| `slow_threshold` | `200ms` | 慢 SQL 阈值，超过记 Warn |
| `log_level` | `warn` | GORM 日志级别，`info` 会打印全部 SQL |
| `auto_migrate` | `true` | 启动自动建表/加列（生产建议关掉，用脚本迁移） |
| `seed` | `true` | 首次启动写入内置菜单/部门/字典/账号（有用户则跳过） |

DSN 示例：

```yaml
# SQLite（框架会自动附加 busy_timeout / WAL / synchronous=NORMAL）
dsn: data/keel.db

# MySQL
driver: mysql
dsn: keel:Keel@123456@tcp(127.0.0.1:3306)/keel?charset=utf8mb4&parseTime=True&loc=Asia%2FShanghai

# PostgreSQL
driver: postgres
dsn: host=127.0.0.1 user=keel password=Keel@123456 dbname=keel port=5432 sslmode=disable TimeZone=Asia/Shanghai
```

## 5. redis

| 键 | 默认值 | 说明 |
| --- | --- | --- |
| `enabled` | `false` | 关闭时自动降级为进程内 LRU 缓存 |
| `addr` | `127.0.0.1:6379` | 地址，哨兵/集群请用支持多地址的客户端替换实现 |
| `password` / `db` | 空 / `0` | 认证与库号 |
| `pool_size` | `32` | 连接池 |
| `prefix` | `keel` | 键前缀，便于多环境共用实例 |
| `dial_timeout` / `read_timeout` | `3s` | 超时 |
| `local_size` | `100000` | 本地缓存容量（未启用 Redis 时生效） |

> **多实例部署必须开启 Redis**：会话、权限、字典缓存不共享会导致"随机掉登录"。

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
| `type` | `local` | 当前实现为本地磁盘，`s3` 字段已预留 |
| `local_dir` | `data/uploads` | 存储根目录，按 `yyyy/MM/dd` 分目录 |
| `base_url` | `/uploads` | 对外访问前缀 |
| `max_size` | `20971520`（20MB） | 单文件上限 |
| `allow_ext` | 图片/文档/压缩包/音视频常见后缀 | 扩展名白名单 |

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

## 13. 配置热更新

`bootstrap` 启动时会调用 `config.Watch()`，监听配置文件变更：

1. 重新解析 → 2. 校验（失败则忽略本次变更，保留旧配置）→ 3. 替换全局配置 → 4. 触发 `OnChange` 回调。

当前已支持热更新的项：日志级别、限流参数、CORS。
**不支持**热更新的项（需要重启）：监听端口、数据库/Redis 连接、JWT 密钥、微服务形态。

---

## 14. 环境变量速查

```bash
# 数据库
KEEL_DB_DRIVER=mysql
KEEL_DB_DSN="keel:pass@tcp(mysql:3306)/keel?charset=utf8mb4&parseTime=True"
# 缓存
KEEL_REDIS_ENABLED=true
KEEL_REDIS_ADDR=redis:6379
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
