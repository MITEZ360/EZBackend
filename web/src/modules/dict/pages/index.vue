<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { dictApi, type DictItemPayload, type DictTypePayload } from '@/api/system'
import type { DictItem, DictTypeRow } from '@/api/types'

const { t } = useI18n()

const types = ref<DictTypeRow[]>([])
const typesLoading = ref(false)
const keyword = ref('')
const active = ref<DictTypeRow | null>(null)

const items = ref<DictItem[]>([])
const itemsLoading = ref(false)

async function loadTypes(keepId?: string) {
  typesLoading.value = true
  try {
    const res = await dictApi.types({ page: 1, page_size: 200, keyword: keyword.value || undefined })
    types.value = res.list ?? []
    const target = keepId ? types.value.find((t) => t.id === keepId) : active.value
    const next = (target && types.value.find((t) => t.id === target.id)) || types.value[0] || null
    await selectType(next)
  } finally {
    typesLoading.value = false
  }
}

async function selectType(t: DictTypeRow | null) {
  active.value = t
  if (!t) {
    items.value = []
    return
  }
  itemsLoading.value = true
  try {
    items.value = (await dictApi.items(t.code)) ?? []
  } finally {
    itemsLoading.value = false
  }
}

function reloadItems() {
  if (active.value) selectType(active.value)
}

/* ------------------------------------------------------------ 类型表单 */
const typeVisible = ref(false)
const editingTypeId = ref('')
const isEditType = computed(() => Boolean(editingTypeId.value))
const typeFormRef = ref<FormInstance>()
const typeSaving = ref(false)

const typeForm = reactive<DictTypePayload>({ code: '', name: '', remark: '', status: 1 })

const typeRules = computed<FormRules>(() => ({
  code: [
    { required: true, message: t('请输入字典编码'), trigger: 'blur' },
    {
      pattern: /^[a-z][a-z0-9_]{1,63}$/,
      message: t('小写字母开头，仅含小写字母、数字、下划线'),
      trigger: 'blur',
    },
  ],
  name: [{ required: true, message: t('请输入字典名称'), trigger: 'blur' }],
}))

function openTypeCreate() {
  editingTypeId.value = ''
  Object.assign(typeForm, { code: '', name: '', remark: '', status: 1 })
  typeFormRef.value?.clearValidate()
  typeVisible.value = true
}

function openTypeEdit(t: DictTypeRow) {
  editingTypeId.value = t.id
  Object.assign(typeForm, {
    code: t.code,
    name: t.name,
    remark: t.remark ?? '',
    status: t.status,
  })
  typeFormRef.value?.clearValidate()
  typeVisible.value = true
}

async function submitType() {
  const valid = await typeFormRef.value?.validate().catch(() => false)
  if (!valid) return
  typeSaving.value = true
  try {
    if (isEditType.value) {
      await dictApi.updateType(editingTypeId.value, { ...typeForm })
      ElMessage.success(t('已保存'))
      typeVisible.value = false
      await loadTypes(editingTypeId.value)
    } else {
      const created = await dictApi.createType({ ...typeForm })
      ElMessage.success(t('字典已创建'))
      typeVisible.value = false
      await loadTypes(created?.id)
    }
  } finally {
    typeSaving.value = false
  }
}

async function removeType(row: DictTypeRow) {
  try {
    await ElMessageBox.confirm(
      t('删除「{name}」会同时删除其下 {count} 个字典项，且引用该字典的页面将取值失败。确定删除吗？', {
        name: row.name,
        count: row.items?.length ?? 0,
      }),
      t('删除字典'),
      { confirmButtonText: t('删除'), cancelButtonText: t('取消'), type: 'error' },
    )
  } catch {
    return
  }
  await dictApi.removeType(row.id)
  ElMessage.success(t('已删除'))
  if (active.value?.id === row.id) active.value = null
  await loadTypes()
}

/* ------------------------------------------------------------ 字典项表单 */
const itemVisible = ref(false)
const editingItemId = ref('')
const isEditItem = computed(() => Boolean(editingItemId.value))
const itemFormRef = ref<FormInstance>()
const itemSaving = ref(false)

const itemForm = reactive<DictItemPayload>({
  typeCode: '',
  label: '',
  value: '',
  tagColor: '',
  sort: 10,
  status: 1,
  remark: '',
})

const itemRules = computed<FormRules>(() => ({
  label: [{ required: true, message: t('请输入显示名称'), trigger: 'blur' }],
  value: [{ required: true, message: t('请输入数据值'), trigger: 'blur' }],
}))

const TAG_COLORS = ['primary', 'success', 'warning', 'danger', 'info']

function openItemCreate() {
  if (!active.value) return
  editingItemId.value = ''
  Object.assign(itemForm, {
    typeCode: active.value.code,
    label: '',
    value: '',
    tagColor: '',
    sort: (items.value.length + 1) * 10,
    status: 1,
    remark: '',
  })
  itemFormRef.value?.clearValidate()
  itemVisible.value = true
}

function openItemEdit(it: DictItem) {
  editingItemId.value = it.id
  Object.assign(itemForm, {
    typeCode: it.typeCode,
    label: it.label,
    value: it.value,
    tagColor: it.tagColor ?? '',
    sort: it.sort,
    status: it.status,
    remark: it.remark ?? '',
  })
  itemFormRef.value?.clearValidate()
  itemVisible.value = true
}

async function submitItem() {
  const valid = await itemFormRef.value?.validate().catch(() => false)
  if (!valid) return
  itemSaving.value = true
  try {
    if (isEditItem.value) {
      await dictApi.updateItem(editingItemId.value, { ...itemForm })
      ElMessage.success(t('已保存'))
    } else {
      await dictApi.createItem({ ...itemForm })
      ElMessage.success(t('字典项已添加'))
    }
    itemVisible.value = false
    reloadItems()
  } finally {
    itemSaving.value = false
  }
}

async function removeItem(it: DictItem) {
  try {
    await ElMessageBox.confirm(t('确定删除字典项「{label}」吗？', { label: it.label }), t('删除字典项'), {
      confirmButtonText: t('删除'),
      cancelButtonText: t('取消'),
      type: 'warning',
    })
  } catch {
    return
  }
  await dictApi.removeItem(it.id)
  ElMessage.success(t('已删除'))
  reloadItems()
}

function tagType(color?: string) {
  return color && TAG_COLORS.includes(color) ? color : 'info'
}

onMounted(() => loadTypes())
</script>

<template>
  <div class="page dict-page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('字典管理') }}</h1>
        <p class="page-desc">{{ $t('维护枚举值，前端通过公共接口一次性拉取并缓存') }}</p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'system:dict:create'" type="primary" :icon="Plus" @click="openTypeCreate">
          {{ $t('新增字典') }}
        </el-button>
      </div>
    </div>

    <div class="dict-grid">
      <!-- 左：字典类型 -->
      <div class="card type-panel">
        <div class="panel-head">
          <el-input
            v-model="keyword"
            :placeholder="$t('搜索字典')"
            :prefix-icon="Search"
            clearable
            size="small"
            @keyup.enter="loadTypes()"
            @clear="loadTypes()"
          />
          <el-button :icon="Refresh" circle size="small" :title="$t('刷新')" @click="loadTypes()" />
        </div>

        <div v-loading="typesLoading" class="type-list">
          <button
            v-for="t in types"
            :key="t.id"
            type="button"
            class="type-item"
            :class="{ active: active?.id === t.id }"
            @click="selectType(t)"
          >
            <div class="ti-main">
              <span class="ti-name">{{ t.name }}</span>
              <span class="ti-code mono">{{ t.code }}</span>
            </div>
            <div class="ti-side">
              <span class="ti-count num">{{ t.items?.length ?? 0 }}</span>
              <span class="status-dot" :class="t.status === 1 ? 'is-on' : 'is-off'" />
            </div>
          </button>

          <div v-if="!typesLoading && !types.length" class="empty">
            <p>{{ $t('还没有字典') }}</p>
            <span>{{ $t('点击右上角「新增字典」创建第一个枚举') }}</span>
          </div>
        </div>
      </div>

      <!-- 右：字典项 -->
      <div class="card item-panel">
        <div class="panel-head item-head">
          <div v-if="active" class="active-info">
            <span class="ai-name">{{ active.name }}</span>
            <code class="ai-code">{{ active.code }}</code>
            <el-tag v-if="active.status === 0" size="small" type="info">{{ $t('已停用') }}</el-tag>
          </div>
          <div v-else class="active-info dim">{{ $t('未选择字典') }}</div>

          <div v-if="active" class="head-actions">
            <el-button v-perm="'system:dict:update'" size="small" @click="openTypeEdit(active)">
              {{ $t('编辑字典') }}
            </el-button>
            <el-button
              v-perm="'system:dict:delete'"
              size="small"
              type="danger"
              plain
              @click="removeType(active)"
            >
              {{ $t('删除') }}
            </el-button>
            <el-button
              v-perm="'system:dict:create'"
              size="small"
              type="primary"
              :icon="Plus"
              @click="openItemCreate"
            >
              {{ $t('新增字典项') }}
            </el-button>
          </div>
        </div>

        <el-table v-loading="itemsLoading" :data="items" row-key="id" class="item-table">
          <el-table-column :label="$t('显示名称')" min-width="180">
            <template #default="{ row }">
              <div class="label-cell">
                <el-tag :type="tagType(row.tagColor)" size="small" effect="light">{{ row.label }}</el-tag>
                <span v-if="row.remark" class="label-remark">{{ row.remark }}</span>
              </div>
            </template>
          </el-table-column>

          <el-table-column :label="$t('数据值')" min-width="110">
            <template #default="{ row }">
              <code class="value-code">{{ row.value }}</code>
            </template>
          </el-table-column>

          <el-table-column :label="$t('排序')" width="64" align="right">
            <template #default="{ row }">
              <span class="num dim">{{ row.sort }}</span>
            </template>
          </el-table-column>

          <el-table-column :label="$t('状态')" width="80" align="center">
            <template #default="{ row }">
              <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
                {{ row.status === 1 ? $t('启用') : $t('停用') }}
              </span>
            </template>
          </el-table-column>

          <el-table-column label="" width="104" align="right" fixed="right">
            <template #default="{ row }">
              <el-button v-perm="'system:dict:update'" link type="primary" @click="openItemEdit(row)">
                {{ $t('编辑') }}
              </el-button>
              <el-button v-perm="'system:dict:delete'" link type="danger" @click="removeItem(row)">
                {{ $t('删除') }}
              </el-button>
            </template>
          </el-table-column>

          <template #empty>
            <div class="table-empty">
              <p>{{ active ? $t('该字典下还没有字典项') : $t('请先在左侧选择字典') }}</p>
              <el-button v-if="active" v-perm="'system:dict:create'" size="small" type="primary" plain @click="openItemCreate">
                {{ $t('添加第一个字典项') }}
              </el-button>
            </div>
          </template>
        </el-table>
      </div>
    </div>

    <!-- 字典类型弹窗 -->
    <el-dialog
      v-model="typeVisible"
      :title="isEditType ? $t('编辑字典') : $t('新增字典')"
      width="520px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="typeFormRef" :model="typeForm" :rules="typeRules" label-position="top">
        <div class="form-grid">
          <el-form-item :label="$t('字典编码')" prop="code">
            <el-input v-model="typeForm.code" :placeholder="$t('如 user_gender')" />
          </el-form-item>
          <el-form-item :label="$t('字典名称')" prop="name">
            <el-input v-model="typeForm.name" :placeholder="$t('如 用户性别')" />
          </el-form-item>
        </div>
        <el-form-item :label="$t('状态')">
          <el-switch v-model="typeForm.status" :active-value="1" :inactive-value="0" :active-text="$t('启用')" :inactive-text="$t('停用')" />
        </el-form-item>
        <el-form-item :label="$t('备注')">
          <el-input v-model="typeForm.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="typeVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="typeSaving" @click="submitType">{{ $t('保存') }}</el-button>
      </template>
    </el-dialog>

    <!-- 字典项弹窗 -->
    <el-dialog
      v-model="itemVisible"
      :title="isEditItem ? $t('编辑字典项') : $t('新增字典项')"
      width="520px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="itemFormRef" :model="itemForm" :rules="itemRules" label-position="top">
        <div class="form-grid">
          <el-form-item :label="$t('显示名称')" prop="label">
            <el-input v-model="itemForm.label" :placeholder="$t('界面展示文案')" />
          </el-form-item>
          <el-form-item :label="$t('数据值')" prop="value">
            <el-input v-model="itemForm.value" :placeholder="$t('实际存储值')" />
          </el-form-item>
        </div>
        <div class="form-grid">
          <el-form-item :label="$t('标签颜色')">
            <el-select v-model="itemForm.tagColor" clearable :placeholder="$t('默认')" style="width: 100%">
              <el-option v-for="c in TAG_COLORS" :key="c" :label="c" :value="c" />
            </el-select>
          </el-form-item>
          <el-form-item :label="$t('排序')">
            <el-input-number v-model="itemForm.sort" :min="0" :max="9999" controls-position="right" style="width: 100%" />
          </el-form-item>
        </div>
        <el-form-item :label="$t('状态')">
          <el-switch v-model="itemForm.status" :active-value="1" :inactive-value="0" :active-text="$t('启用')" :inactive-text="$t('停用')" />
        </el-form-item>
        <el-form-item :label="$t('备注')">
          <el-input v-model="itemForm.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="itemVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="itemSaving" @click="submitItem">{{ $t('保存') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.dict-page {
  display: flex;
  flex-direction: column;
}

.dict-grid {
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}

.panel-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--line-soft);
}

.item-head {
  justify-content: space-between;
  min-height: 57px;
}

.active-info {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.ai-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--ink-900);
}

.ai-code {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--ink-500);
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--ink-100);
}

.head-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.type-list {
  max-height: calc(100vh - 260px);
  max-height: min(640px, calc(100dvh - 260px));
  overflow-y: auto;
  padding: 6px;
}

.type-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  padding: 9px 10px;
  border: 1px solid transparent;
  border-radius: var(--r-md);
  background: transparent;
  text-align: left;
  cursor: pointer;
  transition: background 0.15s ease, border-color 0.15s ease;

  &:hover {
    background: var(--ink-50);
  }

  &.active {
    border-color: var(--brand-200, #dcd9fb);
    background: var(--brand-50);
  }
}

.ti-main {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.ti-name {
  font-size: 13px;
  color: var(--ink-900);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ti-code {
  font-size: 11px;
  color: var(--ink-400);
}

.ti-side {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.ti-count {
  font-size: 11.5px;
  color: var(--ink-400);
}

.empty {
  padding: 36px 16px;
  text-align: center;
  color: var(--ink-400);

  p {
    font-size: 13px;
    color: var(--ink-600);
    margin-bottom: 4px;
  }

  span {
    font-size: 12px;
  }
}

.item-table {
  width: 100%;
}

.label-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.label-remark {
  font-size: 11.5px;
  color: var(--ink-400);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.value-code {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink-700);
}

.table-empty {
  padding: 40px 0;
  color: var(--ink-400);

  p {
    font-size: 13px;
    margin-bottom: 10px;
  }
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}

@media (max-width: 900px) {
  .dict-grid {
    grid-template-columns: 1fr;
  }

  .type-list {
    max-height: 260px;
  }
}

@media (max-width: 720px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
