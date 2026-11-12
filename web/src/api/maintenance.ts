import { api } from './client'

export interface BackupFile {
  name: string
  driver: string
  createdAt: string
  size: number
  compressed: boolean
  sha256: string
  verified: boolean
}

export interface BackupMeta {
  name: string
  driver: string
  createdAt: string
  size: number
  compressed: boolean
  sha256: string
  tables?: string[]
  userCount?: number
}

export interface VerifyResult {
  name: string
  ok: boolean
  driver: string
  size: number
  sha256: string
  createdAt?: string
  metaMatched: boolean
  integrity?: string
  tables?: string[]
  userCount?: number
}

export interface RestoreResult {
  name: string
  mode: string
  restartRequired: boolean
  safetyCopy?: string
  message: string
}

export interface MigrationVersion {
  version: number
  name: string
  applied: boolean
  appliedAt?: string
}

export interface MigrationStatus {
  driver: string
  table: string
  current: number
  latest: number
  pending: number
  versions: MigrationVersion[]
}

export interface MigrationResult {
  mode: string
  enabled: boolean
  message?: string
  status?: MigrationStatus
}

export const maintenanceApi = {
  backups: () => api.get<BackupFile[]>('/maintenance/backups'),
  createBackup: () => api.post<BackupMeta>('/maintenance/backups', {}),
  verify: (name: string) =>
    api.post<VerifyResult>(`/maintenance/backups/${encodeURIComponent(name)}/verify`, {}),
  restore: (name: string) =>
    api.post<RestoreResult>(`/maintenance/backups/${encodeURIComponent(name)}/restore`, {}),
  remove: (name: string) =>
    api.del<void>(`/maintenance/backups/${encodeURIComponent(name)}`),
  migrations: () => api.get<MigrationResult>('/maintenance/migrations'),
  migrateUp: () => api.post<{ applied: unknown[] }>('/maintenance/migrations/up', {}),
  migrateDown: () => api.post<{ applied: unknown[] }>('/maintenance/migrations/down', {}),
}

/** 下载备份：走统一请求实例，自动带令牌与错误处理 */
export async function downloadBackup(name: string) {
  const blob = await api.download(`/maintenance/backups/${encodeURIComponent(name)}/download`)
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
