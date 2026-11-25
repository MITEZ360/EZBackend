import { defineWebModule } from '../types'

// 自助注册是布局之外的独立页面：没登录也能访问，因此放在 publicRoutes。
export default defineWebModule({
  name: 'cnauth',
  publicRoutes: [
    {
      path: '/register',
      name: 'register',
      component: () => import('./pages/index.vue'),
      meta: { title: '用户注册', public: true, hidden: true },
    },
  ],
})
