<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { CopyDocument, Download, Key } from '@element-plus/icons-vue'
import OtpInput from '@/components/OtpInput.vue'
import { authApi, oauthApi, totpApi, type OAuthBinding, type OAuthProvider } from '@/api/auth'
import { membershipApi } from '@/api/membership'
import type { MembershipSnapshot } from '@/api/types'
import { fmtDate, fmtTime } from '@/utils/format'
import { IS_BASE } from '@/utils/edition'
import { useAuthStore } from '@/stores/auth'
import { SUPPORTED_LOCALES, normalizeLocale } from '@/i18n'

const auth = useAuthStore()
const { t } = useI18n()

/* ------------------------------------------------------------ 展示偏好 */
const TIMEZONES = [
  { value: 'Asia/Shanghai', label: 'Beijing (UTC+8)' },
  { value: 'Asia/Tokyo', label: 'Tokyo (UTC+9)' },
  { value: 'Europe/London', label: 'London' },
  { value: 'America/New_York', label: 'New York' },
  { value: 'UTC', label: 'UTC' },
]
const prefs = reactive({ locale: 'zh-CN', timezone: 'Asia/Shanghai' })
const prefsSaving = ref(false)
function initPrefs() {
  prefs.locale = normalizeLocale(auth.user?.locale)
  const tz = auth.user?.timezone
  prefs.timezone = tz && TIMEZONES.some((z) => z.value === tz) ? tz : 'Asia/Shanghai'
}
async function savePrefs() {
  if (prefsSaving.value) return
  prefsSaving.value = true
  try {
    await auth.savePreferences({ locale: prefs.locale, timezone: prefs.timezone })
    ElMessage.success(t('偏好已保存'))
  } catch {
    ElMessage.error(t('保存偏好失败'))
  } finally {
    prefsSaving.value = false
  }
}
const formRef = ref<FormInstance>()
const submitting = ref(false)

/* ------------------------------------------------------------ 会员 */
const membership = ref<MembershipSnapshot | null>(null)
const catalog = computed(() => membership.value?.levels ?? [])

/* ------------------------------------------------------------ 第三方账号 */
const oauthProviders = ref<OAuthProvider[]>([])
const bindings = ref<OAuthBinding[]>([])

function boundOf(name: string) {
  return bindings.value.find((b) => b.provider === name)
}

async function startBind(p: OAuthProvider) {
  try {
    const res = await oauthApi.bind(p.name)
    window.location.href = res.url
  } catch {
    ElMessage.error(t('发起绑定失败，请稍后重试'))
  }
}

async function unbind(provider: string, who: string) {
  try {
    await ElMessageBox.confirm(
      t('解绑后不能再使用 {who} 登录，确认解绑？', { who }),
      t('解绑第三方账号'),
      {
        type: 'warning',
        confirmButtonText: t('解绑'),
        cancelButtonText: t('取消'),
      },
    )
  } catch {
    return
  }
  await oauthApi.unbind(provider)
  bindings.value = bindings.value.filter((x) => x.provider !== provider)
  ElMessage.success(t('已解绑'))
}

onMounted(async () => {
  initPrefs()
  if (!IS_BASE) {
    try {
      membership.value = await membershipApi.me()
    } catch {
      // 会员模块可能未在当前服务进程启用，个人中心其余功能不受影响
    }
  }
  try {
    const [pv, bd] = await Promise.all([oauthApi.providers(), oauthApi.bindings()])
    oauthProviders.value = pv.providers ?? []
    bindings.value = bd ?? []
  } catch {
    // OAuth 未启用时接口仍返回空列表，异常不影响个人中心
  }
})

/** 布尔型权益用符号展示，避免出现裸的 true/false */
function benefitValue(v: string) {
  const s = (v ?? '').trim().toLowerCase()
  if (s === 'true') return '✓'
  if (s === 'false' || s === '') return '—'
  return v
}

function memberStatusText() {
  const m = membership.value
  if (!m) return t('加载中')
  if (m.state === 'expired') return m.expireAt ? t('{date} 已过期', { date: fmtDate(m.expireAt) }) : t('已过期')
  if (m.state === 'disabled') return t('等级已停用')
  if (m.state === 'active') {
    return m.daysLeft === -1 ? t('长期有效') : t('剩余 {n} 天', { n: m.daysLeft })
  }
  return t('未开通')
}

const pwd = reactive({ oldPassword: '', newPassword: '', confirm: '' })

const rules = computed<FormRules>(() => ({
  oldPassword: [{ required: true, message: t('请输入当前密码'), trigger: 'blur' }],
  newPassword: [
    { required: true, message: t('请输入新密码'), trigger: 'blur' },
    { min: 8, max: 64, message: t('密码长度 8-64 位'), trigger: 'blur' },
    { pattern: /^(?=.*[a-z])(?=.*[A-Z])(?=.*\d).+$/, message: t('需包含大小写字母与数字'), trigger: 'blur' },
  ],
  confirm: [
    { required: true, message: t('请再次输入新密码'), trigger: 'blur' },
    {
      validator: (_r, value, cb) =>
        value === pwd.newPassword ? cb() : cb(new Error(t('两次输入的密码不一致'))),
      trigger: 'blur',
    },
  ],
}))

/* ------------------------------------------------------------ 两步验证 */
type DialogMode = 'setup' | 'disable' | 'regen'

const dialog = reactive({
  visible: false,
  mode: 'setup' as DialogMode,
  step: 1,
  loading: false,
})
const setup = reactive({ secret: '', url: '', qrCode: '' })
const otp = ref('')
const disableCode = ref('')
const recoveryCodes = ref<string[]>([])

/** 密钥按 4 位分组，方便手动录入 */
const secretGroups = computed(() => {
  const s = setup.secret
  return s ? (s.match(/.{1,4}/g) ?? []).join(' ') : ''
})

function resetDialog(mode: DialogMode) {
  dialog.mode = mode
  dialog.step = 1
  dialog.visible = true
  dialog.loading = false
  otp.value = ''
  disableCode.value = ''
  recoveryCodes.value = []
  setup.secret = ''
  setup.url = ''
  setup.qrCode = ''
}

async function openSetup() {
  resetDialog('setup')
  dialog.loading = true
  try {
    const res = await totpApi.setup()
    setup.secret = res.secret
    setup.url = res.url
    setup.qrCode = res.qrCode
  } catch {
    dialog.visible = false
  } finally {
    dialog.loading = false
  }
}

function openDisable() {
  resetDialog('disable')
}

function openRegenerate() {
  resetDialog('regen')
  dialog.step = 2
}

async function copySecret() {
  try {
    await navigator.clipboard.writeText(setup.secret)
    ElMessage.success(t('密钥已复制'))
  } catch {
    ElMessage.warning(t('复制失败，请手动选择文本复制'))
  }
}

async function submitOtp() {
  if (!/^\d{6}$/.test(otp.value)) {
    ElMessage.warning(t('请输入 6 位动态验证码'))
    return
  }
  dialog.loading = true
  try {
    if (dialog.mode === 'setup') {
      const res = await totpApi.enable(otp.value)
      recoveryCodes.value = res.recoveryCodes
      dialog.step = 3
      await auth.loadProfile()
      ElMessage.success(t('两步验证已开启'))
    } else {
      const res = await totpApi.regenerate(otp.value)
      recoveryCodes.value = res.recoveryCodes
      dialog.step = 3
      await auth.loadProfile()
      ElMessage.success(t('恢复码已重新生成，旧码已失效'))
    }
  } catch {
    otp.value = ''
  } finally {
    dialog.loading = false
  }
}

async function submitDisable() {
  const code = disableCode.value.trim()
  if (!code) {
    ElMessage.warning(t('请输入动态验证码或恢复码'))
    return
  }
  dialog.loading = true
  try {
    await totpApi.disable(code)
    dialog.visible = false
    await auth.loadProfile()
    ElMessage.success(t('两步验证已关闭'))
  } finally {
    dialog.loading = false
  }
}

async function copyRecoveryCodes() {
  const text = recoveryCodes.value.join('\n')
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(t('恢复码已复制'))
  } catch {
    ElMessage.warning(t('复制失败，请手动选择文本复制'))
  }
}

function downloadRecoveryCodes() {
  const content = [
    t('Tenon 两步验证恢复码'),
    t('账号：{name}', { name: auth.user?.username ?? '' }),
    t('生成时间：{time}', { time: new Date().toLocaleString() }),
    '',
    ...recoveryCodes.value,
    '',
    t('每个恢复码只能使用一次，请离线保存。'),
  ].join('\n')
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `tenon-recovery-${auth.user?.username ?? 'codes'}.txt`
  a.click()
  URL.revokeObjectURL(url)
}

async function changePassword() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    await authApi.changePassword({ oldPassword: pwd.oldPassword, newPassword: pwd.newPassword })
    ElMessage.success(t('密码已更新，下次登录请使用新密码'))
    pwd.oldPassword = ''
    pwd.newPassword = ''
    pwd.confirm = ''
    formRef.value?.clearValidate()
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('个人中心') }}</h1>
        <p class="page-desc">{{ $t('查看账号信息与安全设置') }}</p>
      </div>
    </div>

    <div class="grid">
      <section class="card profile-card">
        <div class="profile-head">
          <div class="big-avatar">{{ auth.initial }}</div>
          <div>
            <h2>{{ auth.displayName }}</h2>
            <p>@{{ auth.user?.username }}</p>
          </div>
        </div>

        <dl class="kv">
          <div>
            <dt>{{ $t('角色') }}</dt>
            <dd>
              <el-tag v-for="r in auth.user?.roles ?? []" :key="r" type="primary" size="small">
                {{ $t(r) }}
              </el-tag>
              <span v-if="!(auth.user?.roles ?? []).length" class="dim">{{ $t('未分配') }}</span>
            </dd>
          </div>
          <div>
            <dt>{{ $t('部门') }}</dt>
            <dd>{{ auth.user?.deptName || '-' }}</dd>
          </div>
          <div>
            <dt>{{ $t('邮箱') }}</dt>
            <dd>{{ auth.user?.email || '-' }}</dd>
          </div>
          <div>
            <dt>{{ $t('手机号') }}</dt>
            <dd class="num">{{ auth.user?.phone || '-' }}</dd>
          </div>
          <div>
            <dt>{{ $t('最近登录') }}</dt>
            <dd>{{ auth.user?.lastLoginAt ? fmtTime(auth.user.lastLoginAt) : '-' }}</dd>
          </div>
          <div>
            <dt>{{ $t('双因素认证') }}</dt>
            <dd>
              <span class="status-dot" :class="auth.user?.totpEnabled ? 'is-on' : 'is-off'">
                {{ auth.user?.totpEnabled ? $t('已开启') : $t('未开启') }}
              </span>
            </dd>
          </div>
        </dl>
      </section>

      <div class="side-col">
        <section
          v-if="!IS_BASE"
          class="card member-card"
          :style="{ '--lv-color': membership?.level?.color || 'var(--brand-500)' }"
        >
          <div class="card-head">
            <h3>{{ $t('会员权益') }}</h3>
            <span
              v-if="membership"
              class="member-state"
              :class="{
                'is-active': membership.state === 'active',
                'is-expired': membership.state === 'expired' || membership.state === 'disabled',
              }"
            >
              {{ memberStatusText() }}
            </span>
          </div>
          <div class="card-body">
            <div v-if="membership?.member && membership.level" class="member-hero">
              <div class="member-mark">{{ membership.level.name.slice(0, 1) }}</div>
              <div class="member-hero-lines">
                <strong>{{ membership.level.name }}</strong>
                <p>
                  <template v-if="membership.daysLeft === -1">{{ $t('长期有效，无需续期') }}</template>
                  <template v-else>
                    {{ membership.expireAt ? $t('{date} 到期', { date: fmtDate(membership.expireAt) }) : '' }}
                  </template>
                </p>
              </div>
            </div>

            <div v-else class="member-hero is-empty">
              <div class="member-mark">—</div>
              <div class="member-hero-lines">
                <strong>{{ $t('非会员') }}</strong>
                <p>{{ $t('联系管理员开通会员后即可享受对应权益') }}</p>
              </div>
            </div>

            <div v-if="membership?.member && membership.level?.benefits?.length" class="benefit-list">
              <div v-for="b in membership.level.benefits" :key="b.key" class="benefit-item">
                <span>{{ b.label }}</span>
                <strong>{{ benefitValue(b.value) }}</strong>
              </div>
            </div>

            <div v-if="membership && !membership.member && catalog.length" class="catalog">
              <p class="catalog-title">{{ $t('可开通等级') }}</p>
              <div
                v-for="lv in catalog"
                :key="lv.id"
                class="catalog-item"
                :style="{ '--lv-color': lv.color || 'var(--brand-500)' }"
              >
                <span class="catalog-dot" />
                <span class="catalog-name">{{ lv.name }}</span>
                <span class="catalog-meta">{{ $t('{n} 项权益', { n: lv.benefits?.length ?? 0 }) }}</span>
              </div>
            </div>
          </div>
        </section>

        <section class="card totp-card">
          <div class="card-head">
            <h3>{{ $t('两步验证') }}</h3>
            <span class="status-dot" :class="auth.user?.totpEnabled ? 'is-on' : 'is-off'">
              {{ auth.user?.totpEnabled ? $t('已开启') : $t('未开启') }}
            </span>
          </div>
          <div class="card-body">
            <p class="totp-desc">
              {{ $t('开启后登录需要额外输入认证器中的 6 位动态码。支持 Google Authenticator、Microsoft Authenticator、Duo Mobile、Bitwarden、1Password、Authy。') }}
            </p>

            <div v-if="auth.user?.totpEnabled" class="totp-meta">
              <span>{{ $t('恢复码剩余') }}</span>
              <strong class="num">{{ auth.user?.recoveryCodesLeft ?? 0 }} / 10</strong>
              <span v-if="(auth.user?.recoveryCodesLeft ?? 0) <= 2" class="totp-warn">
                {{ $t('建议重新生成') }}
              </span>
            </div>

            <div class="totp-actions">
              <template v-if="auth.user?.totpEnabled">
                <el-button @click="openRegenerate">{{ $t('重新生成恢复码') }}</el-button>
                <el-button type="danger" plain @click="openDisable">{{ $t('关闭两步验证') }}</el-button>
              </template>
              <el-button v-else type="primary" :icon="Key" @click="openSetup">
                {{ $t('开启两步验证') }}
              </el-button>
            </div>
          </div>
        </section>

        <section v-if="oauthProviders.length" class="card oauth-card">
          <div class="card-head">
            <h3>{{ $t('第三方账号') }}</h3>
            <span class="dim">{{ $t('绑定后可直接使用该渠道登录') }}</span>
          </div>
          <div class="card-body">
            <div v-for="p in oauthProviders" :key="p.name" class="oauth-row">
              <div class="oauth-left">
                <img v-if="p.icon" :src="p.icon" alt="" class="oauth-icon" />
                <span v-else class="oauth-fallback">{{ (p.label || p.name).slice(0, 1).toUpperCase() }}</span>
                <div>
                  <div class="oauth-label">{{ p.label }}</div>
                  <div class="dim">
                    {{ boundOf(p.name) ? (boundOf(p.name)?.username || boundOf(p.name)?.email || $t('已绑定')) : $t('未绑定') }}
                  </div>
                </div>
              </div>
              <el-button
                v-if="boundOf(p.name)"
                size="small"
                plain
                type="danger"
                @click="unbind(p.name, boundOf(p.name)?.username || p.label)"
              >
                {{ $t('解绑') }}
              </el-button>
              <el-button v-else size="small" type="primary" plain @click="startBind(p)">{{ $t('绑定') }}</el-button>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-head"><h3>{{ $t('展示偏好') }}</h3></div>
          <div class="card-body">
            <el-form label-position="top">
              <el-form-item :label="$t('语言')">
                <el-select v-model="prefs.locale" style="width: 100%">
                  <el-option v-for="opt in SUPPORTED_LOCALES" :key="opt.value" :label="opt.label" :value="opt.value" />
                </el-select>
              </el-form-item>
              <el-form-item :label="$t('时区')">
                <el-select v-model="prefs.timezone" style="width: 100%">
                  <el-option v-for="z in TIMEZONES" :key="z.value" :label="z.label" :value="z.value" />
                </el-select>
              </el-form-item>
              <el-button type="primary" :loading="prefsSaving" @click="savePrefs">{{ $t('保存偏好') }}</el-button>
            </el-form>
            <p class="tip">{{ $t('语言与时区影响界面文案与时间展示；浏览器本地选择会即时生效。') }}</p>
          </div>
        </section>

        <section class="card">
          <div class="card-head"><h3>{{ $t('修改密码') }}</h3></div>
        <div class="card-body">
          <el-form ref="formRef" :model="pwd" :rules="rules" label-position="top">
            <el-form-item :label="$t('当前密码')" prop="oldPassword">
              <el-input v-model="pwd.oldPassword" type="password" show-password />
            </el-form-item>
            <el-form-item :label="$t('新密码')" prop="newPassword">
              <el-input v-model="pwd.newPassword" type="password" show-password :placeholder="$t('8-64 位，含大小写与数字')" />
            </el-form-item>
            <el-form-item :label="$t('确认新密码')" prop="confirm">
              <el-input v-model="pwd.confirm" type="password" show-password />
            </el-form-item>
            <el-button type="primary" :loading="submitting" @click="changePassword">
              {{ $t('更新密码') }}
            </el-button>
          </el-form>
          <p class="tip">{{ $t('修改密码后，其他设备上的登录状态不会被强制下线。') }}</p>
        </div>
      </section>
      </div>
    </div>
  <!-- 两步验证弹窗：绑定 / 重新生成恢复码 / 关闭 -->
  <el-dialog
    v-model="dialog.visible"
    :title="
      dialog.mode === 'setup'
        ? $t('绑定认证器')
        : dialog.mode === 'regen'
          ? $t('重新生成恢复码')
          : $t('关闭两步验证')
    "
    width="520px"
    :close-on-click-modal="false"
    append-to-body
  >
    <div v-loading="dialog.loading" class="dlg">
      <!-- 第一步：扫码 -->
      <template v-if="dialog.mode === 'setup' && dialog.step === 1">
        <p class="dlg-tip">{{ $t('用认证器扫描下方二维码，或手动录入密钥。') }}</p>
        <div class="qr-wrap">
          <img v-if="setup.qrCode" :src="setup.qrCode" :alt="$t('两步验证二维码')" />
        </div>
        <div class="secret-row">
          <code>{{ secretGroups }}</code>
          <el-button link type="primary" :icon="CopyDocument" @click="copySecret">
            {{ $t('复制') }}
          </el-button>
        </div>
        <p class="dlg-sub">
          {{ $t('支持 Google Authenticator、Microsoft Authenticator、Duo Mobile、Bitwarden、1Password、Authy 等，在应用内选择「添加账号 → 扫描二维码」即可。') }}
        </p>
        <div class="dlg-actions">
          <el-button @click="dialog.visible = false">{{ $t('取消') }}</el-button>
          <el-button type="primary" @click="dialog.step = 2">{{ $t('下一步') }}</el-button>
        </div>
      </template>

      <!-- 第二步：确认动态码 -->
      <template v-else-if="dialog.step === 2">
        <p class="dlg-tip">{{ $t('输入认证器中当前显示的 6 位动态码，确认设备已正确绑定。') }}</p>
        <OtpInput
          v-model="otp"
          class="otp-center"
          autofocus
          :disabled="dialog.loading"
          @complete="submitOtp"
        />
        <p class="dlg-sub">{{ $t('若反复提示验证码错误，请检查手机系统时间是否已开启自动同步。') }}</p>
        <div class="dlg-actions">
          <el-button v-if="dialog.mode === 'setup'" @click="dialog.step = 1">{{ $t('上一步') }}</el-button>
          <el-button @click="dialog.visible = false">{{ $t('取消') }}</el-button>
          <el-button type="primary" :loading="dialog.loading" @click="submitOtp">
            {{ $t('确认') }}
          </el-button>
        </div>
      </template>

      <!-- 第三步：恢复码 -->
      <template v-else-if="dialog.step === 3">
        <p class="dlg-tip">
          {{ $t('请立即保存以下恢复码。手机丢失时可代替动态码登录，每个恢复码只能使用一次。') }}
        </p>
        <div class="codes">
          <code v-for="c in recoveryCodes" :key="c">{{ c }}</code>
        </div>
        <div class="code-actions">
          <el-button :icon="CopyDocument" @click="copyRecoveryCodes">{{ $t('复制全部') }}</el-button>
          <el-button :icon="Download" @click="downloadRecoveryCodes">{{ $t('下载 txt') }}</el-button>
        </div>
        <p class="dlg-warn">{{ $t('关闭窗口后将无法再次查看，只能重新生成。') }}</p>
        <div class="dlg-actions">
          <el-button type="primary" @click="dialog.visible = false">{{ $t('我已保存') }}</el-button>
        </div>
      </template>

      <!-- 关闭 -->
      <template v-else>
        <p class="dlg-tip">{{ $t('关闭后登录将不再校验动态码，账号安全性会下降。') }}</p>
        <el-input
          v-model="disableCode"
          size="large"
          maxlength="32"
          :placeholder="$t('6 位动态码，或 10 位恢复码')"
          @keyup.enter="submitDisable"
        />
        <p class="dlg-sub">{{ $t('设备丢失时可用恢复码关闭；两者都没有时，请联系管理员重置。') }}</p>
        <div class="dlg-actions">
          <el-button @click="dialog.visible = false">{{ $t('取消') }}</el-button>
          <el-button type="danger" :loading="dialog.loading" @click="submitDisable">
            {{ $t('确认关闭') }}
          </el-button>
        </div>
      </template>
    </div>
  </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.grid {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}

.profile-card {
  padding: 24px;
}

.profile-head {
  display: flex;
  align-items: center;
  gap: 16px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--line-soft);

  h2 {
    font-size: 18px;
    font-weight: 600;
    color: var(--ink-900);
  }

  p {
    margin-top: 3px;
    font-size: 12.5px;
    color: var(--ink-400);
  }
}

.big-avatar {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  font-size: 22px;
  font-weight: 600;
  color: #fff;
  background: linear-gradient(135deg, #6366f1, #4338ca);
  box-shadow: 0 4px 14px rgba(79, 70, 229, 0.28);
}

.kv {
  margin: 0;
  padding-top: 6px;

  > div {
    display: flex;
    align-items: center;
    padding: 12px 0;
    border-bottom: 1px solid var(--line-soft);

    &:last-child {
      border-bottom: none;
    }
  }

  dt {
    width: 110px;
    flex-shrink: 0;
    font-size: 12.5px;
    color: var(--ink-500);
  }

  dd {
    margin: 0;
    font-size: 13px;
    color: var(--ink-800);
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
}

.tip {
  margin-top: 14px;
  font-size: 12px;
  color: var(--ink-400);
}

/* ------------------------------------------------------------ 两步验证 */
.side-col {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
}

.totp-desc {
  font-size: 12.5px;
  line-height: 1.75;
  color: var(--ink-500);
}

.totp-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  padding: 9px 12px;
  border-radius: var(--r-md);
  background: var(--ink-50);
  font-size: 12.5px;
  color: var(--ink-600);

  strong {
    color: var(--ink-900);
  }
}

.totp-warn {
  color: var(--warn-deep);
  font-size: 12px;
}

.totp-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}

/* ------------------------------------------------------------ 弹窗 */
.dlg {
  min-height: 110px;
}

.dlg-tip {
  font-size: 13px;
  line-height: 1.7;
  color: var(--ink-700);
}

.qr-wrap {
  display: flex;
  justify-content: center;
  margin: 16px 0 12px;

  img {
    width: 200px;
    height: 200px;
    padding: 6px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: #fff;
  }
}

.secret-row {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 8px;
  background: var(--ink-50);

  code {
    font-family: var(--font-mono);
    font-size: 13px;
    letter-spacing: 0.05em;
    color: var(--ink-800);
    word-break: break-all;
    text-align: center;
  }
}

.dlg-sub {
  margin-top: 12px;
  font-size: 12px;
  line-height: 1.7;
  color: var(--ink-400);
}

.dlg-warn {
  margin-top: 12px;
  font-size: 12px;
  color: var(--warn-deep);
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 20px;
}

.otp-center {
  justify-content: center;
}

.codes {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin-top: 14px;

  code {
    padding: 8px 10px;
    border-radius: 6px;
    background: var(--ink-50);
    font-family: var(--font-mono);
    font-size: 12.5px;
    letter-spacing: 0.08em;
    text-align: center;
    color: var(--ink-800);
  }
}

.code-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

/* -------------------------------------------------------------- 会员 */
.member-card {
  position: relative;
  overflow: hidden;

  &::before {
    content: '';
    position: absolute;
    inset: 0 0 auto 0;
    height: 3px;
    background: var(--lv-color);
  }
}

.member-state {
  font-size: 11.5px;
  color: var(--ink-400);

  &.is-active {
    color: var(--ok-deep);
  }

  &.is-expired {
    color: var(--warn-deep);
  }
}

.member-hero {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 14px;
}

.member-mark {
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  flex: none;
  border-radius: 12px;
  font-size: 16px;
  font-weight: 700;
  color: var(--lv-color);
  background: color-mix(in srgb, var(--lv-color) 12%, white);
  border: 1px solid color-mix(in srgb, var(--lv-color) 22%, white);
}

.member-hero.is-empty .member-mark {
  color: var(--ink-400);
  background: var(--ink-100);
  border-color: var(--line);
}

.member-hero-lines {
  min-width: 0;

  strong {
    font-size: 15px;
    font-weight: 650;
    color: var(--ink-900);
  }

  p {
    margin: 2px 0 0;
    font-size: 12px;
    color: var(--ink-400);
  }
}

.benefit-list {
  display: flex;
  flex-direction: column;
  gap: 9px;
  padding-top: 13px;
  border-top: 1px solid var(--line-soft);
}

.benefit-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 12.5px;

  span {
    color: var(--ink-500);
  }

  strong {
    font-weight: 600;
    color: var(--ink-800);
    text-align: right;
  }
}

.catalog {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px dashed var(--line);
}

.catalog-title {
  margin: 0 0 8px;
  font-size: 11.5px;
  color: var(--ink-400);
}

.catalog-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12.5px;
}

.catalog-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--lv-color);
}

.catalog-name {
  color: var(--ink-700);
}

.catalog-meta {
  margin-left: auto;
  font-size: 11.5px;
  color: var(--ink-400);
}

/* ------------------------------------------------------------ 第三方账号 */
.oauth-card {
  .oauth-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 0;
    border-bottom: 1px solid var(--ink-100);

    &:first-child { padding-top: 0; }
    &:last-child { padding-bottom: 0; border-bottom: none; }
  }

  .oauth-left {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }

  .oauth-label {
    font-size: 13.5px;
    font-weight: 550;
    color: var(--ink-800);
  }

  .oauth-icon,
  .oauth-fallback {
    width: 32px;
    height: 32px;
    flex-shrink: 0;
    border-radius: 8px;
    object-fit: contain;
  }

  .oauth-fallback {
    display: grid;
    place-items: center;
    background: var(--brand-600);
    color: #fff;
    font-size: 13px;
    font-weight: 600;
  }
}

@media (max-width: 980px) {
  .grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
