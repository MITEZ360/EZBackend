import { api } from './client'
import type {
  ListQuery,
  MessageDetail,
  MessageItem,
  MessageSentRow,
  PageResult,
} from './types'

export interface InboxQuery extends ListQuery {
  /** '' 全部 / unread 未读 / read 已读 */
  status?: '' | 'unread' | 'read'
  type?: string
}

export interface SendMessagePayload {
  title: string
  content: string
  type: string
  link?: string
  audience: 'all' | 'role' | 'user'
  roleIds?: string[]
  userIds?: string[]
}

export interface SentQuery extends ListQuery {
  status?: '' | 'sent' | 'revoked'
  type?: string
}

export const messageApi = {
  inbox: (query: InboxQuery) => api.get<PageResult<MessageItem>>('/messages', query),

  /** 轮询接口静默失败，避免断网时反复弹提示 */
  unreadCount: () => api.get<{ count: number }>('/messages/unread-count', undefined, { silent: true }),

  recent: (limit = 5) =>
    api.get<MessageItem[]>('/messages/recent', { limit }, { silent: true }),

  detail: (id: string) => api.get<MessageDetail>(`/messages/${id}`),

  markRead: (id: string) => api.put<void>(`/messages/${id}/read`, undefined, { silent: true }),

  markAllRead: (type?: string) =>
    api.put<{ updated: number }>('/messages/read-all', undefined, { params: type ? { type } : undefined }),

  remove: (id: string) => api.del<void>(`/messages/${id}`),

  batchRemove: (ids: string[]) => api.post<{ deleted: number }>('/messages/batch-delete', { ids }),

  clearRead: () => api.del<{ deleted: number }>('/messages/read'),

  sent: (query: SentQuery) => api.get<PageResult<MessageSentRow>>('/messages/sent', query),

  send: (payload: SendMessagePayload) =>
    api.post<{ id: string; recipients: number }>('/messages/send', payload),

  revoke: (id: string) => api.del<void>(`/messages/sent/${id}`),
}
