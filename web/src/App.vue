<script setup lang="ts">
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElConfigProvider } from 'element-plus'
import { currentLocale, elementLocale } from '@/i18n'
import { applyDocumentTitle } from '@/router'

// 包一层 ConfigProvider，语言切换时 Element Plus 内置文案（分页/日期/空态等）同步更新。
const locale = computed(() => elementLocale())
const route = useRoute()

// 语言变化时重建页面组件：保证非响应式的静态表格配置、表单规则等也跟随新语言。
// 用当前语言作为 key，配合下方的 document.title 同步完成整页文案刷新。
watch(currentLocale, (value) => {
  applyDocumentTitle(route.meta.title as string | undefined)
  document.documentElement.lang = value
}, { immediate: true })
</script>

<template>
  <el-config-provider :locale="locale">
    <router-view v-slot="{ Component }">
      <transition name="fade" mode="out-in">
        <component :is="Component" :key="currentLocale()" />
      </transition>
    </router-view>
  </el-config-provider>
</template>
