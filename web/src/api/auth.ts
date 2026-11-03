import { api } from './client'
import type { MenuNode, TokenPair, UserInfo } from './types'

export interface LoginPayload {
  username: string
  password: string
  captchaId?: string
  captchaCode?: string
  totpCode?: string
  recoveryCode?: string
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

/* ---------------------------------------------------------------- 两步验证 */

export interface TOTPSetupResult {
  /** Base32 密钥，用户无法扫码时手动录入 */
  secret: string
  /** otpauth:// 链接 */
  url: string
  /** PNG data URI，直接用于 <img> */
  qrCode: string
}

export const totpApi = {
  /** 生成密钥与二维码（未启用状态） */
  setup: () => api.post<TOTPSetupResult>('/auth/totp/setup'),

  /** 提交动态码完成启用，返回一次性恢复码 */
  enable: (code: string) =>
    api.post<{ message: string; recoveryCodes: string[] }>('/auth/totp/enable', { code }),

  /** 关闭两步验证，code 可为动态码或恢复码 */
  disable: (code: string) => api.post<{ message: string }>('/auth/totp/disable', { code }),

  /** 重新生成恢复码，旧码立即失效 */
  regenerate: (code: string) =>
    api.post<{ recoveryCodes: string[] }>('/auth/totp/recovery', { code }),
}
