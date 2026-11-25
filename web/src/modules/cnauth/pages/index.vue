<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { cnApi } from '@/api/cn'
import { tokenStore } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()

const formRef = ref<FormInstance>()
const form = reactive({ username: '', password: '', nickname: '', phone: '', code: '', email: '' })
const loading = ref(false)
const sending = ref(false)
const countdown = ref(0)
let timer: ReturnType<typeof setInterval> | undefined

const rules = computed<FormRules>(() => ({
  username: [{ required: true, message: t('请输入用户名'), trigger: 'blur' }],
  password: [{ required: true, message: t('请输入密码'), trigger: 'blur' }],
  phone: [{ required: true, message: t('请输入手机号'), trigger: 'blur' }],
  code: [{ required: true, message: t('请输入短信验证码'), trigger: 'blur' }],
}))

const codeLabel = computed(() =>
  countdown.value > 0 ? `${countdown.value}${t('秒后重试')}` : t('获取验证码'),
)

onMounted(async () => {
  try {
    const cfg = await cnApi.publicConfig()
    if (!cfg.registerEnabled || !cfg.smsLoginEnabled) {
      ElMessage.warning(t('注册未开放'))
      router.replace('/login')
    }
  } catch {
    router.replace('/login')
  }
})

onUnmounted(() => timer && clearInterval(timer))

async function sendCode() {
  if (!/^1[3-9]\d{9}$/.test(form.phone)) {
    ElMessage.warning(t('请输入正确的手机号'))
    return
  }
  sending.value = true
  try {
    await cnApi.sendCode({ phone: form.phone, scene: 'register' })
    ElMessage.success(t('验证码已发送'))
    countdown.value = 60
    timer = setInterval(() => {
      if (--countdown.value <= 0 && timer) clearInterval(timer)
    }, 1000)
  } finally {
    sending.value = false
  }
}

async function submit() {
  if (!formRef.value) return
  await formRef.value.validate()
  loading.value = true
  try {
    const res = await cnApi.register({ ...form })
    tokenStore.save(res.token.accessToken, res.token.refreshToken)
    await auth.loadProfile()
    ElMessage.success(t('注册成功'))
    router.replace('/dashboard')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="register-page">
    <el-card class="register-card">
      <h2>{{ t('用户注册') }}</h2>
      <p class="subtle">
        {{ t('已有账号？') }}
        <router-link to="/login">{{ t('返回登录') }}</router-link>
      </p>
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
        <el-form-item :label="t('用户名')" prop="username">
          <el-input v-model="form.username" autocomplete="username" />
        </el-form-item>
        <el-form-item :label="t('密码')" prop="password">
          <el-input v-model="form.password" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <el-form-item :label="t('昵称')">
          <el-input v-model="form.nickname" />
        </el-form-item>
        <el-form-item :label="t('手机号')" prop="phone">
          <el-input v-model="form.phone" inputmode="numeric" maxlength="11" />
        </el-form-item>
        <el-form-item :label="t('短信验证码')" prop="code">
          <el-input v-model="form.code" inputmode="numeric" maxlength="6">
            <template #append>
              <el-button :loading="sending" :disabled="countdown > 0" @click="sendCode">
                {{ codeLabel }}
              </el-button>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('邮箱')">
          <el-input v-model="form.email" />
        </el-form-item>
        <el-button type="primary" class="submit" :loading="loading" @click="submit">
          {{ t('注册') }}
        </el-button>
      </el-form>
    </el-card>
  </div>
</template>

<style scoped lang="scss">
.register-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.register-card {
  width: 440px;
  max-width: 100%;
  h2 { margin: 0 0 4px; }
  .subtle { color: var(--el-text-color-secondary); font-size: 13px; margin-bottom: 20px; }
  .submit { width: 100%; }
}
</style>
