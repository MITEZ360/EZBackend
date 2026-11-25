import { api } from './client'
import type { ListQuery, PageResult } from './types'

/** 审批人来源 */
export interface FlowAssignee {
  type: 'user' | 'role' | 'deptLeader' | 'initiatorLeader' | 'initiatorSelf' | 'initiatorInput'
  value?: string
}

/** 结构化跳过条件 */
export interface FlowCondition {
  field?: string
  op?: 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte' | 'in'
  value?: unknown
  all?: FlowCondition[]
  any?: FlowCondition[]
}

/** 流程节点 */
export interface FlowNode {
  id: string
  name: string
  type?: 'approve' | 'cc'
  assignees?: FlowAssignee[]
  mode?: 'any' | 'all'
  onReject?: 'backToInitiator' | 'end' | 'prevNode'
  sequential?: boolean
  skipWhen?: FlowCondition | null
  timeoutHours?: number
  timeoutAction?: 'notify' | 'approve' | 'reject'
}

export interface FlowDef {
  id: string
  key: string
  name: string
  description?: string
  status: number
  nodes: FlowNode[]
  version: number
  createdAt: string
  updatedAt: string
}

export interface FlowDefPayload {
  key: string
  name: string
  description?: string
  status?: number
  nodes: FlowNode[]
}

export interface FlowInstance {
  id: string
  flowKey: string
  flowName?: string
  businessId?: string
  businessType?: string
  title?: string
  initiatorId: string
  status: 'running' | 'approved' | 'rejected' | 'cancelled'
  currentNode?: string
  nodesSnapshot: FlowNode[]
  variables?: Record<string, unknown>
  round: number
  startedAt?: string
  finishedAt?: string
  createdAt: string
}

export interface FlowTask {
  id: string
  instanceId: string
  nodeId: string
  nodeName?: string
  assigneeId: string
  status: 'pending' | 'approved' | 'rejected' | 'transferred' | 'cancelled'
  action?: string
  comment?: string
  isAdded?: boolean
  round: number
  finishedAt?: string
  createdAt: string
}

export interface FlowRecord {
  id: string
  instanceId: string
  taskId?: string
  nodeId?: string
  nodeName?: string
  action: string
  operatorId?: string
  comment?: string
  fromUser?: string
  toUser?: string
  round: number
  createdAt: string
}

export interface InstanceDetail {
  instance: FlowInstance
  tasks: FlowTask[]
  records: FlowRecord[]
}

export interface TaskDetail {
  task: FlowTask
  instance: FlowInstance
}

export interface FlowDelegate {
  id: string
  fromUserId: string
  toUserId: string
  startAt: string
  endAt: string
  enabled: boolean
  remark?: string
  createdAt: string
}

export interface DelegatePayload {
  fromUserId: string
  toUserId: string
  startAt: string
  endAt: string
  enabled?: boolean
  remark?: string
}

export const flowApi = {
  /* 流程定义 */
  flows: (query: ListQuery) => api.get<PageResult<FlowDef>>('/flows', query),
  flow: (id: string) => api.get<FlowDef>(`/flows/${id}`),
  createFlow: (payload: FlowDefPayload) => api.post<FlowDef>('/flows', payload),
  updateFlow: (id: string, payload: FlowDefPayload) => api.put<void>(`/flows/${id}`, payload),
  setFlowStatus: (id: string, status: number) => api.put<void>(`/flows/${id}/status`, { status }),
  removeFlow: (id: string) => api.del<void>(`/flows/${id}`),

  /* 实例 */
  start: (payload: {
    flowKey: string
    businessId?: string
    businessType?: string
    title?: string
    variables?: Record<string, unknown>
  }) => api.post<FlowInstance>('/flow-instances', payload),
  instances: (query: ListQuery) => api.get<PageResult<FlowInstance>>('/flow-instances', query),
  instance: (id: string) => api.get<InstanceDetail>(`/flow-instances/${id}`),
  cancelInstance: (id: string) => api.post<void>(`/flow-instances/${id}/cancel`),
  rollback: (id: string, nodeId: string, reason?: string) =>
    api.post<void>(`/flow-instances/${id}/rollback`, { nodeId, reason }),

  /* 任务 */
  tasks: (query: ListQuery) => api.get<PageResult<FlowTask>>('/flow-tasks', query),
  task: (id: string) => api.get<TaskDetail>(`/flow-tasks/${id}`),
  approve: (id: string, comment?: string) => api.post<void>(`/flow-tasks/${id}/approve`, { comment }),
  reject: (id: string, comment?: string) => api.post<void>(`/flow-tasks/${id}/reject`, { comment }),
  transfer: (id: string, targetId: string, comment?: string) =>
    api.post<void>(`/flow-tasks/${id}/transfer`, { targetId, comment }),
  addSign: (id: string, targetId: string) => api.post<void>(`/flow-tasks/${id}/addsign`, { targetId }),
  removeSign: (id: string, comment?: string) =>
    api.post<void>(`/flow-tasks/${id}/removesign`, { comment }),
  timeoutScan: () => api.post<{ handled: number }>('/flow-tasks/timeout-scan'),

  /* 委托 */
  delegates: (query: ListQuery) => api.get<PageResult<FlowDelegate>>('/flow-delegates', query),
  createDelegate: (payload: DelegatePayload) => api.post<FlowDelegate>('/flow-delegates', payload),
  updateDelegate: (id: string, payload: DelegatePayload) => api.put<void>(`/flow-delegates/${id}`, payload),
  removeDelegate: (id: string) => api.del<void>(`/flow-delegates/${id}`),
}
