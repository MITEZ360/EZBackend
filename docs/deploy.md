# 部署与运维

## 1. 产物

```bash
make build      # 前端 pnpm build → 编译 Go；bin/keel 约 35MB，静态链接
make build-all  # 交叉编译 linux/arm64、darwin、windows（复用已有 web/dist）
make build-go   # 只编译 Go，跳过前端（dist 已是最新时用）
```

> **构建机需要 pnpm**（`npm i -g pnpm`），但**运行环境不需要 Node**：
> 前端产物在 `web/dist`，已被 `//go:embed` 打进二进制。
> CI 里如果没有 Node，可以用 `docker build` 多阶段构建，Node 只存在于中间层。

部署只需要三样东西：

```
keel              二进制
configs/config.yaml
data/             数据目录（SQLite 与上传文件，可挂载）
```

前端已通过 `//go:embed` 打入二进制，不需要 Node、Nginx 托管静态资源。

---

## 2. 二进制部署（推荐起步）

```bash
# 1. 准备目录
mkdir -p /opt/keel/{configs,data,logs} && cd /opt/keel
cp keel configs/config.yaml .

# 2. 生产配置（示例）
export KEEL_APP_ENV=prod
export KEEL_JWT_SECRET="$(openssl rand -base64 48)"
export KEEL_SECURITY_INTERNAL_TOKEN="$(openssl rand -hex 24)"
export KEEL_SECURITY_CAPTCHA_ENABLED=true

# 3. 前台试跑
./keel -config configs/config.yaml
```

systemd 单元：

```ini
# /etc/systemd/system/keel.service
[Unit]
Description=Keel Server
After=network-online.target

[Service]
Type=simple
User=keel
WorkingDirectory=/opt/keel
EnvironmentFile=/opt/keel/.env
ExecStart=/opt/keel/keel -config /opt/keel/configs/config.yaml
Restart=always
RestartSec=3
LimitNOFILE=65535

# 优雅停机：先发 SIGTERM，等待 15s 再强杀
KillSignal=SIGTERM
TimeoutStopSec=20

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload && systemctl enable --now keel
journalctl -u keel -f
```

---

## 3. Docker

```bash
make docker
# 或
docker build -t keel:1.0.0 .

docker run -d --name keel -p 9000:9000 \
  -e KEEL_APP_ENV=prod \
  -e KEEL_JWT_SECRET="$(openssl rand -base64 48)" \
  -v $PWD/data:/app/data -v $PWD/logs:/app/logs \
  keel:1.0.0
```

镜像特点：

- 多阶段构建，运行层只有 `alpine + ca-certificates + tzdata + 二进制`，约 **42MB**。
- 非 root 运行（uid 10001），内置 `HEALTHCHECK` 调用 `/healthz`。

---

## 4. Compose（MySQL + Redis + Keel）

```bash
make compose        # 等价于 docker compose -f deploy/docker-compose.yml up -d --build
make compose-logs
make compose-down
```

| 服务 | 端口 | 说明 |
| --- | --- | --- |
| `keel` | 9000 | 应用，等待 mysql/redis 健康后启动 |
| `mysql` | 3306（仅内网） | 数据持久化到 `mysql-data` 卷 |
| `redis` | 6379（仅内网） | AOF 持久化，256MB 上限 + LRU |

首次启动会自动建表并写入种子数据，访问 <http://localhost:9000>，账号 `admin / Admin@123456`。

> 生产环境请修改 `deploy/docker-compose.yml` 中的 `KEEL_JWT_SECRET`、数据库密码，
> 并把 `docker-compose.override.yml` 用于本地覆盖。

---

## 5. Kubernetes

两套清单：

```bash
kubectl apply -f deploy/k8s/keel-core.yaml       # 单体形态（推荐）
kubectl apply -f deploy/k8s/keel-gateway.yaml    # 网关 + 领域服务形态
```

`keel-core.yaml` 包含：

| 资源 | 要点 |
| --- | --- |
| Namespace | `keel` |
| ConfigMap | 非敏感配置，环境变量注入 |
| Secret | JWT 密钥、内部令牌、数据库 DSN |
| Deployment | 2 副本，滚动更新 `maxUnavailable=0`，非 root，资源 100m/128Mi 起 |
| PVC | `data` 目录 10Gi（上传文件、SQLite） |
| Service | ClusterIP 9000 |
| HPA | CPU 70% 触发，2 → 10 副本 |
| Ingress | `keel.example.com`，body-size 32m |

探针：

```yaml
startupProbe:   { httpGet: { path: /healthz, port: http }, failureThreshold: 30, periodSeconds: 2 }
livenessProbe:  { httpGet: { path: /healthz, port: http }, periodSeconds: 15 }
readinessProbe: { httpGet: { path: /readyz,  port: http }, periodSeconds: 10 }
```

- `startupProbe` 给慢启动留 60s（首次建表 + 种子数据）。
- `readinessProbe` 只看数据库连通，数据库抖动时不立刻重启 Pod，而是摘掉流量。

**多副本注意**：

1. 必须启用 Redis（`KEEL_REDIS_ENABLED=true`），否则会话/权限缓存不共享。
2. `storage.local_dir` 需要共享存储（RWX PVC / NFS），或改用对象存储。
3. 定时任务需单实例运行：给 `job` 模块单独部署一个 `replicas: 1` 的服务，
   或在其它副本上用 `KEEL_MICRO_MODULES` 排除 `job`。

---

## 6. 可观测

### 6.1 健康检查

```bash
curl -s localhost:9000/healthz | jq
{
  "status": "up",
  "version": "1.0.0",
  "checks": { "self": "up", "database": "up", "cache": "up" },
  "time": "2026-11-02T21:12:35+08:00"
}
```

依赖异常时返回 503，并在 `checks` 中给出具体原因（含下游错误信息）。

### 6.2 Prometheus

```yaml
scrape_configs:
  - job_name: keel
    metrics_path: /metrics
    static_configs:
      - targets: ["keel:9000"]
```

指标：

| 指标 | 类型 | 说明 |
| --- | --- | --- |
| `http_requests_total` | counter | 按 method/path/status 维度 |
| `http_request_duration_seconds` | histogram | 分位数延迟 |
| `http_inflight_requests` | gauge | 在途请求数 |
| `business_operations_total` | counter | 关键业务动作计数 |

常用告警：

```promql
# P99 延迟 > 500ms
histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket[5m]))) > 0.5
# 5xx 比例 > 1%
sum(rate(http_requests_total{status=~"5.."}[5m])) / sum(rate(http_requests_total[5m])) > 0.01
```

### 6.3 日志

- 默认 JSON 落 `logs/keel.log`，按 64MB 轮转、保留 14 天、gzip 压缩。
- 关键字段：`level / time / caller / msg / trace_id / method / path / route / status / cost_ms / ip / ua`。
- 采集参考（Loki / Filebeat）：

```yaml
# docker-compose 里追加
logging:
  driver: json-file
  options: { max-size: "64m", max-file: "5" }
```

按 traceId 串一次请求：

```bash
jq -c 'select(.trace_id=="0f3a1c9e-...")' logs/keel.log
```

### 6.4 审计

管理台「运维中心 → 登录日志 / 操作日志 / 在线用户」可查：

- 谁、什么时候、从哪个 IP、用什么设备、调了哪个接口、耗时多久、结果如何。
- 请求/响应体各截断 4KB，`password`、`token`、`secret` 等字段自动脱敏。
- 支持按天清理，内置任务 `log.cleanup` 每天 03:00 执行。

---

## 7. 数据库运维

### 7.1 迁移

默认 `db.auto_migrate: true`，启动时自动建表与加列（只增不减，安全）。
生产建议：

```yaml
db:
  auto_migrate: false   # 用发布流程控制
  seed: false           # 种子数据只在初始化时写一次
```

首次初始化可以临时打开，或手动执行一次 `keel -config ... ` 后关闭。

### 7.2 备份

SQLite：

```bash
sqlite3 data/keel.db ".backup '/backup/keel-$(date +%F).db'"
```

MySQL：

```bash
mysqldump --single-transaction --quick keel | gzip > /backup/keel-$(date +%F).sql.gz
```

建议每天全备 + binlog 增量，保留 7~30 天。

### 7.3 容量参考

| 数据 | 量级 | 说明 |
| --- | --- | --- |
| 用户/角色/菜单/部门 | 万级 | 常驻缓存，压力极小 |
| 操作日志 | 百万级/年 | 建议按月分表或定期归档，保留 90 天 |
| 登录日志 | 百万级/年 | 同上 |
| 文件 | 取决于业务 | 建议 6 个月以上冷数据转对象存储 |

---

## 8. 扩容与调优

| 场景 | 措施 |
| --- | --- |
| QPS 上不去 | 换 MySQL/PostgreSQL；开启 Redis；增加副本（无状态） |
| 列表接口慢 | 确认 `slow_threshold` 日志，给过滤列加索引，避免深分页（改用游标） |
| 登录慢 | bcrypt cost 固定为 10，属预期；高并发下可前置验证码或提高 Redis 命中率 |
| 内存增长 | 检查本地缓存 `redis.local_size`，必要时启用 Redis 卸载 |
| 上传慢 | `server.write_timeout` 调大；`storage.local_dir` 放大容量或换对象存储 |

GOMEMLIMIT（Go 1.19+）可按容器限额设置，例如容器 512Mi：

```yaml
env:
  - name: GOMEMLIMIT
    value: "400MiB"
```

---

## 9. 升级流程

```bash
# 1. 备份数据库与 data 目录
# 2. 拉取新二进制，滚动替换（K8s 直接改镜像 tag）
# 3. 观察 /healthz、错误率、P99
# 4. 回滚：换回旧二进制即可（表结构只增不减，向后兼容）
```

优雅停机保证在途请求处理完成：

```
SIGTERM → HTTP Server.Shutdown（等待在途请求，最长 shutdown_timeout）
        → 定时任务停止
        → 审计队列 flush
        → 缓存/数据库连接关闭
```

---

## 10. 安全检查清单

- [ ] `jwt.secret` 已替换为 ≥32 位随机串（prod 下不替换无法启动）
- [ ] `security.internal_token` 已替换
- [ ] `security.captcha_enabled=true`
- [ ] `cors.allow_origins` 收敛为具体域名
- [ ] `server.trust_proxy=true` 且只信任已知代理网段
- [ ] `/metrics` 配置 `token` 或仅内网可达
- [ ] 数据库账号最小权限，禁止 root
- [ ] 管理台不直接暴露公网，或前置 WAF / IP 白名单
- [ ] 日志目录与上传目录不在 Web 根目录下
- [ ] 定期检查「在线用户」与「操作日志」异常行为
