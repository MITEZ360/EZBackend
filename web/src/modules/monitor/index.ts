import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'monitor',
  routes: [
    {
      path: 'ops/monitor',
      name: 'ops-monitor',
      component: () => import('./pages/index.vue'),
      meta: { title: '服务监控' },
    },
  ],
})
