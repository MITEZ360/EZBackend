<script setup lang="ts">
import { computed } from 'vue'
import type { MenuNode } from '@/api/types'
import { resolveIcon } from '@/utils/icons'

defineOptions({ name: 'SideMenuItem' })

const props = defineProps<{ item: MenuNode }>()

const icon = computed(() => resolveIcon(props.item.icon))
const children = computed(() => (props.item.children ?? []).filter((c) => c.type !== 'button' && c.visible !== false))
</script>

<template>
  <el-sub-menu v-if="children.length" :index="item.path || item.id">
    <template #title>
      <el-icon><component :is="icon" /></el-icon>
      <span>{{ $t(item.name) }}</span>
    </template>
    <SideMenuItem v-for="child in children" :key="child.id" :item="child" />
  </el-sub-menu>

  <el-menu-item v-else :index="item.path || item.id">
    <el-icon><component :is="icon" /></el-icon>
    <template #title>{{ $t(item.name) }}</template>
  </el-menu-item>
</template>
