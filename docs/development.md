# 二次开发指南

以新增一个「客户管理」模块为例，完整走一遍：模型 → 服务 → 处理器 → 模块声明 → 接入发行版 → 权限点 → 菜单 → 前端页面 → 测试。

---

## 0. 约定

| 项 | 约定 |
| --- | --- |
| 目录 | `internal/modules/<模块>/`，通常三个文件：`service.go`、`handler.go`、`module.go`（模块声明，见第 4 节） |
| 命名 | 服务 `Service` / `XxxService`，处理器 `Handler` / `XxxHandler` |
| 分层 | handler 只做参数与响应；业务、事务、数据权限写在 service |
| 错误 | 一律返回 `*errs.Error`（`errs.NotFound/Conflict/BadRequest/Forbidden`…），由 `response.Fail` 统一渲染 |
| 列表 | 用 `query.Parse` + `p.Filter` + `query.Search` + `p.Order`，字段走白名单 |
| 权限 | 每个写操作都要挂 `middleware.RequirePerm("模块:资源:动作")` |
| 审计 | 写操作成功后调用 `middleware.AuditInfo(c, "模块名", "动作名")` |

> **提交前校验**：`make verify`（= `vet` + `check-arch` + `test-unit`，快）或
> `make verify-full`（把 `test-unit` 换成含 race + 集成的全量 `test`）。
> `check-arch` 自动检查依赖边界、方言分支白名单与 `pkg` 导出签名快照（改公共 API 需
> `python3 scripts/check_architecture.py --update`）；方案见 [工程实践借鉴与改进路线](engineering-practices.md) P1-1 / P1-3。
> 新增重要取舍时建议同步补一条决策记录（P2-1）。

---

## 1. 模型

`internal/model/customer.go`

```go
package model

// Customer 客户。
type Customer struct {
	Base
	Name       string `gorm:"size:128;not null;index" json:"name"`
	Level      string `gorm:"size:16;default:'normal';index" json:"level"`
	Contact    string `gorm:"size:64" json:"contact"`
	Phone      string `gorm:"size:32;index" json:"phone"`
	Email      string `gorm:"size:128" json:"email"`
	OwnerID    string `gorm:"size:32;index" json:"ownerId"`   // 负责人，用于"仅本人"数据权限
	DeptID     string `gorm:"size:32;index" json:"deptId"`    // 归属部门，用于部门级数据权限
	Status     int8   `gorm:"default:1;index" json:"status"`
	Remark     string `gorm:"size:255" json:"remark,omitempty"`
}

// TableName 表名统一 sys_ / biz_ 前缀，避免与保留字冲突。
func (Customer) TableName() string { return "biz_customer" }
```

`Base` 已提供 20 位雪花 ID 与 `created_at/updated_at/deleted_at`，无需重复定义。

在模型的 `module.go` 里声明 `Models`，框架会按模块开关决定是否建表：

```go
Models: []any{&model.Customer{}},
```

版本化迁移的 baseline 也用它，因此新增的模块专属迁移必须先用 `HasTable` 判空，
否则关闭该模块后迁移会因目标表不存在而失败。

---

## 2. 服务

`internal/modules/customer/service.go`

```go
package customer

import (
	"context"

	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/query"
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// Input 新增/修改入参，binding 标签即校验规则，label 用于中文提示。
type Input struct {
	Name    string `json:"name" binding:"required,max=128" label:"客户名称"`
	Level   string `json:"level" binding:"omitempty,oneof=normal vip svip" label:"等级"`
	Contact string `json:"contact" binding:"max=64" label:"联系人"`
	Phone   string `json:"phone" binding:"omitempty,mobile" label:"手机号"`
	Email   string `json:"email" binding:"omitempty,email" label:"邮箱"`
	Status  int8   `json:"status" binding:"oneof=0 1" label:"状态"`
	Remark  string `json:"remark" binding:"max=255" label:"备注"`
}

func (s *Service) List(ctx context.Context, p *query.Params, ownerID string) ([]*model.Customer, int64, error) {
	allowed := map[string]string{
		"name": "name", "level": "level", "status": "status", "owner_id": "owner_id",
	}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.Customer{}), allowed)
	db = query.Search(db, p.Keyword, "name", "contact", "phone")
	if ownerID != "" { // 演示"仅本人"数据权限；真实场景用 user.ResolveScope
		db = db.Where("owner_id = ?", ownerID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.Customer
	err := db.Order(p.Order(map[string]string{"created_at": "created_at", "name": "name"}, "created_at DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

func (s *Service) Create(ctx context.Context, in *Input, ownerID, deptID string) (*model.Customer, error) {
	c := &model.Customer{
		Name: in.Name, Level: in.Level, Contact: in.Contact, Phone: in.Phone,
		Email: in.Email, Status: in.Status, Remark: in.Remark,
		OwnerID: ownerID, DeptID: deptID,
	}
	if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) Update(ctx context.Context, id string, in *Input) error {
	res := s.db.WithContext(ctx).Model(&model.Customer{}).Where("id = ?", id).Updates(map[string]any{
		"name": in.Name, "level": in.Level, "contact": in.Contact,
		"phone": in.Phone, "email": in.Email, "status": in.Status, "remark": in.Remark,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("客户不存在")
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).Delete(&model.Customer{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("客户不存在")
	}
	return nil
}

func (s *Service) Get(ctx context.Context, id string) (*model.Customer, error) {
	var c model.Customer
	if err := s.db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return nil, errs.NotFound("客户不存在")
	}
	return &c, nil
}
```

---

## 3. 处理器

`internal/modules/customer/handler.go`

```go
package customer

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/platform/httpx"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/response"
)

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), p, c.Query("mine") == "1" && middleware.UserID(c) != "" )
	// 上面一行仅为示例：真实实现请把数据范围判断放进 service
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	id := middleware.Identity(c)
	cust, err := h.svc.Create(c.Request.Context(), &in, id.UserID, id.DeptID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "客户管理", "新增客户")
	response.OK(c, cust)
}

func (h *Handler) Update(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "客户管理", "编辑客户")
	response.OK(c, nil)
}

func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "客户管理", "删除客户")
	response.OK(c, nil)
}

func (h *Handler) Get(c *gin.Context) {
	cust, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cust)
}
```

> 上面 `List` 中那行"示例"是反例演示：**数据范围判断属于业务规则，必须放在 service**，
> 参照 `internal/modules/user/service.go` 的 `ResolveScope`。

---

## 4. 模块声明

`internal/modules/customer/module.go`——一个文件声明这个模块的全部装配信息：

```go
package customer

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Name:   "customer",
		Label:  "客户管理",
		Desc:   "客户建档与跟进",
		Models: []any{&model.Customer{}},          // 建表跟着模块开关走
		Menus: []modulekit.Menu{                    // 扁平声明，Parent 指向目录
			modulekit.DirSystem,
			{ID: "m_customer", Parent: "m_system", Name: "客户管理", Type: model.MenuPage,
				Path: "/system/customer", Icon: "users", Perm: "biz:customer:list", Sort: 11},
			{ID: "m_customer_add", Parent: "m_customer", Name: "新增客户",
				Type: model.MenuButton, Perm: "biz:customer:create"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	h := NewHandler(NewService(c.DB))
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/customers", middleware.RequirePerm("biz:customer:list"), h.List)
		g.POST("/customers", middleware.RequirePerm("biz:customer:create"), h.Create)
		g.GET("/customers/:id", middleware.RequirePerm("biz:customer:list"), h.Get)
		g.PUT("/customers/:id", middleware.RequirePerm("biz:customer:update"), h.Update)
		g.DELETE("/customers/:id", middleware.RequirePerm("biz:customer:delete"), h.Delete)
	})
	return nil
}
```

`Setup` 拿到的 `modulekit.Context` 只暴露平台能力：`DB`、`Cache`、`Limiter`、
`Config`、`Log`、`Storage`、`JWT`、`Authz`、`Events`、`Routes`（路由快照）、
`Restart`。路由分组已经带好中间件——`Authed` 是认证 + 限流 + 审计，
`Public` 是匿名 + IP 限流，`Open` 是 API 密钥认证，模块只管路径与权限点。

需要别的模块的能力时不要 import 对方，在自己包里定义一个小接口，用
`c.After` 在装配末尾注入（参考 `user.MembershipFiller`、`system.NoticeNotifier`）。

---

## 5. 接入发行版

发行版清单有两个文件，按模块的定位选一个：

| 文件 | 收录范围 | 适用 |
| --- | --- | --- |
| `internal/distribution/default.go` | 默认发行版（`make build`） | 通用后台能力：登录、RBAC、组织、运维控制台 |
| `internal/distribution/catalog/catalog.go` | 全部可装配模块（仅工具读取） | 演示业务/行业模块：资讯中心这类默认不装的 |

通用能力加进 `default.go`，一行空白导入：

```go
_ "github.com/MITEZ360/tenon/internal/modules/customer"
```

独立模块只加进 `catalog/catalog.go` 的空白导入与 `Optional` 名单，默认构建不含它：

```go
_ "github.com/MITEZ360/tenon/internal/modules/news"

var Optional = []string{"news"}
```

到此为止。`bootstrap`、`router`、`seed` 都不用改。运行期想关掉它，
`modules.enabled` 里不列即可；想从二进制里彻底摘掉，删掉这行导入。

如果这个发行版只要一部分模块，不要手工删导入，用生成器：

```bash
make dist-list                                  # 看模块与依赖，+ 为默认不装
make dist MODULES="user configcenter cluster"   # 算依赖、生成清单、编译
make dist MODULES=all                           # 全量（含资讯中心）
make dist-default                               # 回到默认发行版
```

生成器把结果写入 `internal/distribution/profile.go`（`profile` 构建标签），
与 `default.go`（`!profile`）互斥，几种发行版可以反复切换而不必互相删文件。
它依赖 `Spec.Pkg` 把模块名翻译成包路径，所以 `module.go` 里必须填 `Pkg`。


### 5.1 接入一个新的外部依赖

框架里所有中间件都遵循"未配置不创建、配置了自动激活"，新增依赖时保持同一模式：

1. 配置项放在 `internal/platform/config`，关键字段留空即代表未配置；
   需要三态时用 `*bool`（nil = 自动判断）。
2. 在 `internal/platform/<name>` 提供接口与构造函数，构造函数返回 `(nil, nil)`
   表示未启用；连接失败默认返回错误，不要静默降级。
3. 在 `bootstrap.New` 里装配，并在 `logRuntime()` 中补充一行实现名，
   让"实际连的是什么"出现在启动日志里。
4. 如果是多实例下不能共享的本地实现，在 `checkStateless()` 里登记一条检查项。
5. 健康检查：实现 `Health(ctx)`，并在 `router.healthRoutes` 的 `/healthz` 与
   `/readyz` 中调用。

```go
// 示例：新增消息队列
mq, err := mqclient.New(cfg.MQ, lg) // 未配置时返回 nil
if err != nil {
	return nil, err
}
```

---

## 6. 菜单与权限点

在模块的 `module.go` 里追加（注意 `Perm` 与路由上的权限点一致，`Parent` 指向目录 ID，
父目录也要在本模块或共享目录常量里声明）：

```go
Menus: []modulekit.Menu{
	modulekit.DirSystem, // 共享目录，多个模块重复声明只保留一份
	{ID: "m_customer", Parent: "m_system", Name: "客户管理", Type: model.MenuPage,
		Path: "/biz/customer", Icon: "users", Perm: "biz:customer:list", Sort: 9},
	{ID: "m_customer_add", Parent: "m_customer", Name: "新增客户", Type: model.MenuButton, Perm: "biz:customer:create"},
	{ID: "m_customer_edit", Parent: "m_customer", Name: "编辑客户", Type: model.MenuButton, Perm: "biz:customer:update"},
	{ID: "m_customer_del", Parent: "m_customer", Name: "删除客户", Type: model.MenuButton, Perm: "biz:customer:delete"},
},
```

> **内置菜单必须用 `m_` 前缀**。启动时靠这个前缀区分内置菜单与管理员自建菜单，
> 前者跟随模块启停，后者永不被模块开关影响。用别的命名会被当成自建菜单漏掉，
> 裁剪模块后留下点进去 404 的死入口。

> 业务种子数据只在**首次启动**（库里没有用户时）写入。但**内置菜单**是例外：
> 每次启动都会比对模块声明，补齐缺失的节点并授权给内置超管角色，因此新增框架功能后
> 升级即可看到入口，不必清库。新页面若是业务自建菜单，仍需在「菜单管理」里手工添加。

---

## 7. 前端页面

控制台在 `web/`，技术栈 **Vue 3 + TypeScript + Element Plus + Vite**。

Node 只用于**构建期**：`vite build` 产出 `web/dist`，再由 `//go:embed all:dist` 打进二进制。
部署机上没有 Node、没有 nginx、没有 `node_modules`，仍然是一个文件跑起来。

### 目录

```
web/src/
├── api/         # client.ts（axios + 401 单飞刷新）、跨模块共享的接口封装
├── components/  # TrendChart、MessageBell（铃铛未读预览）等通用组件
├── layouts/     # BasicLayout：侧边栏 + 顶栏 + 内容区
├── modules/     # 业务模块，与 internal/modules/<name> 一一对应
│   ├── types.ts     # WebModule 契约（name / routes / publicRoutes / i18n）
│   ├── registry.ts  # 自动发现 + 按 VITE_MODULES 过滤
│   └── news/        # 单个模块：index.ts + pages/ + api.ts + i18n.ts
├── pages/       # 应用壳：登录、第三方回调、个人中心、403/404
├── router/      # 路由聚合 + 登录守卫 + 菜单校验（全局策略）
├── stores/      # Pinia：auth 管 token/user/perms，message 管未读数与最近消息
├── styles/      # tokens.scss 设计令牌 → element.scss 覆盖 → base.scss
└── utils/       # 格式化、菜单图标映射
```

业务页面在 `modules/` 下自包含，路由不再写在一张总表里。约定与增删步骤见
[frontend-modules.md](frontend-modules.md)，要点：模块名必须等于后端
`modulekit.Spec.Name`，否则 `make dist` 会裁错页面。

### 开发

```bash
# 终端 1：后端
make run

# 终端 2：前端（Vite :5173，/api 代理到 :9000，带 HMR）
make web-dev
```

改完前端只需 `make web` 重新产出 dist；`make build` 会先构建前端再编译 Go。

### 已实现页面

| 路由 | 页面 | 可作为哪种参考 |
| --- | --- | --- |
| `/dashboard` | 仪表盘 | 统计卡片、手写 SVG 折线图、公告列表 |
| `/system/user` | 用户管理 | 部门树筛选、增删改弹窗、Excel 导入导出、批量删除 |
| `/system/role` | 角色管理 | 数据范围单选卡、菜单权限树（半选提交）、抽屉 |
| `/system/menu` | 菜单管理 | 树形表格、按类型联动表单、图标选择 |
| `/system/dept` | 部门管理 | 树形表格、上级选择防环 |
| `/system/dict` | 字典管理 | 左右双栏联动、字典项维护 |
| `/system/config` | 参数配置 | 加密值掩码编辑、分组筛选 |
| `/system/notice` | 通知公告 | 非表格列表、详情抽屉、草稿态 |
| `/ops/job` | 定时任务 | cron 预设、启停开关、手动执行、日志抽屉 |
| `/ops/file` | 文件管理 | 拖拽上传、扩展名色块、下载 |
| `/ops/login-log` | 登录日志 | 日期范围筛选、清理历史 |
| `/ops/operation-log` | 操作日志 | 多条件筛选、请求响应详情、状态码映射 |
| `/ops/online` | 在线用户 | 会话标记、强制下线 |
| `/ops/monitor` | 服务监控 | 自动刷新、指标卡、路由清单 |

写新页面时优先复制同类页面，权限按钮统一用 `v-perm`，接口统一走 `src/api/*.ts`。

### 新增一个页面

页面属于某个业务模块，放到 `web/src/modules/<name>/pages/`，路由写在该模块的
`index.ts`。**不要**改 `router/index.ts`——那是应用壳与全局守卫。

**1. 建组件** `web/src/modules/biz/pages/customer.vue`：

```vue
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api/client'

interface Customer {
  id: string
  name: string
  contact?: string
  phone?: string
  status: number
}

const rows = ref<Customer[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive({ page: 1, page_size: 10, keyword: '' })

async function load() {
  loading.value = true
  try {
    const res = await api.get<{ list: Customer[]; total: number }>('/customers', {
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
    })
    rows.value = res.list
    total.value = res.total
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">客户管理</h1>
        <p class="page-desc">维护客户档案与联系人</p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'biz:customer:create'" type="primary">新增客户</el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <el-input v-model="query.keyword" placeholder="搜索客户名称" style="width: 240px" @keyup.enter="load" />
      </div>
      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column prop="name" label="客户名称" min-width="180" />
        <el-table-column prop="contact" label="联系人" width="120" />
        <el-table-column prop="phone" label="手机号" width="140" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
              {{ row.status === 1 ? '启用' : '禁用' }}
            </span>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>
```

**2. 模块入口** `web/src/modules/biz/index.ts`：

```ts
import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'biz', // 必须等于后端 modulekit.Spec.Name
  routes: [
    {
      path: 'biz/customer',
      name: 'biz-customer',
      component: () => import('./pages/customer.vue'),
      meta: { title: '客户管理' },
    },
  ],
})
```

`registry.ts` 用 `import.meta.glob` 自动发现，无需注册。`make dist MODULES="…"`
时后端会算出实际装配集合传给前端，未选模块的页面 chunk 不进产物。

**3. 后端菜单**：`path` 填 `/biz/customer`，与模块内 `routes[].path` 加前导斜杠后一致；
`icon` 用 `utils/icons.ts` 里已有的键，没有就加一条映射（后端只存语义名，不依赖具体图标库）。

### 权限

| 场景 | 用法 |
| --- | --- |
| 隐藏无权限的按钮 | `v-perm="'system:user:create'"` |
| 置灰而非隐藏 | `v-perm.disable="'system:user:create'"` |
| 代码里判断 | `auth.can('system:user:create')`，数组表示任一满足 |
| 超管 | `auth.isSuper`，权限点含 `*` 时恒为 true |

**前端隐藏只是体验，后端 `RequirePerm` 才是真正的拦截**，两边都要写。

### 设计规范

改界面前先读 `web/src/styles/tokens.scss`，颜色、圆角、阴影只从这里取：

```scss
color: var(--ink-900);          // 主文字
background: var(--bg-page);     // 页面底色 #f7f8fa
border: 1px solid var(--line);  // 卡片描边
border-radius: var(--r-lg);     // 12px，卡片
box-shadow: var(--sh-xs);       // 卡片
```

几条约定：

- **不要写 `#409EFF` 这类 Element 默认色**，用 `var(--brand-600)`
- **卡片靠边框分层，不堆阴影**：只有浮层（弹窗/下拉）用 `--sh-lg` 以上
- **表格不画竖线**，表头底色 + 行分隔线足够
- **数字加 `.num` 类**，用 `tabular-nums` 保证列对齐
- 页面标题区统一用 `.page-head` + `.page-title` + `.page-desc`

## 8. 测试

`test/customer_test.go`（复用 `newEnv`）：

```go
func TestCustomerCRUD(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")

	code, body := e.call(http.MethodPost, "/api/v1/customers", map[string]any{
		"name": "某某科技有限公司", "contact": "王经理", "phone": "13900001111", "status": 1,
	}, e.token)
	if code != http.StatusOK {
		t.Fatalf("创建失败: %d %v", code, body)
	}
	id := dataOf(body)["id"].(string)

	code, body = e.call(http.MethodGet, "/api/v1/customers?page=1&page_size=10&keyword=某某", nil, e.token)
	if code != http.StatusOK || len(dataOf(body)["list"].([]any)) != 1 {
		t.Fatalf("查询失败: %d %v", code, body)
	}

	if code, _ := e.call(http.MethodDelete, "/api/v1/customers/"+id, nil, e.token); code != http.StatusOK {
		t.Fatal("删除失败")
	}
}
```

跑：

```bash
go test ./test/ -run TestCustomerCRUD -v
make test-unit          # 仅单元测试，日常开发最快
make test-integration   # 仅 test/ 集成测试
make test               # 全量（含 -race）
make verify             # vet + check-arch + 单元测试（提交前）
make verify-full        # vet + check-arch + 全量测试（发版前）
make test-dialect       # 临时起 MySQL+PG 容器跑跨方言契约测试（需 Docker）
```

**测试分层**（P1-3）：

| 层 | 位置 | 时机 | 命令 |
| --- | --- | --- | --- |
| 单元 | `internal/**/*_test.go`、`pkg/**/*_test.go` | 每次改动 | `make test-unit` |
| 集成 / 启动自检 | `test/*_test.go` | 提交前、发版前 | `make test-integration` |
| 端到端 / 压测 | `test/e2e_test.go`、`test/bench_test.go` | 里程碑 | `make verify-full` |

**启动自检**：`bootstrap.New` 装配完成后会探测数据库连通性、缓存读写、存储可用性
（关键项，失败即拒绝启动），并检查迁移版本与种子状态（非关键，只告警）。
报告挂 `app.SelfCheck`，启动日志打印 `selfcheck=true/false`；`test/selfcheck_test.go`
断言健康装配全绿、关键依赖不可达时 fail-fast。

**跨方言契约验证**（P1-4）：`test/dialect_test.go` 用同一套断言覆盖 MySQL / PostgreSQL，
重点验证 epoch 毫秒时间列（PG 的 `INTEGER` 装不下，必须 `BIGINT`）、JSON、布尔、唯一索引、
中文 `LIKE` 模糊查询。未设 DSN 时自动跳过，本地 `go test ./...` 不受影响。两种方式运行：

```bash
make test-dialect                    # 自动起/停临时容器（需 Docker，推荐）
# 或手动指定已运行的数据库：
TENON_TEST_MYSQL_DSN='root:pwd@tcp(127.0.0.1:3306)/tenon' \
TENON_TEST_PG_DSN='postgres://u:p@127.0.0.1:5432/tenon?sslmode=disable' \
  go test ./test/ -run TestDialect -v
```

> 教训来源：某项目曾因 5 张表用 `INTEGER` 存 epoch 毫秒在 PG 上完全不可用，
> 而 SQLite（动态类型、`INTEGER` 为 64 位）侧的测试永远发现不了。故发布前务必跑一次真库。

---

## 9. 自检清单

- [ ] 模型注册到 `bootstrap.models()`，`make build` 通过
- [ ] 每个写接口都挂了 `RequirePerm`，权限点与菜单种子一致
- [ ] 写操作成功后调用了 `middleware.AuditInfo`
- [ ] 列表接口的过滤/排序字段都在白名单里
- [ ] 涉及敏感字段（手机号/身份证/金额）做了脱敏或权限控制
- [ ] 删除操作用软删除，并对"有下级/有引用"做拦截
- [ ] 前端按钮用 `can()` 控制，后端仍然校验
- [ ] 至少一个有权限用例 + 一个无权限用例
- [ ] `gofmt`、`go vet` 干净

---

## 10. 分支模型与提交规范

### 10.1 分支约定

`main` 是唯一长期分支，也是**默认的提交目标**。基础框架与核心通用能力直接
提交到 `main`，不为它们单独开分支——这些能力属于框架本身，每一次演进都应
立即成为后续业务分支的起点，额外的分支与合并只会让基线出现分叉。

直接进 `main` 的范围（框架自身）：

| 类别 | 示例 |
| --- | --- |
| 认证与安全 | 登录、JWT、验证码、两步验证、密码策略 |
| 权限模型 | RBAC、数据权限、菜单与权限点 |
| 组织与基础数据 | 用户、部门、角色、字典、参数配置 |
| 平台能力 | 审计日志、任务调度、文件存储、站内消息、监控指标 |
| 前端基座 | 布局、设计系统、路由守卫、通用组件 |
| 工程与构建 | 依赖升级、Makefile、Dockerfile、文档 |

需要开分支的是**面向具体业务的衍生功能**：它们不属于框架基线，是否合入、
何时合入取决于项目排期。

| 分支 | 用途 | 命名示例 |
| --- | --- | --- |
| `feature/*` | 具体业务模块、行业特性、定制页面 | `feature/customer-module` |
| `fix/*` | 非紧急缺陷修复 | `fix/user-export-filter` |
| `hotfix/*` | 生产问题紧急修复，从 `main` 切出 | `hotfix/login-lockout` |
| `chore/*` | 影响面大、需要评审的依赖或构建调整 | `chore/upgrade-gorm` |

判断标准一句话：**改完之后所有下游项目都会受益，就进 `main`；只有当前项目
需要，才开分支。**

### 10.2 日常工作流

核心通用能力直接提交：

```bash
git switch main

# ... 开发、自测 ...
make test && make build
./scripts/smoke.sh

git add -A
git commit -m "feat(auth): 两步验证支持扫码绑定与恢复码"
git push origin main
```

提交前必须满足：可编译、可启动、`make test` 与 `scripts/smoke.sh` 全部通过、
工作区没有残留调试文件。`main` 在任何提交点都应该能直接发版。

衍生业务功能走分支：

```bash
git switch main
git pull --ff-only

git switch -c feature/customer-module
# ... 开发、自测 ...
make test && make build

git switch main
git merge --no-ff feature/customer-module
git branch -d feature/customer-module
```

合并前先同步 `main`，避免历史分叉过深：

```bash
git switch feature/customer-module
git rebase main                    # 或 git merge main
```

如果核心能力误开了分支并已合并，在**尚未推送远端**时可以压平成一次直接提交：

```bash
git reset --soft <合并前的提交>
git commit -C <分支上的提交>
```

一旦推送，就不要再改写公共历史，改用新的正向提交修正。

### 10.3 提交信息

采用 Conventional Commits，`type` 固定下列取值，`scope` 用模块或目录名：

```
feat(user): 新增批量导入与失败明细
fix(auth): 修正刷新令牌并发续期产生的重复会话
refactor(query): 抽取过滤参数白名单
perf(cache): 字典加载改为一次性管道
docs(api): 补充消息中心接口
chore(deps): 升级 gorm 至 1.25.6
```

- 标题不超过 50 字，祈使句，不加句号
- 正文说明**为什么改**与影响面，不重复代码里已能看出的内容
- 一个提交只做一件事；格式化与逻辑改动分开提交

### 10.4 版本标签

发布在 `main` 上打 annotated tag，版本号遵循 `vMAJOR.MINOR.PATCH`：

```bash
git tag -a v1.1.0 -m "新增客户模块与消息批量操作"
git push origin main --tags
```

`Makefile` 会把 `git rev-parse --short HEAD` 写入二进制的 `version.Commit`，
构建产物与提交一一对应。

### 10.5 保护建议

推送到远端后对 `main` 开启分支保护，重点是**检查通过**而不是禁止直推：

- 要求 `make test`、`make build` 通过，且不允许强制推送
- 核心能力的提交允许维护者直接推送；若团队统一要求走 PR，也应由框架维护者
  当天合并，避免基线长期滞后、下游业务分支越拉越远
- 本地仓库不需要这些限制，但每次提交后应跑一遍 `scripts/smoke.sh`

---

## 11. 踩坑记录

> 按「现象 → 根因 → 处理」记录真实踩过的坑，靠检索而非口头传递。重要取舍另见
> [decisions.md](decisions.md)。

| 现象 | 根因 | 处理 |
| --- | --- | --- |
| Swagger 挂载 panic：catch-all `*any` 与静态段 `doc.json` 冲突 | Gin 同一层不能同时注册通配与静态段 | 只注册一条 `GET /docs/*any`，在 handler 内按 `c.Param("any")` 分流 `doc.json` / 首页 / UI 资源 |
| 资讯游标翻页重复、乱序 | 本地时间与 UTC 混用，SQLite 字符串比较时间 | 全链路统一 `UTC()`，游标用 `(published_at, id)` 复合键 |
| 会员筛选 `member + none != total` | `none` 未排除"等级已停用但未过期"的用户 | 重写三态筛选条件，把停用等级算入非会员 |
| 会员等级更新时 `benefits` 清不空 | GORM `Updates(map)` 忽略空切片 | 显式传序列化后的 JSON 字符串 `[]` |
| 图标上传后客户端 404 | 本地存储目录没有公开静态路由 | router 在本地模式挂载 `e.Static(baseURL, localDir)` |
| `go mod tidy` 把 go.mod 升到 1.23、编译失败 | 新依赖（goose/swag/gin-swagger/x 系列）要求更高 Go 版本 | 锁定兼容版本 + `GOTOOLCHAIN=local`，`replace modernc.org/sqlite v1.23.1` 绕过间接升级 |
| OAuth 回调路由与静态段冲突 | `/auth/oauth/:name/callback` 与 `/oauth/providers`  Param 层级冲突风险 | 改为 `/api/v1/oauth/callback/:name`，静态段与参数路由分层 |
| 绑定表允许同一外部账号绑多个用户 | 唯一索引误含 `user_id`，与按 `(provider,subject)` 查询不一致 | v4 迁移重建为两个两列唯一约束，冲突返回 409 |
| GFS 演练把 40 份备份全保留 | 只做同槽位去重，没限制每档槽位总数 | 每档只取最近 N 个槽位，达上限即停止该档选取 |
| GORM 故意制造唯一键冲突后，后续查询报错 | 复用同一 `*gorm.DB` 句柄，Statement 被失败的 INSERT 污染 | 每次操作新建句柄 `q()`，不复用带状态的 Session |
| 前端构建清掉被追踪的 `web/dist/.gitkeep` | Vite 构建清空 dist 目录 | 构建后 `git checkout -- web/dist/.gitkeep` 再提交 |

