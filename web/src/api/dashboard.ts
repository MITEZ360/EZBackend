import { api } from './client'

export interface DashboardStats {
  userCount: number
  activeUsers: number
  roleCount: number
  deptCount: number
  fileCount: number
  todayLogins: number
  todayOps: number
  onlineCount: number
  loginFailures: number
}

export interface TrendPoint {
  date: string
  logins: number
  ops: number
}

export interface RecentLogin {
  id: string
  username: string
  ip: string
  browser: string
  os: string
  status: number
  message: string
  createdAt: string
}

export interface NoticeItem {
  id: string
  title: string
  content: string
  type: string
  publishAt?: string
  createdAt: string
}

export interface DeptDist {
  name: string
  value: number
}

export interface Overview {
  stats: DashboardStats
  trend: TrendPoint[]
  recent: RecentLogin[]
  notices: NoticeItem[]
  userDeptDist: DeptDist[]
}

export const dashboardApi = {
  overview: () => api.get<Overview>('/dashboard/overview'),
}
