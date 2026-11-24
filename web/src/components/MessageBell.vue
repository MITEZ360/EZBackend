<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Bell } from '@element-plus/icons-vue'
import { messageApi } from '@/api/message'
import { useMessageStore } from '@/stores/message'
import type { MessageItem } from '@/api/types'
import { fromNow } from '@/utils/format'

const router = useRouter()
const store = useMessageStore()
const { t } = useI18n()
const visible = ref(false)
const acting = ref(false)

const unreadText = computed(() => (store.unread > 99 ? '99+' : String(store.unread)))

async function onShow() {
  await store.refresh()
}

async function openItem(item: MessageItem) {
  visible.value = false
  if (!item.readAt) {
    try {
      await messageApi.markRead(item.messageId)
      await store.refresh()
    } catch {
      // 消息可能已被删除，跳转仍然进行
    }
  }
  router.push(item.link || '/system/message')
}

async function markAll() {
  if (acting.value) return
  acting.value = true
  try {
    const res = await messageApi.markAllRead()
    await store.refresh()
    ElMessage.success(res.updated > 0 ? t('已标记 {count} 条为已读', { count: res.updated }) : t('没有未读消息'))
  } finally {
    acting.value = false
  }
}

onMounted(() => store.start())
onBeforeUnmount(() => store.stop())
</script>

<template>
  <el-popover
    v-model:visible="visible"
    placement="bottom-end"
    :width="364"
    trigger="click"
    popper-class="msg-pop"
    @show="onShow"
  >
    <template #reference>
      <button class="bell" :class="{ active: store.unread > 0 }" :title="t('消息中心')" :aria-label="t('消息中心')">
        <el-icon :size="17"><Bell /></el-icon>
        <span v-if="store.unread > 0" class="bell-dot">{{ unreadText }}</span>
      </button>
    </template>

    <div class="mp">
      <div class="mp-head">
        <span class="mp-title">{{ t('消息') }}</span>
        <button v-if="store.unread > 0" class="mp-link" :disabled="acting" @click="markAll">
          {{ t('全部已读') }}
        </button>
      </div>

      <div v-if="store.recent.length" class="mp-list">
        <button
          v-for="m in store.recent"
          :key="m.id"
          class="mp-item"
          :class="{ unread: !m.readAt }"
          @click="openItem(m)"
        >
          <span class="mp-unread-dot" />
          <span class="mp-main">
            <span class="mp-row">
              <span class="mp-item-title">{{ m.title }}</span>
              <span class="mp-time">{{ fromNow(m.createdAt) }}</span>
            </span>
            <span class="mp-excerpt">{{ m.excerpt }}</span>
          </span>
        </button>
      </div>

      <div v-else class="mp-empty">
        <el-icon :size="20"><Bell /></el-icon>
        <p>{{ t('暂无消息') }}</p>
      </div>

      <div class="mp-foot">
        <router-link to="/system/message" @click="visible = false">{{ t('查看全部消息') }}</router-link>
      </div>
    </div>
  </el-popover>
</template>

<style scoped lang="scss">
.bell {
  position: relative;
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 8px;
  border: 1px solid transparent;
  color: var(--ink-500);
  background: transparent;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;

  &:hover,
  &.active {
    color: var(--ink-800);
    background: var(--ink-100);
  }
}

.bell-dot {
  position: absolute;
  top: 2px;
  right: 0;
  min-width: 15px;
  height: 15px;
  padding: 0 4px;
  border-radius: 8px;
  background: #e5484d;
  border: 1.5px solid var(--bg-card, #fff);
  color: #fff;
  font-size: 9.5px;
  font-weight: 600;
  line-height: 12px;
  text-align: center;
  font-variant-numeric: tabular-nums;
}
</style>

<style lang="scss">
/* popover 由 teleport 挂到 body，样式必须放在非 scoped 块里 */
.msg-pop.el-popover {
  padding: 0;
  border-radius: 12px;
  border-color: var(--line);
  box-shadow: 0 12px 32px rgba(16, 24, 40, 0.12);
}

.msg-pop .mp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 13px 16px 11px;
  border-bottom: 1px solid var(--line-soft);
}

.msg-pop .mp-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-900);
}

.msg-pop .mp-link {
  border: 0;
  background: none;
  padding: 0;
  font-size: 12px;
  color: var(--brand-600);
  cursor: pointer;

  &:disabled {
    color: var(--ink-300);
    cursor: default;
  }
}

.msg-pop .mp-list {
  max-height: 320px;
  overflow-y: auto;
}

.msg-pop .mp-item {
  display: flex;
  gap: 9px;
  width: 100%;
  padding: 11px 16px;
  border: 0;
  border-bottom: 1px solid var(--line-soft);
  background: transparent;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s ease;

  &:hover {
    background: var(--ink-50, #fafafa);
  }
}

.msg-pop .mp-unread-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  margin-top: 6px;
  flex-shrink: 0;
  background: transparent;
}

.msg-pop .mp-item.unread .mp-unread-dot {
  background: var(--brand-600);
}

.msg-pop .mp-main {
  min-width: 0;
  flex: 1;
}

.msg-pop .mp-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.msg-pop .mp-item-title {
  font-size: 12.8px;
  color: var(--ink-700);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msg-pop .mp-item.unread .mp-item-title {
  color: var(--ink-900);
  font-weight: 600;
}

.msg-pop .mp-time {
  font-size: 11px;
  color: var(--ink-400);
  flex-shrink: 0;
}

.msg-pop .mp-excerpt {
  margin-top: 3px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--ink-400);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.msg-pop .mp-empty {
  display: grid;
  place-items: center;
  gap: 6px;
  padding: 34px 0;
  color: var(--ink-300);

  p {
    font-size: 12px;
    color: var(--ink-400);
  }
}

.msg-pop .mp-foot {
  padding: 10px 16px;
  text-align: center;

  a {
    font-size: 12px;
    color: var(--brand-600);
    text-decoration: none;

    &:hover {
      text-decoration: underline;
    }
  }
}
</style>
