# 服务治理面设计方案

> 状态：草案，待评审
> 日期：2026-11-26
> 范围：集群管理、服务发现、动态路由、配置中心
> 相关文档：[architecture.md](architecture.md)、[microservices.md](microservices.md)、[frontend-modules.md](frontend-modules.md)

## 1. 目标与非目标

### 1.1 目标

把现在"配置写死在 yaml、网关改路由要重启、实例列表靠手工维护"的状态，变成一套可运营的治理面：

- 服务与实例自动注册，健康状态可观测，支持优雅上下线
- 路由规则在线增删改，**不重启网关**即可生效
- 业务配置与治理配置统一版本化，支持灰度、回滚、审计
- 单实例到 100+ 实例同一套代码，只调参数
- 全部自研，不引入 Nacos / Consul / Etcd / Apollo 等外部依赖

### 1.2 非目标

| 不做 | 原因 |
| --- | --- |
| 接入外部注册中心 | 架构是共享库多实例，外部注册中心是多余依赖 |
| K8s Ingress Controller / CRD | 与当前交付形态无关，留扩展位即可 |
| 跨机房多活、异地容灾 | 超出治理面职责，需要独立的流量调度层 |
| 链路追踪、日志采集 | 属可观测体系，单独设计 |
| 服务网格（Sidecar） | 侵入部署形态，与单二进制交付冲突 |

## 2. 设计准则

1. **单一真相源在库**。所有治理状态落 DB，进程内存只是缓存，重启可完整恢复。
2. **数据面零外部依赖**。网关只认控制面 HTTP，不连 DB、不加载业务模块。
3. **读路径只走内存**。请求处理不查库、不加锁、不分配，配置切换靠指针替换。
4. **版本单调，永不回退**。用 revision 判断新旧，不用时间戳；回滚也是产生新 revision。
5. **变更不中断**。校验前置、原子切换、在途排空，三层缺一不可。
6. **规模靠参数，不靠分支**。单实例和 100+ 实例共用一套代码路径。
7. **模块可拆可合**。每个能力是独立模块，既能装进主程序，也能单独生成一个二进制。
8. **借鉴设计，不借依赖**。Apollo 的 Release 模型、Nacos 的长轮询、Envoy xDS 的版本 ACK、K8s 的 resourceVersion，全部自己实现。

## 3. 总体架构

### 3.1 三个平面

| 平面 | 组成 | 职责 | 依赖 |
| --- | --- | --- | --- |
| 控制面 | registry / route / configcenter / cluster 模块 | 存储、校验、编译、发布、灰度、回滚 | DB |
| 数据面 | 网关、进程内 SDK（`pkg/gov`） | 内存快照、路由匹配、负载均衡、限流熔断 | 只依赖控制面 HTTP |
| 存储 | `gov_*` 表 + 快照表 | 唯一真相源，保留全部历史版本 | DB |

数据面**不做任何解释性工作**：路由规则在控制面编译成有序匹配表，数据面只执行。复杂逻辑留在控制面，数据面保持简单——这是 xDS 的核心思路，也是网关能始终保持低延迟的前提。

### 3.2 部署形态

| 形态 | 组成 | 适用 |
| --- | --- | --- |
| 单体 | 全部模块 + 数据面 SDK 同进程 | 单实例、中小集群；配置变更走进程内事件，零网络开销 |
| 控制面 + 网关 | tenon-core（模块 + DB）+ tenon-gateway（纯转发） | 需要独立扩容入口、灰度前置 |
| 单模块独立 | `make dist MODULES=registry NAME=Registry` | 只做注册中心、只做配置中心等 |

`distgen` 会自动补齐依赖（`route` 依赖 `registry`），因此单模块发行版也一定能编译运行。网关是唯一不按模块装配的形态，见 §4.3。

## 4. 模块划分

### 4.1 控制面模块

| 模块 | 职责 | 依赖 | 现有代码 |
| --- | --- | --- | --- |
| `registry` | 服务目录、实例注册、健康检查、元数据、拓扑 | 无 | 新建，吸收 `cluster` 的节点模型 |
| `route` | 路由规则、编译、发布、灰度、回滚 | `registry` | **重写** `routemanage`（现仅展示 gin 路由） |
| `configcenter` | 命名空间、配置项、版本、灰度、监听 | 无 | **重写**（现为运行参数在线编辑） |
| `cluster` | 节点视角、指令下发、版本分布、优雅下线 | `registry` | 增强现有 `cluster` |

`registry` 与 `cluster` 的分工：`registry` 管"服务与实例的事实"，`cluster` 管"对 Tenon 自身实例的动作"（重启、配置纠偏、批量指令）。两者共用 `gov_instance` 表，`cluster` 是操作层，不重复建模。

### 4.2 数据面库（`pkg/`，不是模块）

| 包 | 职责 |
| --- | --- |
| `pkg/gov/snapshot` | 快照类型、校验和、原子容器 `atomic.Pointer` |
| `pkg/gov/client` | 拉取、SSE 订阅、轮询兜底、`Apply(rev)`、本地快照落盘 |
| `pkg/gov/routing` | 编译产物的执行器：最长前缀 + 规则链匹配 |
| `pkg/gov/balance` | 加权轮询、最少连接、一致性哈希（升级现 `pkg/discovery.Balancer`） |
| `pkg/gov/limit` | 进程内令牌桶，按路由/租户维度 |
| `pkg/gov/breaker` | 熔断器，失败率 + 半开探测 |

放在 `pkg/` 而不是 `internal/modules/` 的原因：网关不加载模块，但需要这些能力；业务服务也可能直接嵌入 SDK。模块是"带表带菜单的管理功能"，`pkg/gov` 是"无状态的运行时能力"，边界不能混。

### 4.3 网关

网关不是 modulekit 模块，是装配形态：

- 现在：`micro.mode=gateway`，同一二进制，不加载模块、不连 DB
- M3：新增 `cmd/tenon-gateway`，只 import `pkg/gov` + `internal/micro`，产物从 40MB+ 降到 10MB 以内

两者共享同一份数据面实现，区别只在编译裁剪。

## 5. 核心模型

### 5.1 Revision：全局号 + 分区号

```
gov_revision(id=1, global_rev, updated_at)
```

- **全局号**：每次任意治理配置发布 +1，用于"这台实例落后几版"的观测口径
- **分区号**：每个域独立递增，用于"拉哪一块"。分区键形如 `route`、`registry`、`config:<namespace>`
- 数据面持有 `(globalRev, fingerprint)`，其中 `fingerprint` 是"分区键 + 分区校验和"排序后的 SHA-256
- 轮询带上自己的 `fingerprint`；控制面算出来一样就回 `304`，不一样就给全量

**去重判据必须是内容指纹，不能是全局号。**全局号只在发布时递增，而 registry 分区的内容会随时间变化——实例超时下线不需要任何写操作，注册新实例也不分配新版本。用全局号做判据，数据面会拿到 304 然后永远停在旧的实例表上：新扩容的机器一台流量都不接，控制面上却一切正常。反过来，"某个无关域发布了一下、全局号涨了但内容没变"也不该让所有数据面重建路由表。指纹只覆盖内容，两头都对。

分配必须在事务内完成：SQLite 用 `BEGIN IMMEDIATE` + `UPDATE ... RETURNING`，MySQL/PG 用 `SELECT ... FOR UPDATE`。并发冲突重试 3 次，不做无锁自增——单调性是所有一致性的基础，不能赌。

### 5.2 快照

快照是**已编译、不可变**的发布单元：

```go
type Snapshot struct {
    GlobalRev int64
    Parts     map[string]Part // key: route / registry / config:default
    Checksum  string          // sha256(payload)
}

type Part struct {
    Rev      int64
    Payload  []byte // 压缩后的 JSON
    Checksum string
}
```

规则：已发布快照永不修改；校验和不匹配不应用；快照体积超过阈值（默认 4MB）时按分区拆成多段传输。

### 5.3 服务与实例

```go
type Service struct {
    ID      string   // 服务名，全局唯一
    Name    string   // 展示名
    Owner   string   // 负责人
    Group   string   // 分组
    Desc    string
    Meta    JSON     // 扩展元数据
    Status  string   // enabled / disabled
}

type Instance struct {
    ID       string   // 稳定 ID，默认 host:port，可用 instance_id 覆盖
    Service  string
    Host     string
    Port     int
    Version  string   // 应用版本，灰度用
    Zone     string   // 机房/区域
    Weight   int      // 默认 100
    Labels   JSON     // canary / env / 自定义
    Status   string   // online / draining / offline
    LastSeen time.Time
    StartedAt time.Time
}
```

状态机：`online → draining → offline`。`draining` 表示"从发现列表摘除，但在途请求允许完成"；只有 `online` 参与负载均衡。

### 5.4 路由规则

```go
type Route struct {
    ID       string
    Name     string
    Priority int      // 越大越先匹配
    Match    Match    // prefix / methods / headers / query
    Target   Target   // service / version / weight
    Timeout  Duration
    Retries  int
    Status   string   // draft / published / archived
}
```

匹配顺序必须**完全确定**，否则同一请求可能命中不同规则：

1. `priority` 降序
2. 条件具体度降序（方法 + Header + Query 的条件数量）
3. 前缀长度降序
4. ID 字典序（最终兜底，保证稳定性）

编译产物是按前缀分组的规则链，匹配复杂度 O(路径段数)，与规则总数无关。

### 5.5 配置项

```go
type ConfigItem struct {
    Namespace string // 环境/业务域隔离
    Key       string
    Value     string
    Type      string // string / number / bool / json
    Encrypted bool   // AES-GCM 落库，接口不回显
    Scope     string // global / service / instance
    Service   string // scope=service 时生效
}
```

发布粒度是**命名空间整体**，不是单 key——客户端按命名空间订阅，单 key 发布会把订阅语义复杂化。灰度按实例标签选择，不做按 key 灰度。

## 6. 数据面

### 6.1 读路径

```go
var current atomic.Pointer[snapshot.Snapshot]

func Snapshot() *Snapshot { return current.Load() }
```

请求进来 load 一次，之后全用局部变量。快照不可变，切换是换指针，不存在"半新半旧"。热路径零锁、零分配，P99 与配置规模无关。

路由匹配在编译产物上执行：先按路径段逐级查前缀表，命中后按规则链检查方法/Header/Query，第一个全匹配的规则生效。

### 6.2 刷新协议

| 方式 | 协议 | 触发 |
| --- | --- | --- |
| 实时 | SSE `/internal/gov/watch`，只推 `{globalRev}` 变更信号，不推 payload | 常态 |
| 兜底 | 轮询 `/internal/gov/snapshot?fp=<指纹>`，内容一致返回 `304` | 断连、跨网段 |
| 手动 | 管理端下发 → 并发 POST `/internal/gov/reload` | 排障、强制收敛 |

三个入口收敛到同一个 `Sync()`：

```go
func (c *Client) Sync() error {
    snap, err := c.fetch(c.fp)          // 带上手上的指纹，没变就 304
    if err != nil || snap == nil { return err }
    if snap.Fingerprint() == c.fp { return nil }
    c.store.Store(snap)                 // 原子切换，OnApply 重建路由表
    return nil
}
```

**SSE 只发信号不发数据**，是为了避免大快照堵住事件流；数据面收到信号后主动拉取，天然支持重连补偿。

### 6.3 原子切换与在途排空

1. **构建期**：解码 → 校验 → 建索引，全部在临时对象上完成；任何错误直接丢弃，保留旧快照并上报。
2. **切换期**：`Store(ptr)`，纳秒级。
3. **生效期**：旧快照等待引用自然消亡；实例从 `online` 转 `draining` 时，先从负载均衡列表摘除，等在途请求完成或到达 `drain_timeout`（默认 30s）再关闭。

### 6.4 三级降级启动

网关启动时按顺序取配置：

1. 本地磁盘快照 `data/gov-snapshot-<service>.json`（上次成功应用的那份）
2. 控制面 HTTP 全量拉取（覆盖本地快照）
3. 配置文件里的 `micro.registry.endpoints`（首次启动且控制面不可用时的兜底）

这三级保证：**控制面挂掉不影响已运行的数据面，也不影响重启后的数据面启动**。

## 7. 控制面

### 7.1 表结构

| 表 | 关键字段 | 说明 |
| --- | --- | --- |
| `gov_revision` | id, global_rev | 单行，全局版本分配器 |
| `gov_service` | id, name, owner, group, meta, status | 服务目录 |
| `gov_instance` | id, service_id, host, port, version, zone, weight, labels, status, last_seen | 实例，唯一索引 `(service_id, host, port)` |
| `gov_health_check` | service_id, type, path, interval, timeout, threshold | 主动探测配置 |
| `gov_route` | id, name, priority, match, target, timeout, retries, status | 路由规则，`match/target` 存 JSON |
| `gov_release` | id, domain, revision, payload, checksum, targets, status, operator, note | 发布记录，含灰度批次 |
| `gov_config_ns` | id, namespace, env, desc | 配置命名空间 |
| `gov_config_item` | id, ns_id, key, value, type, encrypted, scope, service_id, updated_by | 配置项 |
| `gov_command` | id, action, scope, status, total, succeeded, failed, operator | 指令下发（由 `cluster` 使用） |
| `gov_command_target` | command_id, instance_id, status, message, started_at, finished_at | 指令执行明细 |

`sys_cluster_node` 迁移到 `gov_instance`（`service_id = tenon-core`），迁移脚本保留旧表一个版本周期再删。

### 7.2 接口

控制面（全部走鉴权 + 审计）：

```
GET    /api/v1/gov-services
POST   /api/v1/gov-services
GET    /api/v1/gov-services/:id/instances
POST   /api/v1/gov-instances/:id/drain
POST   /api/v1/gov-instances/:id/undrain
GET    /api/v1/gov-routes
POST   /api/v1/gov-routes
PUT    /api/v1/gov-routes/:id
DELETE /api/v1/gov-routes/:id
POST   /api/v1/gov-routes/compile        # 预编译校验，不发布
GET    /api/v1/gov-releases?domain=route
POST   /api/v1/gov-releases              # 创建发布（草稿→灰度）
POST   /api/v1/gov-releases/:id/promote  # 进入下一批/全量
POST   /api/v1/gov-releases/:id/rollback
GET    /api/v1/gov-configs?namespace=
PUT    /api/v1/gov-configs               # 批量更新一个命名空间
POST   /api/v1/gov-configs/publish
GET    /api/v1/gov-nodes?service=tenon-core
POST   /api/v1/gov-commands
```

数据面（内置 token + HMAC，不进 swagger）：

```
GET  /internal/gov/snapshot?fp=<sha256>   # 带指纹，内容一致回 304
GET  /internal/gov/watch          # SSE
POST /internal/gov/reload         # 手动刷新，不带指纹，强制给全量
POST /internal/gov/ack            # 应用成功回报，用于收敛观测与灰度门禁
```

### 7.3 发布流程

```
草稿 → 校验 → 创建 release → 灰度批次 → 观察窗口 → 全量 → 完成
                                  ↓ 失败/超阈值
                                回滚
```

约束：

- 同一域同一时间只允许一个处于 `canary` 的 release，避免多版本交叉
- 灰度批次 = 实例集合（标签选择器或显式列表），每批一个 `gov_release` 记录
- M2 起：批次下发后等待 ACK + 观察窗口（默认 60s），错误率或 P99 超阈值可自动回滚
- 回滚 = 用旧 payload 生成新 revision，**不做版本号回退**

### 7.4 校验规则

写入时就拒绝，不留到数据面：

| 域 | 校验 |
| --- | --- |
| 路由 | 前缀不能为空；同优先级下前缀+条件不能完全重复；target 服务必须存在且 enabled；timeout/retries 在合法区间 |
| 服务 | 名称符合 `^[a-z][a-z0-9-]{1,62}$`；实例 host/port 合法；同一服务内 ID 唯一 |
| 配置 | key 符合命名规范；`scope=service` 时必须给 service；`encrypted` 项禁止明文回显 |
| 通用 | 引用的服务/命名空间必须存在；payload 序列化后不超过阈值 |

## 8. 规模自适应

同一套代码，只调参数：

| 参数 | 单实例 | 100+ 实例 | 说明 |
| --- | --- | --- | --- |
| `gov.refresh.mode` | `local` | `sse` | 单体模式下直接内存事件，不启 HTTP 订阅 |
| `gov.refresh.poll` | 30s | 5s | 轮询兜底间隔 |
| `gov.heartbeat.interval` | 30s | 10s | 实例心跳 |
| `gov.heartbeat.batch` | 1 | 100/s | 控制面内存聚合后批量 upsert |
| `gov.revision.window` | 0 | 500ms | 变更合并窗口，窗口内多次修改只产一个 revision |
| `gov.subscribe.scope` | all | by-service | 实例只订阅自己承载的服务 |
| `gov.fanout.concurrency` | 同步 | 16 | 手动下发并发池 |

容量估算（100 实例、1000 条路由）：

- 轮询：100 / 5s = 20 QPS，304 响应约 200B，带宽 4KB/s（指纹一致才有 304，所以内容真变时该拉就拉）
- SSE：100 连接 × 约 20KB 内存 = 2MB
- 心跳：100 / 10s = 10 写/s，批量合并后 1 次 upsert/s
- 快照：1000 条规则约 300KB JSON，压缩后 40KB；构建约 5ms，变更时才发生
- 下发：100 个实例、并发 16，约 500ms 完成一轮

瓶颈不在读，在**变更瞬间的 fan-out** 和**心跳写库**，所以合并窗口和批量 upsert 是必做项，不是优化项。

## 9. 不变量

这些条件任何一条被破坏都是设计缺陷，不是 bug：

1. 数据面永不因配置问题停止服务——加载失败保留旧快照
2. 已发布的 revision 不可变
3. 数据面持有的 revision 单调不减
4. 网关不连数据库
5. 所有治理写操作有审计记录
6. 一个灰度批次只对应一个 revision
7. 校验和不匹配的快照不应用
8. 单体模式与分离模式的行为差异只允许在网络开销，不允许在语义
9. 同一服务 + 同一 instanceId 只有一条实例记录（注册是幂等的）
10. 同一域（`route` / `config:<ns>`）同时最多一个 `active` 发布
11. 一个 revision 只对应一次发布，并发发布不得复用版本号

### 9.1 输入边界与并发约定

治理面的写操作全部走严格校验，越界值一律在入口拒绝，不做"静默修正"——
静默修正会让数据面拿到一个和页面上不一致的值。

| 对象 | 规则 |
| --- | --- |
| 服务名 / 配置命名空间 / `gov.service_name` / `gov.self_service` | 标识符：字母或数字开头，只含 `A-Za-z0-9._-`，长度 ≤ 64 |
| 实例 | `host` 不含空白与斜杠、长度 ≤ 255；`port` 1~65535；`weight` 0~10000；`instanceId` ≤ 128 |
| 路由前缀 | 以 `/` 开头（自动补），折叠连续斜杠、去尾部斜杠；禁止空白、`?`、`#`、反斜杠与控制字符；长度 ≤ 200 |
| 路由属性 | `timeoutMs` 0~60000、`retries` 0~3、`priority` ±10000、`weight` 0~10000（0 取默认 100）、方法限 7 种且去重归一化为大写 |
| 路由状态 | 入参只接受 `draft` / `archived`；`published` 只能由发布动作产生 |
| 配置项 | `key` ≤ 128 且无空白/控制字符；`value` ≤ 64 KiB；`type` ≤ 32 字符；`scope` 只接受 `global` / `service`（后者必填服务） |
| 请求体 | 超过 `server.max_body_size` 直接 413（业务码 41300），不落成 400 |

并发约定：

- **注册幂等**：注册用 `(service_id, instance_id)` upsert；服务不存在时自动建档，
  并发建档撞唯一索引后回查一次继续，不把冲突抛给调用方（扩容时最不该失败）。
- **同名创建**：并发创建同名服务/命名空间，唯一索引挡下的那一方回查后返回 409。
- **发布串行**：`NextRev` 必须在事务内执行 `global_rev = global_rev + 1`，
  多个并发发布拿到互不相同的号；`active` 收口与新版本插入同事务。
- **批量保存原子**：配置项批量写入包在一个事务里，任一条校验失败整批不落库。
- **快照缓存代际**：`Build` 有一秒缓存，`Bump` 递增代际并清缓存；慢构建只在
  代际未变时才允许回填，避免发布后把旧视图塞回缓存。
- **首版之前不下发 route 分区**：从未发布过路由时数据面保留自己的兜底；
  发布过一次（哪怕空规则集）之后路由就是权威，未命中即 404。

## 10. 安全

- **控制面 ↔ 数据面**：`X-Internal-Token` 常数时间比对；快照接口还带 `X-Instance-Id` 与 `X-Caller-Service`，便于灰度定位调用方
- **配置加密**：AES-256-GCM，主密钥取自 `TENON_MASTER_KEY` 环境变量，经 SHA-256 派生，不落库、不进日志；未配置时明文配置照常工作，加密项在保存时明确报错，服务启动时打一条 Warn 提示
- **权限点**：`registry:*`、`route:*`、`configcenter:*`，与现有 RBAC 一致
- **高危操作**：全量发布、回滚、删除服务、实例下线，要求二次确认并写入审计
- **实例注册**：只允许内置 token 调用，不接受匿名注册；注册接口挂在 `/api/v1` 而不是 `/internal`，因为它面向任意业务进程

## 11. 故障模式

| 故障 | 影响 | 处理 |
| --- | --- | --- |
| 控制面不可用 | 无法改配置 | 数据面继续用内存快照，转发不受影响；重启走本地快照 |
| SSE 断开 | 收不到实时通知 | 指数退避重连 + 轮询兜底 |
| 快照校验失败 | 本次加载失败 | 保留旧快照，上报错误事件，不切换 |
| DB 不可用 | 控制面只读/不可写 | 数据面无感；控制面告警 |
| 推送风暴 | 控制面 CPU 升高 | 变更合并窗口 + 并发池限流 + 队列积压告警 |
| 实例假死 | 流量打到死实例 | 主动探测 + 被动失败摘除，双保险 |
| 快照过大 | 拉取慢、内存涨 | 分区拆分 + 压缩 + 增量，超过阈值告警 |

## 12. 与现有代码的映射

| 现有 | 处理 |
| --- | --- |
| `pkg/discovery`（static/dns + 轮询均衡） | 保留为第三级兜底；常态来源改为 `pkg/gov` 快照 |
| `internal/micro/gateway.go` | 保留转发骨架，路由来源从配置文件改为内存快照；快照就位后不再回落配置文件 |
| `internal/modules/cluster` | **保持独立**：它管的是"进程可运维"（重启/重载指令、运行参数版本），不是"流量可路由"；取消 SQLite 降级分支，`cluster.enabled` 是唯一开关 |
| `internal/modules/routemanage` | 由 `route` 取代；旧模块保留只读的 gin 路由清单视图 |
| `internal/modules/configcenter` | 双轨：`sys_runtime_config` 管进程参数（`/ops/config`），`gov_config_item` 管业务配置（`/gov/config`），分开发布与权限 |
| `config.gateway.yaml` | 保留 `micro.routes` 作为快照不可用时的兜底（仅在 `gov.enabled=false` 时生效） |
| 迁移 | v12 建 `gov_*` 表；v13 写入 `gov_revision` 单行并搬迁 `sys_cluster_node` 到 `gov_instance`；v14 幂等补建版本分配器 |

前端：`web/src/modules/{registry,route}` 新增，`configcenter` 增加 `/gov/config` 页，
`cluster` 去掉失效的单机降级分支；顶级菜单「服务治理」。

### 12.1 独立发行版

治理面可以只作为能力集活在主程序里，也可以单独编成一个进程独立运营。两者是同一份
源码的两个 profile，不是两套代码：

```bash
make dist-gov     # distgen -modules "<GOV_MODULES>" -name Gov
                  # 产物 bin/tenon-gov，前端以 VITE_EDITION=gov 同步裁剪
```

模块清单（19 个）：

| 范围 | 模块 |
| --- | --- |
| 后台基础 | `auth` `user` `role` `dept` `menu` `dict` `config` `notice` `message` `audit` `monitor` `job` `file` `maintenance` `cacheadmin` `dashboard` |
| 治理三件套 | `registry` `route` `configcenter` |

与 Base 的差异只有两处：多 `registry` + `route`，少 `routemanage`（静态路由清单与
动态路由语义重复，且它只读 gin 注册表，对治理面没有价值）。

独立运营的边界靠**编译期裁剪**保证，不靠运行时开关：被裁掉的模块连路由注册代码都
不在二进制里，访问 `/api/v1/news/*`、`/api/v1/membership/*` 直接 404，不存在
「接口在但功能未启用」的中间状态。

配置要点（`configs/config.gov.yaml`）：

| 项 | 值 | 原因 |
| --- | --- | --- |
| `server.port` | 9020 | 与主程序 9000、Base 9010、网关 9100 错开 |
| `db.dsn` | `data/tenon-gov.db` | 治理数据（服务目录、路由规则、配置项、发布历史）与被治理系统分库 |
| `gov.enabled` | `false` | 控制面不订阅自己；数据面订阅才打开 |
| `gov.self_service` | `tenon-gov` | 自注册进服务目录的服务名，避免独立部署时仍显示成 `tenon-core` |
| `TENON_MASTER_KEY` | 环境变量 | 配置中心加密项的唯一密钥来源，未配置时保存加密项直接报错 |

部署形态上它替代了 Nacos / Apollo 的位置：数据面只认这台进程的 HTTP 接口
（`/internal/gov/{snapshot,watch}` + 实例注册心跳），控制面挂了数据面继续用内存
快照与本地快照文件转发，恢复后按 revision 自动追平。

## 13. 里程碑

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| M1 地基 | revision 分配、快照模型、`pkg/gov/client`、路由编译、网关热更新、手动下发、审计 | 已完成 |
| M2 治理 | 实例注册与心跳、健康检查、平滑加权轮询、SSE + 轮询双通道、灰度与提升、回滚、版本分布、数据面自注册与版本回报 | 已完成 |
| M2 交付 | 独立发行版 `bin/tenon-gov`（`make dist-gov`）：后台基础 + 治理三件套，独立库与端口，前端同步裁剪 | 已完成 |
| M3 规模化 | 心跳批量写、按服务分片订阅、治理大盘、自动回滚门禁 | 待做 |

M2 的验收项已实测：改一条规则 t+1s 网关生效（新增 200 / 下发 404），实例
`online → draining → online` 状态机可用，灰度发布后 `promote` 与 `rollback`
都能生成新版本并被数据面接受，网关与内核实例的 `configVersion` 在 10s 内收敛。

独立发行版实测：`bin/tenon-gov` 在 9020 启动（19 模块 / 166 路由），治理三块接口全通，
实例自注册为 `tenon-gov` 并 10s 心跳，加密配置写入后回显为 `******`，
发布与回滚各自产出新 revision；`/api/v1/news/*`、`/api/v1/membership/*`、
`/api/v1/cluster/*` 等被裁模块一律 404，前端四个治理页面零 console 错误。

## 14. 关键取舍

| 方案 | 否决理由 |
| --- | --- |
| 引入 Nacos/Consul | 多一套要运维的中间件，与"单二进制交付"冲突；共享库架构下 DB 就是天然注册中心 |
| 路由规则存 KV 配置中心 | 需要强校验、排序、索引、灰度，KV 承担不了；改为强类型表，共用 revision 与推送通道 |
| 网关直连 DB | 破坏 20MB 轻量定位，连接池与迁移都进来了 |
| 网关长轮询控制面 | SSE 更省连接与 QPS；长轮询作为兜底保留在轮询里 |
| 数据面热加载解释 JSON | 每次请求解析规则，延迟不可控；改为控制面预编译 |
| 配置按 key 发布 | 订阅语义复杂，客户端要维护 key 级版本；改为命名空间整体发布 |

## 15. 风险

1. **DB 是控制面单点**：数据面不受影响，但控制面多副本需要选主做健康探测，复用 `cluster` 的 leader 机制。
2. **SQLite 写并发**：WAL + 单写者，100 实例心跳合并后可以扛；再大必须换 MySQL/PG，代码路径不变。
3. **SSE 连接数**：受文件描述符限制，1000 实例以上要按服务分片或改长轮询。
4. **自研健康检查**：没有成熟插件生态，HTTP/TCP 够用，gRPC 探针要自己写。
5. **快照体积**：路由规则膨胀会线性推高拉取成本，需要分区与压缩，超过 4MB 必须拆。
6. **灰度自动回滚的误判**：阈值定太紧会频繁回滚，M2 先只告警不自动执行，跑一段时间再开。
