import type { Directive, DirectiveBinding } from 'vue'
import { useAuthStore } from '@/stores/auth'

/**
 * 按钮级权限指令。
 * v-perm="'system:user:create'"        —— 无权限则隐藏元素
 * v-perm.disable="'system:user:create'" —— 无权限则置灰（保留占位）
 *
 * 用 display 控制而非 removeChild：直接改 DOM 会让 Vue 的虚拟 DOM 与真实树失配，
 * 权限在会话中途刷新时也无法恢复显示。
 */
function apply(el: HTMLElement, binding: DirectiveBinding<string | string[]>) {
  const auth = useAuthStore()
  const allowed = auth.can(binding.value)

  if (binding.modifiers.disable) {
    el.classList.toggle('is-disabled', !allowed)
    if (allowed) {
      el.removeAttribute('disabled')
      el.style.pointerEvents = ''
      el.style.opacity = ''
    } else {
      el.setAttribute('disabled', 'disabled')
      el.style.pointerEvents = 'none'
      el.style.opacity = '0.45'
    }
    return
  }

  if (allowed) {
    if (el.dataset.permHidden === '1') {
      el.style.display = el.dataset.permDisplay || ''
      delete el.dataset.permHidden
      delete el.dataset.permDisplay
    }
  } else if (el.dataset.permHidden !== '1') {
    el.dataset.permDisplay = el.style.display
    el.dataset.permHidden = '1'
    el.style.display = 'none'
  }
}

export const permDirective: Directive<HTMLElement, string | string[]> = {
  mounted: apply,
  updated: apply,
}
