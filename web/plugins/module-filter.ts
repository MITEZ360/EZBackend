import type { Plugin } from 'vite'

/** 匹配 web/src/modules/<name>/index.ts，兼容 Windows 反斜杠 */
const MODULE_ENTRY = /[\\/]src[\\/]modules[\\/]([^\\/]+)[\\/]index\.ts$/

/**
 * 按 VITE_MODULES 裁剪前端模块。
 *
 * 未列出的模块，其 index.ts 被替换成空模块——里面的动态 import 不会展开，
 * 对应页面 chunk 也就不进产物。这样 `make dist MODULES="user news"` 的前端
 * 与后端保持同一份清单，而不是靠运行期开关隐藏入口。
 *
 * VITE_MODULES 为空（pnpm dev、默认 pnpm build）时不做任何替换。
 */
export function moduleFilter(): Plugin {
  const enabled = (process.env.VITE_MODULES ?? '')
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean)

  return {
    name: 'tenon:module-filter',
    enforce: 'pre',
    load(id) {
      if (!enabled.length) return null
      const matched = MODULE_ENTRY.exec(id)
      if (!matched || enabled.includes(matched[1])) return null
      return 'export default { name: "", routes: [], publicRoutes: [], i18n: {} }'
    },
  }
}
