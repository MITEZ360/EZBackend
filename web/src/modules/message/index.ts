import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'message',
  routes: [
    {
      path: 'system/message',
      name: 'system-message',
      component: () => import('./pages/index.vue'),
      meta: { title: '消息中心' },
    },
  ],
})
