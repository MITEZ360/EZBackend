<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { CopyDocument, Download, Key } from '@element-plus/icons-vue'
import OtpInput from '@/components/OtpInput.vue'
import { authApi, totpApi } from '@/api/auth'
import { fmtTime } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const formRef = ref<FormInstance>()
const submitting = ref(false)

const pwd = reactive({ oldPassword: '', newPassword: '', confirm: '' })

const rules: FormRules = {
  oldPassword: [{ required: true, message: '请输入当前密码', trigger: 'blur' }],
  newPassword: [
    { required: true, message: '请输入新密码', trigger: 'blur' },
    { min: 8, max: 64, message: '密码长度 8-64 位', trigger: 'blur' },
    { pattern: /^(?=.*[a-z])(?=.*[A-Z])(?=.*\d).+$/, message: '需包含大小写字母与数字', trigger: 'blur' },
  ],
  confirm: [
    { required: true, message: '请再次输入新密码', trigger: 'blur' },
    {
      validator: (_r, value, cb) =>
        value === pwd.newPassword ? cb() : cb(new Error('两次输入的密码不一致')),
      trigger: 'blur',
    },
  ],
}

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
    ElMessage.success('密钥已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择文本复制')
  }
}

async function submitOtp() {
  if (!/^\d{6}$/.test(otp.value)) {
    ElMessage.warning('请输入 6 位动态验证码')
    return
  }
  dialog.loading = true
  try {
    if (dialog.mode === 'setup') {
      const res = await totpApi.enable(otp.value)
      recoveryCodes.value = res.recoveryCodes
      dialog.step = 3
      await auth.loadProfile()
      ElMessage.success('两步验证已开启')
    } else {
      const res = await totpApi.regenerate(otp.value)
      recoveryCodes.value = res.recoveryCodes
      dialog.step = 3
      await auth.loadProfile()
      ElMessage.success('恢复码已重新生成，旧码已失效')
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
    ElMessage.warning('请输入动态验证码或恢复码')
    return
  }
  dialog.loading = true
  try {
    await totpApi.disable(code)
    dialog.visible = false
    await auth.loadProfile()
    ElMessage.success('两步验证已关闭')
  } finally {
    dialog.loading = false
  }
}

async function copyRecoveryCodes() {
  const text = recoveryCodes.value.join('\n')
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('恢复码已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择文本复制')
  }
}

function downloadRecoveryCodes() {
  const content = [
    'Keel 两步验证恢复码',
    `账号：${auth.user?.username ?? ''}`,
    `生成时间：${new Date().toLocaleString()}`,
    '',
    ...recoveryCodes.value,
    '',
    '每个恢复码只能使用一次，请离线保存。',
  ].join('\n')
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `keel-recovery-${auth.user?.username ?? 'codes'}.txt`
  a.click()
  URL.revokeObjectURL(url)
}

async function changePassword() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    await authApi.changePassword({ oldPassword: pwd.oldPassword, newPassword: pwd.newPassword })
    ElMessage.success('密码已更新，请使用新密码重新登录')
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
        <h1 class="page-title">个人中心</h1>
        <p class="page-desc">查看账号信息与安全设置</p>
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
            <dt>角色</dt>
            <dd>
              <el-tag v-for="r in auth.user?.roles ?? []" :key="r" type="primary" size="small">
                {{ r }}
              </el-tag>
              <span v-if="!(auth.user?.roles ?? []).length" class="dim">未分配</span>
            </dd>
          </div>
          <div>
            <dt>部门</dt>
            <dd>{{ auth.user?.deptName || '-' }}</dd>
          </div>
          <div>
            <dt>邮箱</dt>
            <dd>{{ auth.user?.email || '-' }}</dd>
          </div>
          <div>
            <dt>手机号</dt>
            <dd class="num">{{ auth.user?.phone || '-' }}</dd>
          </div>
          <div>
            <dt>最近登录</dt>
            <dd>{{ auth.user?.lastLoginAt ? fmtTime(auth.user.lastLoginAt) : '-' }}</dd>
          </div>
          <div>
            <dt>双因素认证</dt>
            <dd>
              <span class="status-dot" :class="auth.user?.totpEnabled ? 'is-on' : 'is-off'">
                {{ auth.user?.totpEnabled ? '已开启' : '未开启' }}
              </span>
            </dd>
          </div>
        </dl>
      </section>

      <div class="side-col">
        <section class="card totp-card">
          <div class="card-head">
            <h3>两步验证</h3>
            <span class="status-dot" :class="auth.user?.totpEnabled ? 'is-on' : 'is-off'">
              {{ auth.user?.totpEnabled ? '已开启' : '未开启' }}
            </span>
          </div>
          <div class="card-body">
            <p class="totp-desc">
              开启后登录需要额外输入认证器中的 6 位动态码。支持 Google
              Authenticator、Microsoft Authenticator、Duo Mobile、Bitwarden、1Password、Authy。
            </p>

            <div v-if="auth.user?.totpEnabled" class="totp-meta">
              <span>恢复码剩余</span>
              <strong class="num">{{ auth.user?.recoveryCodesLeft ?? 0 }} / 10</strong>
              <span v-if="(auth.user?.recoveryCodesLeft ?? 0) <= 2" class="totp-warn">
                建议重新生成
              </span>
            </div>

            <div class="totp-actions">
              <template v-if="auth.user?.totpEnabled">
                <el-button @click="openRegenerate">重新生成恢复码</el-button>
                <el-button type="danger" plain @click="openDisable">关闭两步验证</el-button>
              </template>
              <el-button v-else type="primary" :icon="Key" @click="openSetup">
                开启两步验证
              </el-button>
            </div>
          </div>
        </section>

        <section class="card">
          <div class="card-head"><h3>修改密码</h3></div>
        <div class="card-body">
          <el-form ref="formRef" :model="pwd" :rules="rules" label-position="top">
            <el-form-item label="当前密码" prop="oldPassword">
              <el-input v-model="pwd.oldPassword" type="password" show-password />
            </el-form-item>
            <el-form-item label="新密码" prop="newPassword">
              <el-input v-model="pwd.newPassword" type="password" show-password placeholder="8-64 位，含大小写与数字" />
            </el-form-item>
            <el-form-item label="确认新密码" prop="confirm">
              <el-input v-model="pwd.confirm" type="password" show-password />
            </el-form-item>
            <el-button type="primary" :loading="submitting" @click="changePassword">
              更新密码
            </el-button>
          </el-form>
          <p class="tip">修改密码后，其他设备上的登录状态不会被强制下线。</p>
        </div>
      </section>
      </div>
    </div>
  <!-- 两步验证弹窗：绑定 / 重新生成恢复码 / 关闭 -->
  <el-dialog
    v-model="dialog.visible"
    :title="
      dialog.mode === 'setup'
        ? '绑定认证器'
        : dialog.mode === 'regen'
          ? '重新生成恢复码'
          : '关闭两步验证'
    "
    width="520px"
    :close-on-click-modal="false"
    append-to-body
  >
    <div v-loading="dialog.loading" class="dlg">
      <!-- 第一步：扫码 -->
      <template v-if="dialog.mode === 'setup' && dialog.step === 1">
        <p class="dlg-tip">用认证器扫描下方二维码，或手动录入密钥。</p>
        <div class="qr-wrap">
          <img v-if="setup.qrCode" :src="setup.qrCode" alt="两步验证二维码" />
        </div>
        <div class="secret-row">
          <code>{{ secretGroups }}</code>
          <el-button link type="primary" :icon="CopyDocument" @click="copySecret">
            复制
          </el-button>
        </div>
        <p class="dlg-sub">
          支持 Google Authenticator、Microsoft Authenticator、Duo Mobile、Bitwarden、
          1Password、Authy 等，在应用内选择「添加账号 → 扫描二维码」即可。
        </p>
        <div class="dlg-actions">
          <el-button @click="dialog.visible = false">取消</el-button>
          <el-button type="primary" @click="dialog.step = 2">下一步</el-button>
        </div>
      </template>

      <!-- 第二步：确认动态码 -->
      <template v-else-if="dialog.step === 2">
        <p class="dlg-tip">输入认证器中当前显示的 6 位动态码，确认设备已正确绑定。</p>
        <OtpInput
          v-model="otp"
          class="otp-center"
          autofocus
          :disabled="dialog.loading"
          @complete="submitOtp"
        />
        <p class="dlg-sub">若反复提示验证码错误，请检查手机系统时间是否已开启自动同步。</p>
        <div class="dlg-actions">
          <el-button v-if="dialog.mode === 'setup'" @click="dialog.step = 1">上一步</el-button>
          <el-button @click="dialog.visible = false">取消</el-button>
          <el-button type="primary" :loading="dialog.loading" @click="submitOtp">
            确认
          </el-button>
        </div>
      </template>

      <!-- 第三步：恢复码 -->
      <template v-else-if="dialog.step === 3">
        <p class="dlg-tip">
          请立即保存以下恢复码。手机丢失时可代替动态码登录，每个恢复码只能使用一次。
        </p>
        <div class="codes">
          <code v-for="c in recoveryCodes" :key="c">{{ c }}</code>
        </div>
        <div class="code-actions">
          <el-button :icon="CopyDocument" @click="copyRecoveryCodes">复制全部</el-button>
          <el-button :icon="Download" @click="downloadRecoveryCodes">下载 txt</el-button>
        </div>
        <p class="dlg-warn">关闭窗口后将无法再次查看，只能重新生成。</p>
        <div class="dlg-actions">
          <el-button type="primary" @click="dialog.visible = false">我已保存</el-button>
        </div>
      </template>

      <!-- 关闭 -->
      <template v-else>
        <p class="dlg-tip">关闭后登录将不再校验动态码，账号安全性会下降。</p>
        <el-input
          v-model="disableCode"
          size="large"
          maxlength="32"
          placeholder="6 位动态码，或 10 位恢复码"
          @keyup.enter="submitDisable"
        />
        <p class="dlg-sub">设备丢失时可用恢复码关闭；两者都没有时，请联系管理员重置。</p>
        <div class="dlg-actions">
          <el-button @click="dialog.visible = false">取消</el-button>
          <el-button type="danger" :loading="dialog.loading" @click="submitDisable">
            确认关闭
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

@media (max-width: 980px) {
  .grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
