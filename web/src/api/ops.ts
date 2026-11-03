import { api } from './client'
import type {
  FileRow,
  JobLogRow,
  JobRow,
  ListQuery,
  LoginLogRow,
  OnlineSessionRow,
  OperationLogRow,
  PageResult,
  RouteInfo,
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

/* ------------------------------------------------------------------ 监控 */
export const monitorApi = {
  system: () => api.get<SystemInfo>('/monitor/system'),
  routes: () => api.get<RouteInfo[]>('/monitor/routes'),
}
