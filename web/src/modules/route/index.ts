import { defineWebModule } from '../types'

export default defineWebModule({
  name: 'route',
  routes: [
    {
      path: 'gov/routes',
      name: 'gov-routes',
      component: () => import('./pages/rules.vue'),
      meta: { title: '动态路由' },
    },
    {
      path: 'gov/releases',
      name: 'gov-releases',
      component: () => import('./pages/releases.vue'),
      meta: { title: '发布记录' },
    },
  ],
})
