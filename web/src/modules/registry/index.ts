import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'registry',
  routes: [
    {
      path: 'gov/services',
      name: 'gov-services',
      component: () => import('./pages/services.vue'),
      meta: { title: '服务目录' },
    },
  ],
})
