import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'membership',
  routes: [
    {
      path: 'system/membership',
      name: 'system-membership',
      component: () => import('./pages/index.vue'),
      meta: { title: '会员等级' },
    },
  ],
})
