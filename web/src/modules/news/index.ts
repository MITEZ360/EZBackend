import { defineWebModule } from '../types'

// 资讯中心：列表、抓取来源、分类白名单三个页面同属 news 模块。
export default defineWebModule({
  name: 'news',
  routes: [
    {
      path: 'content/news',
      name: 'content-news',
      component: () => import('./pages/index.vue'),
      meta: { title: '资讯列表' },
    },
    {
      path: 'content/source',
      name: 'content-source',
      component: () => import('./pages/source.vue'),
      meta: { title: '数据源' },
    },
    {
      path: 'content/category',
      name: 'content-category',
      component: () => import('./pages/category.vue'),
      meta: { title: '资讯分类' },
    },
  ],
})
