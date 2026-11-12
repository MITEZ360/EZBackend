<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { tokenStore } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const message = ref('正在完成第三方登录…')
const failed = ref(false)

onMounted(async () => {
  const query = new URLSearchParams(window.location.search)
  // 已登录状态下发起的账号绑定，回调不带令牌
  if (query.has('oauth_bound')) {
    ElMessage.success('已绑定第三方账号')
    router.replace('/profile')
    return
  }

  // 令牌放在 URL fragment，不会出现在服务端日志与 Referer 里
  const hash = new URLSearchParams(window.location.hash.replace(/^#/, ''))
  const access = hash.get('access_token')
  const refresh = hash.get('refresh_token') ?? undefined
  if (!access) {
    failed.value = true
    message.value = '未收到登录令牌，请重新登录'
    window.setTimeout(() => router.replace('/login'), 1600)
    return
  }

  tokenStore.save(access, refresh)
  try {
    await auth.loadProfile()
    ElMessage.success('登录成功')
    router.replace('/dashboard')
  } catch {
    tokenStore.clear()
    failed.value = true
    message.value = '登录信息加载失败，请重新登录'
    window.setTimeout(() => router.replace('/login'), 1600)
  }
})
</script>

<template>
  <div class="callback">
    <div class="card">
      <div class="logo">K</div>
      <div v-if="!failed" class="spinner" />
      <div v-else class="failed">!</div>
      <h2>{{ failed ? '登录未完成' : '正在登录' }}</h2>
      <p>{{ message }}</p>
      <button v-if="failed" type="button" class="back" @click="router.replace('/login')">
        返回登录
      </button>
    </div>
  </div>
</template>

<style scoped lang="scss">
.callback {
  display: grid;
  place-items: center;
  min-height: 100vh;
  background: var(--bg-subtle);
}

.card {
  width: 340px;
  padding: 40px 32px;
  border: 1px solid var(--ink-100);
  border-radius: 14px;
  background: #fff;
  text-align: center;
  box-shadow: 0 12px 40px rgba(15, 23, 42, 0.06);
}

.logo {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  margin: 0 auto 22px;
  border-radius: 12px;
  background: var(--brand-600);
  color: #fff;
  font-size: 22px;
  font-weight: 700;
}

.spinner {
  width: 26px;
  height: 26px;
  margin: 0 auto 18px;
  border: 2px solid var(--ink-100);
  border-top-color: var(--brand-600);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.failed {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  margin: 0 auto 16px;
  border-radius: 50%;
  background: #fef2f2;
  color: #dc2626;
  font-weight: 700;
}

h2 {
  margin: 0 0 8px;
  font-size: 17px;
  color: var(--ink-800);
}

p {
  margin: 0;
  font-size: 13px;
  color: var(--ink-400);
}

.back {
  margin-top: 20px;
  padding: 7px 16px;
  border: 1px solid var(--brand-600);
  border-radius: 7px;
  background: #fff;
  color: var(--brand-600);
  font-size: 13px;
  cursor: pointer;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
