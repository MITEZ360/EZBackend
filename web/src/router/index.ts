import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { translate } from '@/i18n'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    /** 免登录访问 */
    public?: boolean
    /** 不在面包屑/菜单中体现 */
    hidden?: boolean
  }
}

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/pages/login/index.vue'),
    meta: { title: '登录', public: true, hidden: true },
  },
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
      {
        path: 'dashboard',
        name: 'dashboard',
        component: () => import('@/pages/dashboard/index.vue'),
        meta: { title: '仪表盘' },
      },
      {
        path: 'system/user',
        name: 'system-user',
        component: () => import('@/pages/system/user/index.vue'),
        meta: { title: '用户管理' },
      },
      {
        path: 'system/role',
        name: 'system-role',
        component: () => import('@/pages/system/role/index.vue'),
        meta: { title: '角色管理' },
      },
      {
        path: 'system/menu',
        name: 'system-menu',
        component: () => import('@/pages/system/menu/index.vue'),
        meta: { title: '菜单管理' },
      },
      {
        path: 'system/membership',
        name: 'system-membership',
        component: () => import('@/pages/system/membership/index.vue'),
        meta: { title: '会员等级' },
      },
      {
        path: 'system/message',
        name: 'system-message',
        component: () => import('@/pages/system/message/index.vue'),
        meta: { title: '消息中心' },
      },
      {
        path: 'system/apikey',
        name: 'system-apikey',
        component: () => import('@/pages/system/apikey/index.vue'),
        meta: { title: 'API 密钥' },
      },
      {
        path: 'content/news',
        name: 'content-news',
        component: () => import('@/pages/content/news/index.vue'),
        meta: { title: '资讯列表' },
      },
      {
        path: 'content/source',
        name: 'content-source',
        component: () => import('@/pages/content/source/index.vue'),
        meta: { title: '数据源' },
      },
      {
        path: 'content/category',
        name: 'content-category',
        component: () => import('@/pages/content/category/index.vue'),
        meta: { title: '资讯分类' },
      },
      {
        path: 'system/dept',
        name: 'system-dept',
        component: () => import('@/pages/system/dept/index.vue'),
        meta: { title: '部门管理' },
      },
      {
        path: 'system/dict',
        name: 'system-dict',
        component: () => import('@/pages/system/dict/index.vue'),
        meta: { title: '字典管理' },
      },
      {
        path: 'system/config',
        name: 'system-config',
        component: () => import('@/pages/system/config/index.vue'),
        meta: { title: '参数配置' },
      },
      {
        path: 'system/notice',
        name: 'system-notice',
        component: () => import('@/pages/system/notice/index.vue'),
        meta: { title: '通知公告' },
      },
      {
        path: 'ops/job',
        name: 'ops-job',
        component: () => import('@/pages/ops/job/index.vue'),
        meta: { title: '定时任务' },
      },
      {
        path: 'ops/file',
        name: 'ops-file',
        component: () => import('@/pages/ops/file/index.vue'),
        meta: { title: '文件管理' },
      },
      {
        path: 'ops/login-log',
        name: 'ops-login-log',
        component: () => import('@/pages/ops/login-log/index.vue'),
        meta: { title: '登录日志' },
      },
      {
        path: 'ops/operation-log',
        name: 'ops-operation-log',
        component: () => import('@/pages/ops/operation-log/index.vue'),
        meta: { title: '操作日志' },
      },
      {
        path: 'ops/online',
        name: 'ops-online',
        component: () => import('@/pages/ops/online/index.vue'),
        meta: { title: '在线用户' },
      },
      {
        path: 'ops/monitor',
        name: 'ops-monitor',
        component: () => import('@/pages/ops/monitor/index.vue'),
        meta: { title: '服务监控' },
      },
      {
        path: 'ops/maintenance',
        name: 'ops-maintenance',
        component: () => import('@/pages/ops/maintenance/index.vue'),
        meta: { title: '备份与迁移' },
      },
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
  const hasToken = Boolean(localStorage.getItem('keel.access.token'))

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

  // 页面级权限：以服务端返回的菜单树为唯一依据，避免用户点到空白页
  if (to.name !== 'forbidden' && !isFreeRoute(to.path) && !inMenus(to.path)) {
    return { name: 'forbidden' }
  }
  return true
})

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
  const base = translate('Keel 管理控制台')
  document.title = title ? `${translate(title)} · ${base}` : base
}

router.afterEach((to) => {
  applyDocumentTitle(to.meta.title)
})

export default router
