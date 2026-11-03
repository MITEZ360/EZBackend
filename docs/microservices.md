# 微服务实践

同一份代码，三种运行形态。切换只需要改配置（或 `-mode` 参数），业务代码一行不动。

| 形态 | 进程加载内容 | 适用阶段 |
| --- | --- | --- |
| `mono` | 全部模块 + 数据库 + 缓存 + 定时任务 | 起步、单机、私有化交付 |
| `gateway` | 无数据库、无业务模块，仅转发 + 前端 + 指标 | 需要统一入口 / 灰度 / 独立扩缩容 |
| `service` | 全部基础设施，仅暴露 `micro.modules` 中的模块 | 热点模块独立扩容、团队按域拆分 |

---

## 1. 演进路径

```
① 单体          1 个进程，1 个数据库            —— 覆盖 95% 场景
        ↓ 流量变大 / 团队拆分
② 网关 + 单体    网关 2 副本，单体 3 副本        —— 入口与业务解耦，可灰度
        ↓ 单模块成为瓶颈
③ 网关 + 领域服务  用户服务 / 内容服务 独立部署     —— 按需拆分，共用一套代码
```

**建议**：不要一上来就拆。先用单体跑通业务，等到"某模块需要独立扩容"或"某模块需要独立发布"时再拆。

---

## 2. 单体（mono）

```yaml
micro:
  mode: mono
```

- 14 个模块全部挂载，114 条路由。
- 数据库、缓存、定时任务都在本进程。
- 只有 1 个进程时，默认的 SQLite + 进程内缓存即可，无需外部依赖。

---

## 3. 网关（gateway）

```yaml
micro:
  mode: gateway
  timeout: 5s
  retries: 2
  registry:
    type: static
    endpoints:
      - "keel-core=10.0.0.11:9000,10.0.0.12:9000"
  routes:
    - prefix: /api/v1/auth        # 认证单独指向 auth 服务
      service: keel-auth
    - prefix: /api/v1             # 其余走核心服务
      service: keel-core
      strip_prefix: false
```

特点：

- **不连数据库、不加载业务模块**，进程常驻内存约 20MB，可独立扩容到几十副本。
- 路由按**最长前缀**匹配，支持 `strip_prefix` 去掉前缀后转发。
- 每个请求按轮询挑选实例；失败实例进入 5 秒冷却（连续失败线性延长至 1 分钟），下次自动恢复。
- 转发失败会**换实例重试**（`micro.retries`），且只在响应未写出时重试，避免重复写。
- 自动注入 `X-Internal-Token`、`X-Caller-Service`、`X-Forwarded-For/Proto`，透传 `X-Trace-Id`。
- 后端全挂返回 `503` + 统一错误体，不会挂住连接。

启动：

```bash
./keel -config configs/config.gateway.yaml
# 或纯环境变量（容器友好）
KEEL_MICRO_MODE=gateway \
KEEL_SERVER_PORT=9100 \
KEEL_GATEWAY_ROUTES="/api/v1=keel-core,/uploads=keel-file" \
KEEL_MICRO_REGISTRY_ENDPOINTS="keel-core=127.0.0.1:9000" \
./keel
```

> 网关形态下 `/healthz` 会返回 `database: disabled`、`cache: disabled`，这是预期行为。

---

## 4. 领域服务（service）

```yaml
micro:
  mode: service
  modules: [user, role, dept, membership, monitor]   # 只暴露这些模块的接口
```

- 未列出的模块**不注册路由**，访问返回 404（而不是 403），减少攻击面。
- `auth` 与公共接口始终启用（否则无法登录）。
- 多个服务实例共用同一个数据库与 Redis；`micro.modules` 只是路由裁剪，不隔离数据。

可用的模块名：

```
auth  user  role  menu  dept  dict  config  notice  audit  job  file  dashboard  monitor
```

拆分示例：

| 服务 | modules | 说明 |
| --- | --- | --- |
| keel-auth | `[auth, menu]` | 登录、令牌、导航树 |
| keel-user | `[user, role, dept, membership]` | 组织、账号与会员等级 |
| keel-ops | `[audit, job, file, monitor]` | 运维能力，可单独限流 |
| keel-core | 全部 | 兜底，未拆分部分 |

---

## 5. 服务发现

| 类型 | 配置 | 适用 |
| --- | --- | --- |
| `static` | `registry.endpoints: ["svc=ip1:port,ip2:port"]` | 物理机、小集群、Compose |
| `dns` | `registry.dns_suffix: "keel.svc.cluster.local:9000"` | K8s Headless Service、内网 DNS |

`dns` 模式解析 `服务名 + "." + suffix` 的 A 记录，结果缓存 `registry.interval`（默认 30s），
实例增减自动生效，无需重启网关。

新增注册中心（Consul/Nacos/Etcd）只需实现一个接口：

```go
type Registry interface {
	Name() string
	Resolve(ctx context.Context, service string) ([]Instance, error)
	Close() error
}
```

然后在 `pkg/discovery.New` 里加一个分支即可，网关与客户端无需改动。

---

## 6. 服务间调用

`pkg/client` 封装了寻址、重试、熔断与链路透传：

```go
import (
    "github.com/keel-framework/keel/pkg/client"
    "github.com/keel-framework/keel/pkg/discovery"
)

reg, _ := discovery.New("static", []string{"keel-user=10.0.0.21:9000"}, "", 0)

userCli := client.New(client.Options{
    Service:        "keel-user",
    Registry:       reg,
    Timeout:        2 * time.Second,
    Retries:        2,
    BreakerEnabled: true,
    MaxFailed:      10,
    Headers:        http.Header{"X-Internal-Token": {cfg.Security.InternalToken}},
    Logger:         log,
})

var out struct {
    Code int `json:"code"`
    Data struct {
        ID       string `json:"id"`
        Nickname string `json:"nickname"`
    } `json:"data"`
}
if err := userCli.Get(ctx, "/api/v1/users/"+id, &out); err != nil {
    // 网络错误、5xx、熔断打开都会返回 error；4xx 以 *client.Error 返回，不重试
}

// 透传链路标识，跨服务日志可用同一个 traceId 检索
ctx = client.WithTraceID(ctx, traceID)
```

行为约定：

| 场景 | 行为 |
| --- | --- |
| 网络错误 / 超时 | 换实例重试，指数退避（30ms、60ms…） |
| 5xx / 429 | 重试 |
| 4xx | 不重试，返回 `*client.Error`（含状态码与响应体） |
| 连续失败 ≥ `max_failed` | 熔断打开 `breaker.timeout` 秒，期间快速失败 |
| 单实例失败 | 该实例冷却 5s，其余实例继续服务 |

---

## 7. 拆分的必要前提

| 项 | 要求 | 原因 |
| --- | --- | --- |
| 共享数据库 | 必须同一个库（或做好分库与同步） | 用户/角色/菜单是跨服务基础数据 |
| 共享缓存 | **必须配置 `redis.addr`** | 验证码、登录失败计数、权限点、字典缓存、限流配额要跨进程共享，否则随机掉登录 |
| JWT 密钥 | 所有服务一致 | 令牌由 auth 签发，其它服务校验 |
| 内部令牌 | `security.internal_token` 一致 | 服务间调用鉴权 |
| 时钟 | 各节点 NTP 同步 | 令牌 exp/nbf、雪花 ID 依赖时间 |
| 定时任务 | 无需特殊处理 | 已用 Redis 锁抢占，多实例只执行一份；未启用 Redis 时锁仅在进程内有效 |
| 文件存储 | 使用对象存储 | 本地磁盘只有当前实例可见，上传后换实例下载会 404 |

后两条最容易被忽略，线上表现为"任务跑了两遍""令牌刚签发就过期""文件上传成功却下载 404"。
启动时 `runtime.stateless=auto` 会针对这些项给出告警，`strict` 直接拒绝启动。

---

## 8. 可观测

- 网关会透传 `X-Trace-Id`，各服务日志中的 `trace_id` 相同，可直接串起来查。
- `/metrics` 每个服务独立暴露，Prometheus 用 `kubernetes_sd` 或静态目标抓取。
- 建议在 Grafana 里按 `service` 维度看 `http_request_duration_seconds` 分位数，定位是哪个环节慢。

---

## 9. 常见问题

**Q：网关需要鉴权吗？**
默认不鉴权，只转发。如需在网关做统一鉴权（例如屏蔽未登录请求），把 `Auth` 中间件挂到网关路由上即可，
但要注意网关不连数据库，会话校验需走 Redis。

**Q：service 形态下访问未启用的模块返回什么？**
404 + `{"code":40400,"message":"接口不存在"}`。这是有意为之：不暴露"该模块存在于别处"的信息。

**Q：能否不同服务用不同数据库？**
可以，`db.dsn` 各自配置。但跨库的用户/角色数据需要同步方案，超出框架范围。

**Q：拆分后文件上传怎么处理？**
文件默认落本地磁盘。多实例请把 `storage.local_dir` 挂到共享存储（NFS/OSS 挂载），
或按 `storage.s3` 预留字段实现对象存储适配。
