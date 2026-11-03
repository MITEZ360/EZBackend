<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { configApi, type ConfigPayload } from '@/api/system'
import type { ConfigRow } from '@/api/types'

const rows = ref<ConfigRow[]>([])
const total = ref(0)
const loading = ref(false)

const query = reactive({
  page: 1,
  page_size: 20,
  keyword: '',
  group: '',
})

const groups = ref<string[]>([])

async function load() {
  loading.value = true
  try {
    const res = await configApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
      group: query.group || undefined,
    })
    rows.value = res.list ?? []
    total.value = res.total ?? 0
    const set = new Set(groups.value)
    for (const r of rows.value) if (r.group) set.add(r.group)
    groups.value = [...set].sort()
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
  query.group = ''
  reload()
}

/* ---------------------------------------------------------------- 表单 */
const formVisible = ref(false)
const editingId = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const form = reactive<ConfigPayload>({
  key: '',
  value: '',
  name: '',
  group: 'default',
  encrypted: false,
  public: false,
  remark: '',
})

const rules: FormRules = {
  key: [
    { required: true, message: '请输入配置键', trigger: 'blur' },
    {
      pattern: /^[a-zA-Z][a-zA-Z0-9_.]{1,63}$/,
      message: '字母开头，可含字母、数字、下划线与点',
      trigger: 'blur',
    },
  ],
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
}

function resetForm() {
  Object.assign(form, {
    key: '',
    value: '',
    name: '',
    group: 'default',
    encrypted: false,
    public: false,
    remark: '',
  })
  formRef.value?.clearValidate()
}

function openCreate() {
  editingId.value = ''
  resetForm()
  if (query.group) form.group = query.group
  formVisible.value = true
}

function openEdit(row: ConfigRow) {
  editingId.value = row.id
  Object.assign(form, {
    key: row.key,
    // 后端约定：敏感项提交 ****** 表示保持原值不动
    value: row.encrypted ? '******' : row.value,
    name: row.name,
    group: row.group || 'default',
    encrypted: row.encrypted,
    public: row.public,
    remark: row.remark ?? '',
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
      await configApi.update(editingId.value, { ...form })
      ElMessage.success('已保存')
    } else {
      await configApi.create({ ...form })
      ElMessage.success('参数已创建')
    }
    formVisible.value = false
    load()
  } finally {
    submitting.value = false
  }
}

async function removeConfig(row: ConfigRow) {
  try {
    await ElMessageBox.confirm(
      `删除参数「${row.name}」后，读取该键的业务将回退到默认值或报错。确定删除吗？`,
      '删除参数',
      { confirmButtonText: '删除', cancelButtonText: '取消', type: 'error' },
    )
  } catch {
    return
  }
  await configApi.remove(row.id)
  ElMessage.success('已删除')
  if (rows.value.length === 1 && query.page > 1) query.page -= 1
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">参数配置</h1>
        <p class="page-desc">集中管理运行期可调参数，支持敏感值加密与前端公开读取</p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'system:config:update'" type="primary" :icon="Plus" @click="openCreate">
          新增参数
        </el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            placeholder="搜索参数名 / 键 / 备注"
            :prefix-icon="Search"
            clearable
            style="width: 248px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select
            v-model="query.group"
            placeholder="全部分组"
            clearable
            filterable
            allow-create
            style="width: 150px"
            @change="reload"
          >
            <el-option v-for="g in groups" :key="g" :label="g" :value="g" />
          </el-select>
          <el-button @click="reload">查询</el-button>
          <el-button text @click="resetQuery">重置</el-button>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle title="刷新" @click="load" />
        </div>
      </div>

      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column label="参数名称" min-width="180">
          <template #default="{ row }">
            <div class="cfg-cell">
              <span class="c-name">{{ row.name }}</span>
              <div class="c-tags">
                <span v-if="row.public" class="mini-tag is-public">公开</span>
                <span v-if="row.encrypted" class="mini-tag is-secret">加密</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="配置键" min-width="200">
          <template #default="{ row }">
            <code class="cfg-key">{{ row.key }}</code>
          </template>
        </el-table-column>

        <el-table-column label="配置值" min-width="220">
          <template #default="{ row }">
            <span v-if="row.encrypted" class="secret">••••••••</span>
            <span v-else class="cfg-value" :title="row.value">{{ row.value || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="分组" width="120">
          <template #default="{ row }">
            <el-tag size="small" type="info" effect="plain">{{ row.group || 'default' }}</el-tag>
          </template>
        </el-table-column>

        <el-table-column label="备注" min-width="180">
          <template #default="{ row }">
            <span :class="row.remark ? 'muted' : 'dim'">{{ row.remark || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="" width="120" align="right" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'system:config:update'" link type="primary" @click="openEdit(row)">
              编辑
            </el-button>
            <el-button v-perm="'system:config:update'" link type="danger" @click="removeConfig(row)">
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <span class="total">共 {{ total }} 项参数</span>
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[20, 50, 100]"
          layout="sizes, prev, pager, next"
          background
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </div>

    <el-dialog
      v-model="formVisible"
      :title="isEdit ? '编辑参数' : '新增参数'"
      width="600px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="form-grid">
          <el-form-item label="配置键" prop="key">
            <el-input v-model="form.key" placeholder="如 site.title" class="mono-input" />
          </el-form-item>
          <el-form-item label="参数名称" prop="name">
            <el-input v-model="form.name" placeholder="如 站点标题" />
          </el-form-item>
        </div>

        <el-form-item label="配置值">
          <el-input
            v-model="form.value"
            :type="form.encrypted ? 'password' : 'text'"
            :show-password="form.encrypted"
            :placeholder="form.encrypted ? '敏感值，保持 ****** 则不变更' : '请输入参数值'"
          />
        </el-form-item>

        <div class="form-grid">
          <el-form-item label="分组">
            <el-input v-model="form.group" placeholder="default" />
          </el-form-item>
          <el-form-item label="权限属性">
            <div class="switch-col">
              <el-switch v-model="form.public" active-text="公开" inactive-text="私有" />
              <span class="switch-hint">公开参数允许未登录读取</span>
            </div>
          </el-form-item>
        </div>

        <el-form-item label="加密存储">
          <el-switch v-model="form.encrypted" active-text="加密" inactive-text="明文" />
          <span class="switch-hint">密钥、令牌等敏感值建议加密，列表中将以掩码展示</span>
        </el-form-item>

        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.cfg-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.c-name {
  font-size: 13px;
  color: var(--ink-900);
}

.c-tags {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.mini-tag {
  padding: 0 5px;
  border-radius: 4px;
  font-size: 10px;
  line-height: 16px;

  &.is-public {
    color: #0f766e;
    background: #e6f6f4;
  }

  &.is-secret {
    color: #b45309;
    background: #fef7e6;
  }
}

.cfg-key,
.cfg-value {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink-600);
}

.cfg-value {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 300px;
}

.secret {
  font-size: 12px;
  letter-spacing: 2px;
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

.switch-col {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.switch-hint {
  font-size: 11.5px;
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
