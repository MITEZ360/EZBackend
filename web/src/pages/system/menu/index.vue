<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Translation, useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { menuApi, type MenuPayload } from '@/api/system'
import type { MenuNode } from '@/api/types'
import { menuIcons, resolveIcon } from '@/utils/icons'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const { t } = useI18n()

const tree = ref<MenuNode[]>([])
const loading = ref(false)

const TYPE_META = computed<Record<string, { label: string; tone: string }>>(() => ({
  dir: { label: t('目录'), tone: 'is-dir' },
  menu: { label: t('菜单'), tone: 'is-page' },
  button: { label: t('按钮'), tone: 'is-btn' },
}))

const iconNames = Object.keys(menuIcons)

async function load() {
  loading.value = true
  try {
    tree.value = (await menuApi.tree()) ?? []
  } finally {
    loading.value = false
  }
}

/* -------------------------------------------------------------- 上级选项 */
/** 扁平化菜单树，用于上级选择；编辑时排除自身与全部后代，避免形成环 */
function flatten(nodes: MenuNode[], depth = 0): (MenuNode & { depth: number })[] {
  const out: (MenuNode & { depth: number })[] = []
  for (const n of nodes) {
    out.push({ ...n, depth })
    if (n.children?.length) out.push(...flatten(n.children, depth + 1))
  }
  return out
}

const flatList = computed(() => flatten(tree.value))
const buttonCount = computed(() => flatList.value.filter((o) => o.type === 'button').length)

function descendants(node: MenuNode): Set<string> {
  const set = new Set<string>()
  const walk = (n: MenuNode) => {
    for (const c of n.children ?? []) {
      set.add(c.id)
      walk(c)
    }
  }
  walk(node)
  return set
}

const parentOptions = computed(() => {
  if (!editingNode.value) return flatList.value
  const blocked = descendants(editingNode.value)
  blocked.add(editingNode.value.id)
  return flatList.value.filter((o) => !blocked.has(o.id))
})

/* ---------------------------------------------------------------- 表单 */
const formVisible = ref(false)
const editingId = ref('')
const editingNode = ref<MenuNode | null>(null)
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const form = reactive<MenuPayload>({
  parentId: '0',
  name: '',
  type: 'menu',
  path: '',
  component: '',
  icon: '',
  perm: '',
  sort: 10,
  visible: true,
  keepAlive: false,
  status: 1,
})

const rules = computed<FormRules>(() => ({
  name: [{ required: true, message: t('请输入名称'), trigger: 'blur' }],
  path: [
    {
      validator: (_r, v: string, cb) => {
        if (form.type !== 'button' && !String(v ?? '').trim()) cb(new Error(t('目录与菜单必须填写路由路径')))
        else cb()
      },
      trigger: 'blur',
    },
  ],
  perm: [
    {
      validator: (_r, v: string, cb) => {
        if (form.type === 'button' && !String(v ?? '').trim()) cb(new Error(t('按钮必须填写权限标识')))
        else cb()
      },
      trigger: 'blur',
    },
  ],
}))

function resetForm() {
  Object.assign(form, {
    parentId: '0',
    name: '',
    type: 'menu',
    path: '',
    component: '',
    icon: '',
    perm: '',
    sort: 10,
    visible: true,
    keepAlive: false,
    status: 1,
  })
  formRef.value?.clearValidate()
}

function openCreate(parent?: MenuNode) {
  editingId.value = ''
  editingNode.value = null
  resetForm()
  if (parent) {
    form.parentId = parent.id
    // 在菜单下新增，默认建按钮；在目录下新增，默认建菜单
    form.type = parent.type === 'menu' ? 'button' : 'menu'
    form.sort = (parent.children?.length ?? 0) * 10 + 10
  }
  formVisible.value = true
}

function openEdit(node: MenuNode) {
  editingId.value = node.id
  editingNode.value = node
  Object.assign(form, {
    parentId: node.parentId || '0',
    name: node.name,
    type: node.type,
    path: node.path ?? '',
    component: node.component ?? '',
    icon: node.icon ?? '',
    perm: node.perm ?? '',
    sort: node.sort ?? 0,
    visible: node.visible !== false,
    keepAlive: Boolean(node.keepAlive),
    status: node.status ?? 1,
  })
  formRef.value?.clearValidate()
  formVisible.value = true
}

async function submitForm() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  const payload: MenuPayload = {
    ...form,
    path: form.type === 'button' ? '' : form.path?.trim(),
    component: form.type === 'menu' ? form.component?.trim() : '',
    perm: form.perm?.trim(),
    icon: form.type === 'button' ? '' : form.icon,
  }
  submitting.value = true
  try {
    if (isEdit.value) {
      await menuApi.update(editingId.value, payload)
      ElMessage.success(t('已保存'))
    } else {
      await menuApi.create(payload)
      ElMessage.success(t('菜单已创建'))
    }
    formVisible.value = false
    await load()
    // 侧边栏来源于 profile，变更后重新拉取使导航立即生效
    auth.loadProfile().catch(() => undefined)
  } finally {
    submitting.value = false
  }
}

async function removeMenu(node: MenuNode) {
  try {
    await ElMessageBox.confirm(
      node.children?.length
        ? t('「{name}」下还有 {count} 个子项，需先删除子项。', { name: node.name, count: node.children.length })
        : t('删除「{name}」后，已分配该菜单的角色将失去对应权限。确定删除吗？', { name: node.name }),
      t('删除菜单'),
      { confirmButtonText: t('删除'), cancelButtonText: t('取消'), type: 'error' },
    )
  } catch {
    return
  }
  if (node.children?.length) {
    ElMessage.warning(t('请先删除子菜单'))
    return
  }
  await menuApi.remove(node.id)
  ElMessage.success(t('已删除'))
  await load()
  auth.loadProfile().catch(() => undefined)
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('菜单管理') }}</h1>
        <p class="page-desc">
          {{ $t('维护导航结构、页面路由与按钮级权限点；变更后相关角色需重新授权') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'system:menu:create'" type="primary" :icon="Plus" @click="openCreate()">
          {{ $t('新增菜单') }}
        </el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <span class="hint">
            <Translation keypath="共 {total} 个菜单项，其中按钮权限点 {buttons} 个">
              <template #total><b class="num">{{ flatList.length }}</b></template>
              <template #buttons><b class="num">{{ buttonCount }}</b></template>
            </Translation>
          </span>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle :title="$t('刷新')" @click="load" />
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
        <el-table-column :label="$t('名称')" min-width="230">
          <template #default="{ row }">
            <div class="menu-cell">
              <el-icon class="menu-icon" :size="14">
                <component :is="resolveIcon(row.icon)" />
              </el-icon>
              <span class="m-name" :class="{ 'is-btn': row.type === 'button' }">{{ row.name }}</span>
              <span class="type-tag" :class="TYPE_META[row.type]?.tone">
                {{ TYPE_META[row.type]?.label ?? row.type }}
              </span>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('路由路径')" min-width="170">
          <template #default="{ row }">
            <span v-if="row.path" class="mono path">{{ row.path }}</span>
            <span v-else class="dim">—</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('权限标识')" min-width="190">
          <template #default="{ row }">
            <code v-if="row.perm" class="perm">{{ row.perm }}</code>
            <span v-else class="dim">—</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('排序')" width="72" align="right">
          <template #default="{ row }">
            <span class="num dim">{{ row.sort }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('可见')" width="76" align="center">
          <template #default="{ row }">
            <span v-if="row.type === 'button'" class="dim">—</span>
            <span v-else class="status-dot" :class="row.visible ? 'is-on' : 'is-off'">
              {{ row.visible ? $t('显示') : $t('隐藏') }}
            </span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('状态')" width="80" align="center">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 0 ? 'is-off' : 'is-on'">
              {{ row.status === 0 ? $t('停用') : $t('正常') }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="" width="168" align="right" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.type !== 'button'"
              v-perm="'system:menu:create'"
              link
              type="primary"
              @click="openCreate(row)"
            >
              {{ $t('新增子项') }}
            </el-button>
            <el-button v-perm="'system:menu:update'" link type="primary" @click="openEdit(row)">
              {{ $t('编辑') }}
            </el-button>
            <el-button v-perm="'system:menu:delete'" link type="danger" @click="removeMenu(row)">
              {{ $t('删除') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog
      v-model="formVisible"
      :title="isEdit ? $t('编辑菜单') : $t('新增菜单')"
      width="620px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="form-grid">
          <el-form-item :label="$t('上级菜单')">
            <el-select v-model="form.parentId" :placeholder="$t('选择上级')" filterable style="width: 100%">
              <el-option :label="$t('顶级菜单')" value="0" />
              <el-option
                v-for="o in parentOptions"
                :key="o.id"
                :label="`${'　'.repeat(o.depth)}${o.name}`"
                :value="o.id"
              />
            </el-select>
          </el-form-item>

          <el-form-item :label="$t('类型')">
            <el-radio-group v-model="form.type">
              <el-radio-button value="dir">{{ $t('目录') }}</el-radio-button>
              <el-radio-button value="menu">{{ $t('菜单') }}</el-radio-button>
              <el-radio-button value="button">{{ $t('按钮') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
        </div>

        <div class="form-grid">
          <el-form-item :label="$t('名称')" prop="name">
            <el-input v-model="form.name" :placeholder="$t('展示在侧边栏或权限树中')" />
          </el-form-item>
          <el-form-item :label="$t('排序')">
            <el-input-number v-model="form.sort" :min="0" :max="9999" controls-position="right" style="width: 100%" />
          </el-form-item>
        </div>

        <div v-if="form.type !== 'button'" class="form-grid">
          <el-form-item :label="$t('路由路径')" prop="path">
            <el-input v-model="form.path" :placeholder="$t('如 /system/role')" />
          </el-form-item>
          <el-form-item :label="$t('图标')">
            <el-select v-model="form.icon" :placeholder="$t('选择图标')" clearable filterable style="width: 100%">
              <el-option v-for="name in iconNames" :key="name" :label="name" :value="name">
                <span class="icon-option">
                  <el-icon :size="14"><component :is="resolveIcon(name)" /></el-icon>
                  <span>{{ name }}</span>
                </span>
              </el-option>
            </el-select>
          </el-form-item>
        </div>

        <el-form-item v-if="form.type === 'menu'" :label="$t('组件路径')">
          <el-input v-model="form.component" :placeholder="$t('预留字段，静态路由模式下可留空')" />
        </el-form-item>

        <el-form-item :label="$t('权限标识')" prop="perm">
          <el-input v-model="form.perm" :placeholder="$t('如 system:role:list，留空表示不做权限校验')" class="mono-input" />
        </el-form-item>

        <div v-if="form.type !== 'button'" class="form-grid">
          <el-form-item :label="$t('是否可见')">
            <el-switch v-model="form.visible" :active-text="$t('显示')" :inactive-text="$t('隐藏')" />
          </el-form-item>
          <el-form-item v-if="form.type === 'menu'" :label="$t('页面缓存')">
            <el-switch v-model="form.keepAlive" :active-text="$t('缓存')" :inactive-text="$t('不缓存')" />
          </el-form-item>
        </div>

        <el-form-item :label="$t('状态')">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" :active-text="$t('启用')" :inactive-text="$t('停用')" />
          <span class="switch-hint">{{ $t('停用后该菜单及其权限点对全部角色失效') }}</span>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ $t('保存') }}</el-button>
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

.menu-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.menu-icon {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 6px;
  color: var(--ink-600);
  background: var(--ink-100);
  flex-shrink: 0;
}

.m-name {
  font-size: 13px;
  color: var(--ink-900);

  &.is-btn {
    color: var(--ink-700);
  }
}

.type-tag {
  padding: 0 5px;
  border-radius: 4px;
  font-size: 10px;
  line-height: 16px;
  flex-shrink: 0;

  &.is-dir {
    color: var(--brand-700);
    background: var(--brand-50);
  }

  &.is-page {
    color: #0f766e;
    background: #e6f6f4;
  }

  &.is-btn {
    color: var(--ink-500);
    background: var(--ink-100);
  }
}

.path {
  color: var(--ink-600);
  font-size: 12px;
}

.perm {
  padding: 1px 6px;
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--ink-600);
  background: var(--ink-100);
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}

/* 菜单表单字段多，收紧间距避免小屏下弹窗内容被挤出视口 */
:deep(.el-form-item) {
  margin-bottom: 14px;
}

.icon-option {
  display: flex;
  align-items: center;
  gap: 8px;
}

.switch-hint {
  margin-left: 10px;
  font-size: 12px;
  color: var(--ink-400);
}

.mono-input :deep(.el-input__inner) {
  font-family: var(--font-mono);
  font-size: 12.5px;
}

@media (max-width: 720px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
