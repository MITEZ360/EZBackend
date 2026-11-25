import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'recharge',
  routes: [
    {
      path: 'recharge',
      name: 'recharge',
      component: () => import('./pages/index.vue'),
      meta: { title: '会员充值' },
    },
  ],
})
