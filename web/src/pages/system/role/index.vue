<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { menuApi, roleApi, type RolePayload } from '@/api/system'
import type { MenuNode, RoleRow } from '@/api/types'
import { fmtTime } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()

/** 数据范围：与后端 model.DataScope* 常量一一对应 */
const DATA_SCOPES = [
  { value: 1, label: '全部数据', desc: '不限部门，可查看全部数据' },
  { value: 2, label: '本部门', desc: '仅本部门内的数据' },
  { value: 3, label: '本部门及以下', desc: '含全部下级部门' },
  { value: 4, label: '仅本人', desc: '仅本人创建的数据' },
]

const scopeMap = new Map(DATA_SCOPES.map((s) => [s.value, s]))

const rows = ref<RoleRow[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive({
  page: 1,
  page_size: 10,
  keyword: '',
  status: undefined as number | undefined,
})

async function load() {
  loading.value = true
  try {
    const res = await roleApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
      status: query.status,
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
  reload()
}

/* -------------------------------------------------------------- 新增编辑 */
const formVisible = ref(false)
const editingId = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const form = reactive<RolePayload>({
  code: '',
  name: '',
  description: '',
  dataScope: 4,
  sort: 10,
  status: 1,
})

const rules: FormRules = {
  code: [
    { required: true, message: '请输入角色编码', trigger: 'blur' },
    { pattern: /^[a-z][a-z0-9_]{1,31}$/, message: '小写字母开头，仅含小写字母、数字、下划线', trigger: 'blur' },
  ],
  name: [{ required: true, message: '请输入角色名称', trigger: 'blur' }],
}

function resetForm() {
  Object.assign(form, {
    code: '',
    name: '',
    description: '',
    dataScope: 4,
    sort: 10,
    status: 1,
  })
  formRef.value?.clearValidate()
}

function openCreate() {
  editingId.value = ''
  resetForm()
  formVisible.value = true
}

function openEdit(row: RoleRow) {
  editingId.value = row.id
  Object.assign(form, {
    code: row.code,
    name: row.name,
    description: row.description ?? '',
    dataScope: row.dataScope,
    sort: row.sort,
    status: row.status,
  })
  formRef.value?.clearValidate()
  formVisible.value = true
}

async function submitForm() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    if (isEdit.value) {
      await roleApi.update(editingId.value, { ...form })
      ElMessage.success('已保存')
    } else {
      await roleApi.create({ ...form })
      ElMessage.success('角色已创建')
    }
    formVisible.value = false
    load()
  } finally {
    submitting.value = false
  }
}

/* -------------------------------------------------------------- 权限分配 */
const permVisible = ref(false)
const permLoading = ref(false)
const permSaving = ref(false)
const permRole = ref<RoleRow | null>(null)
const menuTree = ref<MenuNode[]>([])
const treeRef = ref()
const treeKeyword = ref('')
const expandAll = ref(true)

/** 叶子节点集合：回显只设置叶子，父级由联动推导，避免「父目录被勾」导致整棵子树被误勾 */
const leafIds = computed<Set<string>>(() => {
  const out = new Set<string>()
  const walk = (nodes: MenuNode[]) => {
    for (const n of nodes) {
      if (n.children?.length) walk(n.children)
      else out.add(n.id)
    }
  }
  walk(menuTree.value)
  return out
})

const treeProps = { label: 'name', children: 'children' }

function filterNode(value: string, data: MenuNode) {
  return !value || data.name.includes(value)
}

watch(treeKeyword, (v) => treeRef.value?.filter(v))

async function openPerm(row: RoleRow) {
  permRole.value = row
  permVisible.value = true
  permLoading.value = true
  treeKeyword.value = ''
  try {
    if (!menuTree.value.length) menuTree.value = (await menuApi.tree()) ?? []
    const ids = (await roleApi.menus(row.id)) ?? []
    const granted = new Set(ids)
    await nextTick()
    treeRef.value?.setCheckedKeys([...leafIds.value].filter((id) => granted.has(id)))
  } finally {
    permLoading.value = false
  }
}

async function savePerm() {
  if (!permRole.value || !treeRef.value) return
  const checked: string[] = treeRef.value.getCheckedKeys() ?? []
  const half: string[] = treeRef.value.getHalfCheckedKeys() ?? []
  permSaving.value = true
  try {
    await roleApi.setMenus(permRole.value.id, [...checked, ...half])
    ElMessage.success('权限已更新，相关用户下次请求即生效')
    permVisible.value = false
    load()
  } finally {
    permSaving.value = false
  }
}

function toggleExpand() {
  expandAll.value = !expandAll.value
  const nodes = treeRef.value?.store?.nodesMap ?? {}
  Object.values(nodes).forEach((n: unknown) => {
    const node = n as { expanded: boolean }
    node.expanded = expandAll.value
  })
}

function checkAll(on: boolean) {
  treeRef.value?.setCheckedKeys(on ? [...leafIds.value] : [])
}

/* ---------------------------------------------------------------- 删除 */
async function removeRole(row: RoleRow) {
  try {
    await ElMessageBox.confirm(
      `删除后该角色下的授权关系将一并清除。确定删除「${row.name}」吗？`,
      '删除角色',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'error' },
    )
  } catch {
    return
  }
  await roleApi.remove(row.id)
  ElMessage.success('已删除')
  if (rows.value.length === 1 && query.page > 1) query.page -= 1
  load()
}

function scopeLabel(v: number) {
  return scopeMap.get(v)?.label ?? '-'
}

function scopeTone(v: number) {
  return v === 1 ? 'is-all' : v === 4 ? 'is-self' : 'is-dept'
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">角色管理</h1>
        <p class="page-desc">定义角色、数据可见范围与菜单权限</p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'system:role:create'" type="primary" :icon="Plus" @click="openCreate">
          新增角色
        </el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            placeholder="搜索角色名称 / 编码"
            :prefix-icon="Search"
            clearable
            style="width: 236px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select
            v-model="query.status"
            placeholder="全部状态"
            clearable
            style="width: 118px"
            @change="reload"
          >
            <el-option label="启用" :value="1" />
            <el-option label="停用" :value="0" />
          </el-select>
          <el-button @click="reload">查询</el-button>
          <el-button text @click="resetQuery">重置</el-button>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle title="刷新" @click="load" />
        </div>
      </div>

      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column label="角色" min-width="176">
          <template #default="{ row }">
            <div class="role-cell">
              <div class="role-shield" :class="{ 'is-super': row.code === 'super' }">
                <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true">
                  <path
                    d="M12 3l7 3v6c0 4.2-2.9 7.6-7 9-4.1-1.4-7-4.8-7-9V6l7-3z"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.8"
                    stroke-linejoin="round"
                  />
                </svg>
              </div>
              <div class="role-lines">
                <span class="r-name">
                  {{ row.name }}
                  <em v-if="row.code === 'super'" class="badge">内置</em>
                </span>
                <span class="r-code mono">{{ row.code }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="描述" min-width="150">
          <template #default="{ row }">
            <span :class="row.description ? 'muted' : 'dim'">{{ row.description || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="数据范围" width="118">
          <template #default="{ row }">
            <span class="scope-tag" :class="scopeTone(row.dataScope)">
              {{ scopeLabel(row.dataScope) }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="成员" width="72" align="right">
          <template #default="{ row }">
            <span class="num member">{{ row.userCount ?? 0 }}</span>
          </template>
        </el-table-column>

        <el-table-column label="状态" width="82">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
              {{ row.status === 1 ? '启用' : '停用' }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="创建时间" width="124">
          <template #default="{ row }">
            <span class="num muted fs-12">{{ fmtTime(row.createdAt, false) }}</span>
          </template>
        </el-table-column>

        <el-table-column label="" width="156" align="right" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'system:role:assign'" link type="primary" @click="openPerm(row)">
              权限
            </el-button>
            <el-button v-perm="'system:role:update'" link type="primary" @click="openEdit(row)">
              编辑
            </el-button>
            <el-button
              v-perm="'system:role:delete'"
              link
              type="danger"
              :disabled="row.code === 'super'"
              @click="removeRole(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <span class="total">共 {{ total }} 个角色</span>
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          layout="prev, pager, next"
          background
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </div>

    <!-- 角色表单 -->
    <el-dialog
      v-model="formVisible"
      :title="isEdit ? '编辑角色' : '新增角色'"
      width="560px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="form-grid">
          <el-form-item label="角色编码" prop="code">
            <el-input v-model="form.code" placeholder="如 auditor" :disabled="isEdit && form.code === 'super'" />
          </el-form-item>
          <el-form-item label="角色名称" prop="name">
            <el-input v-model="form.name" placeholder="如 审计员" />
          </el-form-item>
        </div>

        <el-form-item label="数据范围">
          <div class="scope-group">
            <label
              v-for="s in DATA_SCOPES"
              :key="s.value"
              class="scope-option"
              :class="{ active: form.dataScope === s.value }"
            >
              <input v-model="form.dataScope" type="radio" :value="s.value" />
              <span class="so-label">{{ s.label }}</span>
              <span class="so-desc">{{ s.desc }}</span>
            </label>
          </div>
        </el-form-item>

        <div class="form-grid">
          <el-form-item label="排序">
            <el-input-number v-model="form.sort" :min="0" :max="9999" controls-position="right" style="width: 100%" />
          </el-form-item>
          <el-form-item label="状态">
            <el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="启用" inactive-text="停用" />
          </el-form-item>
        </div>

        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" maxlength="255" show-word-limit placeholder="选填，说明该角色的职责" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">保存</el-button>
      </template>
    </el-dialog>

    <!-- 权限分配 -->
    <el-drawer v-model="permVisible" :size="480" append-to-body>
      <template #header>
        <div class="perm-head">
          <h3>分配权限</h3>
          <p v-if="permRole">{{ permRole.name }} · <span class="mono">{{ permRole.code }}</span></p>
        </div>
      </template>

      <div v-loading="permLoading" class="perm-body">
        <div class="perm-tools">
          <el-input v-model="treeKeyword" placeholder="筛选菜单" size="small" clearable style="width: 168px" />
          <el-button size="small" text @click="checkAll(true)">全选</el-button>
          <el-button size="small" text @click="checkAll(false)">清空</el-button>
          <el-button size="small" text @click="toggleExpand">{{ expandAll ? '折叠' : '展开' }}</el-button>
        </div>

        <el-tree
          ref="treeRef"
          :data="menuTree"
          :props="treeProps"
          node-key="id"
          show-checkbox
          default-expand-all
          :filter-node-method="filterNode"
          :expand-on-click-node="false"
          class="perm-tree"
        >
          <template #default="{ data }">
            <span class="tree-node">
              <span class="tn-name">{{ data.name }}</span>
              <span v-if="data.type === 'button'" class="tn-kind is-btn">按钮</span>
              <span v-else-if="data.type === 'dir'" class="tn-kind is-dir">目录</span>
              <span v-if="data.perm" class="tn-perm mono">{{ data.perm }}</span>
            </span>
          </template>
        </el-tree>

        <p class="perm-tip">
          勾选页面即授予访问权限，勾选按钮即授予对应操作权限；取消勾选后用户下次请求即失效。
        </p>
      </div>

      <template #footer>
        <div class="perm-foot">
          <el-button @click="permVisible = false">取消</el-button>
          <el-button
            type="primary"
            :loading="permSaving"
            :disabled="!auth.can('system:role:assign')"
            @click="savePerm"
          >
            保存权限
          </el-button>
        </div>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.role-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}

.role-shield {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 7px;
  color: var(--ink-600);
  background: var(--ink-100);
  flex-shrink: 0;

  &.is-super {
    color: var(--brand-700);
    background: var(--brand-50);
  }
}

.role-lines {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
  min-width: 0;
}

.r-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-900);
}

.r-code {
  font-size: 11.5px;
  color: var(--ink-400);
}

.badge {
  padding: 0 5px;
  border-radius: 4px;
  font-size: 10px;
  font-style: normal;
  font-weight: 600;
  line-height: 15px;
  color: var(--brand-700);
  background: var(--brand-50);
}

.scope-tag {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border-radius: 5px;
  font-size: 12px;
  font-weight: 500;

  &.is-all {
    color: var(--brand-700);
    background: var(--brand-50);
  }

  &.is-dept {
    color: #b45309;
    background: #fef7e6;
  }

  &.is-self {
    color: var(--ink-600);
    background: var(--ink-100);
  }
}

.member {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-800);
}

.pager {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
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

.scope-group {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  width: 100%;
}

.scope-option {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 9px 12px;
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;

  input {
    display: none;
  }

  &:hover {
    border-color: var(--brand-300);
  }

  &.active {
    border-color: var(--brand-500);
    background: var(--brand-50);
  }
}

.so-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-900);
}

.so-desc {
  font-size: 11.5px;
  color: var(--ink-500);
  line-height: 1.5;
}

/* ---------------------------------------------------------- 权限抽屉 */
.perm-head {
  h3 {
    font-size: 15px;
    font-weight: 600;
    color: var(--ink-900);
  }

  p {
    margin-top: 3px;
    font-size: 12px;
    color: var(--ink-500);
  }
}

.perm-body {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.perm-tools {
  display: flex;
  align-items: center;
  gap: 4px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--line-soft);
}

.perm-tree {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 8px 0;
  --el-tree-node-hover-bg-color: var(--ink-50);
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.tn-name {
  font-size: 13px;
  color: var(--ink-800);
}

.tn-kind {
  padding: 0 5px;
  border-radius: 4px;
  font-size: 10px;
  line-height: 15px;
  flex-shrink: 0;

  &.is-btn {
    color: var(--ink-500);
    background: var(--ink-100);
  }

  &.is-dir {
    color: var(--brand-700);
    background: var(--brand-50);
  }
}

.tn-perm {
  font-size: 10.5px;
  color: var(--ink-400);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.perm-tip {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px solid var(--line-soft);
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--ink-400);
}

.perm-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

@media (max-width: 720px) {
  .form-grid,
  .scope-group {
    grid-template-columns: 1fr;
  }
}
</style>
