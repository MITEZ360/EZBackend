import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'configcenter',
  routes: [
    {
      path: 'ops/config',
      name: 'ops-config-center',
      component: () => import('./pages/index.vue'),
      meta: { title: '运行参数' },
    },
    {
      path: 'gov/config',
      name: 'gov-config-center',
      component: () => import('./pages/govconfig.vue'),
      meta: { title: '配置中心' },
    },
  ],
})
