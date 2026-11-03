<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { deptApi, type DeptPayload } from '@/api/system'
import type { DeptNode } from '@/api/types'

const tree = ref<DeptNode[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    tree.value = (await deptApi.tree()) ?? []
  } finally {
    loading.value = false
  }
}

const flatList = computed(() => flatten(tree.value))
const deptCount = computed(() => flatList.value.length)
const memberTotal = computed(() => flatList.value.reduce((sum, d) => sum + (d.userCount ?? 0), 0))

function flatten(nodes: DeptNode[], depth = 0): (DeptNode & { depth: number })[] {
  const out: (DeptNode & { depth: number })[] = []
  for (const n of nodes) {
    out.push({ ...n, depth })
    if (n.children?.length) out.push(...flatten(n.children, depth + 1))
  }
  return out
}

function descendants(node: DeptNode): Set<string> {
  const set = new Set<string>()
  const walk = (n: DeptNode) => {
    for (const c of n.children ?? []) {
      set.add(c.id)
      walk(c)
    }
  }
  walk(node)
  return set
}

/** 上级部门候选：编辑时排除自身与后代，防止把上级设成自己的子节点 */
const parentTree = computed<DeptNode[]>(() => {
  if (!editingNode.value) return tree.value
  const blocked = descendants(editingNode.value)
  blocked.add(editingNode.value.id)
  return prune(tree.value, blocked)
})

function prune(nodes: DeptNode[], blocked: Set<string>): DeptNode[] {
  const out: DeptNode[] = []
  for (const n of nodes) {
    if (blocked.has(n.id)) continue
    out.push({ ...n, children: n.children ? prune(n.children, blocked) : undefined })
  }
  return out
}

/* ---------------------------------------------------------------- 表单 */
const formVisible = ref(false)
const editingId = ref('')
const editingNode = ref<DeptNode | null>(null)
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const form = reactive<DeptPayload>({
  parentId: '0',
  name: '',
  leader: '',
  phone: '',
  email: '',
  sort: 10,
  status: 1,
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入部门名称', trigger: 'blur' }],
  email: [{ type: 'email', message: '邮箱格式不正确', trigger: 'blur' }],
  phone: [{ pattern: /^[\d\-+() ]{6,20}$/, message: '联系电话格式不正确', trigger: 'blur' }],
}

function resetForm() {
  Object.assign(form, {
    parentId: '0',
    name: '',
    leader: '',
    phone: '',
    email: '',
    sort: 10,
    status: 1,
  })
  formRef.value?.clearValidate()
}

function openCreate(parent?: DeptNode) {
  editingId.value = ''
  editingNode.value = null
  resetForm()
  if (parent) {
    form.parentId = parent.id
    form.sort = (parent.children?.length ?? 0) * 10 + 10
  }
  formVisible.value = true
}

function openEdit(node: DeptNode) {
  editingId.value = node.id
  editingNode.value = node
  Object.assign(form, {
    parentId: node.parentId || '0',
    name: node.name,
    leader: node.leader ?? '',
    phone: node.phone ?? '',
    email: node.email ?? '',
    sort: node.sort ?? 0,
    status: node.status ?? 1,
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
      await deptApi.update(editingId.value, { ...form })
      ElMessage.success('已保存')
    } else {
      await deptApi.create({ ...form })
      ElMessage.success('部门已创建')
    }
    formVisible.value = false
    load()
  } finally {
    submitting.value = false
  }
}

async function removeDept(node: DeptNode) {
  const blocked = node.children?.length
    ? `该部门下还有 ${node.children.length} 个下级部门，需先删除下级。`
    : (node.userCount ?? 0) > 0
      ? `该部门下还有 ${node.userCount} 名成员，需先调整成员归属。`
      : `删除「${node.name}」后不可恢复，确定继续吗？`
  try {
    await ElMessageBox.confirm(blocked, '删除部门', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'error',
    })
  } catch {
    return
  }
  if (node.children?.length || (node.userCount ?? 0) > 0) return
  await deptApi.remove(node.id)
  ElMessage.success('已删除')
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">部门管理</h1>
        <p class="page-desc">维护组织架构，部门层级同时决定数据权限的可见范围</p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'system:dept:create'" type="primary" :icon="Plus" @click="openCreate()">
          新增部门
        </el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <span class="hint">
            <b class="num">{{ deptCount }}</b> 个部门 · <b class="num">{{ memberTotal }}</b> 名成员
          </span>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle title="刷新" @click="load" />
        </div>
      </div>

      <el-table
        v-loading="loading"
        :data="tree"
        row-key="id"
        default-expand-all
        :tree-props="{ children: 'children' }"
        :indent="18"
      >
        <el-table-column label="部门名称" min-width="240">
          <template #default="{ row }">
            <div class="dept-cell">
              <span class="dept-mark" :class="{ root: !row.parentId || row.parentId === '0' }">
                {{ row.name.slice(0, 1) }}
              </span>
              <span class="d-name">{{ row.name }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="负责人" width="130">
          <template #default="{ row }">
            <span :class="row.leader ? '' : 'dim'">{{ row.leader || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="联系电话" width="150">
          <template #default="{ row }">
            <span class="num" :class="row.phone ? 'muted' : 'dim'">{{ row.phone || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="邮箱" min-width="180">
          <template #default="{ row }">
            <span :class="row.email ? 'muted' : 'dim'">{{ row.email || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="直属成员" width="96" align="right">
          <template #default="{ row }">
            <span class="num member" :class="{ zero: !row.userCount }">{{ row.userCount ?? 0 }}</span>
          </template>
        </el-table-column>

        <el-table-column label="排序" width="70" align="right">
          <template #default="{ row }">
            <span class="num dim">{{ row.sort }}</span>
          </template>
        </el-table-column>

        <el-table-column label="状态" width="88" align="center">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
              {{ row.status === 1 ? '正常' : '停用' }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="" width="180" align="right" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'system:dept:create'" link type="primary" @click="openCreate(row)">
              新增下级
            </el-button>
            <el-button v-perm="'system:dept:update'" link type="primary" @click="openEdit(row)">
              编辑
            </el-button>
            <el-button v-perm="'system:dept:delete'" link type="danger" @click="removeDept(row)">
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog
      v-model="formVisible"
      :title="isEdit ? '编辑部门' : '新增部门'"
      width="560px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item label="上级部门">
          <el-tree-select
            v-model="form.parentId"
            :data="parentTree"
            :props="{ label: 'name', children: 'children', value: 'id' }"
            node-key="id"
            placeholder="选择上级部门"
            check-strictly
            default-expand-all
            clearable
            style="width: 100%"
          />
          <span class="field-hint">留空表示作为顶级部门</span>
        </el-form-item>

        <div class="form-grid">
          <el-form-item label="部门名称" prop="name">
            <el-input v-model="form.name" placeholder="如 研发中心" />
          </el-form-item>
          <el-form-item label="负责人">
            <el-input v-model="form.leader" placeholder="选填" />
          </el-form-item>
        </div>

        <div class="form-grid">
          <el-form-item label="联系电话" prop="phone">
            <el-input v-model="form.phone" placeholder="选填" />
          </el-form-item>
          <el-form-item label="邮箱" prop="email">
            <el-input v-model="form.email" placeholder="选填" />
          </el-form-item>
        </div>

        <div class="form-grid">
          <el-form-item label="排序">
            <el-input-number v-model="form.sort" :min="0" :max="9999" controls-position="right" style="width: 100%" />
          </el-form-item>
          <el-form-item label="状态">
            <el-switch v-model="form.status" :active-value="1" :inactive-value="0" active-text="正常" inactive-text="停用" />
          </el-form-item>
        </div>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.hint {
  font-size: 12.5px;
  color: var(--ink-500);

  b {
    color: var(--ink-800);
    font-weight: 600;
  }
}

.dept-cell {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
}

.dept-mark {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 6px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--ink-600);
  background: var(--ink-100);
  flex-shrink: 0;

  &.root {
    color: var(--brand-700);
    background: var(--brand-50);
  }
}

.d-name {
  font-size: 13px;
  color: var(--ink-900);
}

.member {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-800);

  &.zero {
    color: var(--ink-300);
    font-weight: 400;
  }
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}

.field-hint {
  margin-top: 2px;
  font-size: 11.5px;
  color: var(--ink-400);
}

@media (max-width: 720px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
