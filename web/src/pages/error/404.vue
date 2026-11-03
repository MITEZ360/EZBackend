<script setup lang="ts">
import { useRouter } from 'vue-router'

const router = useRouter()
</script>

<template>
  <div class="notfound">
    <div class="inner">
      <span class="code">404</span>
      <h1>页面不存在</h1>
      <p>地址可能已变更，或你没有访问该页面的权限。</p>
      <div class="actions">
        <el-button type="primary" @click="router.push('/dashboard')">返回仪表盘</el-button>
        <el-button @click="router.back()">返回上一页</el-button>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.notfound {
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
  background: linear-gradient(180deg, #c7d2fe, #4f46e5);
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
