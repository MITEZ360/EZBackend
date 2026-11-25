/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** 发行版：base 表示只含后台管理基础功能；未设置即完整版 */
  readonly VITE_EDITION?: 'base' | 'default'
  /**
   * 启用的前端模块清单，空格或逗号分隔（make dist 从后端模块表传进来）。
   * 未设置表示全部启用；未列出的模块由 vite 插件替换成空模块，页面不进产物。
   */
  readonly VITE_MODULES?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, unknown>, Record<string, unknown>, unknown>
  export default component
}
