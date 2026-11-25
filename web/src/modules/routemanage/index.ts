import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'routemanage',
  routes: [
    {
      path: 'ops/routes',
      name: 'ops-routes',
      component: () => import('./pages/index.vue'),
      meta: { title: '路由管理' },
    },
  ],
})
