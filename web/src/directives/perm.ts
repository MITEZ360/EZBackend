import type { Directive } from 'vue'
import { useAuthStore } from '@/stores/auth'

/**
 * 按钮级权限指令。
 * v-perm="'system:user:create'"        —— 无权限则移除元素
 * v-perm.disable="'system:user:create'" —— 无权限则置灰（保留占位）
 */
export const permDirective: Directive<HTMLElement, string | string[]> = {
  mounted(el, binding) {
    const auth = useAuthStore()
    if (auth.can(binding.value)) return

    if (binding.modifiers.disable) {
      el.classList.add('is-disabled')
      el.setAttribute('disabled', 'disabled')
      el.style.pointerEvents = 'none'
      el.style.opacity = '0.45'
      return
    }
    el.parentNode?.removeChild(el)
  },
}
