import { api } from './client'
import type { APIKeyRow, PageResult } from './types'

export interface APIKeyPayload {
  name: string
  kind: 'service' | 'client'
  scopes: string[]
  ipAllow: string[]
  rateRps: number
  rateBurst: number
  dailyQuota: number
  expireAt?: string | null
  remark?: string
  status?: number
}

/** 签发与轮换的结果：明文只出现一次，关闭弹窗后无法再取回 */
export interface IssuedKey {
  key: APIKeyRow
  plain: string
}

export const apikeyApi = {
  list: (query: Record<string, unknown>) =>
    api.get<PageResult<APIKeyRow>>('/api-keys', query),

  /** 可作为 scope 的权限点清单 */
  scopes: () => api.get<string[]>('/api-keys/scopes'),

  create: (payload: APIKeyPayload) => api.post<IssuedKey>('/api-keys', payload),

  update: (id: string, payload: APIKeyPayload) =>
    api.put<APIKeyRow>(`/api-keys/${id}`, payload),

  rotate: (id: string) => api.post<IssuedKey>(`/api-keys/${id}/rotate`),

  setStatus: (id: string, status: number) =>
    api.patch<void>(`/api-keys/${id}/status`, { status }),

  remove: (id: string) => api.del<void>(`/api-keys/${id}`),
}
