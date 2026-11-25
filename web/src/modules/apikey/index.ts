import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'apikey',
  routes: [
    {
      path: 'system/apikey',
      name: 'system-apikey',
      component: () => import('./pages/index.vue'),
      meta: { title: 'API 密钥' },
    },
  ],
})
