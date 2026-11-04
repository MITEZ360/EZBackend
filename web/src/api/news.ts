import { api } from './client'
import type { NewsArticle, NewsCategory, NewsSource, NewsStats, PageResult } from './types'

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
  icon?: string
  color?: string
  sort?: number
  homepage?: string
  status?: number
  remark?: string
}

export interface CategoryPayload {
  code: string
  name: string
  icon?: string
  color?: string
  sort?: number
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
  list: (query: { page?: number; page_size?: number; keyword?: string; status?: number }) =>
    api.get<PageResult<NewsSource>>('/news-sources', query),
  create: (payload: SourcePayload) => api.post<NewsSource>('/news-sources', payload),
  update: (id: string, payload: SourcePayload) =>
    api.put<NewsSource>(`/news-sources/${id}`, payload),
  remove: (id: string) => api.del<void>(`/news-sources/${id}`),
}

export const newsCategoryApi = {
  /** 分类数量有限，接口一次返回全量，不分页 */
  list: () => api.get<NewsCategory[]>('/news-categories'),
  create: (payload: CategoryPayload) => api.post<NewsCategory>('/news-categories', payload),
  update: (id: string, payload: CategoryPayload) =>
    api.put<NewsCategory>(`/news-categories/${id}`, payload),
  remove: (id: string) => api.del<void>(`/news-categories/${id}`),
}
