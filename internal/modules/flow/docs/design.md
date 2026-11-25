# flow 模块设计：轻量工作流引擎

> 遵循 2-8 定律：只做常见的单线、并行、会审、移交等场景，用最小的模型覆盖约 95% 的审批需求。不引入 BPMN/图模型。

## 1. 核心抽象

**有序节点数组 + 聚合规则**，而非「节点 + 连线 + 坐标」的图。

- 单线 = 节点串行
- 并行 / 会审 = 一个节点内多个审批人同时生成 task，用 `mode=any|all` 决定何时推进
- 移交 = 任务转办，不改节点
- 条件分支 = 节点级 `skipWhen`，进入节点前判断变量决定是否跳过

后端存有序数组，前端画布经无损转换层双向映射（见第 7 节），前后端零语义损失。

## 2. 覆盖率边界

| 档位 | 场景 |
|---|---|
| 直接覆盖（~65%） | 单线逐级、或签、会签/并行、抄送、驳回发起人、任务移交、发起人撤销、审批意见+轨迹 |
| 少量扩展（+~30%） | 条件分支（skipWhen）、顺序会签（sequential）、发起时指定审批人、超时提醒/自动通过、加签/减签、回退任意节点、委托代理、多部门并行后汇聚（多审批人节点模拟） |
| 不做（~5%，需图模型） | 子流程嵌套、运行中改写流程、异构分支汇聚（不同长度分支 fork/join） |

不做项的共同点：需要真正的图或实例嵌套，进核心就得上 BPMN，投入产出不划算。

## 3. 数据模型

嵌 `model.Base`（雪花 ID + 软删除），放 `internal/model/flow.go`。

| 表 | 关键字段 | 说明 |
|---|---|---|
| `wf_flow` | `key`(唯一), `name`, `description`, `status`, `nodes`(JSON), `version` | 流程模板；nodes 为有序节点数组 |
| `wf_instance` | `flow_key`, `business_id`, `business_type`, `title`, `initiator_id`, `status`, `nodes_snapshot`(JSON), `variables`(JSON), `current_node`, `round`, `started_at`, `finished_at` | 一次运行；发起时复制 nodes 快照，改模板不影响在跑实例 |
| `wf_task` | `instance_id`, `node_id`, `node_name`, `assignee_id`, `status`, `action`, `comment`, `is_added`, `round`, `finished_at` | 待办；每个审批人一条，并行天然成立 |
| `wf_record` | `instance_id`, `task_id`, `node_id`, `action`, `operator_id`, `comment`, `from_user`, `to_user` | 流转轨迹，只追加，不软删 |
| `wf_delegate` | `from_user_id`, `to_user_id`, `start_at`, `end_at`, `enabled` | 委托代理，带时段 |

### 节点 JSON 结构

```json
{
  "id": "n2",
  "name": "部门会签",
  "type": "approve",
  "assignees": [
    {"type": "role", "value": "finance"},
    {"type": "deptLeader", "value": "initiatorDept"},
    {"type": "initiatorInput", "value": "assigneeId"}
  ],
  "mode": "all",
  "onReject": "backToInitiator",
  "sequential": false,
  "skipWhen": {"field": "amount", "op": "lte", "value": 5000},
  "timeoutHours": 48,
  "timeoutAction": "notify"
}
```

- `type`: `approve` / `cc`（抄送自动完成，不阻塞）
- `assignees.type`: `user` / `role` / `deptLeader` / `initiatorLeader` / `initiatorSelf` / `initiatorInput`
- `mode`: `any`（或签，任一同意即过）/ `all`（会签，全部同意才过）
- `onReject`: `backToInitiator` / `end` / `prevNode`
- `skipWhen`: 结构化条件 `{field, op, value}`，op 支持 `eq/ne/gt/gte/lt/lte/in`；多条件用 `all`/`any` 组合
- `sequential`: true 时逐个展开任务（顺序会签），false 并行
- `timeoutHours` + `timeoutAction`: `notify` / `approve` / `reject`

**对齐点**：`model.Dept.Leader` 现为姓名文本。做「部门负责人」审批来源时，解析优先取 `leader_user_id`（若扩展），否则按姓名匹配 `sys_user.real_name`；匹配不到则该节点无审批人并记轨迹。

## 4. 引擎逻辑

`internal/modules/flow/engine.go`，状态推进五个函数：

1. `Start(flowKey, businessId, businessType, title, initiatorId, variables)` — 校验模板已发布 → 复制 nodes 为快照 → 建实例 → 进入第 0 个节点。
2. `resolveAssignees(instance, node)` — 解析每个来源为具体用户列表，逐个查 `wf_delegate` 在时段内则替换；去重、过滤停用用户。
3. `enterNode(instance, node)` — 先判 `skipWhen`（命中则直接推进）→ `cc` 自动完成 → 按 `sequential` 并行/逐个创建 task → 更新 `current_node`。
4. `handleTask(taskId, action, comment, operatorId)` — 事务内：更新 task → 写 record → 按 `mode` 重算有效 task 集合聚合 → 通过则推进下一节点或置 `approved`；否决按 `onReject` 处理。
5. `rollback(instanceId, targetNodeId, reason)` — 按有序数组 index 作作废目标之后所有 task、重置目标节点 task，`round` 递增，写 record。`transfer`/`addSign`/`removeSign` 是 task 级操作，不新增节点。

**并发一致性**：`handleTask` 在事务内对 instance 加行锁（`SELECT ... FOR UPDATE`）；task 非 pending 直接拒绝保证幂等；聚合判断与推进同事务，避免并发审批重复推进。

**超时**：挂现有 `job` 模块的 cron，扫 `wf_task` 中 pending 且创建时间超 `timeoutHours` 的记录，按 `timeoutAction` 发站内信或自动处理。

## 5. 状态机

- 实例：`running` → `approved` / `rejected` / `cancelled`
- 任务：`pending` → `approved` / `rejected` / `transferred` / `cancelled`

## 6. API

沿用 Gin + `pkg/response` + `pkg/errs`，权限点 `RequirePerm`。

流程定义：
- `POST /flows` · `PUT /flows/:id` · `GET /flows` · `GET /flows/:id` · `POST /flows/:id/status`

实例：
- `POST /instances`（发起）· `GET /instances/:id` · `GET /instances`（我发起的）· `POST /instances/:id/cancel` · `POST /instances/:id/rollback`

任务：
- `GET /tasks/todo` · `GET /tasks/done` · `GET /tasks/:id`
- `POST /tasks/:id/approve` · `POST /tasks/:id/reject` · `POST /tasks/:id/transfer` · `POST /tasks/:id/addsign` · `POST /tasks/:id/removesign`

委托：
- `GET|POST|PUT|DELETE /delegates`

## 7. 前端

依赖 `@vue-flow/core` + `background/controls/minimap`（Vue3 原生、TS、MIT），配套无损转换层。

三栏设计器：左节点类型面板 → 中 Vue Flow 画布（竖排自动串联，禁用自由连线/删边） → 右 `el-drawer` 节点配置。

**无损转换**：
- `fromNodes(array)` → `{nodes, edges}`：按数组顺序生成竖排坐标与串联边
- `toNodes({nodes})` → array：按 y 坐标排序导出
- 约束只许竖排调序、不许画分支，故用户画不出图，转换双向无损

页面：`web/src/pages/workflow/{design,instances,tasks,delegates}`。待办/已办用 `el-table` + `el-drawer` + `el-timeline`；样式用 `tokens.scss` 现有令牌；i18n key 用中文原文；权限 `v-perm`。

## 8. 模块接入

- `internal/modules/flow/{module.go,engine.go,service.go,handler.go}`，`init()` 注册 Spec，`Models` 列 5 张表 + 菜单
- `internal/distribution/default.go` 加空白导入（Default 版收录；Base 版不含，前端路由同样由 `OPTIONAL_ROUTES` 按 `VITE_EDITION=base` 裁剪）；`configs/config.yaml` 的 `modules.enabled` 为空即全启用，无需单独登记
- `DependsOn: ["message"]`：超时提醒通过 `message.Service.Send` 发站内信；未启用时降级为日志留痕
- 超时扫描挂 Spec.Jobs（`flow.timeout`，每 5 分钟），由 job 模块统一调度
- 权限点：`flow:def:*` / `flow:instance:*` / `flow:task:*` / `flow:delegate:*`

## 9. 实现校验记录

- 后端：`go build ./...` 与 `go test ./...` 全绿（含 8 个引擎单测：单线、会签、或签、跳过条件、加减签、回退、委托、驳回）
- 前端：`vue-tsc` 类型检查通过；完整版与 `VITE_EDITION=base` 均构建通过（Base 版 Vue Flow 依赖被裁剪，vendor 从 359KB 降到 148KB）
- 端到端：在 9020 独立端口实测「建流程 → 发布 → 发起 → 两级审批 → 状态 approved」，轨迹完整
- 修复记录：`ListTasks` 的 `status=todo` 不能同时进白名单 filter，否则追加 `status='todo'` 字面量导致待办为空；已在 service 层排除

## 10. 实施顺序

1. model + 表 + 迁移注册
2. 引擎核心（含单测：四种节点模式、加减签、回退、委托、跳过条件）
3. service / handler / module 注册
4. 前端页面与设计器
5. 编译、单测、构建验证
