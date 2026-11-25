import { defineWebModule } from '../types'

// 危险操作依赖集群指令下发（重载配置、重启节点、清缓存都走广播），
// 与 cluster 同进同出，没有集群就没有可下发的对象。
export default defineWebModule({
  name: 'cluster',
  routes: [
    {
      path: 'ops/cluster',
      name: 'ops-cluster',
      component: () => import('./pages/index.vue'),
      meta: { title: '集群管理' },
    },
    {
      path: 'ops/danger',
      name: 'ops-danger',
      component: () => import('./pages/danger.vue'),
      meta: { title: '危险操作' },
    },
  ],
})
