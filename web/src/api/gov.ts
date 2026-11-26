import { api } from './client'

/**
 * 服务治理接口。
 *
 * 服务目录、动态路由、配置中心三块共用同一个全局版本号：发布一次就自增一次，
 * 数据面按这个号决定是否需要重新拉快照。页面不直接消费它，只在展示层显示
 * 当前生效版本，便于判断实例是否跟上。
 */

/** 服务目录中的一项，附带实例统计 */
export interface GovServiceView {
  id: string
  name: string
  owner: string
  group: string
  desc: string
  meta?: string
  status: string
  checkType: string
  checkPath: string
  checkInterval: number
  checkTimeout: number
  checkFailThreshold: number
  /** 实例总数 */
  instances: number
  /** 在线实例数 */
  online: number
  /** 出现过的版本数 */
  versions: number
  createdAt: string
  updatedAt: string
}

export interface GovServicePayload {
  name: string
  owner: string
  group: string
  desc: string
  meta?: string
  status: string
  checkType: string
  checkPath: string
  checkInterval: number
  checkTimeout: number
  checkFailThreshold: number
}

export interface GovInstanceView {
  id: string
  serviceId: string
  /** 实例自报的稳定标识，默认 host:port */
  instanceId: string
  name: string
  host: string
  port: number
  version: string
  zone: string
  weight: number
  labels?: string
  status: 'online' | 'draining' | 'offline' | string
  configVersion: number
  lastSeen: string
  startedAt: string
}

export interface GovVersionStat {
  service: string
  version: string
  count: number
}

export interface GovRouteView {
  id: string
  name: string
  priority: number
  prefix: string
  /** 逗号分隔的方法白名单，空表示不限 */
  methods?: string
  /** JSON 对象字符串 */
  headers?: string
  query?: string
  service: string
  version?: string
  weight: number
  strip: boolean
  timeoutMs: number
  retries: number
  status: 'draft' | 'published' | 'archived' | string
  createdAt: string
  updatedAt: string
}

export interface GovRoutePayload {
  name: string
  priority: number
  prefix: string
  methods: string[]
  headers: Record<string, string>
  query: Record<string, string>
  service: string
  version: string
  weight: number
  strip: boolean
  timeoutMs: number
  retries: number
  status: string
}

export interface GovReleaseView {
  id: string
  domain: string
  revision: number
  checksum: string
  /** 灰度目标实例 ID 的 JSON 数组，空数组表示全量 */
  targets?: string
  stage: 'canary' | 'full' | string
  status: 'active' | 'done' | 'rolled_back' | string
  operator?: string
  note?: string
  createdAt: string
}

export interface GovNamespace {
  id: string
  namespace: string
  env: string
  desc: string
  createdAt: string
  updatedAt: string
}

export interface GovConfigItem {
  id: string
  namespace: string
  key: string
  /** 加密项回显为掩码，保存时回传掩码表示不改动 */
  value: string
  type: string
  encrypted: boolean
  scope: string
  service: string
  updatedBy: string
}

export interface GovConfigItemInput {
  key: string
  value: string
  type: string
  encrypted: boolean
  scope: string
  service: string
}

/** 编译后的规则：与规则表同构，但方法/条件已解析成结构化形态 */
export interface GovCompiledRoute {
  id: string
  name: string
  priority: number
  prefix: string
  methods?: string[]
  headers?: Record<string, string>
  query?: Record<string, string>
  service: string
  version?: string
  weight: number
  strip: boolean
  timeoutMs: number
  retries: number
}

export const registryApi = {
  services: () =>
    api.get<{ list: GovServiceView[] }>('/gov/services').then((r) => r.list ?? []),

  createService: (payload: GovServicePayload) => api.post<GovServiceView>('/gov/services', payload),

  updateService: (id: string, payload: GovServicePayload) =>
    api.put<GovServiceView>(`/gov/services/${id}`, payload),

  removeService: (id: string) => api.del<{ deleted: boolean }>(`/gov/services/${id}`),

  instances: (query: { service?: string; status?: string } = {}) =>
    api.get<{ list: GovInstanceView[] }>('/gov/instances', query).then((r) => r.list ?? []),

  setInstanceStatus: (id: string, status: 'online' | 'draining' | 'offline') =>
    api.put<{ status: string }>(`/gov/instances/${id}/status`, { status }),

  versions: () =>
    api.get<{ list: GovVersionStat[] }>('/gov/versions').then((r) => r.list ?? []),
}

export const routeApi = {
  list: () => api.get<{ list: GovRouteView[] }>('/gov/routes').then((r) => r.list ?? []),

  create: (payload: GovRoutePayload) => api.post<GovRouteView>('/gov/routes', payload),

  update: (id: string, payload: GovRoutePayload) =>
    api.put<GovRouteView>(`/gov/routes/${id}`, payload),

  remove: (id: string) => api.del<unknown>(`/gov/routes/${id}`),

  /** 只校验不落库，返回编译后的匹配顺序 */
  compile: (routes: GovRoutePayload[]) =>
    api.post<{ routes: GovCompiledRoute[]; count: number }>('/gov/routes/compile', routes),

  publish: (payload: { targets: string[]; note: string }) =>
    api.post<GovReleaseView>('/gov/routes/publish', payload),

  releases: () => api.get<{ list: GovReleaseView[] }>('/gov/releases').then((r) => r.list ?? []),

  promote: (id: string) => api.post<GovReleaseView>(`/gov/releases/${id}/promote`),

  rollback: (id: string) => api.post<GovReleaseView>(`/gov/releases/${id}/rollback`),
}

export const configApi = {
  namespaces: () =>
    api.get<{ list: GovNamespace[] }>('/gov/config/namespaces').then((r) => r.list ?? []),

  createNamespace: (payload: { namespace: string; env: string; desc: string }) =>
    api.post<GovNamespace>('/gov/config/namespaces', payload),

  removeNamespace: (id: string) => api.del<unknown>(`/gov/config/namespaces/${id}`),

  items: (namespace: string) =>
    api.get<{ list: GovConfigItem[] }>('/gov/config/items', { namespace }).then((r) => r.list ?? []),

  saveItems: (namespace: string, items: GovConfigItemInput[]) =>
    api.post<{ saved: number }>('/gov/config/items', { namespace, items }),

  removeItem: (id: string) => api.del<unknown>(`/gov/config/items/${id}`),

  publish: (payload: { namespace: string; note: string }) =>
    api.post<GovReleaseView>('/gov/config/publish', payload),

  releases: (namespace: string) =>
    api
      .get<{ list: GovReleaseView[] }>('/gov/config/releases', { namespace })
      .then((r) => r.list ?? []),

  rollback: (id: string) => api.post<GovReleaseView>(`/gov/config/releases/${id}/rollback`),
}
