import { api } from './client'
import type { ListQuery, PageResult, RoleBrief, UserInfo, UserRow } from './types'

export interface CreateUserPayload {
  username: string
  password: string
  nickname: string
  realName?: string
  email?: string
  phone?: string
  deptId?: string
  roleIds?: string[]
  gender?: number
  status?: number
  remark?: string
}

export interface UpdateUserPayload {
  nickname: string
  realName?: string
  email?: string
  phone?: string
  deptId?: string
  roleIds?: string[]
  gender?: number
  remark?: string
}

export interface ImportFailure {
  row: number
  username?: string
  message: string
}

export interface ImportResult {
  total: number
  success: number
  failed: number
  truncated: boolean
  errors: ImportFailure[]
}

export const userApi = {
  list: (query: ListQuery) => api.get<PageResult<UserRow>>('/users', query),

  get: (id: string) => api.get<UserInfo & { roles?: RoleBrief[] }>(`/users/${id}`),

  create: (payload: CreateUserPayload) => api.post<{ id: string }>('/users', payload),

  update: (id: string, payload: UpdateUserPayload) => api.put<void>(`/users/${id}`, payload),

  remove: (id: string) => api.del<void>(`/users/${id}`),

  batchDelete: (ids: string[]) =>
    api.post<{ deleted: number; skipped: string[] }>('/users/batch-delete', { ids }),

  setStatus: (id: string, status: number) => api.put<void>(`/users/${id}/status`, { status }),

  resetPassword: (id: string, password: string) =>
    api.put<void>(`/users/${id}/password`, { password }),

  setRoles: (id: string, roleIds: string[]) => api.put<void>(`/users/${id}/roles`, { roleIds }),

  options: () => api.get<{ id: string; username: string; nickname: string }[]>('/options/users'),

  exportExcel: (query: ListQuery) => api.download('/exports/users', query),

  importTemplate: () => api.download('/imports/users/template'),

  importExcel: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return api.upload<ImportResult>('/imports/users', form)
  },
}
