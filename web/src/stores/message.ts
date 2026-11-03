import { ref } from 'vue'
import { defineStore } from 'pinia'
import { messageApi } from '@/api/message'
import type { MessageItem } from '@/api/types'

/**
 * 站内消息状态：未读数与最近消息由顶部铃铛与消息中心共享。
 * 轮询间隔 60s，页面切到后台时不暂停——内网后台通常常驻一个标签页，
 * 保持简单比省一次请求更重要。
 */
export const useMessageStore = defineStore('message', () => {
  const unread = ref(0)
  const recent = ref<MessageItem[]>([])
  const loading = ref(false)
  let timer: number | undefined

  async function refresh() {
    loading.value = true
    try {
      const [count, list] = await Promise.all([messageApi.unreadCount(), messageApi.recent(5)])
      unread.value = Number(count?.count ?? 0)
      recent.value = list ?? []
    } catch {
      // 未登录、断网或后端重启时保持上一次的数值，不打断页面
    } finally {
      loading.value = false
    }
  }

  function start(intervalMs = 60000) {
    stop()
    void refresh()
    timer = window.setInterval(() => void refresh(), intervalMs)
  }

  function stop() {
    if (timer !== undefined) {
      window.clearInterval(timer)
      timer = undefined
    }
  }

  function reset() {
    stop()
    unread.value = 0
    recent.value = []
  }

  return { unread, recent, loading, refresh, start, stop, reset }
})
