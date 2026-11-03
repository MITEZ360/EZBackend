<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { Lock, Refresh, User } from '@element-plus/icons-vue'
import { authApi } from '@/api/auth'
import { ApiError } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const formRef = ref<FormInstance>()
const form = reactive({ username: '', password: '', captchaCode: '' })
const loading = ref(false)
const errorMsg = ref('')

const captcha = reactive({ enabled: false, id: '', image: '' })

const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
  captchaCode: [{ required: true, message: '请输入验证码', trigger: 'blur' }],
}

const redirect = computed(() => {
  const r = route.query.redirect
  return typeof r === 'string' && r.startsWith('/') ? r : '/dashboard'
})

async function loadCaptcha() {
  try {
    const res = await authApi.captcha()
    captcha.enabled = res.enabled
    captcha.id = res.captchaId
    captcha.image = res.image
    form.captchaCode = ''
  } catch {
    captcha.enabled = false
  }
}

async function submit() {
  errorMsg.value = ''
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return

  loading.value = true
  try {
    await auth.login({
      username: form.username.trim(),
      password: form.password,
      captchaId: captcha.enabled ? captcha.id : undefined,
      captchaCode: captcha.enabled ? form.captchaCode : undefined,
    })
    ElMessage.success(`欢迎回来，${auth.displayName}`)
    router.replace(redirect.value)
  } catch (err) {
    errorMsg.value = err instanceof ApiError ? err.message : '登录失败，请稍后重试'
    if (captcha.enabled) await loadCaptcha()
  } finally {
    loading.value = false
  }
}

onMounted(loadCaptcha)
</script>

<template>
  <div class="login">
    <!-- 左：品牌叙事区 -->
    <section class="brand-side">
      <div class="brand-inner">
        <div class="brand-top">
          <div class="logo">K</div>
          <span class="logo-text">Keel</span>
        </div>

        <div class="brand-copy">
          <h1>把基础设施<br />做成可以长期依赖的东西</h1>
          <p>
            权限、审计、任务、文件与可观测能力开箱可用，
            单二进制交付，也能按需拆成微服务。
          </p>

          <ul class="points">
            <li>
              <span class="idx">01</span>
              <div>
                <strong>RBAC 与数据权限</strong>
                <em>用户、角色、菜单三级模型，支持按部门范围过滤数据</em>
              </div>
            </li>
            <li>
              <span class="idx">02</span>
              <div>
                <strong>全链路可观测</strong>
                <em>结构化日志、Prometheus 指标与慢请求追踪</em>
              </div>
            </li>
            <li>
              <span class="idx">03</span>
              <div>
                <strong>单体 / 微服务同一套代码</strong>
                <em>切换配置即可在单进程与网关模式之间迁移</em>
              </div>
            </li>
          </ul>
        </div>

        <div class="brand-foot">
          <span>© 2026 Keel Framework</span>
          <span class="ver">v1.0.0</span>
        </div>
      </div>
    </section>

    <!-- 右：登录表单 -->
    <section class="form-side">
      <div class="form-inner">
        <header class="form-head">
          <h2>欢迎回来</h2>
          <p>登录以继续管理你的服务</p>
        </header>

        <el-form
          ref="formRef"
          :model="form"
          :rules="rules"
          label-position="top"
          size="large"
          @submit.prevent="submit"
        >
          <el-form-item label="账号" prop="username">
            <el-input
              v-model="form.username"
              placeholder="请输入用户名"
              autocomplete="username"
              :prefix-icon="User"
            />
          </el-form-item>

          <el-form-item label="密码" prop="password">
            <el-input
              v-model="form.password"
              type="password"
              placeholder="请输入密码"
              autocomplete="current-password"
              show-password
              :prefix-icon="Lock"
              @keyup.enter="submit"
            />
          </el-form-item>

          <el-form-item v-if="captcha.enabled" label="验证码" prop="captchaCode">
            <div class="captcha-row">
              <el-input
                v-model="form.captchaCode"
                placeholder="请输入验证码"
                maxlength="6"
                @keyup.enter="submit"
              />
              <button type="button" class="captcha-img" title="点击刷新" @click="loadCaptcha">
                <img v-if="captcha.image" :src="captcha.image" alt="验证码" />
                <el-icon v-else><Refresh /></el-icon>
              </button>
            </div>
          </el-form-item>

          <p v-if="errorMsg" class="error">{{ errorMsg }}</p>

          <el-button type="primary" class="submit" :loading="loading" @click="submit">
            {{ loading ? '登录中…' : '登 录' }}
          </el-button>
        </el-form>

        <div class="hint">
          默认账号 <code>admin</code> / <code>Admin@123456</code>，登录后请尽快修改密码
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped lang="scss">
.login {
  display: grid;
  grid-template-columns: 1.15fr 0.85fr;
  height: 100vh;
  height: 100dvh;
  background: #fff;
}

/* ---------------------------------------------------------- 品牌区 */
.brand-side {
  position: relative;
  overflow: hidden;
  background: #0a0c13;
  color: #fff;
  display: flex;

  /* 光晕 */
  &::before {
    content: '';
    position: absolute;
    top: -30%;
    left: -20%;
    width: 120%;
    height: 900px;
    background:
      radial-gradient(circle at 30% 34%, rgba(99, 102, 241, 0.52), transparent 56%),
      radial-gradient(circle at 76% 10%, rgba(56, 189, 248, 0.26), transparent 50%),
      radial-gradient(circle at 12% 92%, rgba(168, 85, 247, 0.24), transparent 52%),
      radial-gradient(circle at 88% 78%, rgba(45, 212, 191, 0.12), transparent 46%);
    filter: blur(8px);
  }

  /* 网格纹理，边缘渐隐 */
  &::after {
    content: '';
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(rgba(255, 255, 255, 0.05) 1px, transparent 1px),
      linear-gradient(90deg, rgba(255, 255, 255, 0.05) 1px, transparent 1px);
    background-size: 50px 50px;
    mask-image: radial-gradient(circle at 38% 40%, #000 28%, transparent 76%);
    -webkit-mask-image: radial-gradient(circle at 38% 40%, #000 28%, transparent 76%);
  }
}

.brand-inner {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  width: 100%;
  padding: 40px 56px 36px;
}

.brand-top {
  display: flex;
  align-items: center;
  gap: 10px;
}

.logo {
  width: 32px;
  height: 32px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  font-size: 16px;
  font-weight: 700;
  background: linear-gradient(135deg, #6366f1, #4338ca);
  box-shadow: 0 4px 14px rgba(79, 70, 229, 0.45);
}

.logo-text {
  font-size: 15px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.brand-copy {
  margin: auto 0;
  max-width: 480px;

  h1 {
    font-size: 34px;
    line-height: 1.32;
    font-weight: 600;
    letter-spacing: -0.018em;
    background: linear-gradient(180deg, #fff 30%, rgba(255, 255, 255, 0.72));
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }

  > p {
    margin-top: 18px;
    font-size: 14px;
    line-height: 1.75;
    color: rgba(255, 255, 255, 0.56);
    max-width: 400px;
  }
}

.points {
  margin-top: 40px;
  display: flex;
  flex-direction: column;
  gap: 20px;

  li {
    display: flex;
    gap: 14px;
  }

  .idx {
    flex-shrink: 0;
    width: 26px;
    padding-top: 1px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    color: rgba(129, 140, 248, 0.9);
    font-variant-numeric: tabular-nums;
  }

  strong {
    display: block;
    font-size: 13.5px;
    font-weight: 500;
    color: rgba(255, 255, 255, 0.92);
  }

  em {
    display: block;
    margin-top: 3px;
    font-size: 12.5px;
    font-style: normal;
    line-height: 1.6;
    color: rgba(255, 255, 255, 0.42);
  }
}

.brand-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 11.5px;
  color: rgba(255, 255, 255, 0.34);

  .ver {
    padding: 1px 7px;
    border-radius: 999px;
    border: 1px solid rgba(255, 255, 255, 0.12);
  }
}

/* ---------------------------------------------------------- 表单区 */
.form-side {
  display: grid;
  place-items: center;
  padding: 40px;
  background: #fff;
}

.form-inner {
  width: 100%;
  max-width: 352px;
}

.form-head {
  margin-bottom: 28px;

  h2 {
    font-size: 24px;
    font-weight: 650;
    letter-spacing: -0.02em;
    color: var(--ink-900);
  }

  p {
    margin-top: 7px;
    font-size: 13px;
    color: var(--ink-500);
  }
}

:deep(.el-form-item__label) {
  font-size: 12.5px;
  font-weight: 500;
  color: var(--ink-700);
  padding-bottom: 6px;
}

:deep(.el-input__wrapper) {
  min-height: 44px;
  border-radius: 10px;
  padding: 1px 13px;
}

.captcha-row {
  display: flex;
  gap: 10px;
  width: 100%;

  :deep(.el-input) {
    flex: 1;
  }
}

.captcha-img {
  width: 104px;
  height: 44px;
  flex-shrink: 0;
  padding: 0;
  border: 1px solid var(--ink-200);
  border-radius: 10px;
  background: var(--ink-50);
  cursor: pointer;
  overflow: hidden;
  display: grid;
  place-items: center;
  color: var(--ink-400);
  transition: border-color 0.15s ease;

  &:hover {
    border-color: var(--ink-300);
  }

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
}

.error {
  margin: -4px 0 14px;
  padding: 9px 12px;
  border-radius: 8px;
  background: var(--danger-bg);
  color: var(--danger-deep);
  font-size: 12.5px;
}

.submit {
  width: 100%;
  height: 44px;
  margin-top: 6px;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 500;
  letter-spacing: 0.04em;
}

.hint {
  margin-top: 22px;
  font-size: 12px;
  line-height: 1.7;
  color: var(--ink-400);
  text-align: center;

  code {
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--ink-100);
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--ink-600);
  }
}

/* ---------------------------------------------------------- 响应式 */
@media (max-width: 900px) {
  .login {
    grid-template-columns: 1fr;
  }

  .brand-side {
    display: none;
  }
}
</style>
