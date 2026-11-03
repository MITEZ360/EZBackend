import { api } from './client'
import type { NewsArticle, NewsSource, NewsStats, PageResult } from './types'

export interface NewsQuery {
  page?: number
  page_size?: number
  keyword?: string
  category?: string
  sourceId?: string
  status?: number
  from?: string
  to?: string
}

export interface SourcePayload {
  code: string
  name: string
  kind: 'crawler' | 'api' | 'rss'
  homepage?: string
  status?: number
  remark?: string
}

export const newsApi = {
  list: (query: NewsQuery) =>
    api.get<PageResult<NewsArticle>>('/news', query as Record<string, unknown>),
  detail: (id: string) => api.get<NewsArticle>(`/news/${id}`),
  stats: () => api.get<NewsStats>('/news/stats'),
  remove: (id: string) => api.del<void>(`/news/${id}`),
  /** 手动触发过期清理，返回删除条数 */
  cleanup: () => api.post<{ deleted: number }>('/news/cleanup'),
}

export const newsSourceApi = {
  list: (query: { page?: number; page_size?: number; keyword?: string }) =>
    api.get<PageResult<NewsSource>>('/news-sources', query),
  create: (payload: SourcePayload) => api.post<NewsSource>('/news-sources', payload),
  update: (id: string, payload: SourcePayload) =>
    api.put<NewsSource>(`/news-sources/${id}`, payload),
  remove: (id: string) => api.del<void>(`/news-sources/${id}`),
}
