import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'notice',
  routes: [
    {
      path: 'system/notice',
      name: 'system-notice',
      component: () => import('./pages/index.vue'),
      meta: { title: '通知公告' },
    },
  ],
})
