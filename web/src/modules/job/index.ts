import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'job',
  routes: [
    {
      path: 'ops/job',
      name: 'ops-job',
      component: () => import('./pages/index.vue'),
      meta: { title: '定时任务' },
    },
  ],
})
