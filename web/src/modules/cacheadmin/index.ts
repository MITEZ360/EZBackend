import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'cacheadmin',
  routes: [
    {
      path: 'ops/cache',
      name: 'ops-cache',
      component: () => import('./pages/index.vue'),
      meta: { title: '缓存管理' },
    },
  ],
})
