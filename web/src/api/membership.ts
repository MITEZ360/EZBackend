import { api } from './client'
import type {
  ListQuery,
  MembershipBenefit,
  MembershipLevel,
  MembershipSnapshot,
  PageResult,
} from './types'

export interface MembershipLevelPayload {
  code: string
  name: string
  rank: number
  status: number
  description?: string
  defaultDays: number
  color?: string
  benefits: MembershipBenefit[]
}

export interface GrantMembershipPayload {
  levelId: string
  /** 有效天数；0 时回落到等级默认时长 */
  days?: number
  /** 长期有效，优先于 days */
  permanent?: boolean
  /** 指定到期日（YYYY-MM-DD），优先于 days */
  expireAt?: string
}

export const membershipApi = {
  list: (query: ListQuery) => api.get<PageResult<MembershipLevel>>('/membership/levels', query),

  /** 启用中的等级目录，个人中心用于展示与对比 */
  catalog: () => api.get<MembershipLevel[]>('/membership/catalog'),

  create: (payload: MembershipLevelPayload) =>
    api.post<MembershipLevel>('/membership/levels', payload),

  update: (id: string, payload: MembershipLevelPayload) =>
    api.put<void>(`/membership/levels/${id}`, payload),

  remove: (id: string) => api.del<void>(`/membership/levels/${id}`),

  /** 当前用户的会员状态、权益与等级目录 */
  me: () => api.get<MembershipSnapshot>('/membership/me'),

  /** 管理员给用户开通 / 续费会员 */
  grant: (userId: string, payload: GrantMembershipPayload) =>
    api.put<MembershipSnapshot>(`/users/${userId}/membership`, payload),

  /** 管理员取消用户会员 */
  revoke: (userId: string) => api.del<void>(`/users/${userId}/membership`),
}
