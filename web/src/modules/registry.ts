import type { RouteRecordRaw } from 'vue-router'
import type { WebModule } from './types'

/**
 * 模块注册表：扫描 web/src/modules/<name>/index.ts，按构建期清单过滤。
 *
 * 裁剪由 VITE_MODULES 驱动（Makefile 把后端的模块清单原样传进来），
 * 未列出的模块由 vite 插件的 load 钩子替换成空模块，其页面 chunk 不会进入产物。
 * 未设置该变量时全部启用，`pnpm dev` 与默认 `pnpm build` 都按全量工作。
 */
function enabledNames(): string[] {
  const raw = (import.meta.env.VITE_MODULES ?? '').trim()
  return raw
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

const discovered = Object.entries(
  import.meta.glob<{ default: WebModule }>('./*/index.ts', { eager: true }),
)
  .map(([, mod]) => mod?.default)
  .filter((mod): mod is WebModule => Boolean(mod?.name))
  .sort((a, b) => a.name.localeCompare(b.name))

const enabled = enabledNames()

/** 当前构建实际生效的模块，顺序固定（按 name 排序）便于路由表稳定 */
export const WEB_MODULES: WebModule[] = enabled.length
  ? discovered.filter((m) => enabled.includes(m.name))
  : discovered

/** 布局内路由，装配到 BasicLayout 的 children */
export const MODULE_ROUTES: RouteRecordRaw[] = WEB_MODULES.flatMap((m) => m.routes ?? [])

/** 布局外路由，装配到根级 */
export const MODULE_PUBLIC_ROUTES: RouteRecordRaw[] = WEB_MODULES.flatMap(
  (m) => m.publicRoutes ?? [],
)

/**
 * 模块词条合并。
 *
 * 词条是全局共享的 key 空间，同名 key 后写覆盖先写；模块之间不应复用
 * 同一句中文表达不同含义，否则翻译会互相覆盖。共享的通用词条仍在
 * i18n/locales 下集中维护。
 */
export const MODULE_I18N: Record<string, string> = Object.assign(
  {},
  ...WEB_MODULES.map((m) => m.i18n ?? {}),
)
