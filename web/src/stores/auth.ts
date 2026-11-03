import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { authApi, type LoginPayload } from '@/api/auth'
import { tokenStore } from '@/api/client'
import type { MenuNode, UserInfo } from '@/api/types'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<UserInfo | null>(null)
  const menus = ref<MenuNode[]>([])
  const perms = ref<Set<string>>(new Set())
  const loaded = ref(false)

  const isSuper = computed(() => Boolean(user.value?.isSuper) || perms.value.has('*'))
  const displayName = computed(() => user.value?.nickname || user.value?.username || '未登录')
  const initial = computed(() => (displayName.value || '?').slice(0, 1).toUpperCase())

  /** 是否具备某个权限点；传数组表示「任一满足」 */
  function can(perm?: string | string[]): boolean {
    if (!perm || (Array.isArray(perm) && perm.length === 0)) return true
    if (isSuper.value) return true
    const list = Array.isArray(perm) ? perm : [perm]
    return list.some((p) => perms.value.has(p))
  }

  function hasRole(role: string): boolean {
    return Boolean(user.value?.roles?.includes(role))
  }

  async function login(payload: LoginPayload) {
    const res = await authApi.login(payload)
    tokenStore.save(res.token.accessToken, res.token.refreshToken)
    user.value = res.user
    await loadProfile()
  }

  /** 拉取用户信息与菜单权限；刷新页面后由路由守卫调用 */
  async function loadProfile() {
    const res = await authApi.profile()
    user.value = res.user
    menus.value = res.menus ?? []
    perms.value = new Set(res.user.perms ?? [])
    loaded.value = true
  }

  async function logout() {
    try {
      await authApi.logout()
    } catch {
      // 会话可能已失效，忽略服务端错误，本地照样清理
    }
    reset()
  }

  function reset() {
    tokenStore.clear()
    user.value = null
    menus.value = []
    perms.value = new Set()
    loaded.value = false
  }

  return { user, menus, perms, loaded, isSuper, displayName, initial, can, hasRole, login, loadProfile, logout, reset }
})
