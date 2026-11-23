<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { ArrowDown, Expand, Fold, SwitchButton, User } from '@element-plus/icons-vue'
import SideMenuItem from '@/components/SideMenuItem.vue'
import MessageBell from '@/components/MessageBell.vue'
import { useAuthStore } from '@/stores/auth'
import { membershipApi } from '@/api/membership'
import type { MembershipSnapshot, MenuNode } from '@/api/types'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const mini = ref(localStorage.getItem('keel.side.mini') === '1')

// 会员徽章：会员模块未启用时静默忽略，不影响其余功能。
const membership = ref<MembershipSnapshot | null>(null)
onMounted(async () => {
  try {
    membership.value = await membershipApi.me()
  } catch {
    membership.value = null
  }
})

const navMenus = computed(() =>
  (auth.menus ?? []).filter((m) => m.type !== 'button' && m.visible !== false),
)

const activePath = computed(() => route.path)

/** 面包屑：从菜单树里回溯当前路径的祖先链 */
const breadcrumbs = computed<string[]>(() => {
  const walk = (nodes: MenuNode[], path: string, trail: string[]): string[] | null => {
    for (const node of nodes) {
      if (node.path === path) return [...trail, node.name]
      if (node.children?.length) {
        const found = walk(node.children, path, [...trail, node.name])
        if (found) return found
      }
    }
    return null
  }
  const chain = walk(navMenus.value, route.path, [])
  if (chain) return chain
  return route.meta.title ? [route.meta.title as string] : []
})

function toggleMini() {
  mini.value = !mini.value
  localStorage.setItem('keel.side.mini', mini.value ? '1' : '0')
}

async function onCommand(cmd: string) {
  if (cmd === 'profile') {
    router.push('/profile')
    return
  }
  if (cmd === 'logout') {
    try {
      await ElMessageBox.confirm('退出后需要重新登录才能继续操作。', '确定退出登录？', {
        confirmButtonText: '退出登录',
        cancelButtonText: '取消',
        type: 'warning',
      })
    } catch {
      return
    }
    await auth.logout()
    router.replace({ name: 'login' })
  }
}

const envLabel = import.meta.env.DEV ? '开发环境' : '生产环境'

/**
 * 滚动只发生在内容区：切页后回到顶部。
 * 不依赖 transition 卸载时高度塌缩的副作用 —— 长页面之间跳转时，
 * 新页面可能瞬间撑起旧 scrollTop 的高度，导致停留在半截位置。
 */
const contentRef = ref<HTMLElement>()
watch(
  () => route.path,
  () => {
    nextTick(() => {
      if (contentRef.value) contentRef.value.scrollTop = 0
    })
  },
)
</script>

<template>
  <div class="shell">
    <aside class="side" :class="{ mini }">
      <div class="brand">
        <div class="logo">K</div>
        <div v-show="!mini" class="brand-text">
          <span class="brand-name">Keel</span>
          <span class="brand-sub">Admin Console</span>
        </div>
      </div>

      <nav class="side-nav">
        <el-menu
          :default-active="activePath"
          :collapse="mini"
          :collapse-transition="false"
          router
        >
          <SideMenuItem v-for="item in navMenus" :key="item.id" :item="item" />
        </el-menu>
      </nav>

      <div class="side-foot">
        <span class="dot" />
        <span v-show="!mini">服务正常 · v1.0.0</span>
      </div>
    </aside>

    <div class="main">
      <header class="top">
        <button class="icon-btn" :title="mini ? '展开菜单' : '收起菜单'" :aria-label="mini ? '展开菜单' : '收起菜单'" @click="toggleMini">
          <el-icon :size="17"><Expand v-if="mini" /><Fold v-else /></el-icon>
        </button>

        <div class="crumb">
          <template v-for="(name, i) in breadcrumbs" :key="i">
            <span v-if="i > 0" class="sep">/</span>
            <span :class="{ current: i === breadcrumbs.length - 1 }">{{ name }}</span>
          </template>
        </div>

        <div class="top-right">
          <span class="env">{{ envLabel }}</span>
          <MessageBell />
          <span class="divider" />
          <el-dropdown trigger="click" placement="bottom-end" @command="onCommand">
            <div class="user-chip" role="button" tabindex="0" aria-label="用户菜单"
              @keydown.enter.self="($event.target as HTMLElement).click()"
              @keydown.space.prevent.self="($event.target as HTMLElement).click()">
              <div class="avatar">{{ auth.initial }}</div>
              <div class="user-meta">
                <span class="user-name">
                  {{ auth.displayName }}
                  <em
                    v-if="membership?.member && membership.level"
                    class="member-badge"
                    :style="{ '--lv-color': membership.level.color || 'var(--brand-500)' }"
                  >
                    {{ membership.level.name }}
                  </em>
                </span>
                <span class="user-role">{{ auth.isSuper ? '超级管理员' : '普通用户' }}</span>
              </div>
              <el-icon class="caret" :size="12"><ArrowDown /></el-icon>
            </div>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="profile">
                  <el-icon><User /></el-icon>个人中心
                </el-dropdown-item>
                <el-dropdown-item command="logout" divided>
                  <el-icon><SwitchButton /></el-icon>退出登录
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </header>

      <div ref="contentRef" class="content">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.shell {
  display: flex;
  height: 100vh;
  height: 100dvh; /* 移动端/分屏下跟随可视区域，避免 100vh 高于可视高度 */
  overflow: hidden;
  background: var(--bg-page);
}

/* ------------------------------------------------------------ 侧边栏 */
.side {
  width: var(--side-w);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: #fff;
  border-right: 1px solid var(--line);
  transition: width 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  overflow: hidden;

  &.mini {
    width: var(--side-w-mini);

    .brand {
      padding: 0 19px;
    }
  }
}

.brand {
  height: var(--top-h);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 18px;
  flex-shrink: 0;
}

.logo {
  width: 30px;
  height: 30px;
  flex-shrink: 0;
  border-radius: 9px;
  display: grid;
  place-items: center;
  font-size: 15px;
  font-weight: 700;
  color: #fff;
  background: linear-gradient(135deg, #6366f1, #4338ca);
  box-shadow: 0 2px 8px rgba(79, 70, 229, 0.32);
}

.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
  white-space: nowrap;
}

.brand-name {
  font-size: 14.5px;
  font-weight: 650;
  letter-spacing: -0.01em;
  color: var(--ink-900);
}

.brand-sub {
  font-size: 10px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--ink-400);
}

.side-nav {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
  padding: 4px 0 12px;

  :deep(.el-menu) {
    width: 100%;
  }
}

.side-foot {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 13px 18px;
  border-top: 1px solid var(--line-soft);
  font-size: 11.5px;
  color: var(--ink-400);
  white-space: nowrap;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ok);
  box-shadow: 0 0 0 3px rgba(18, 183, 106, 0.14);
  flex-shrink: 0;
}

/* -------------------------------------------------------------- 主区 */
.main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.top {
  height: var(--top-h);
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 20px;
  background: #fff;
  border-bottom: 1px solid var(--line);
}

.icon-btn {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-md);
  background: transparent;
  color: var(--ink-500);
  cursor: pointer;
  transition: background-color 0.14s ease, color 0.14s ease;

  &:hover {
    background: var(--ink-100);
    color: var(--ink-900);
  }
}

.crumb {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  color: var(--ink-400);

  .sep {
    color: var(--ink-300);
  }

  .current {
    color: var(--ink-900);
    font-weight: 500;
  }
}

.top-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 6px;
}

.env {
  padding: 3px 9px;
  border-radius: var(--r-full);
  background: var(--ink-100);
  font-size: 11.5px;
  font-weight: 500;
  color: var(--ink-500);
}

.divider {
  width: 1px;
  height: 18px;
  background: var(--line);
  margin: 0 6px;
}

.user-chip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px 4px 4px;
  border-radius: var(--r-full);
  cursor: pointer;
  transition: background-color 0.14s ease;
  outline: none;

  &:focus-visible {
    outline: 2px solid var(--brand-500);
    outline-offset: 2px;
  }

  &:hover {
    background: var(--ink-100);
  }
}

.avatar {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 12px;
  font-weight: 600;
  color: #fff;
  background: linear-gradient(135deg, #6366f1, #4338ca);
}

.user-meta {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}

.user-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-800);
}

.member-badge {
  padding: 1px 6px;
  border-radius: var(--r-full);
  font-style: normal;
  font-size: 10px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--lv-color);
  background: color-mix(in srgb, var(--lv-color) 13%, white);
  border: 1px solid color-mix(in srgb, var(--lv-color) 25%, white);
  white-space: nowrap;
}

.user-role {
  font-size: 10.5px;
  color: var(--ink-400);
}

.caret {
  color: var(--ink-400);
  margin-left: 2px;
}

.content {
  flex: 1;
  /* flex 子项默认 min-height:auto，内容过高时会把自身撑开；
     显式归零，滚动条才会落在内容区而不是溢出到外层。 */
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  /* 滚到边界后不再把滚动链传给 body，触控板下不会出现整页回弹 */
  overscroll-behavior: contain;
}
</style>
