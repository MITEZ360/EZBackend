<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { ArrowLeft, Key, Lock, Refresh, User } from '@element-plus/icons-vue'
import OtpInput from '@/components/OtpInput.vue'
import { authApi, oauthApi, type OAuthProvider } from '@/api/auth'
import { ApiError } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const isDev = import.meta.env.DEV
const auth = useAuthStore()

const formRef = ref<FormInstance>()
const form = reactive({ username: '', password: '', captchaCode: '' })
const loading = ref(false)
const errorMsg = ref('')

const captcha = reactive({ enabled: false, id: '', image: '' })

/** 由后端配置下发的第三方登录渠道；未配置时整块不显示 */
const providers = ref<OAuthProvider[]>([])
const oauthLoading = ref('')

/** credential：账号密码；totp：第二步动态码 */
const step = ref<'credential' | 'totp'>('credential')
const totpCode = ref('')
const recoveryMode = ref(false)
const recoveryCode = ref('')
const verifying = ref(false)

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

/** 两步登录共用的凭据部分 */
function credentials(extra: { totpCode?: string; recoveryCode?: string }) {
  return {
    username: form.username.trim(),
    password: form.password,
    captchaId: captcha.enabled ? captcha.id : undefined,
    captchaCode: captcha.enabled ? form.captchaCode : undefined,
    ...extra,
  }
}

function finish() {
  ElMessage.success(`欢迎回来，${auth.displayName}`)
  router.replace(redirect.value)
}

/** 统一处理登录失败：40104 进入第二步，其余展示后端文案 */
function handleFailure(err: unknown) {
  if (err instanceof ApiError && err.code === 40104) {
    step.value = 'totp'
    totpCode.value = ''
    recoveryCode.value = ''
    recoveryMode.value = false
    errorMsg.value = ''
    return
  }

  if (err instanceof ApiError) {
    errorMsg.value = err.message
    // 第二步失败时动态码已用掉，清空让用户重新输入
    if (step.value === 'totp') {
      totpCode.value = ''
      recoveryCode.value = ''
    }
    // 验证码被消费或过期，回到第一步重新取一张
    if (step.value === 'totp' && captcha.enabled && err.code === 40000) {
      backToCredential()
      errorMsg.value = '验证已超时，请重新登录'
    }
    return
  }
  errorMsg.value = '登录失败，请稍后重试'
}

async function submit() {
  errorMsg.value = ''
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return

  loading.value = true
  try {
    await auth.login(credentials({}))
    finish()
  } catch (err) {
    handleFailure(err)
    if (captcha.enabled && step.value === 'credential') await loadCaptcha()
  } finally {
    loading.value = false
  }
}

async function verify() {
  const value = (recoveryMode.value ? recoveryCode.value : totpCode.value).trim()
  if (!value) {
    errorMsg.value = recoveryMode.value ? '请输入恢复码' : '请输入 6 位动态验证码'
    return
  }
  if (!recoveryMode.value && !/^\d{6}$/.test(value)) {
    errorMsg.value = '动态验证码为 6 位数字'
    return
  }

  errorMsg.value = ''
  verifying.value = true
  try {
    await auth.login(
      credentials(
        recoveryMode.value ? { recoveryCode: value } : { totpCode: value },
      ),
    )
    finish()
  } catch (err) {
    handleFailure(err)
  } finally {
    verifying.value = false
  }
}

function backToCredential() {
  step.value = 'credential'
  errorMsg.value = ''
  totpCode.value = ''
  recoveryCode.value = ''
  recoveryMode.value = false
  if (captcha.enabled) void loadCaptcha()
}

function toggleRecovery() {
  recoveryMode.value = !recoveryMode.value
  errorMsg.value = ''
  totpCode.value = ''
  recoveryCode.value = ''
}

async function loadProviders() {
  try {
    const res = await oauthApi.providers()
    providers.value = res.providers ?? []
  } catch {
    providers.value = []
  }
}

/** 跳转到第三方授权页；回调会把令牌交回 /oauth/callback */
async function goOAuth(p: OAuthProvider) {
  oauthLoading.value = p.name
  try {
    const res = await oauthApi.authorize(p.name)
    window.location.href = res.url
  } catch {
    errorMsg.value = `${p.label} 暂时不可用，请稍后重试`
    oauthLoading.value = ''
  }
}

onMounted(() => {
  loadCaptcha()
  loadProviders()
  const err = route.query.oauth_error
  if (typeof err === 'string' && err) errorMsg.value = err
})
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
          <h2>{{ step === 'credential' ? '欢迎回来' : '两步验证' }}</h2>
          <p>
            {{
              step === 'credential'
                ? '登录以继续管理你的服务'
                : '该账号已开启两步验证，请完成安全校验'
            }}
          </p>
        </header>

        <el-form
          v-if="step === 'credential'"
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

        <!-- 第二步：动态码 / 恢复码 -->
        <div v-else class="totp-panel">
          <button type="button" class="back" @click="backToCredential">
            <el-icon :size="13"><ArrowLeft /></el-icon>
            返回重新登录
          </button>

          <div class="totp-hint">
            <div class="totp-icon"><el-icon :size="18"><Key /></el-icon></div>
            <div class="totp-copy">
              <strong>{{ recoveryMode ? '输入恢复码' : '输入 6 位动态码' }}</strong>
              <p v-if="!recoveryMode">
                打开 Google Authenticator、Microsoft Authenticator、Duo、Bitwarden
                等认证器查看当前验证码。
              </p>
              <p v-else>恢复码为 10 位，格式如 7K3M9-QP2XR，每个只能使用一次。</p>
            </div>
          </div>

          <OtpInput
            v-if="!recoveryMode"
            v-model="totpCode"
            class="otp-wrap"
            autofocus
            :disabled="verifying"
            @complete="verify"
          />
          <el-input
            v-else
            v-model="recoveryCode"
            class="recovery-input"
            size="large"
            maxlength="12"
            placeholder="XXXXX-XXXXX"
            @keyup.enter="verify"
          />

          <p v-if="errorMsg" class="error">{{ errorMsg }}</p>

          <el-button type="primary" class="submit" :loading="verifying" @click="verify">
            {{ verifying ? '验证中…' : '验证并登录' }}
          </el-button>

          <button type="button" class="switch-mode" @click="toggleRecovery">
            {{ recoveryMode ? '使用动态验证码' : '无法使用认证器？改用恢复码' }}
          </button>
        </div>

        <div v-if="step === 'credential' && providers.length" class="oauth">
          <div class="oauth-divider"><span>或使用第三方账号</span></div>
          <div class="oauth-list">
            <button
              v-for="p in providers"
              :key="p.name"
              type="button"
              class="oauth-btn"
              :disabled="oauthLoading !== ''"
              @click="goOAuth(p)"
            >
              <img v-if="p.icon" :src="p.icon" alt="" class="oauth-icon" />
              <span v-else class="oauth-fallback">{{ (p.label || p.name).slice(0, 1).toUpperCase() }}</span>
              <span>{{ oauthLoading === p.name ? '跳转中…' : p.label }}</span>
            </button>
          </div>
        </div>

        <!-- 默认凭据只在开发环境提示，生产环境展示等于公开超管账号密码 -->
        <div v-if="step === 'credential' && isDev" class="hint">
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

/* ------------------------------------------------------------ 两步验证 */
.totp-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;

  .error {
    margin: 0;
  }
}

.back {
  align-self: flex-start;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0;
  border: none;
  background: none;
  font-size: 12.5px;
  color: var(--ink-500);
  cursor: pointer;

  &:hover {
    color: var(--ink-800);
  }
}

.totp-hint {
  display: flex;
  gap: 11px;
  padding: 13px 14px;
  border-radius: var(--r-md);
  background: var(--ink-50);
}

.totp-icon {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border-radius: 10px;
  color: #fff;
  background: linear-gradient(135deg, #6366f1, #4338ca);
  box-shadow: 0 3px 10px rgba(79, 70, 229, 0.24);
}

.totp-copy {
  min-width: 0;

  strong {
    display: block;
    font-size: 13px;
    font-weight: 600;
    color: var(--ink-900);
  }

  p {
    margin-top: 4px;
    font-size: 12px;
    line-height: 1.6;
    color: var(--ink-500);
  }
}

.otp-wrap {
  justify-content: space-between;
}

.recovery-input {
  :deep(.el-input__inner) {
    font-family: var(--font-mono);
    letter-spacing: 0.16em;
    text-transform: uppercase;
  }
}

.switch-mode {
  align-self: center;
  padding: 2px 4px;
  border: none;
  background: none;
  font-size: 12.5px;
  color: var(--brand-600);
  cursor: pointer;

  &:hover {
    color: var(--brand-700);
    text-decoration: underline;
  }
}

/* ---------------------------------------------------------- 第三方登录 */
.oauth {
  margin-top: 26px;
}

.oauth-divider {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  font-size: 12px;
  color: var(--ink-400);

  &::before,
  &::after {
    content: '';
    flex: 1;
    height: 1px;
    background: var(--ink-100);
  }
}

.oauth-list {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.oauth-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-width: 132px;
  padding: 9px 14px;
  border: 1px solid var(--ink-100);
  border-radius: 8px;
  background: #fff;
  font-size: 13px;
  color: var(--ink-700);
  cursor: pointer;
  transition: border-color 0.18s, box-shadow 0.18s, transform 0.18s;

  &:hover:not(:disabled) {
    border-color: var(--brand-600);
    box-shadow: 0 2px 10px rgba(15, 23, 42, 0.06);
    transform: translateY(-1px);
  }

  &:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
}

.oauth-icon,
.oauth-fallback {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  border-radius: 5px;
  object-fit: contain;
}

.oauth-fallback {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--brand-600);
  color: #fff;
  font-size: 11px;
  font-weight: 600;
}

.hint {
  margin-top: 16px;
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
