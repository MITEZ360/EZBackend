# 前端模块约定

前端页面按业务模块自包含，与后端 `internal/modules/<name>` 一一对应。
新增、删除、裁剪模块都只动模块目录，不改中心路由表。

## 1. 目录布局

```
web/src/modules/<name>/
  index.ts            模块入口（必须）
  pages/              页面组件（必须，除非模块只有公开页面）
    index.vue         主页面
    <sub>.vue         次要页面（同一模块的多个页面）
    <sub>/            页面带局部组件时用同名目录
      index.vue
      converter.ts
  api.ts              仅该模块使用的接口封装（可选）
  i18n.ts             仅该模块使用的英文词条（可选）
```

`<name>` 必须与后端 `modulekit.Spec.Name` 完全一致。
不一致的后果：`make dist MODULES="..."` 时前端裁错页面，菜单点进去 404。

后端模块名以 `go run ./cmd/distgen -list` 为准，不要凭目录名猜。
例如 `internal/modules/system/` 里注册的是 `dict` 与 `config` 两个模块，
前端对应 `modules/dict/` 与 `modules/config/`，没有 `modules/system/`。

## 2. 模块入口

`index.ts` 默认导出一个 `WebModule`：

```ts
import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'news',
  routes: [
    {
      path: 'content/news',           // 相对 BasicLayout，不带前导斜杠
      name: 'content-news',           // 全局唯一
      component: () => import('./pages/index.vue'),
      meta: { title: '资讯列表' },     // 中文原文即翻译 key
    },
  ],
})
```

字段说明：

| 字段 | 必填 | 用途 |
| --- | --- | --- |
| `name` | 是 | 与后端模块名一致，裁剪依据 |
| `routes` | 否 | 挂在 `BasicLayout` 下的页面，`path` 不带前导斜杠 |
| `publicRoutes` | 否 | 布局之外的页面（免登录），`path` 带前导斜杠，`meta.public = true` |
| `i18n` | 否 | 该模块的英文词条，中文包以原文为 key，不必登记中文侧 |

`meta.title` 必须是**中文原文**：它同时是页面标题的翻译 key，英文由 `i18n` 提供。

## 3. 装配与裁剪

```
web/src/modules/*/index.ts
        │ import.meta.glob（自动发现，按 name 排序保证顺序稳定）
        ▼
web/src/modules/registry.ts  ──▶  router（MODULE_ROUTES / MODULE_PUBLIC_ROUTES）
                             ──▶  i18n（MODULE_I18N 合并进英文包）
```

裁剪由 `VITE_MODULES` 驱动，取值来自后端：

```bash
make dist MODULES="user news"
  ├─ go run ./cmd/distgen -modules "user news" -name ...
  └─ make web MODULES="user news"
       ├─ FRONT_MODULES := go run ./cmd/distgen -modules "user news" -print
       └─ VITE_MODULES="$FRONT_MODULES" pnpm build
```

`distgen -print` 输出的是**最终装配集合**，含依赖补齐与常驻模块
（`auth`、`config`、`dict`、`notice`、`menu`）。因此前端拿到的清单与后端
实际装进二进制的模块完全相同，不会出现"后端有菜单、前端没页面"。

裁剪在后端不可见的地方发生：`web/plugins/module-filter.ts` 在 `load` 阶段
把未启用模块的 `index.ts` 替换成空模块，其内部的动态 `import()` 不再展开，
对应页面 chunk 不进入 `dist`。这不是运行期开关，产物里真的没有这些文件。

`VITE_MODULES` 未设置时全部启用，`pnpm dev` 与默认 `make build` 走这条路径。

## 4. 什么该放进模块，什么不该

| 内容 | 位置 | 理由 |
| --- | --- | --- |
| 模块自己的页面 | `modules/<name>/pages/` | 自包含，删模块即删页面 |
| 模块自己的路由 | `modules/<name>/index.ts` | 同上 |
| 只在模块内使用的接口封装 | `modules/<name>/api.ts` | 同上 |
| 模块特有的英文词条 | `modules/<name>/i18n.ts` | 同上 |
| 跨模块共享的接口封装 | `web/src/api/*.ts` | 见下方"已知边界" |
| 通用组件（表格、上传、表单） | `web/src/components/` | 与业务无关 |
| 布局、登录、个人中心、错误页 | `web/src/pages/` | 应用壳，任何发行版都有 |
| 通用词条（确定/取消/共 {n} 条） | `web/src/i18n/locales/` | 全局共享 key 空间 |
| 路由守卫、菜单校验、首页兜底 | `web/src/router/index.ts` | 全局策略，下放会导致每个模块重写一遍 |

**不要**在模块之间互相 import 页面。可以引用共享组件与共享 API。

## 5. 新增一个模块

1. 后端建 `internal/modules/<name>/`，实现 `modulekit.Spec` 并自注册。
2. 前端建 `web/src/modules/<name>/index.ts` 与 `pages/`。
3. 若要参与裁剪，在 `internal/distribution/catalog/catalog.go` 的可选清单里登记。
4. 分别验证：

```bash
go run ./cmd/distgen -list                      # 确认模块名与依赖
make dist MODULES="<name>" && ./bin/tenon -config configs/config.yaml
```

不需要改 `router/index.ts`、`i18n/index.ts`、`vite.config.ts`。改了就说明约定没跟上。

## 6. 删除一个模块

```bash
rm -rf internal/modules/<name> web/src/modules/<name>
# 若登记过可选清单，从 internal/distribution/catalog/catalog.go 移除
# 若有共享 API 仍在被别处引用（如 api/membership.ts），一并清理调用点
```

## 7. 已知边界

**共享 API 不随模块裁剪**。`web/src/api/membership.ts`（390 字节）被
`BasicLayout`（会员徽标）、`pages/profile`、`modules/user` 三处引用，
即使 Base 版不含 membership 模块，这个文件仍会打进产物。

处理原则：接口封装本身不含页面与业务逻辑，体积可忽略，调用点由
`IS_BASE` 关闭。若某天需要彻底消除，得先把这些横切引用改成
按模块提供的可选能力，成本远高于收益。

**模块间 i18n 冲突**。同一句中文只能对应一个英文翻译。若两个模块都要用
「共 {n} 条」，写在全局；若某个模块对同一句话有不同译法，那是文案问题，
应当改写中文而不是争夺同一个 key。

**孤儿 chunk**。删除模块后，如果 `web/dist` 没有重新构建，旧的
`assets/*.js` 仍可能残留。`vite build` 默认 `emptyOutDir`，正常构建不会有。

## 8. 改完必须跑的校验

```bash
cd web && pnpm typecheck      # 路由与模块契约的类型错误
make build                    # 全量：前端 + 二进制
make dist MODULES="<清单>"   # 裁剪：确认未选模块的页面 chunk 不存在
```

裁剪是否真的生效，看产物而不是看页面：

```bash
ls web/dist/assets | grep -iE "news|recharge|apikey|cluster|workflow"
```

有输出说明模块过滤没起作用，检查 `VITE_MODULES` 是否传到了 vite 进程。
