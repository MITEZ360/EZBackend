import { api } from './client'
import type {
  ConfigRow,
  DeptNode,
  DictItem,
  DictTypeRow,
  ListQuery,
  MenuNode,
  NoticeRow,
  PageResult,
  RoleRow,
} from './types'

/* ------------------------------------------------------------------ 角色 */
export interface RolePayload {
  code: string
  name: string
  description?: string
  dataScope: number
  sort: number
  status: number
}

export const roleApi = {
  list: (query: ListQuery) => api.get<PageResult<RoleRow>>('/roles', query),
  get: (id: string) => api.get<RoleRow>(`/roles/${id}`),
  create: (payload: RolePayload) => api.post<RoleRow>('/roles', payload),
  update: (id: string, payload: RolePayload) => api.put<void>(`/roles/${id}`, payload),
  remove: (id: string) => api.del<void>(`/roles/${id}`),
  /** 角色已勾选的菜单 ID */
  menus: (id: string) => api.get<string[]>(`/roles/${id}/menus`),
  setMenus: (id: string, menuIds: string[]) => api.put<void>(`/roles/${id}/menus`, { menuIds }),
  options: () => api.get<RoleRow[]>('/options/roles'),
}

/* ------------------------------------------------------------------ 菜单 */
export interface MenuPayload {
  parentId: string
  name: string
  type: 'dir' | 'menu' | 'button'
  path?: string
  component?: string
  icon?: string
  perm?: string
  sort: number
  visible?: boolean
  keepAlive?: boolean
  status?: number
}

export const menuApi = {
  list: () => api.get<MenuNode[]>('/menus'),
  tree: () => api.get<MenuNode[]>('/menus/tree'),
  create: (payload: MenuPayload) => api.post<MenuNode>('/menus', payload),
  update: (id: string, payload: MenuPayload) => api.put<void>(`/menus/${id}`, payload),
  remove: (id: string) => api.del<void>(`/menus/${id}`),
}

/* ------------------------------------------------------------------ 部门 */
export interface DeptPayload {
  parentId: string
  name: string
  leader?: string
  phone?: string
  email?: string
  sort: number
  status: number
}

export const deptApi = {
  tree: () => api.get<DeptNode[]>('/depts/tree'),
  create: (payload: DeptPayload) => api.post<DeptNode>('/depts', payload),
  update: (id: string, payload: DeptPayload) => api.put<void>(`/depts/${id}`, payload),
  remove: (id: string) => api.del<void>(`/depts/${id}`),
  /** 下拉用，结构同树 */
  options: () => api.get<DeptNode[]>('/options/depts'),
}

/* ------------------------------------------------------------------ 字典 */
export interface DictTypePayload {
  code: string
  name: string
  remark?: string
  status: number
}

export interface DictItemPayload {
  typeCode: string
  label: string
  value: string
  tagColor?: string
  sort: number
  status: number
  remark?: string
}

export const dictApi = {
  types: (query: ListQuery) => api.get<PageResult<DictTypeRow>>('/dicts/types', query),
  createType: (payload: DictTypePayload) => api.post<DictTypeRow>('/dicts/types', payload),
  updateType: (id: string, payload: DictTypePayload) => api.put<void>(`/dicts/types/${id}`, payload),
  removeType: (id: string) => api.del<void>(`/dicts/types/${id}`),
  items: (code: string) => api.get<DictItem[]>(`/dicts/types/${code}/items`),
  createItem: (payload: DictItemPayload) => api.post<DictItem>('/dicts/items', payload),
  updateItem: (id: string, payload: DictItemPayload) => api.put<void>(`/dicts/items/${id}`, payload),
  removeItem: (id: string) => api.del<void>(`/dicts/items/${id}`),
  /** 前端下拉数据（公开接口，带缓存） */
  data: (code: string) => api.get<DictItem[]>(`/public/dicts/${code}`),
}

/* ------------------------------------------------------------------ 参数 */
export interface ConfigPayload {
  key: string
  value: string
  name: string
  group: string
  encrypted: boolean
  public: boolean
  remark?: string
}

export const configApi = {
  list: (query: ListQuery) => api.get<PageResult<ConfigRow>>('/configs', query),
  create: (payload: ConfigPayload) => api.post<ConfigRow>('/configs', payload),
  update: (id: string, payload: ConfigPayload) => api.put<void>(`/configs/${id}`, payload),
  remove: (id: string) => api.del<void>(`/configs/${id}`),
}

/* ------------------------------------------------------------------ 公告 */
export interface NoticePayload {
  title: string
  content: string
  type: string
  status: number
}

export const noticeApi = {
  list: (query: ListQuery) => api.get<PageResult<NoticeRow>>('/notices', query),
  get: (id: string) => api.get<NoticeRow>(`/notices/${id}`),
  create: (payload: NoticePayload) => api.post<NoticeRow>('/notices', payload),
  update: (id: string, payload: NoticePayload) => api.put<void>(`/notices/${id}`, payload),
  remove: (id: string) => api.del<void>(`/notices/${id}`),
}
