/** 时间、字节、时长等展示格式化（语言与时区跟随 i18n 偏好） */
import { translate } from '@/i18n'

/** 当前用户时区；空值或非法值回退浏览器本地时区 */
function zone(): string | undefined {
  try {
    const tz = (window as unknown as { __keelTz?: string }).__keelTz
    if (!tz) return undefined
    new Intl.DateTimeFormat('en-US', { timeZone: tz }).format(new Date())
    return tz
  } catch {
    return undefined
  }
}

/** 由偏好模块设置展示时区，格式化工具不直接依赖 store，避免循环引用 */
export function setDisplayTimezone(tz?: string) {
  ;(window as unknown as { __keelTz?: string }).__keelTz = tz || undefined
}

function parts(d: Date) {
  const tz = zone()
  const fmt = new Intl.DateTimeFormat('en-CA', {
    timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  })
  const map: Record<string, string> = {}
  for (const p of fmt.formatToParts(d)) map[p.type] = p.value
  // en-CA 的 24 小时制在部分环境会输出 24:00:00，归零为 00
  const hour = map.hour === '24' ? '00' : map.hour
  return { date: `${map.year}-${map.month}-${map.day}`, time: `${hour}:${map.minute}:${map.second}` }
}

export function fmtTime(value?: string | Date | null, withSeconds = true): string {
  if (!value) return '-'
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return String(value)
  const { date, time } = parts(d)
  return `${date} ${withSeconds ? time : time.slice(0, 5)}`
}

export function fmtDate(value?: string | Date | null): string {
  if (!value) return '-'
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return String(value)
  return parts(d).date
}

/** 相对时间：3 分钟前 / 昨天 / 2026-10-01 */
export function fromNow(value?: string | Date | null): string {
  if (!value) return '-'
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return String(value)
  const diff = Date.now() - d.getTime()
  // 未来时间（如任务的下次执行）需要正向表达，否则一律落到「刚刚」
  if (diff < 0) {
    const mins = Math.ceil(-diff / 60000)
    if (mins < 60) return translate('{n} 分钟后', { n: mins })
    const hours = Math.floor(mins / 60)
    if (hours < 24) return translate('{n} 小时后', { n: hours })
    return translate('{n} 天后', { n: Math.floor(hours / 24) })
  }
  const min = Math.floor(diff / 60000)
  if (min < 1) return translate('刚刚')
  if (min < 60) return translate('{n} 分钟前', { n: min })
  const hour = Math.floor(min / 60)
  if (hour < 24) return translate('{n} 小时前', { n: hour })
  const day = Math.floor(hour / 24)
  if (day === 1) return translate('昨天')
  if (day < 30) return translate('{n} 天前', { n: day })
  return fmtDate(d)
}

export function fmtBytes(n?: number | null): string {
  if (n === undefined || n === null) return '-'
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = n
  let i = -1
  do {
    value /= 1024
    i += 1
  } while (value >= 1024 && i < units.length - 1)
  return `${value.toFixed(1)} ${units[i]}`
}

export function fmtDuration(seconds?: number | null): string {
  if (!seconds && seconds !== 0) return '-'
  if (seconds < 60) return `${Math.round(seconds)} ${translate('秒')}`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} ${translate('分钟')}`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} ${translate('小时')}`
  return `${Math.floor(seconds / 86400)} ${translate('天')}`
}

/** 触发浏览器下载 */
export function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
