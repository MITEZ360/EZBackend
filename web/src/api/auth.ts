import { api } from './client'
import type { MenuNode, TokenPair, UserInfo } from './types'

export interface LoginPayload {
  username: string
  password: string
  captchaId?: string
  captchaCode?: string
  totpCode?: string
}

export interface LoginResult {
  token: TokenPair
  user: UserInfo
}

export interface CaptchaResult {
  enabled: boolean
  captchaId: string
  image: string
}

export interface ProfileResult {
  user: UserInfo
  menus: MenuNode[]
}

export const authApi = {
  login: (payload: LoginPayload) => api.post<LoginResult>('/auth/login', payload, { silent: true }),

  logout: () => api.post<void>('/auth/logout'),

  profile: () => api.get<ProfileResult>('/auth/profile'),

  captcha: () => api.get<CaptchaResult>('/auth/captcha'),

  /** 登录页展示的系统名称等公开配置 */
  publicConfig: () => api.get<Record<string, string>>('/public/config'),

  updateProfile: (payload: Partial<UserInfo>) => api.put<void>('/auth/profile', payload),

  changePassword: (payload: { oldPassword: string; newPassword: string }) =>
    api.put<void>('/auth/password', payload),
}

export interface SessionRow {
  id: string
  userAgent: string
  browser: string
  os: string
  ip: string
  createdAt: string
  lastActiveAt: string
  current?: boolean
}

export const sessionApi = {
  list: () => api.get<SessionRow[]>('/auth/sessions'),
  revoke: (id: string) => api.del<void>(`/auth/sessions/${id}`),
}
