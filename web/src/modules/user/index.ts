import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'user',
  routes: [
    {
      path: 'system/user',
      name: 'system-user',
      component: () => import('./pages/index.vue'),
      meta: { title: '用户管理' },
    },
  ],
})
