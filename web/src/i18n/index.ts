import { createI18n } from 'vue-i18n'
import en from 'element-plus/es/locale/lang/en'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import zhMessages from './locales/zh-CN'
import enMessages from './locales/en'

export type AppLocale = 'zh-CN' | 'en'
export const LOCALE_KEY = 'keel.locale'
export const SUPPORTED_LOCALES: { value: AppLocale; label: string }[] = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'en', label: 'English' },
]

export function normalizeLocale(value?: string | null): AppLocale {
  const v = (value ?? '').replace('_', '-').toLowerCase()
  if (v.startsWith('en')) return 'en'
  return 'zh-CN'
}

function initialLocale(): AppLocale {
  const saved = localStorage.getItem(LOCALE_KEY)
  if (saved) return normalizeLocale(saved)
  const browser = typeof navigator !== 'undefined' ? navigator.language : ''
  return browser.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en'
}

// Composition API 模式 + 全局注入，模板里可直接用 $t 而不用逐页 useI18n。
export const i18n = createI18n({
  legacy: false,
  globalInjection: true,
  locale: initialLocale(),
  fallbackLocale: 'zh-CN',
  messages: { 'zh-CN': zhMessages, en: enMessages },
  missingWarn: false,
  fallbackWarn: false,
})

export function currentLocale(): AppLocale {
  return normalizeLocale(i18n.global.locale.value)
}

/** Element Plus 控件文案跟随应用语言 */
export function elementLocale() {
  return currentLocale() === 'en' ? en : zhCn
}

export function setLocale(value: AppLocale) {
  i18n.global.locale.value = value
  localStorage.setItem(LOCALE_KEY, value)
  document.documentElement.lang = value
}

// 启动时同步 html lang，便于浏览器翻译与无障碍工具识别。
document.documentElement.lang = currentLocale()

/** 供没有组件上下文的模块（格式化工具、请求拦截）使用 */
export function translate(key: string, params?: Record<string, unknown>): string {
  return i18n.global.t(key, params ?? {})
}
