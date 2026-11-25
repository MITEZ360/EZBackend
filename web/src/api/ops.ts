import { api } from './client'
import type {
  CacheKeyPage,
  CacheOverview,
  ClusterCommand,
  ClusterCommandsResult,
  ClusterNodesResult,
  ConfigApplyResult,
  ConfigListResult,
  FileRow,
  JobLogRow,
  JobRow,
  ListQuery,
  LoginLogRow,
  OnlineSessionRow,
  OperationLogRow,
  PageResult,
  RouteInfo,
  RoutePage,
  SystemInfo,
} from './types'

/* -------------------------------------------------------------- 定时任务 */
export interface JobPayload {
  name: string
  cron: string
  handler: string
  params?: string
  status: number
  remark?: string
}

export const jobApi = {
  list: (query: ListQuery) => api.get<PageResult<JobRow>>('/jobs', query),
  handlers: () => api.get<string[]>('/jobs/handlers'),
  create: (payload: JobPayload) => api.post<JobRow>('/jobs', payload),
  update: (id: string, payload: JobPayload) => api.put<void>(`/jobs/${id}`, payload),
  remove: (id: string) => api.del<void>(`/jobs/${id}`),
  setStatus: (id: string, status: number) => api.put<void>(`/jobs/${id}/status`, { status }),
  run: (id: string) => api.post<unknown>(`/jobs/${id}/run`),
  logs: (query: ListQuery & { jobId?: string }) =>
    api.get<PageResult<JobLogRow>>('/jobs/logs', query),
}

/* ------------------------------------------------------------------ 文件 */
export const fileApi = {
  list: (query: ListQuery) => api.get<PageResult<FileRow>>('/files', query),
  upload: (form: FormData) => api.upload<FileRow>('/files', form),
  remove: (id: string) => api.del<void>(`/files/${id}`),
  download: (id: string) => api.download(`/files/${id}/download`),
}

/* -------------------------------------------------------------- 审计日志 */
export const logApi = {
  logins: (query: ListQuery) => api.get<PageResult<LoginLogRow>>('/logs/login', query),
  clearLogins: (days: number) => api.del<{ deleted: number }>(`/logs/login?days=${days}`),

  operations: (query: ListQuery) =>
    api.get<PageResult<OperationLogRow>>('/logs/operation', query),
  operation: (id: string) => api.get<OperationLogRow>(`/logs/operation/${id}`),
  clearOperations: (days: number) =>
    api.del<{ deleted: number }>(`/logs/operation?days=${days}`),

  online: (query: ListQuery) =>
    api.get<PageResult<OnlineSessionRow>>('/online/sessions', query),
  kick: (id: string) => api.del<void>(`/online/sessions/${id}`),
}

/* -------------------------------------------------------------- 路由目录 */
export const routeApi = {
  list: (query: ListQuery & { method?: string; group?: string }) =>
    api.get<RoutePage>('/routes', query),
}

/* -------------------------------------------------------------- 缓存管理 */
export const cacheApi = {
  overview: () => api.get<CacheOverview>('/cache/overview'),
  keys: (query: ListQuery & { namespace?: string }) => api.get<CacheKeyPage>('/cache/keys', query),
  remove: (keys: string[]) => api.del<{ deleted: number }>('/cache/keys', { keys }),
  flush: (namespace: string) =>
    api.post<{ deleted: number }>('/cache/flush', { namespace, confirm: true }),
}

/* ------------------------------------------------------------------ 监控 */
export const monitorApi = {
  system: () => api.get<SystemInfo>('/monitor/system'),
  routes: () => api.get<RouteInfo[]>('/monitor/routes'),
  restart: () => api.post<void>('/monitor/restart'),
}

/* -------------------------------------------------------------- 配置中心 */
export const configApi = {
  items: () => api.get<ConfigListResult>('/ops/config/items'),
  save: (items: { key: string; value: string }[]) =>
    api.put<ConfigApplyResult>('/ops/config/items', { items }),
  reload: () => api.post<ConfigApplyResult>('/ops/config/reload'),
}

/* ---------------------------------------------------------------- 集群 */
export const clusterApi = {
  nodes: () => api.get<ClusterNodesResult>('/ops/cluster/nodes'),
  dispatch: (payload: { action: 'restart' | 'reload-config'; scope: 'all' | 'selected'; nodeIds?: string[] }) =>
    api.post<ClusterCommand>('/ops/cluster/dispatch', payload),
  commands: () => api.get<ClusterCommandsResult>('/ops/cluster/commands'),
}
