import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'role',
  routes: [
    {
      path: 'system/role',
      name: 'system-role',
      component: () => import('./pages/index.vue'),
      meta: { title: '角色管理' },
    },
  ],
})
