import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'maintenance',
  routes: [
    {
      path: 'ops/maintenance',
      name: 'ops-maintenance',
      component: () => import('./pages/index.vue'),
      meta: { title: '备份与迁移' },
    },
  ],
})
