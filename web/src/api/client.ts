import axios, {
  type AxiosError,
  type AxiosRequestConfig,
  type InternalAxiosRequestConfig,
} from 'axios'
import { ElMessage } from 'element-plus'

/** 后端统一响应体 */
export interface Envelope<T = unknown> {
  code: number
  message: string
  data: T
  traceId?: string
}

/** 业务异常：携带业务码与 HTTP 状态，页面可按 code 做差异化处理 */
export class ApiError extends Error {
  code: number
  status: number
  detail?: unknown

  constructor(message: string, code: number, status: number, detail?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
    this.detail = detail
  }
}

declare module 'axios' {
  export interface AxiosRequestConfig {
    /** 为 true 时不弹全局错误提示，交由调用方处理 */
    silent?: boolean
  }
}

interface RetriableConfig extends InternalAxiosRequestConfig {
  _retried?: boolean
  /** 该请求实际携带的令牌，用于判断晚到的 401 是否已经刷新过 */
  _token?: string
  silent?: boolean
}

const ACCESS_KEY = 'keel.access.token'
const REFRESH_KEY = 'keel.refresh.token'

export const tokenStore = {
  access: () => localStorage.getItem(ACCESS_KEY) ?? '',
  refresh: () => localStorage.getItem(REFRESH_KEY) ?? '',
  save(access: string, refresh?: string) {
    localStorage.setItem(ACCESS_KEY, access)
    if (refresh) localStorage.setItem(REFRESH_KEY, refresh)
  },
  clear() {
    localStorage.removeItem(ACCESS_KEY)
    localStorage.removeItem(REFRESH_KEY)
  },
}

/** 会话彻底失效时的回调，由 auth store 注入，避免模块循环依赖 */
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 20000,
  headers: { 'Content-Type': 'application/json' },
})

http.interceptors.request.use((cfg: RetriableConfig) => {
  const token = tokenStore.access()
  if (token) {
    cfg.headers.Authorization = `Bearer ${token}`
    cfg._token = token
  }
  return cfg
})

/** 单飞刷新：并发 401 只发起一次刷新，其余请求排队等待 */
let refreshing: Promise<boolean> | null = null
/** 已经用它发起过刷新的 accessToken，避免刷新完成后晚到的 401 触发二次轮换 */
let refreshedFrom = ''

async function refreshToken(): Promise<boolean> {
  const rt = tokenStore.refresh()
  if (!rt) return false
  try {
    // 用裸 axios，绕开拦截器，避免刷新失败再次触发刷新
    const res = await axios.post<Envelope<{ accessToken: string; refreshToken: string }>>(
      '/api/v1/auth/refresh',
      { refreshToken: rt },
      { timeout: 15000 },
    )
    const data = res.data?.data
    if (!data?.accessToken) return false
    tokenStore.save(data.accessToken, data.refreshToken)
    return true
  } catch {
    return false
  }
}

http.interceptors.response.use(
  (res) => res,
  async (error: AxiosError<Envelope>) => {
    const cfg = error.config as RetriableConfig | undefined
    const status = error.response?.status ?? 0
    const body = error.response?.data

    // 登录接口的 401 是业务失败（密码错误、需要动态码），不是会话过期：
    // 不能走刷新令牌逻辑，否则错误码会被改写成 40100，前端无法区分登录步骤。
    const isLoginCall = (cfg?.url ?? '').includes('/auth/login')

    if (status === 401 && cfg && !cfg._retried && !isLoginCall) {
      cfg._retried = true
      // 用请求发出时的令牌判断；刷新完成后 tokenStore 已是新值，
      // 若在这里读当前令牌会误判为“未刷新过”而触发第二次轮换。
      const seenToken = cfg._token ?? ''
      if (!refreshing && seenToken !== refreshedFrom) {
        refreshedFrom = seenToken
        refreshing = refreshToken().finally(() => {
          refreshing = null
        })
      }
      const ok = refreshing ? await refreshing : true
      if (ok) {
        cfg.headers.Authorization = `Bearer ${tokenStore.access()}`
        return http.request(cfg)
      }
      tokenStore.clear()
      onUnauthorized?.()
      return Promise.reject(new ApiError('登录已过期，请重新登录', 40100, 401))
    }

    const err = new ApiError(
      body?.message || (status ? `请求失败（${status}）` : '网络异常，请检查服务是否可用'),
      body?.code ?? status * 100,
      status,
      body?.data,
    )

    if (!cfg?.silent && status !== 401) {
      ElMessage.error(err.message)
    }
    return Promise.reject(err)
  },
)

/** 统一请求入口：自动解包 data 字段 */
export async function request<T>(cfg: AxiosRequestConfig): Promise<T> {
  const res = await http.request<Envelope<T>>(cfg)
  const body = res.data
  if (body && typeof body.code === 'number' && body.code !== 0) {
    throw new ApiError(body.message, body.code, res.status, body.data)
  }
  return body?.data as T
}

export const api = {
  get: <T>(url: string, params?: Record<string, unknown>, cfg?: AxiosRequestConfig) =>
    request<T>({ ...cfg, url, method: 'GET', params }),
  post: <T>(url: string, data?: unknown, cfg?: AxiosRequestConfig) =>
    request<T>({ ...cfg, url, method: 'POST', data }),
  put: <T>(url: string, data?: unknown, cfg?: AxiosRequestConfig) =>
    request<T>({ ...cfg, url, method: 'PUT', data }),
  patch: <T>(url: string, data?: unknown, cfg?: AxiosRequestConfig) =>
    request<T>({ ...cfg, url, method: 'PATCH', data }),
  del: <T>(url: string, cfg?: AxiosRequestConfig) =>
    request<T>({ ...cfg, url, method: 'DELETE' }),
  /** 文件下载：返回 Blob */
  async download(url: string, params?: Record<string, unknown>): Promise<Blob> {
    const res = await http.request<Blob>({
      url,
      method: 'GET',
      params,
      responseType: 'blob',
    })
    return res.data
  },
  /** 表单上传 */
  upload: <T>(url: string, form: FormData) =>
    request<T>({
      url,
      method: 'POST',
      data: form,
      headers: { 'Content-Type': 'multipart/form-data' },
    }),
}
