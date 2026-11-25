import { defineWebModule } from '../types'

// 流程引擎：设计器、我发起的、待办、委托。
export default defineWebModule({
  name: 'flow',
  routes: [
    {
      path: 'workflow/design',
      name: 'workflow-design',
      component: () => import('./pages/design/index.vue'),
      meta: { title: '流程设计' },
    },
    {
      path: 'workflow/instances',
      name: 'workflow-instances',
      component: () => import('./pages/instances.vue'),
      meta: { title: '我发起的' },
    },
    {
      path: 'workflow/tasks',
      name: 'workflow-tasks',
      component: () => import('./pages/tasks.vue'),
      meta: { title: '审批任务' },
    },
    {
      path: 'workflow/delegates',
      name: 'workflow-delegates',
      component: () => import('./pages/delegates.vue'),
      meta: { title: '审批委托' },
    },
  ],
})
