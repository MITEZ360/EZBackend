/** 通用分页结果，与后端 PageData 对齐 */
export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  pageSize: number
  pages: number
}

/** 列表查询参数（后端白名单：page / page_size / sort / order / keyword + 过滤字段） */
export interface ListQuery {
  page?: number
  page_size?: number
  sort?: string
  order?: 'asc' | 'desc'
  keyword?: string
  [key: string]: unknown
}

export interface TokenPair {
  accessToken: string
  refreshToken: string
  tokenType: string
  expiresIn: number
}

export interface UserInfo {
  id: string
  username: string
  nickname: string
  realName?: string
  email?: string
  phone?: string
  gender?: number
  deptId?: string
  deptName?: string
  isSuper: boolean
  totpEnabled?: boolean
  /** 剩余可用恢复码数量（仅个人中心接口返回） */
  recoveryCodesLeft?: number
  roles: string[]
  perms: string[]
  lastLoginAt?: string
  /** 当前会话 ID，用于在在线用户列表中标记自己 */
  sessionId?: string
}

/** 菜单节点：type=dir 目录 / menu 页面 / button 权限点 */
export interface MenuNode {
  id: string
  parentId: string
  name: string
  path?: string
  component?: string
  icon?: string
  type: 'dir' | 'menu' | 'button'
  perm?: string
  sort: number
  visible: boolean
  keepAlive?: boolean
  status?: number
  createdAt?: string
  children?: MenuNode[]
}

export interface RoleBrief {
  id: string
  code: string
  name: string
  dataScope: number
  status: number
}

export interface RoleRow extends RoleBrief {
  createdAt: string
  updatedAt: string
  description?: string
  sort: number
  menuIds?: string[]
  userCount?: number
}

export interface DeptNode {
  id: string
  parentId: string
  name: string
  leader?: string
  phone?: string
  email?: string
  sort: number
  status: number
  userCount?: number
  createdAt?: string
  children?: DeptNode[]
}

export interface UserRow {
  id: string
  createdAt: string
  updatedAt: string
  username: string
  nickname: string
  realName?: string
  email?: string
  phone?: string
  gender?: number
  deptId?: string
  status: number
  isSuper: boolean
  remark?: string
  lastLoginAt?: string
  lastLoginIp?: string
  loginCount?: number
  totpEnabled?: boolean
  roles?: RoleBrief[]
  dept?: { id: string; name: string }
  /* 会员信息 */
  membershipLevelId?: string
  membershipStartAt?: string | null
  membershipExpireAt?: string | null
  membership?: MembershipLevel | null
  membershipState?: MembershipState
}

/* ---------------------------------------------------------------- 会员 */
export type MembershipState = 'none' | 'active' | 'expired' | 'disabled'

export interface MembershipBenefit {
  key: string
  label: string
  value: string
}

export interface MembershipLevel {
  id: string
  code: string
  name: string
  rank: number
  status: number
  description?: string
  /** 开通时的默认天数，0 表示长期有效 */
  defaultDays: number
  /** 徽章标识色 #RRGGBB */
  color?: string
  benefits: MembershipBenefit[]
  /** 当前有效会员数（列表接口返回） */
  userCount?: number
  createdAt?: string
  updatedAt?: string
}

export interface MembershipSnapshot {
  member: boolean
  state: MembershipState
  level?: MembershipLevel | null
  levelId?: string
  startAt?: string | null
  expireAt?: string | null
  /** 剩余天数，-1 表示长期有效 */
  daysLeft: number
  benefits: Record<string, string>
  /** 可对比的等级目录（仅 /membership/me 返回） */
  levels?: MembershipLevel[]
}

/* ------------------------------------------------------------------ 字典 */
export interface DictItem {
  id: string
  typeCode: string
  label: string
  value: string
  tagColor?: string
  sort: number
  status: number
  remark?: string
}

export interface DictTypeRow {
  id: string
  createdAt: string
  code: string
  name: string
  remark?: string
  status: number
  items?: DictItem[]
}

/* ------------------------------------------------------------ 参数与公告 */
export interface ConfigRow {
  id: string
  createdAt: string
  key: string
  value: string
  name: string
  group: string
  encrypted: boolean
  public: boolean
  remark?: string
}

export type NoticeType = 'notice' | 'announcement' | 'warning'

export interface NoticeRow {
  id: string
  createdAt: string
  title: string
  content: string
  type: NoticeType
  status: number
  publisherId?: string
  publishAt?: string
}

/* -------------------------------------------------------------- 定时任务 */
export interface JobRow {
  id: string
  createdAt: string
  name: string
  cron: string
  handler: string
  params?: string
  status: number
  remark?: string
  lastRunAt?: string
  nextRunAt?: string
  runCount: number
  failCount: number
  lastResult?: string
}

export interface JobLogRow {
  id: string
  jobId: string
  jobName: string
  status: number
  output?: string
  costMs: number
  startedAt: string
}

/* ------------------------------------------------------------------ 文件 */
export interface FileRow {
  id: string
  createdAt: string
  name: string
  path: string
  url: string
  mime: string
  ext: string
  size: number
  hash?: string
  uploaderId?: string
  storage: string
}

/* ------------------------------------------------------------------ 审计 */
export interface LoginLogRow {
  id: string
  userId?: string
  username: string
  ip: string
  location?: string
  browser?: string
  os?: string
  status: number
  message?: string
  createdAt: string
}

export interface OperationLogRow {
  id: string
  userId?: string
  username?: string
  module: string
  action: string
  method: string
  path: string
  ip?: string
  request?: string
  response?: string
  status: number
  costMs: number
  traceId?: string
  error?: string
  createdAt: string
}

export interface OnlineSessionRow {
  id: string
  userId: string
  username: string
  nickname: string
  ip: string
  browser: string
  os: string
  lastSeenAt: string
  loginAt: string
}

/* ------------------------------------------------------------------ 监控 */
export interface PoolStats {
  open: number
  in_use: number
  idle: number
  max_open: number
  wait_count: number
  wait_duration: string
  error?: string
}

export interface SystemInfo {
  app: {
    name: string
    version: string
    env: string
    mode: string
    startedAt: string
    uptime: number
  }
  runtime: {
    goVersion: string
    os: string
    arch: string
    cpu: number
    goroutines: number
    memAllocMB: number
    memSysMB: number
    heapMB: number
    gcCount: number
  }
  database: { driver: string; version: string; pool: PoolStats }
  cache: { kind: string }
  ratelimit: { kind: string }
  storage: { kind: string }
  http: { routes: number; port: number }
}

export interface RouteInfo {
  method: string
  path: string
  name?: string
}

/* -------------------------------------------------------------- 站内消息 */
export type MessageType = 'notice' | 'announcement' | 'warning' | 'system'

export interface MessageItem {
  id: string
  messageId: string
  title: string
  excerpt: string
  type: MessageType
  link?: string
  senderName?: string
  readAt?: string
  createdAt: string
}

export interface MessageDetail {
  id: string
  messageId: string
  title: string
  content: string
  type: MessageType
  link?: string
  senderName?: string
  readAt?: string
  createdAt: string
}

export interface MessageSentRow {
  id: string
  createdAt: string
  title: string
  type: MessageType
  audience: 'all' | 'role' | 'user'
  link?: string
  senderName?: string
  status: number
  total: number
  read: number
  refType?: string
}

/** 机器身份：服务端密钥可用于爬虫/服务间调用，客户端标识仅用于识别来源 */
export interface APIKeyRow {
  id: string
  createdAt: string
  updatedAt: string
  name: string
  prefix: string
  kind: 'service' | 'client'
  scopes: string[]
  ipAllow: string[]
  rateRps: number
  rateBurst: number
  dailyQuota: number
  expireAt?: string
  lastUsedAt?: string
  lastUsedIp?: string
  status: number
  remark?: string
  creatorName?: string
  todayUsage: number
}

/** 资讯条目。列表接口不返回 content，详情接口才带正文 */
export interface NewsArticle {
  id: string
  sourceId: string
  sourceCode: string
  sourceName: string
  externalId: string
  title: string
  summary?: string
  content?: string
  url?: string
  author?: string
  category: string
  tags: string[]
  coverUrl?: string
  wordCount: number
  viewCount: number
  publishedAt: string
  ingestedAt: string
  status: number
}

export interface NewsSource {
  id: string
  createdAt: string
  updatedAt: string
  code: string
  name: string
  kind: 'crawler' | 'api' | 'rss'
  icon?: string
  color?: string
  sort: number
  homepage?: string
  status: number
  lastIngestAt?: string
  totalIngested: number
  remark?: string
}

export interface NewsCategory {
  id: string
  createdAt: string
  updatedAt: string
  code: string
  name: string
  icon?: string
  color?: string
  sort: number
  status: number
  remark?: string
  articleCount: number
}

export interface NewsCount {
  name: string
  count: number
}

export interface NewsStats {
  total: number
  today: number
  last24h: number
  categories: NewsCount[]
  sources: NewsCount[]
  retentionDays: number
}
