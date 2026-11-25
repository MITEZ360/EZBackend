import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'menu',
  routes: [
    {
      path: 'system/menu',
      name: 'system-menu',
      component: () => import('./pages/index.vue'),
      meta: { title: '菜单管理' },
    },
  ],
})
