import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'dept',
  routes: [
    {
      path: 'system/dept',
      name: 'system-dept',
      component: () => import('./pages/index.vue'),
      meta: { title: '部门管理' },
    },
  ],
})
