<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { ArrowDown, Delete, Download, MoreFilled, Plus, Refresh, Search, Upload } from '@element-plus/icons-vue'
import { userApi, type CreateUserPayload, type ImportResult } from '@/api/user'
import { membershipApi, type GrantMembershipPayload } from '@/api/membership'
import { deptApi, roleApi } from '@/api/system'
import type { DeptNode, MembershipLevel, RoleBrief, UserRow } from '@/api/types'
import { fmtDate, fmtTime, fromNow, saveBlob } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()

const rows = ref<UserRow[]>([])
const total = ref(0)
const loading = ref(false)
const exporting = ref(false)

const deptTree = ref<DeptNode[]>([])
const roles = ref<RoleBrief[]>([])

const query = reactive({
  page: 1,
  page_size: 10,
  keyword: '',
  status: undefined as number | undefined,
  deptId: '',
  membershipState: '' as '' | 'member' | 'expired' | 'none',
  membershipLevelId: '',
})

const deptOptions = computed(() => toOptions(deptTree.value))

function toOptions(nodes: DeptNode[]): Record<string, unknown>[] {
  return nodes.map((n) => ({
    value: n.id,
    label: n.name,
    children: n.children?.length ? toOptions(n.children) : undefined,
  }))
}

async function load() {
  loading.value = true
  try {
    const res = await userApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
      status: query.status,
      deptId: query.deptId || undefined,
      membershipState: query.membershipState || undefined,
      membershipLevelId: query.membershipLevelId || undefined,
    })
    rows.value = res.list ?? []
    total.value = res.total ?? 0
  } finally {
    loading.value = false
  }
}

function reload() {
  query.page = 1
  load()
}

function resetQuery() {
  query.keyword = ''
  query.status = undefined
  query.deptId = ''
  query.membershipState = ''
  query.membershipLevelId = ''
  reload()
}

async function onExport() {
  exporting.value = true
  try {
    const blob = await userApi.exportExcel({
      keyword: query.keyword || undefined,
      status: query.status,
      deptId: query.deptId || undefined,
      membershipState: query.membershipState || undefined,
      membershipLevelId: query.membershipLevelId || undefined,
    })
    saveBlob(blob, `用户列表_${fmtDate(new Date())}.xlsx`)
    ElMessage.success('已导出')
  } finally {
    exporting.value = false
  }
}

/* ------------------------------------------------------------ 表单弹窗 */
const formVisible = ref(false)
const editingId = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const form = reactive<CreateUserPayload>({
  username: '',
  password: '',
  nickname: '',
  realName: '',
  email: '',
  phone: '',
  deptId: '',
  roleIds: [],
  gender: 0,
  status: 1,
  remark: '',
})

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 2, max: 32, message: '用户名 2-32 位', trigger: 'blur' },
    { pattern: /^[a-zA-Z][a-zA-Z0-9_]{1,31}$/, message: '以字母开头，仅含字母、数字、下划线', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入初始密码', trigger: 'blur' },
    { min: 8, max: 64, message: '密码长度 8-64 位', trigger: 'blur' },
    {
      pattern: /^(?=.*[a-z])(?=.*[A-Z])(?=.*\d).+$/,
      message: '需同时包含大小写字母与数字',
      trigger: 'blur',
    },
  ],
  nickname: [{ required: true, message: '请输入昵称', trigger: 'blur' }],
  email: [{ type: 'email', message: '邮箱格式不正确', trigger: 'blur' }],
  phone: [{ pattern: /^1[3-9]\d{9}$/, message: '手机号格式不正确', trigger: 'blur' }],
}

function resetForm() {
  Object.assign(form, {
    username: '',
    password: '',
    nickname: '',
    realName: '',
    email: '',
    phone: '',
    deptId: '',
    roleIds: [],
    gender: 0,
    status: 1,
    remark: '',
  })
  formRef.value?.clearValidate()
}

function openCreate() {
  editingId.value = ''
  resetForm()
  formVisible.value = true
}

let editSeq = 0
async function openEdit(row: UserRow) {
  const seq = ++editSeq
  try {
    // 列表里的手机号是脱敏的，必须先取详情再回填；成功后才打开弹窗，
    // 避免请求期间显示空表单，seq 防止快速切换行时旧响应覆盖新数据。
    const detail = await userApi.get(row.id)
    if (seq !== editSeq) return
    editingId.value = row.id
    resetForm()
    Object.assign(form, {
      username: detail.username,
      password: '',
      nickname: detail.nickname,
      realName: detail.realName ?? '',
      email: detail.email ?? '',
      phone: detail.phone ?? '',
      deptId: detail.deptId ?? '',
      roleIds: (detail as unknown as { roleIds?: string[] }).roleIds ?? [],
      gender: detail.gender ?? 0,
      status: 1,
      remark: (detail as unknown as { remark?: string }).remark ?? '',
    })
    formVisible.value = true
  } catch {
    ElMessage.error('读取用户详情失败')
  }
}

async function submitForm() {
  form.nickname = form.nickname?.trim() ?? ''
  form.realName = form.realName?.trim() ?? ''
  form.email = form.email?.trim() ?? ''
  form.phone = form.phone?.trim() ?? ''
  form.remark = form.remark?.trim() ?? ''
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    if (isEdit.value) {
      await userApi.update(editingId.value, {
        nickname: form.nickname,
        realName: form.realName,
        email: form.email,
        phone: form.phone,
        deptId: form.deptId,
        roleIds: form.roleIds,
        gender: form.gender,
        remark: form.remark,
      })
      ElMessage.success('已保存')
    } else {
      await userApi.create({ ...form })
      ElMessage.success('用户已创建')
    }
    formVisible.value = false
    load()
  } finally {
    submitting.value = false
  }
}

/* ---------------------------------------------------------- 会员设置 */
const memberLevels = ref<MembershipLevel[]>([])
const memberVisible = ref(false)
const memberTarget = ref<UserRow | null>(null)
const memberSaving = ref(false)
const memberForm = reactive({
  levelId: '',
  mode: 'days' as 'days' | 'date' | 'forever',
  days: 365,
  expireAt: '',
})

const previewExpire = computed(() => {
  if (memberForm.mode === 'forever') return '长期有效'
  if (memberForm.mode === 'date') return memberForm.expireAt || '请选择日期'
  const d = new Date()
  d.setDate(d.getDate() + (memberForm.days || 0))
  return fmtDate(d)
})

function onMemberLevelChange(id: string) {
  const lv = memberLevels.value.find((l) => l.id === id)
  if (!lv) return
  if (lv.defaultDays > 0) {
    memberForm.mode = 'days'
    memberForm.days = lv.defaultDays
  } else {
    memberForm.mode = 'forever'
  }
}

function openMember(row: UserRow) {
  memberTarget.value = row
  const usable = memberLevels.value.filter((l) => l.status === 1)
  const preferred = memberLevels.value.find((l) => l.id === row.membershipLevelId) || usable[0]
  memberForm.levelId = preferred?.id || ''
  memberForm.mode = 'days'
  memberForm.days = preferred?.defaultDays || 365
  memberForm.expireAt = ''
  if (memberForm.days <= 0) memberForm.mode = 'forever'
  memberVisible.value = true
}

async function submitMember() {
  const target = memberTarget.value
  if (!target) return
  if (!memberForm.levelId) {
    ElMessage.warning('请选择会员等级')
    return
  }
  const payload: GrantMembershipPayload = { levelId: memberForm.levelId }
  if (memberForm.mode === 'days') {
    payload.days = memberForm.days
  } else if (memberForm.mode === 'date') {
    if (!memberForm.expireAt) {
      ElMessage.warning('请选择到期日期')
      return
    }
    payload.expireAt = memberForm.expireAt
  } else {
    payload.permanent = true
  }

  memberSaving.value = true
  try {
    await membershipApi.grant(target.id, payload)
    ElMessage.success('会员已更新')
    memberVisible.value = false
    await load()
  } finally {
    memberSaving.value = false
  }
}

async function revokeMember() {
  const target = memberTarget.value
  if (!target) return
  try {
    await ElMessageBox.confirm(
      `取消后「${target.nickname}」立即变为非会员，相关权益同时失效。确定继续吗？`,
      '取消会员',
      { confirmButtonText: '取消会员', cancelButtonText: '返回', type: 'warning' },
    )
  } catch {
    return
  }
  await membershipApi.revoke(target.id)
  ElMessage.success('已取消会员')
  memberVisible.value = false
  await load()
}

/* ---------------------------------------------------------- 行内操作 */
async function toggleStatus(row: UserRow) {
  const next = row.status === 1 ? 0 : 1
  const action = next === 1 ? '启用' : '停用'
  try {
    await ElMessageBox.confirm(`确定${action}用户「${row.nickname}」？`, `${action}确认`, {
      confirmButtonText: action,
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
  await userApi.setStatus(row.id, next)
  ElMessage.success(`已${action}`)
  load()
}

async function resetPassword(row: UserRow) {
  let value: string
  try {
    const res = await ElMessageBox.prompt(
      `为用户「${row.nickname}」设置新密码，需包含大小写字母与数字，至少 8 位。`,
      '重置密码',
      {
        confirmButtonText: '确认重置',
        cancelButtonText: '取消',
        inputType: 'password',
        inputPattern: /^(?=.*[a-z])(?=.*[A-Z])(?=.*\d).{8,64}$/,
        inputErrorMessage: '密码需 8-64 位且包含大小写字母与数字',
      },
    )
    value = res.value
  } catch {
    return
  }
  await userApi.resetPassword(row.id, value)
  ElMessage.success('密码已重置')
}

async function removeUser(row: UserRow) {
  try {
    await ElMessageBox.confirm(
      `删除后该账号将无法登录，且不可恢复。确定删除「${row.nickname}」吗？`,
      '删除用户',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'error' },
    )
  } catch {
    return
  }
  await userApi.remove(row.id)
  ElMessage.success('已删除')
  if (rows.value.length === 1 && query.page > 1) query.page -= 1
  load()
}

function onCommand(cmd: string, row: UserRow) {
  if (cmd === 'member') return openMember(row)
  if (cmd === 'reset') return resetPassword(row)
  if (cmd === 'totp') return resetTOTP(row)
  if (cmd === 'status') return toggleStatus(row)
  if (cmd === 'delete') return removeUser(row)
}

async function resetTOTP(row: UserRow) {
  try {
    await ElMessageBox.confirm(
      `将清除「${row.nickname}」的两步验证配置，并强制其重新登录后重新绑定认证器。确定继续吗？`,
      '重置两步验证',
      { confirmButtonText: '重置', cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return
  }
  await userApi.resetTOTP(row.id)
  ElMessage.success('已重置两步验证')
  await load()
}

/* ---------------------------------------------------------- 导入 / 批量 */
const selected = ref<UserRow[]>([])
const batchDeleting = ref(false)
const importing = ref(false)
const importInput = ref<HTMLInputElement>()

function onSelectionChange(rows: UserRow[]) {
  selected.value = rows
}

async function batchRemove() {
  const ids = selected.value.map((r) => r.id)
  if (!ids.length) return
  try {
    await ElMessageBox.confirm(
      `已选择 ${ids.length} 个账号，删除后立即失效且不可恢复。确定继续吗？`,
      '批量删除',
      { confirmButtonText: `删除 ${ids.length} 项`, cancelButtonText: '取消', type: 'error' },
    )
  } catch {
    return
  }
  batchDeleting.value = true
  try {
    const res = await userApi.batchDelete(ids)
    if (res.skipped?.length) {
      ElMessage.warning(`已删除 ${res.deleted} 个，${res.skipped.length} 个被跳过（内置管理员或当前账号不可删除）`)
    } else {
      ElMessage.success(`已删除 ${res.deleted} 个账号`)
    }
    if (rows.value.length === ids.length && query.page > 1) query.page -= 1
    load()
  } finally {
    batchDeleting.value = false
  }
}

async function downloadTemplate() {
  const blob = await userApi.importTemplate()
  saveBlob(blob, '用户导入模板.xlsx')
}

function pickImport() {
  importInput.value?.click()
}

async function onImportPick(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  importing.value = true
  try {
    const res = await userApi.importExcel(file)
    importResult.value = res
    importVisible.value = true
    if (res.success) load()
  } finally {
    importing.value = false
  }
}

const importVisible = ref(false)
const importResult = ref<ImportResult | null>(null)

function closeImport() {
  importVisible.value = false
  importResult.value = null
}

function onImportCommand(cmd: string) {
  if (cmd === 'import') pickImport()
  if (cmd === 'template') downloadTemplate()
}

function canSelect(row: UserRow) {
  return !row.isSuper
}

function roleNames(row: UserRow) {
  return row.roles?.map((r) => r.name) ?? []
}

onMounted(async () => {
  await Promise.all([
    load(),
    deptApi.options().then((d) => (deptTree.value = d ?? [])).catch(() => undefined),
    roleApi.options().then((r) => (roles.value = r ?? [])).catch(() => undefined),
    membershipApi.catalog().then((l) => (memberLevels.value = l ?? [])).catch(() => undefined),
  ])
})
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">用户管理</h1>
        <p class="page-desc">管理账号、所属部门与角色授权</p>
      </div>
      <div class="page-actions">
        <el-dropdown trigger="click" @command="onImportCommand">
          <el-button :icon="Upload" :loading="importing">
            导入<el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="import" :disabled="!auth.can('system:user:create')">
                从 Excel 导入
              </el-dropdown-item>
              <el-dropdown-item command="template" divided>下载导入模板</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button :icon="Download" :loading="exporting" @click="onExport">导出</el-button>
        <el-button
          v-perm="'system:user:create'"
          type="primary"
          :icon="Plus"
          @click="openCreate"
        >
          新增用户
        </el-button>
      </div>
    </div>

    <input ref="importInput" type="file" accept=".xlsx" hidden @change="onImportPick" />

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            placeholder="搜索用户名 / 昵称 / 手机号"
            :prefix-icon="Search"
            clearable
            style="width: 248px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-tree-select
            v-model="query.deptId"
            :data="deptOptions"
            placeholder="全部部门"
            check-strictly
            clearable
            style="width: 160px"
            @change="reload"
          />
          <el-select
            v-model="query.status"
            placeholder="全部状态"
            clearable
            style="width: 118px"
            @change="reload"
          >
            <el-option label="正常" :value="1" />
            <el-option label="停用" :value="0" />
          </el-select>
          <el-select
            v-model="query.membershipState"
            placeholder="会员状态"
            clearable
            style="width: 128px"
            @change="reload"
          >
            <el-option label="有效会员" value="member" />
            <el-option label="已过期" value="expired" />
            <el-option label="非会员" value="none" />
          </el-select>
          <el-button @click="reload">查询</el-button>
          <el-button text @click="resetQuery">重置</el-button>
        </div>
        <div class="toolbar-right">
          <el-button
            v-if="selected.length"
            type="danger"
            plain
            :icon="Delete"
            :loading="batchDeleting"
            @click="batchRemove"
          >
            删除选中 {{ selected.length }}
          </el-button>
          <el-button :icon="Refresh" circle title="刷新" @click="load" />
        </div>
      </div>

      <el-table
        v-loading="loading"
        :data="rows"
        row-key="id"
        :show-overflow-tooltip="true"
        @selection-change="onSelectionChange"
      >
        <el-table-column type="selection" width="44" :selectable="canSelect" />
        <el-table-column label="用户" min-width="200">
          <template #default="{ row }">
            <div class="user-cell">
              <div class="avatar-dot">{{ (row.nickname || row.username).slice(0, 1) }}</div>
              <div class="user-lines">
                <span class="u-name">
                  {{ row.nickname }}
                  <em v-if="row.isSuper" class="badge">超管</em>
                </span>
                <span class="u-sub">@{{ row.username }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="部门" min-width="120">
          <template #default="{ row }">
            <span class="muted">{{ row.dept?.name || '-' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="角色" min-width="150">
          <template #default="{ row }">
            <div class="role-tags">
              <el-tag v-for="name in roleNames(row)" :key="name" type="primary" size="small">
                {{ name }}
              </el-tag>
              <span v-if="!roleNames(row).length" class="dim">未分配</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="会员" width="136">
          <template #default="{ row }">
            <div
              v-if="row.membership"
              class="member-cell"
              :style="{ '--lv-color': row.membership.color || '#6366f1' }"
            >
              <span class="member-dot" />
              <div class="member-lines">
                <span class="member-name">{{ row.membership.name }}</span>
                <span class="member-sub">
                  <template v-if="row.membershipState === 'expired'">
                    {{ row.membershipExpireAt ? `${fmtDate(row.membershipExpireAt)} 已过期` : '已过期' }}
                  </template>
                  <template v-else-if="row.membershipState === 'disabled'">等级已停用</template>
                  <template v-else-if="row.membershipExpireAt">
                    {{ fmtDate(row.membershipExpireAt) }} 到期
                  </template>
                  <template v-else>长期有效</template>
                </span>
              </div>
            </div>
            <span v-else class="dim">非会员</span>
          </template>
        </el-table-column>

        <el-table-column label="手机号" width="140">
          <template #default="{ row }">
            <span class="num muted">{{ row.phone || '-' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="状态" width="96">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
              {{ row.status === 1 ? '正常' : '停用' }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="最后登录" width="150">
          <template #default="{ row }">
            <template v-if="row.lastLoginAt">
              <div class="cell-time" :title="fmtTime(row.lastLoginAt)">
                {{ fromNow(row.lastLoginAt) }}
              </div>
              <div class="cell-sub num">{{ row.lastLoginIp || '-' }}</div>
            </template>
            <span v-else class="dim">从未登录</span>
          </template>
        </el-table-column>

        <el-table-column label="" width="110" align="right" fixed="right">
          <template #default="{ row }">
            <el-button
              v-perm="'system:user:update'"
              link
              type="primary"
              @click="openEdit(row)"
            >
              编辑
            </el-button>
            <el-dropdown trigger="click" @command="(c: string) => onCommand(c, row)">
              <el-button link type="primary" :icon="MoreFilled" />
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item
                    command="member"
                    :disabled="!auth.can('system:user:grant-membership')"
                  >
                    设置会员
                  </el-dropdown-item>
                  <el-dropdown-item command="reset" divided :disabled="!auth.can('system:user:reset')">
                    重置密码
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="totp"
                    :disabled="
                      !auth.can('system:user:reset-totp') || !row.totpEnabled || row.isSuper
                    "
                  >
                    重置两步验证
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="status"
                    :disabled="!auth.can('system:user:update') || row.isSuper"
                  >
                    {{ row.status === 1 ? '停用账号' : '启用账号' }}
                  </el-dropdown-item>
                  <el-dropdown-item
                    command="delete"
                    divided
                    :disabled="!auth.can('system:user:delete') || row.isSuper"
                  >
                    删除用户
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <span class="total">共 {{ total }} 条</span>
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="sizes, prev, pager, next, jumper"
          background
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </div>

    <!-- 新增 / 编辑 -->
    <el-dialog
      v-model="formVisible"
      :title="isEdit ? '编辑用户' : '新增用户'"
      width="640px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="form-grid">
          <el-form-item label="用户名" prop="username">
            <el-input v-model="form.username" :disabled="isEdit" placeholder="登录账号" />
          </el-form-item>

          <el-form-item v-if="!isEdit" label="初始密码" prop="password">
            <el-input v-model="form.password" type="password" show-password placeholder="8-64 位，含大小写与数字" />
          </el-form-item>

          <el-form-item label="昵称" prop="nickname">
            <el-input v-model="form.nickname" placeholder="用于界面展示" />
          </el-form-item>

          <el-form-item label="姓名" prop="realName">
            <el-input v-model="form.realName" placeholder="选填" />
          </el-form-item>

          <el-form-item label="邮箱" prop="email">
            <el-input v-model="form.email" placeholder="选填" />
          </el-form-item>

          <el-form-item label="手机号" prop="phone">
            <el-input v-model="form.phone" placeholder="选填" />
          </el-form-item>

          <el-form-item label="所属部门">
            <el-tree-select
              v-model="form.deptId"
              :data="deptOptions"
              placeholder="请选择部门"
              check-strictly
              clearable
            />
          </el-form-item>

          <el-form-item label="性别">
            <el-radio-group v-model="form.gender">
              <el-radio :value="0">未知</el-radio>
              <el-radio :value="1">男</el-radio>
              <el-radio :value="2">女</el-radio>
            </el-radio-group>
          </el-form-item>
        </div>

        <el-form-item label="角色">
          <el-select v-model="form.roleIds" multiple placeholder="选择角色" style="width: 100%">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>

        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
        </el-form-item>

        <el-form-item v-if="!isEdit" label="账号状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
          <span class="switch-hint">{{ form.status === 1 ? '创建后可立即登录' : '创建后处于停用状态' }}</span>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">保存</el-button>
      </template>
    </el-dialog>

    <!-- 导入结果 -->
    <el-dialog
      v-model="importVisible"
      title="导入结果"
      width="640px"
      append-to-body
      @closed="closeImport"
    >
      <div v-if="importResult" class="import-summary">
        <div class="is-item">
          <span class="is-label">总行数</span>
          <b class="is-value">{{ importResult.total }}</b>
        </div>
        <div class="is-item is-ok">
          <span class="is-label">导入成功</span>
          <b class="is-value">{{ importResult.success }}</b>
        </div>
        <div class="is-item" :class="{ 'is-fail': importResult.failed }">
          <span class="is-label">导入失败</span>
          <b class="is-value">{{ importResult.failed }}</b>
        </div>
      </div>

      <div v-if="importResult?.errors?.length" class="import-errors">
        <div class="ie-head">
          <span>失败明细</span>
          <span v-if="importResult.truncated" class="dim">仅展示前 100 条</span>
        </div>
        <el-table :data="importResult.errors" size="small" max-height="300">
          <el-table-column prop="row" label="行号" width="70" align="center" />
          <el-table-column prop="username" label="用户名" width="150" />
          <el-table-column prop="message" label="失败原因" />
        </el-table>
      </div>

      <p v-else-if="importResult" class="import-ok">全部账号导入成功，可立即登录。</p>

      <template #footer>
        <el-button @click="downloadTemplate">下载模板</el-button>
        <el-button type="primary" @click="closeImport">知道了</el-button>
      </template>
    </el-dialog>

    <!-- 设置会员 -->
    <el-dialog
      v-model="memberVisible"
      title="设置会员"
      width="540px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <div v-if="memberTarget" class="member-dialog">
        <div class="member-user">
          <div class="avatar-dot">
            {{ (memberTarget.nickname || memberTarget.username).slice(0, 1) }}
          </div>
          <div class="member-user-lines">
            <strong>{{ memberTarget.nickname }}</strong>
            <span class="dim">@{{ memberTarget.username }}</span>
          </div>
          <el-tag
            v-if="memberTarget.membership && memberTarget.membershipState !== 'expired'"
            size="small"
            effect="plain"
            :style="{ color: memberTarget.membership.color, borderColor: memberTarget.membership.color }"
          >
            当前 {{ memberTarget.membership.name }}
          </el-tag>
          <span v-else class="dim">当前非会员</span>
        </div>

        <el-form label-position="top">
          <el-form-item label="会员等级">
            <el-select
              v-model="memberForm.levelId"
              placeholder="选择会员等级"
              style="width: 100%"
              @change="onMemberLevelChange"
            >
              <el-option
                v-for="lv in memberLevels"
                :key="lv.id"
                :label="lv.name"
                :value="lv.id"
                :disabled="lv.status === 0"
              >
                <div class="level-option">
                  <span class="member-dot" :style="{ '--lv-color': lv.color || '#6366f1' }" />
                  <span>{{ lv.name }}</span>
                  <span class="dim">{{ lv.code }}</span>
                  <span v-if="lv.status === 0" class="dim">已停用</span>
                </div>
              </el-option>
            </el-select>
            <p v-if="!memberLevels.length" class="member-tip">
              还没有可用的会员等级，请先到「系统管理 → 会员等级」创建。
            </p>
          </el-form-item>

          <el-form-item label="有效期">
            <el-radio-group v-model="memberForm.mode">
              <el-radio-button value="days">按天数</el-radio-button>
              <el-radio-button value="date">指定日期</el-radio-button>
              <el-radio-button value="forever">长期有效</el-radio-button>
            </el-radio-group>
          </el-form-item>

          <el-form-item v-if="memberForm.mode === 'days'" label="有效天数">
            <el-input-number
              v-model="memberForm.days"
              :min="1"
              :max="36500"
              controls-position="right"
              style="width: 170px"
            />
            <span class="expire-hint">到期日 {{ previewExpire }}</span>
          </el-form-item>

          <el-form-item v-else-if="memberForm.mode === 'date'" label="到期日期">
            <el-date-picker
              v-model="memberForm.expireAt"
              type="date"
              value-format="YYYY-MM-DD"
              placeholder="选择到期日期"
              :disabled-date="(d: Date) => d.getTime() < Date.now() - 86400000"
            />
          </el-form-item>

          <p v-if="memberForm.mode === 'forever'" class="member-tip">
            长期有效不会自动到期，需要手动取消会员。
          </p>

          <p class="member-tip">
            同等级续费会在原到期日上顺延；切换等级则从今天重新计算。到期后自动按非会员处理，无需手动清理。
          </p>
        </el-form>
      </div>

      <template #footer>
        <div class="member-footer">
          <el-button
            v-if="memberTarget?.membership"
            type="danger"
            plain
            @click="revokeMember"
          >
            取消会员
          </el-button>
          <span v-else />
          <div>
            <el-button @click="memberVisible = false">返回</el-button>
            <el-button type="primary" :loading="memberSaving" @click="submitMember">保存</el-button>
          </div>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.user-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}

.user-lines {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
  min-width: 0;
}

.u-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-900);
  display: flex;
  align-items: center;
  gap: 6px;
}

.badge {
  padding: 0 5px;
  border-radius: 4px;
  font-size: 10px;
  font-style: normal;
  font-weight: 600;
  color: var(--brand-700);
  background: var(--brand-50);
  line-height: 15px;
}

.u-sub {
  font-size: 11.5px;
  color: var(--ink-400);
}

.role-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.cell-time {
  font-size: 12.5px;
  color: var(--ink-700);
}

.cell-sub {
  font-size: 11px;
  color: var(--ink-400);
}

.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-top: 1px solid var(--line-soft);
}

.total {
  font-size: 12.5px;
  color: var(--ink-500);
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}

.switch-hint {
  margin-left: 10px;
  font-size: 12px;
  color: var(--ink-400);
}

/* ------------------------------------------------------------ 导入结果 */
.import-summary {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
  margin-bottom: 16px;
}

.is-item {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 11px 14px;
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  background: var(--ink-50);

  &.is-ok .is-value {
    color: var(--ok);
  }

  &.is-fail {
    border-color: var(--danger-border);
    background: var(--danger-bg);

    .is-value {
      color: var(--danger-deep);
    }
  }
}

.is-label {
  font-size: 11.5px;
  color: var(--ink-500);
}

.is-value {
  font-size: 20px;
  font-weight: 650;
  line-height: 1.2;
  color: var(--ink-900);
  font-variant-numeric: tabular-nums;
}

.import-errors {
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  overflow: hidden;
}

.ie-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  font-size: 12px;
  color: var(--ink-600);
  background: var(--ink-50);
  border-bottom: 1px solid var(--line-soft);
}

.import-ok {
  padding: 22px 0;
  text-align: center;
  font-size: 13px;
  color: var(--ink-500);
}

/* -------------------------------------------------------------- 会员 */
.member-cell {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}

.member-dot {
  width: 7px;
  height: 7px;
  flex: none;
  border-radius: 50%;
  background: var(--lv-color, var(--brand-500));
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--lv-color, var(--brand-500)) 16%, transparent);
}

.member-lines {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
  min-width: 0;
}

.member-name {
  font-size: 12.5px;
  font-weight: 500;
  color: var(--ink-700);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.member-sub {
  font-size: 11px;
  color: var(--ink-400);
  white-space: nowrap;
}

.member-user {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  margin-bottom: 18px;
  background: var(--bg-subtle);
  border: 1px solid var(--line-soft);
  border-radius: var(--r-md);
}

.member-user-lines {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
  margin-right: auto;

  strong {
    font-size: 13px;
    color: var(--ink-900);
  }

  span {
    font-size: 11.5px;
  }
}

.level-option {
  display: flex;
  align-items: center;
  gap: 8px;

  .dim {
    margin-left: auto;
    font-size: 11px;
  }
}

.expire-hint {
  margin-left: 12px;
  font-size: 12px;
  color: var(--ink-400);
}

.member-tip {
  margin: 2px 0 0;
  font-size: 11.5px;
  line-height: 1.6;
  color: var(--ink-400);
}

.member-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

:deep(.el-table .cell) {
  line-height: 1.45;
}

@media (max-width: 720px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
