import type { Component } from 'vue'
import {
  Avatar,
  Bell,
  ChatDotRound,
  Clock,
  Connection,
  Document,
  Folder,
  Key,
  Menu as MenuIcon,
  Monitor,
  Notebook,
  OfficeBuilding,
  Odometer,
  Operation,
  Setting,
  Trophy,
  TrendCharts,
  User,
  UserFilled,
} from '@element-plus/icons-vue'

/** 后端菜单的 icon 字段是语义字符串，这里映射到实际图标组件 */
export const menuIcons: Record<string, Component> = {
  dashboard: Odometer,
  settings: Setting,
  users: User,
  user: UserFilled,
  shield: Avatar,
  menu: MenuIcon,
  org: OfficeBuilding,
  book: Notebook,
  sliders: Operation,
  trophy: Trophy,
  bell: Bell,
  message: ChatDotRound,
  server: Monitor,
  clock: Clock,
  folder: Folder,
  login: Key,
  'file-text': Document,
  wifi: Connection,
  activity: TrendCharts,
}

export function resolveIcon(name?: string): Component {
  if (!name) return MenuIcon
  return menuIcons[name] ?? MenuIcon
}
