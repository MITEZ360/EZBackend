import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'file',
  routes: [
    {
      path: 'ops/file',
      name: 'ops-file',
      component: () => import('./pages/index.vue'),
      meta: { title: '文件管理' },
    },
  ],
})
