<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

const { t } = useI18n()
const router = useRouter()
</script>

<template>
  <div class="forbidden">
    <div class="inner">
      <span class="code">403</span>
      <h1>{{ t('无权访问') }}</h1>
      <p>{{ t('当前账号未被授予该页面的权限，如需访问请联系管理员分配角色。') }}</p>
      <div class="actions">
        <el-button type="primary" @click="router.push('/dashboard')">{{ t('返回仪表盘') }}</el-button>
        <el-button @click="router.back()">{{ t('返回上一页') }}</el-button>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.forbidden {
  height: 100vh;
  height: 100dvh;
  display: grid;
  place-items: center;
  background: var(--bg-page);
}

.inner {
  text-align: center;

  h1 {
    margin-top: 4px;
    font-size: 20px;
    font-weight: 600;
    color: var(--ink-900);
  }

  p {
    margin: 10px 0 24px;
    font-size: 13px;
    color: var(--ink-500);
  }
}

.code {
  font-size: 72px;
  font-weight: 700;
  letter-spacing: -0.04em;
  background: linear-gradient(180deg, #fecdd3, #e11d48);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  font-variant-numeric: tabular-nums;
}

.actions {
  display: flex;
  justify-content: center;
  gap: 10px;
}
</style>
