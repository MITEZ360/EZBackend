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

  /** 语言/时区偏好，与资料更新分离，支持只提交其一 */
  updatePreferences: (payload: { locale?: string; timezone?: string }) =>
    api.put<void>('/auth/preferences', payload),

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

/* ---------------------------------------------------------------- 第三方登录 */

/** 后端按配置下发的可用渠道，不含任何凭据 */
export interface OAuthProvider {
  name: string
  label: string
  icon?: string
}

/** 我的第三方账号绑定 */
export interface OAuthBinding {
  id: string
  provider: string
  label: string
  username: string
  email: string
  avatar: string
  createdAt: string
  lastLoginAt?: string
}

export const oauthApi = {
  /** 登录页拉取可用渠道 */
  providers: () =>
    api.get<{ enabled: boolean; providers: OAuthProvider[] }>('/oauth/providers'),

  /** 发起登录跳转，返回第三方授权地址 */
  authorize: (name: string) =>
    api.get<{ url: string; state: string }>(`/oauth/authorize/${encodeURIComponent(name)}`),

  /** 已登录用户发起账号绑定 */
  bind: (name: string) =>
    api.post<{ url: string; state: string }>(`/oauth/bind/${encodeURIComponent(name)}`),

  bindings: () => api.get<OAuthBinding[]>('/oauth/bindings'),

  unbind: (name: string) =>
    api.del<void>(`/oauth/bindings/${encodeURIComponent(name)}`),
}
