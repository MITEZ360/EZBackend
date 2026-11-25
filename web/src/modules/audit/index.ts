import { defineWebModule } from '../types'

// 在线用户与两类日志同属审计模块：接口与菜单都挂在 audit 下，
// 会话查询需要审计里的会话登记信息。
export default defineWebModule({
  name: 'audit',
  routes: [
    {
      path: 'ops/login-log',
      name: 'ops-login-log',
      component: () => import('./pages/login-log.vue'),
      meta: { title: '登录日志' },
    },
    {
      path: 'ops/operation-log',
      name: 'ops-operation-log',
      component: () => import('./pages/operation-log.vue'),
      meta: { title: '操作日志' },
    },
    {
      path: 'ops/online',
      name: 'ops-online',
      component: () => import('./pages/online.vue'),
      meta: { title: '在线用户' },
    },
  ],
})
