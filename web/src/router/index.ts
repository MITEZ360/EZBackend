import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { translate } from '@/i18n'
import type { MenuNode } from '@/api/types'
import { MODULE_PUBLIC_ROUTES, MODULE_ROUTES } from '@/modules/registry'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    /** 免登录访问 */
    public?: boolean
    /** 不在面包屑/菜单中体现 */
    hidden?: boolean
  }
}

/**
 * 路由表由两段拼成：
 *
 *  1. 应用壳：登录、第三方回调、个人中心、错误页。任何发行版都有；
 *  2. 业务页面：来自 web/src/modules/<name>/index.ts，registry 自动发现，
 *     按 VITE_MODULES 裁剪。加页面只改模块目录，不改这里。
 */
const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/pages/login/index.vue'),
    meta: { title: '登录', public: true, hidden: true },
  },
  // 布局之外的模块页面（当前只有 cnauth 的自助注册）
  ...MODULE_PUBLIC_ROUTES,
  {
    path: '/oauth/callback',
    name: 'oauth-callback',
    component: () => import('@/pages/oauth/callback.vue'),
    meta: { title: '第三方登录', public: true, hidden: true },
  },
  {
    path: '/',
    component: () => import('@/layouts/BasicLayout.vue'),
    redirect: '/dashboard',
    children: [
      ...MODULE_ROUTES,
      {
        path: 'profile',
        name: 'profile',
        component: () => import('@/pages/profile/index.vue'),
        meta: { title: '个人中心' },
      },
    ],
  },
  {
    path: '/403',
    name: 'forbidden',
    component: () => import('@/pages/error/403.vue'),
    meta: { title: '无权访问', hidden: true },
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/pages/error/404.vue'),
    meta: { title: '页面不存在', public: true, hidden: true },
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  const hasToken = Boolean(localStorage.getItem('tenon.access.token'))

  if (to.meta.public) {
    // 已登录再访问登录页，直接进控制台
    if (to.name === 'login' && hasToken) return { path: '/dashboard' }
    return true
  }

  if (!hasToken) {
    return { name: 'login', query: to.fullPath === '/' ? undefined : { redirect: to.fullPath } }
  }

  if (!auth.loaded) {
    try {
      await auth.loadProfile()
    } catch {
      auth.reset()
      return { name: 'login', query: { redirect: to.fullPath } }
    }
  }

  // 首页兜底：定制发行版未必含 dashboard 模块（make dist 按需组装），
  // 写死 /dashboard 会让登录后直接撞上 403。落到菜单里第一个可访问页面。
  if ((to.path === '/' || to.path === '/dashboard') && !inMenus(to.path)) {
    const home = homePath()
    if (home !== to.path) return { path: home }
  }

  // 页面级权限：以服务端返回的菜单树为唯一依据，避免用户点到空白页
  if (to.name !== 'forbidden' && !isFreeRoute(to.path) && !inMenus(to.path)) {
    return { name: 'forbidden' }
  }
  return true
})

/** 菜单树里第一个可访问的页面路径 */
function homePath(nodes: MenuNode[] = useAuthStore().menus): string {
  return pickPath(nodes) || '/profile'
}

/** 目录节点本身没有页面，取它的第一个页面后代；递归空串表示这条分支没有可用页面 */
function pickPath(nodes: MenuNode[]): string {
  for (const node of nodes) {
    if (node.type === 'button') continue
    if (node.type === 'dir') {
      const inner = pickPath(node.children ?? [])
      if (inner) return inner
      continue
    }
    if (node.path) return node.path
  }
  return ''
}

/** profile 等不在菜单中的页面放行 */
function isFreeRoute(path: string): boolean {
  return path === '/profile'
}

/** 判断路径是否出现在当前用户的菜单树中（含子节点） */
function inMenus(path: string, nodes = useAuthStore().menus): boolean {
  for (const node of nodes) {
    if (node.path === path) return true
    if (node.children?.length && inMenus(path, node.children)) return true
  }
  return false
}

/** 页面标题以中文原文作为翻译 key，切换语言时由 App.vue 重新调用 */
export function applyDocumentTitle(title?: string) {
  const base = translate('Tenon 管理控制台')
  document.title = title ? `${translate(title)} · ${base}` : base
}

router.afterEach((to) => {
  applyDocumentTitle(to.meta.title)
})

export default router
