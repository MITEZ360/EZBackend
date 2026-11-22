# 工程实践借鉴与改进路线

> **状态：调研结论 / 改进路线。** P0-1 ~ P0-3 已落地（详见第 7 节状态表 ✅），其余项仍为待办。
> 本文记录「值得做什么、怎么做、怎么验收」；已实现项以代码与 `git log` 为准，
> 未实现项（☐）不代表当前代码已具备。每项落地后请回来更新第 7 节状态表与对应功能文档。

---

## 1. 来源与边界

| 项 | 内容 |
| --- | --- |
| 调研对象 | `MITEZ360/EZProject` — Headless CMS Platform（自研无头内容管理平台） |
| 调研版本 | commit `98c60cc`（2026-11-22） |
| 对方技术栈 | TypeScript + Bun（兼容 Node）+ Hono + Drizzle + SQLite / PostgreSQL / MySQL；后台 React + Vite |
| 对方结构 | `kernel/` 契约 → `modules/` 可插拔能力 → `scenarios/` 场景产品，依赖单向向内 |
| 调研日期 | 2026-11-22 |
| 本地参考副本 | `/workspace/EZProject`（HTTPS 匿名克隆，只读，不属于本仓库） |
| 本文范围 | **只借鉴工程约束与运维设计**，不移植代码、框架、运行时、前端组件 |

**核心结论**：两套系统技术栈完全不同，却独立收敛到了同一批实践（第 3 节），说明 Keel 的基础方向是对的。
差距集中在三处：**备份完备性**、**恢复可演练性**、**架构约束是否由机器把关**。

对方最值得读的三份材料：

| 文件 | 价值 |
| --- | --- |
| `docs/engineering-spec.md` | 「四条宪法」、策略模式、契约设计、CI 检查清单、明示取舍 |
| `docs/operations.md` | 数据风险地图、备份/恢复流程、定期演练、监控清单 |
| `scripts/check-*.mjs` + `scripts/backup*.mjs` | 把架构约束和运维策略变成可执行检查 |

---

## 2. 架构对照速览

| 维度 | EZProject | Keel |
| --- | --- | --- |
| 定位 | 无头 CMS 平台（内容领域） | 企业服务端基础框架（通用后台） |
| 分层 | 内核契约 / 能力模块 / 场景产品 | `internal/platform` / `internal/modules` / `internal/bootstrap` 装配 |
| 模块关系 | `PlatformModule` 契约 + 注册表，可插拔 | `bootstrap/app.go` 手动装配，16 个模块全量挂载 |
| 边界保证 | 三个 CI 脚本 + 契约签名快照强制 | `docs/architecture.md` 文字约定 + 人工 Review |
| 事件 | 内核事件总线，模块间只发订事件 | `pkg/event` 总线，topic 形如 `user.created` |
| 多数据库 | `StorageDriver` 策略 + 工厂 + 策略表 | GORM + Viper 驱动推断，方言判断集中在 `platform/database` 与 `maintenance/backup.go` |
| 备份 | `VACUUM INTO` + 全表 JSON + 媒体目录 + 清单 + GFS 轮转 | `VACUUM INTO` + gzip + SHA-256 + `.meta.json` + 简单保留 N 份 |
| 运行时 | 构建 Bun / 运行 Node（受原生模块限制） | 单二进制，`web/dist` 由 `go:embed` 打入，**运行时不依赖 Node** |

---

## 3. 已重合的实践（方向确认，无需改动）

| 实践 | Keel | EZProject |
| --- | --- | --- |
| 在线一致性备份 | SQLite `VACUUM INTO`，不停服 | 同（`scripts/backup.mjs`） |
| 备份完整性 | gzip + SHA-256 + `.meta.json` + `PRAGMA integrity_check` | `manifest.json` + 逐表真读 |
| 恢复前留底 | `pre-restore-*.db` 安全副本 | 恢复前 `VACUUM INTO` 留底 |
| 版本化迁移 | goose 版本账本，支持 up/down/up-to/down-to | Drizzle `migrate()`（弱于 Keel 的版本/回滚表达） |
| 事件总线解耦 | `pkg/event`，同步/异步/通配订阅 | 内核事件总线，模块间只发订事件 |
| 策略化切换 | 缓存/存储/数据库按配置激活 | `StorageDriver` + 工厂 + 策略表 |
| 自动接口文档 | swag 生成 OpenAPI 2.0，路由表脚本注入注解 | 无（对应能力弱于 Keel） |
| 配置驱动扩展 | `oauth.providers` 新增渠道不改代码 | 模块注册表，新增模块不改内核 |

> 这一节的意义是：**不要为了「借鉴」而重写已经做对的部分**。

---

## 4. 改进清单

每项格式：**现状 → 借鉴做法 → 落地目标 → 涉及文件 → 验收标准**。

### P0-1　备份必须纳入文件资产（当前最实在的缺口）

- **现状**：`internal/modules/maintenance/backup.go` 的 `Backup()` 只导出数据库（SQLite `VACUUM INTO` / `mysqldump` / `pg_dump`）。`BackupMeta` 只有 `Tables []string`，没有文件清单。本地存储目录由 `internal/platform/storage` 管理，`internal/router/router.go` 挂载了 `/uploads`，**资讯来源与分类的图标就存在那里**。结论：恢复之后数据库完好、图标全裂。
- **借鉴**：对方 `scripts/backup.mjs` 一次产出四件 —— `app.db` 快照、`dump.json` 全表 JSON、`media/` 媒体目录、`manifest.json` 清单；`docs/operations.md` 明确写道「内容里存的只是 asset id，DB 备份不含图片字节，只备 DB 不备媒体等于内容全在、图全裂」。
- **落地目标**：
  1. `BackupMeta` 增加 `Files int`、`FileBytes int64`、`StorageRoot string`、`StorageMode string`；
  2. 本地存储模式下，把 `uploads/` 一并纳入同一份归档（归档内目录）或生成同名兄弟文件，并在 `.meta.json` 中记录每个文件的相对路径与哈希；
  3. `Verify()` 增加文件清单与哈希校验；
  4. `RestoreOnline()` / `RestoreOffline()` 还原文件资产（先写临时目录再原子替换，避免半截状态）；
  5. 对象存储模式下**显式跳过并写明原因**，不能假装已备份；
  6. 运维中心页面展示文件数，下载时说明归档内容。
- **涉及文件**：`internal/modules/maintenance/backup.go`、`internal/modules/maintenance/handler.go`、`web/src/api/maintenance.ts`、`web/src/pages/ops/maintenance/index.vue`、`docs/deploy.md` 7.2、`docs/api.md` 4.22。
- **验收**：上传图标 → 立即备份 → 删除 `uploads/` → 恢复 → 图标可访问；`Verify()` 报告文件数一致且哈希匹配；对象存储模式下 `Verify()` 明确提示「文件资产由对象存储自身保障」。

### P0-2　恢复演练（dry-run）与恢复后自检

- **现状**：`Verify(name)` 校验备份，`RestoreOnline/RestoreOffline` 直接执行，恢复前生成 `pre-restore-*.db`；有 `backup.allow_restore` 开关。缺演练入口，也缺恢复后的数据比对。
- **借鉴**：`scripts/restore.mjs` 支持 `--list` / `--inspect` / `--dry-run` / `--yes`；`make restore-dry` 只打印计划不动数据，`make restore` 会**自动先演练一次**；恢复后逐表比对行数与清单，差异明确列出。
- **落地目标**：
  1. `RestoreResult` 增加 `DryRun bool`、`Plan []string`、`PostCheck []string`；
  2. 新增 `POST /api/v1/maintenance/backups/:name/restore?dry_run=true`，以及 CLI `restore --dry-run <备份>`；
  3. 演练只输出「将校验什么 / 将覆盖什么 / 将留哪份底 / 将还原哪些文件」，**不改动任何文件**；校验不通过时在演练阶段就拒绝；
  4. 真实恢复后，按 `.meta.json` 的 `Tables` 与用户数逐项比对，差异写入结果并返回给页面。
- **涉及文件**：`backup.go`、`handler.go`、`internal/router/routes_maintenance.go`、`internal/bootstrap/cli.go`、运维前端页面、`docs/deploy.md` 7.2、`docs/api.md` 4.22。
- **验收**：`dry_run=true` 前后 `data/keel.db` 的 SHA-256 不变、备份目录不变、无 `pre-restore-*` 产生；传入损坏备份时演练即拒绝；真实恢复后结果里能逐表列出「清单行数 / 实际行数」。

### P0-3　GFS 备份保留策略

- **现状**：`backup.go` 的 `prune()` 是 `len(list) <= Retain` 的简单策略（默认 7 份）。备份断档后，长时间跑可能把唯一可用备份挤出窗口。
- **借鉴**：`scripts/backup-rotate.mjs` 按小时 / 天 / 周 / 月分档（默认 24 / 14 / 8 / 12），逐档按**槽位**挑选而非按时间窗（避免断档时把窗口里唯一一份删掉），**永不删除最后一份**，并支持 `--dry-run`。
- **落地目标**：`config.Backup` 增加 `retain_hourly` / `retain_daily` / `retain_weekly` / `retain_monthly`，旧 `retain` 保留为「最近 N 份」兜底；`prune()` 改为分档并强制保留最新一份；CLI 增加 `backup-prune --dry-run`；页面展示各档命中数。
- **涉及文件**：`internal/platform/config/config.go`、`configs/config.yaml`、`backup.go`、`internal/bootstrap/cli.go`、`docs/config.md`、`docs/deploy.md`。
- **验收**：构造 40 份跨月备份 → 演练输出保留集合与预计释放量 → 最新一份必在集合内 → 实跑后释放量与预期一致；四档全设 0 时仍保留最新一份。

### P1-1　把架构约束交给机器（性价比最高）

- **现状**：`docs/architecture.md` 第 2 节写明依赖方向铁律（`modules → platform → pkg`），但只靠人工 Review；`Makefile` 有 `vet` / `test` / `build` / `docs`，没有边界扫描。
- **借鉴**：对方三个脚本接进 `make lint` 与 CI：
  - `check-kernel-vocab.mjs`：内核源码出现 `article`/`blog`/`billing` 等业务词即失败；
  - `check-no-dialect.mjs`：除 `storage/factory.ts` 与 `registry.ts` 白名单外，任何 `dialect === ...` 分支即失败；
  - `check-contract-snapshot.mjs`：内核契约签名变更必须显式 `--update` 快照（防偷改契约）。
- **落地目标**：新增 `scripts/check_architecture.py`，实现三条贴合 Go 项目的规则：
  1. `internal/platform/**` 不得 import `internal/modules/**`（内核不反向依赖业务）；
  2. 驱动/方言字面量判断只允许出现在 `internal/platform/database/**` 与 `internal/modules/maintenance/backup.go`（备份必须分支，列入白名单）；
  3. `pkg/**` 公开导出签名快照存为 `pkg/.api-snapshot.json`，变更必须 `--update` 并在提交说明里写原因。
  `Makefile` 增加 `check-arch` 并纳入 `verify`；失败时打印 `文件:行号` 与违规规则。
- **涉及文件**：`scripts/`、`Makefile`、`docs/architecture.md` 第 2 节、`docs/development.md`。
- **验收**：故意在 `platform` 里加一行 `import .../modules/user` → 脚本非零退出并定位；误改 `pkg` 导出 → 快照检查失败；`make verify` 包含该检查且当前代码全绿。

### P1-2　模块可拔插契约与启动自检（结构性改动，放最后）

- **现状**：`internal/bootstrap/app.go` 手动装配全部 16 个模块；`docs/microservices.md` 描述单体/网关/领域服务三种形态，但模块不能按配置启停。
- **借鉴**：`PlatformModule` 契约（`name` / `dependsOn` / `setup` / `teardown`）+ 注册表；启动测试断言「只加载部分模块平台仍能启动」「切方言能生效」。
- **落地目标**：定义 `Module` 契约（`Name()`、`DependsOn()`、`Setup(ctx, *Deps)`、`Teardown()`）；`router.Deps` 逐步改为按注册表装配；配置 `modules.enabled` 白名单；启动日志结构化输出 `[kernel] 已挂载模块: ...`，自检不通过则 fail-fast；补启动测试「只挂载 auth + system 时 `/healthz` 返回 200」。
- **涉及文件**：`internal/bootstrap/app.go`、`internal/router/router.go`、`internal/platform/config/config.go`、`docs/microservices.md`。
- **风险提示**：这是本清单里唯一的大范围结构性改动，必须在 P0 全部完成后进行，并保证现有 158 条路由零回归。
- **落地结论（已完成）**：采用声明式契约（`Module.Name/DependsOn/AlwaysOn`）+ 注册表 + `modules.enabled`；
  服务/处理器仍由 `app.go` 统一构造，路由按注册表结果挂载，因此风险受控。
  全量模式实测 159 条路由（原 158 + P0 的 prune-plan 1 条）零回归；基础模块常驻、
  依赖自动补齐、未知模块 fail-fast 均有单测与集成测试覆盖。

### P1-3　测试分层与启动自检

- **现状**：`go test ./...` 一把跑；`test/` 下有集成测试；没有启动自检断言，也没有日志断言。
- **借鉴**：unit（每次提交、毫秒级、无真实 IO）/ boot（里程碑）/ e2e（发版前）三档；启动打印关键状态并校验，不通过则启动失败；关键路径日志出现预期外 `ERROR`/`WARN` 即判失败。
- **落地目标**：`Makefile` 拆 `test-unit` / `test-integration` / `verify` / `verify-full`；给 `bootstrap.New` 补自检项（驱动、迁移版本、存储可写、缓存可用、种子状态），启动日志结构化并写一个 boot 测试断言无 ERROR。
- **验收**：`make verify` 只跑快测试；故意让迁移版本落后或存储目录不可写 → 启动自检失败并给出明确日志。

### P1-4　跨方言契约验证

- **现状**：迁移与备份覆盖 SQLite / MySQL / PostgreSQL，但日常测试只在 SQLite 跑。
- **借鉴**：对方 CI 起真实 PostgreSQL 跑同一套契约测试；其踩坑记录值得直接引用 —— **「5 张表在 PG 上因 `INTEGER` 装不下 epoch 毫秒完全不可用，而 SQLite 侧测试永远发现不了」**（SQLite 动态类型，`INTEGER` 是 64 位）。
- **落地目标**：增加 `make test-mysql` / `make test-pg`（容器或 Testcontainers），把核心查询、迁移、备份恢复在 MySQL / PG 各跑一遍；至少覆盖时间列、JSON 列、布尔列、唯一索引、全文/模糊查询的方言差异；差异写入本文档附录。
- **验收**：同一套 `go test` 在三种驱动下全绿；文档中记录已知差异与规避方式。
- **落地结论（已完成）**：`test/dialect_test.go` 覆盖 epoch 毫秒 / JSON / 布尔 / 唯一索引 / 中文 LIKE；
  `make test-dialect` 自动起停 MySQL 8.0 + PostgreSQL 16 容器，未设 DSN 时自动跳过。
  已在真实两库实测通过：三类型均无差异，无需特殊规避。

### P2-1　决策日志与踩坑录

- **借鉴**：`docs/dev-log.md`（每开发时段记录做了什么、为什么）、`docs/vibe-cookbook.md`（有效/无效的提示与踩坑）、`CHANGELOG.md`、带编号的决策（如「决策 #26：字段调色板 v1 定稿」）。
- **落地目标**：新增 `docs/decisions.md`（轻量 ADR：编号、日期、背景、决定、后果），把已经发生过的重要取舍补录进去，例如「为什么 OAuth 令牌经 URL fragment 回传」「为什么 SQLite 恢复必须停机」「为什么来源/分类改成白名单」；`docs/development.md` 增加「踩坑记录」章节。
- **验收**：新增能力时有一条对应决策记录；踩坑能被检索到，不靠口头传递。

### P2-2　内容中台能力参考（仅当业务需要）

| 可借鉴设计 | 要点 | Keel 现状 |
| --- | --- | --- |
| 动态内容类型 | `FieldDef` 11 种字段调色板，逻辑 Schema 存数据、不跑 DDL | 资讯是固定模型 `internal/model/news.go` |
| 结构化富文本 | Portable Text；渲染 HTML 只是可再生缓存，不作数据源 | 无 |
| 媒体引用 | 存 `asset._ref` 引用 ID，不存最终 URL（换 CDN 不影响数据） | 存 URL（故 P0-1 必须补备份） |
| 修订与状态 | 变更写版本快照，状态机流转 | 无 |
| 定时发布 / 多语言 | 计划只管调度，locale 在实体级 | 无 |
| 全文检索 | SQLite FTS5 / PG tsvector 策略 | 目前是 `LIKE` + 白名单筛选 |
| Webhook 通知 | 内容变更通知外部系统 | 无（有事件总线可复用） |

**结论**：短期不需要，Keel 的定位是企业后台框架而非 CMS。若将来要做通用内容中台，先读对方 `docs/engineering-spec.md` 第 4 章，重点借鉴「顶层物理列 + JSON 字段 + 物化字段」的查询效率策略，而不是照搬 JS 实现。

---

## 5. 明确不采纳（边界）

| 项 | 不采纳原因 |
| --- | --- |
| Bun / Node 运行时 | 「运行时不依赖 Node」是项目硬约束。且对方自身被 `better-sqlite3` 原生模块逼成「构建用 Bun、运行走 Node」，反而是反面教材 |
| React + antd | 前端固定 Vue3 + TypeScript + Element Plus |
| 强制 `tenant_id` / 多租户 | 此前明确排除的基础功能，不在本轮范围 |
| CI/CD 流水线 | 此前明确排除；仓库已推送至 GitHub，可另行重新评估，但不属于本文落地范围 |
| 策略表模式（二维查找表消灭 if/else） | GORM 已屏蔽大部分方言差异，投入产出比低 |
| 内核「业务词黑名单」照搬 | 直接搬会误伤（`category`、`order` 等在 Go 项目里是中性词），改用 P1-1 的 import 边界检查更贴合 |

---

## 6. 建议落地顺序

1. **P0-1** 备份纳入文件资产 —— P0-2 的演练需要依赖它产出的文件清单；
2. **P0-2** 恢复演练与恢复后自检；
3. **P0-3** GFS 保留策略（三项都集中在 `internal/modules/maintenance/`，一次改完）；
4. **P1-1** 架构约束自动化 —— 独立、成本低、能立刻防回归，建议在动结构性代码之前先加；
5. **P1-3** 测试分层 → **P1-4** 跨方言验证，为后续大改动铺好安全网；
6. **P1-2** 模块可拔插（最后做，风险最高）；
7. **P2-1 / P2-2** 按需，不排期。

每完成一项：更新本文第 7 节状态表、补充对应功能文档（`docs/api.md` / `docs/config.md` / `docs/deploy.md`）、并在 `docs/decisions.md` 留一条决策记录。

---

## 7. 状态表

| 编号 | 改进项 | 优先级 | 状态 | 备注 |
| --- | --- | --- | --- | --- |
| P0-1 | 备份纳入文件资产 | P0 | ✅ 已实现 | 备份改为 tar 容器，含 `database.db` + `files/` + `manifest.json`；对象存储显式跳过 |
| P0-2 | 恢复演练 dry-run + 恢复后自检 | P0 | ✅ 已实现 | `restore --dry-run` / `?dry_run=true`，恢复后逐表比对 + 文件资产还原计数 |
| P0-3 | GFS 备份保留策略 | P0 | ✅ 已实现 | `retain_hourly/daily/weekly/monthly` 分档按槽位挑选，`backup-prune --dry-run`，最新必留 |
| P1-1 | 架构约束自动化 | P1 | ✅ 已实现 | `make check-arch` 三条规则，已纳入 `make verify`；违规可定位到文件:行号 |
| P1-2 | 模块可拔插契约与启动自检 | P1 | ✅ 已实现 | `Module` 契约 + 注册表 + `modules.enabled` 白名单，依赖自动补齐、未知模块 fail-fast；全量 159 路由零回归 |
| P1-3 | 测试分层与启动自检 | P1 | ✅ 已实现 | `test-unit`/`test-integration`/`verify`/`verify-full` 四档；`bootstrap.New` 关键依赖 fail-fast + 自检报告 |
| P1-4 | 跨方言契约验证 | P1 | ✅ 已实现 | `test/dialect_test.go` 同套断言跑 MySQL/PG，`make test-dialect` 起停容器；已在真实 MySQL 8.0 + PG 16 实测通过 |
| P2-1 | 决策日志与踩坑录 | P2 | ☐ 未实现 | 建议与首次落地同步建立 |
| P2-2 | 内容中台能力参考 | P2 | ☐ 未实现 | 仅当业务需要，不排期 |
