<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { authApi } from '@/api/auth'
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

@media (max-width: 980px) {
  .grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
