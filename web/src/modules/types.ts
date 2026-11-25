import type { RouteRecordRaw } from 'vue-router'

/**
 * 前端模块契约。
 *
 * 每个业务模块在 web/src/modules/<name>/index.ts 默认导出本结构，
 * registry 用 import.meta.glob 自动发现，router 与 i18n 据此装配。
 * name 必须与后端 internal/modules/<name> 的 modulekit.Spec.Name 一致——
 * 这是前后端裁剪保持同步的唯一依据（见 docs/frontend-modules.md）。
 */
export interface WebModule {
  /** 模块名，与后端 modulekit.Spec.Name 相同 */
  name: string
  /** 挂在 BasicLayout 下的页面，path 不带前导斜杠 */
  routes?: RouteRecordRaw[]
  /** 布局之外的页面（如自助注册），path 要带前导斜杠 */
  publicRoutes?: RouteRecordRaw[]
  /** 中文原文 → 英文。中文包以原文为 key，因此不必登记中文侧 */
  i18n?: Record<string, string>
}

/** 参数化定义，只为让 index.ts 里的对象获得类型提示 */
export function defineWebModule(mod: WebModule): WebModule {
  return mod
}
