<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'

/**
 * 6 位动态码输入：逐格输入、自动前进、支持整段粘贴。
 * 用单字符 input 而不是一个长 input，是为了让用户清楚看到还差几位。
 */
const props = withDefaults(
  defineProps<{
    modelValue: string
    length?: number
    disabled?: boolean
    autofocus?: boolean
  }>(),
  { length: 6, disabled: false, autofocus: false },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  complete: [value: string]
}>()

const boxes = ref<HTMLInputElement[]>([])
const digits = ref<string[]>(Array.from({ length: props.length }, () => ''))

watch(
  () => props.modelValue,
  (value) => {
    const chars = (value ?? '').replace(/\D/g, '').slice(0, props.length).split('')
    digits.value = Array.from({ length: props.length }, (_, i) => chars[i] ?? '')
  },
)

function focusAt(index: number) {
  const target = Math.max(0, Math.min(index, props.length - 1))
  nextTick(() => boxes.value[target]?.focus())
}

function emitValue() {
  const value = digits.value.join('')
  emit('update:modelValue', value)
  if (value.length === props.length) emit('complete', value)
}

function onInput(index: number, event: Event) {
  const el = event.target as HTMLInputElement
  const raw = el.value.replace(/\D/g, '')
  if (!raw) {
    digits.value[index] = ''
    emitValue()
    return
  }
  // 一次性粘贴多位时向后铺开
  let cursor = index
  for (const ch of raw) {
    if (cursor >= props.length) break
    digits.value[cursor] = ch
    cursor += 1
  }
  el.value = digits.value[index] ?? ''
  emitValue()
  focusAt(cursor)
}

function onKeydown(index: number, event: KeyboardEvent) {
  if (event.key === 'Backspace') {
    if (digits.value[index]) {
      digits.value[index] = ''
      emitValue()
      return
    }
    if (index > 0) {
      event.preventDefault()
      digits.value[index - 1] = ''
      emitValue()
      focusAt(index - 1)
    }
    return
  }
  if (event.key === 'ArrowLeft') {
    event.preventDefault()
    focusAt(index - 1)
  } else if (event.key === 'ArrowRight') {
    event.preventDefault()
    focusAt(index + 1)
  }
}

function onPaste(event: ClipboardEvent) {
  const text = event.clipboardData?.getData('text') ?? ''
  const clean = text.replace(/\D/g, '').slice(0, props.length)
  if (!clean) return
  event.preventDefault()
  digits.value = Array.from({ length: props.length }, (_, i) => clean[i] ?? '')
  emitValue()
  focusAt(clean.length)
}

onMounted(() => {
  if (props.autofocus) focusAt(0)
})
</script>

<template>
  <div class="otp">
    <input
      v-for="(_, i) in digits"
      :key="i"
      :ref="(el) => (boxes[i] = el as HTMLInputElement)"
      :value="digits[i]"
      :aria-label="`第 ${i + 1} 位动态码`"
      class="otp-box"
      type="text"
      inputmode="numeric"
      autocomplete="one-time-code"
      maxlength="1"
      :disabled="disabled"
      @input="onInput(i, $event)"
      @keydown="onKeydown(i, $event)"
      @paste="onPaste"
      @focus="($event.target as HTMLInputElement).select()"
    />
  </div>
</template>

<style scoped lang="scss">
.otp {
  display: flex;
  gap: 8px;
}

.otp-box {
  /* 自适应容器宽度：登录表单很窄，固定宽度会被 flex 压扁变形 */
  flex: 1 1 0;
  min-width: 0;
  max-width: 58px;
  height: 54px;
  padding: 0;
  text-align: center;
  font-size: 22px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: var(--ink-900);
  background: #fff;
  border: 1.5px solid var(--line);
  border-radius: var(--r-md);
  outline: none;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;

  &:hover:not(:disabled) {
    border-color: var(--ink-300);
  }

  &:focus {
    border-color: var(--brand-500);
    box-shadow: 0 0 0 3px rgba(79, 70, 229, 0.12);
  }

  &:disabled {
    background: var(--ink-50);
    color: var(--ink-400);
  }
}
</style>
