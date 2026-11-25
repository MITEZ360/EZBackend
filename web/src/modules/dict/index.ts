import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'dict',
  routes: [
    {
      path: 'system/dict',
      name: 'system-dict',
      component: () => import('./pages/index.vue'),
      meta: { title: '字典管理' },
    },
  ],
})
