import { api } from './client'
import type { TokenPair, UserInfo } from './types'

/** CN 分支公开开关 */
export interface CNPublicConfig {
  registerEnabled: boolean
  smsLoginEnabled: boolean
}

export interface RegisterPayload {
  username: string
  password: string
  nickname?: string
  phone: string
  code: string
  email?: string
}

export interface PhoneLoginPayload {
  phone: string
  code: string
}

export interface SMSCodePayload {
  phone: string
  scene: 'register' | 'login' | 'reset'
}

export interface LoginResult {
  token: TokenPair
  user: UserInfo
}

/** 充值订单。金额单位为分。 */
export interface RechargeOrder {
  id: string
  orderNo: string
  userId: string
  levelId: string
  levelName?: string
  days: number
  amount: number
  currency: string
  payType: 'wechat' | 'alipay'
  status: 'pending' | 'paid' | 'closed' | 'refunded'
  tradeNo?: string
  paidAt?: string
  createdAt: string
  payUrl?: string
  qrCode?: string
}

export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  pageSize: number
}

/** 管理端支付配置。敏感字段读回为 ******，原样提交表示不修改。 */
export interface PaymentConfigView {
  enabled: boolean
  wechat: {
    enabled: boolean
    appId: string
    mchId: string
    apiKey: string
    notifyUrl: string
    signType: string
    sandbox: boolean
  }
  alipay: {
    enabled: boolean
    appId: string
    privateKey: string
    publicKey: string
    notifyUrl: string
    returnUrl: string
    sandbox: boolean
  }
  source?: 'file' | 'database'
}

export const cnApi = {
  publicConfig: () => api.get<CNPublicConfig>('/public/cn-config'),
  sendCode: (payload: SMSCodePayload) => api.post<void>('/auth/sms/code', payload, { silent: true }),
  register: (payload: RegisterPayload) => api.post<LoginResult>('/auth/register', payload, { silent: true }),
  loginByPhone: (payload: PhoneLoginPayload) => api.post<LoginResult>('/auth/login/phone', payload, { silent: true }),
  resetPassword: (payload: PhoneLoginPayload & { newPassword: string }) =>
    api.post<void>('/auth/password/reset', payload, { silent: true }),

  channels: () => api.get<{ channels: string[] }>('/recharge/channels'),
  createOrder: (payload: { levelId: string; payType: 'wechat' | 'alipay' }) =>
    api.post<RechargeOrder>('/recharge/orders', payload),
  queryOrder: (orderNo: string) => api.get<RechargeOrder>(`/recharge/orders/${encodeURIComponent(orderNo)}`),
  listOrders: (params: Record<string, unknown> = {}) => api.get<PageResult<RechargeOrder>>('/recharge/orders', params),
  closeOrder: (orderNo: string) =>
    api.post<void>(`/recharge/orders/${encodeURIComponent(orderNo)}/close`),
  paymentConfig: () => api.get<PaymentConfigView>('/recharge/admin/payment-config'),
  savePaymentConfig: (payload: PaymentConfigView) =>
    api.put<void>('/recharge/admin/payment-config', payload),
}
