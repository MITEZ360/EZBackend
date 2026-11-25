import { defineWebModule } from '../types'

// 目录名沿用后端 internal/modules/system，模块名取 Spec.Name = config：
// 那个包里还有 dict 模块，前端只对应"参数配置"这一个页面。
export default defineWebModule({
  name: 'config',
  routes: [
    {
      path: 'system/config',
      name: 'system-config',
      component: () => import('./pages/index.vue'),
      meta: { title: '参数配置' },
    },
  ],
})
