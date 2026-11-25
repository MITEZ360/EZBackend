import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'dashboard',
  routes: [
    {
      path: 'dashboard',
      name: 'dashboard',
      component: () => import('./pages/index.vue'),
      meta: { title: '仪表盘' },
    },
  ],
})
