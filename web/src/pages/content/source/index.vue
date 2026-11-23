<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { newsSourceApi } from '@/api/news'
import type { NewsSource } from '@/api/types'
import { fmtTime } from '@/utils/format'
import IconField from '@/components/IconField.vue'

/** 列表里用紧凑时间，完整时间放在 tooltip 中 */
function fmtShort(v?: string) {
  if (!v) return '—'
  // 走 fmtTime 而不是切字符串：后端存 UTC，直接切会把时区丢掉
  const s = fmtTime(v, false)
  return s.length >= 16 ? s.slice(5) : s
}

const rows = ref<NewsSource[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive({ page: 1, page_size: 20, keyword: '' })

async function load() {
  loading.value = true
  try {
    const res = await newsSourceApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
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

onMounted(load)

/* ------------------------------------------------------------ 编辑 */
const drawer = ref(false)
const editingId = ref('')
const saving = ref(false)
const formRef = ref<FormInstance>()
const isEdit = ref(false)

const form = ref({
  code: '',
  name: '',
  kind: 'crawler' as 'crawler' | 'api' | 'rss',
  icon: '',
  color: '',
  sort: 0,
  homepage: '',
  status: 1,
  remark: '',
})

const rules: FormRules = {
  code: [
    { required: true, message: '请输入编码', trigger: 'blur' },
    {
      pattern: /^[a-z][a-z0-9_-]{1,31}$/,
      message: '小写字母开头，可含小写字母、数字、下划线、连字符',
      trigger: 'blur',
    },
  ],
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
}

function openCreate() {
  editingId.value = ''
  isEdit.value = false
  Object.assign(form.value, {
    code: '', name: '', kind: 'crawler', icon: '', color: '', sort: 0,
    homepage: '', status: 1, remark: '',
  })
  formRef.value?.clearValidate()
  drawer.value = true
}

function openEdit(row: NewsSource) {
  editingId.value = row.id
  isEdit.value = true
  Object.assign(form.value, {
    code: row.code,
    name: row.name,
    kind: row.kind,
    icon: row.icon ?? '',
    color: row.color ?? '',
    sort: row.sort ?? 0,
    homepage: row.homepage ?? '',
    status: row.status,
    remark: row.remark ?? '',
  })
  formRef.value?.clearValidate()
  drawer.value = true
}

async function submit() {
  const ok = await formRef.value?.validate().catch(() => false)
  if (!ok) return
  saving.value = true
  try {
    if (isEdit.value) {
      await newsSourceApi.update(editingId.value, { ...form.value })
      ElMessage.success('已保存')
    } else {
      await newsSourceApi.create({ ...form.value })
      ElMessage.success('已创建')
    }
    drawer.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function remove(row: NewsSource) {
  try {
    await ElMessageBox.confirm(
      `删除后该来源不再出现在筛选中。若仍有资讯引用会拒绝删除，可先停用。确认删除「${row.name}」？`,
      '删除数据源',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  await newsSourceApi.remove(row.id)
  ElMessage.success('已删除')
  await load()
}

async function toggle(row: NewsSource) {
  const next = row.status === 1 ? 0 : 1
  await newsSourceApi.update(row.id, {
    code: row.code,
    name: row.name,
    kind: row.kind,
    icon: row.icon,
    color: row.color,
    sort: row.sort,
    homepage: row.homepage,
    remark: row.remark,
    status: next,
  })
  row.status = next
  ElMessage.success(next === 1 ? '已启用' : '已停用')
}

const KIND_LABEL: Record<string, string> = {
  crawler: '爬虫',
  api: '接口',
  rss: 'RSS',
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">数据源</h1>
        <p class="page-desc">
          来源是爬虫推送的白名单：未配置或已停用的来源会被整批拒收。名称、图标与颜色随元数据接口下发，客户端直接用于展示与筛选。
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" @click="load">刷新</el-button>
        <el-button v-perm="'content:news:source'" type="primary" :icon="Plus" @click="openCreate">
          新增数据源
        </el-button>
      </div>
    </div>

    <section class="card">
      <div class="toolbar">
        <el-input
          v-model="query.keyword"
          placeholder="搜索编码或名称"
          clearable
          style="width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button @click="reload">查询</el-button>
      </div>

      <el-table :data="rows" v-loading="loading" row-key="id">
        <el-table-column label="名称" width="240">
          <template #default="{ row }">
            <div class="src-cell">
              <div class="src-icon" :style="{ background: row.color || 'var(--bg-subtle)' }">
                <img v-if="row.icon" :src="row.icon" alt="" />
                <span v-else>{{ row.name.slice(0, 1) }}</span>
              </div>
              <div class="src-meta">
                <div class="src-name">{{ row.name }}</div>
                <div v-if="row.remark" class="dim src-remark">{{ row.remark }}</div>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="编码" width="150">
          <template #default="{ row }">
            <span class="mono code">{{ row.code }}</span>
          </template>
        </el-table-column>

        <el-table-column label="类型" width="92">
          <template #default="{ row }">
            <el-tag size="small" effect="plain" type="info">{{ KIND_LABEL[row.kind] ?? row.kind }}</el-tag>
          </template>
        </el-table-column>

        <el-table-column label="累计导入" width="110" align="right">
          <template #default="{ row }">
            <span class="mono">{{ row.totalIngested.toLocaleString() }}</span>
          </template>
        </el-table-column>

        <el-table-column label="最后导入" width="118">
          <template #default="{ row }">
            <el-tooltip :content="fmtTime(row.lastIngestAt)" placement="top">
              <span class="mono dim nowrap">{{ fmtShort(row.lastIngestAt) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>

        <el-table-column label="状态" width="76">
          <template #default="{ row }">
            <el-switch
              v-perm="'content:news:source'"
              :model-value="row.status"
              :active-value="1"
              :inactive-value="0"
              @change="toggle(row)"
            />
          </template>
        </el-table-column>

        <el-table-column label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'content:news:source'" link type="primary" @click="openEdit(row)">
              编辑
            </el-button>
            <el-button v-perm="'content:news:delete'" link type="danger" @click="remove(row)">
              删除
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <div class="empty">
            <p>还没有数据源</p>
            <p class="dim">请先创建来源，爬虫推送未配置的编码会被拒绝</p>
          </div>
        </template>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </section>

    <el-drawer v-model="drawer" :title="isEdit ? '编辑数据源' : '新增数据源'" size="480px" destroy-on-close>
      <el-form ref="formRef" :model="form" :rules="rules" label-width="88px" label-position="left">
        <el-form-item label="编码" prop="code">
          <el-input v-model="form.code" :disabled="isEdit" placeholder="如：tech-daily" maxlength="32" />
          <p class="field-tip">爬虫推送时按此编码归属，创建后不可修改</p>
        </el-form-item>
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如：科技日报" maxlength="64" />
        </el-form-item>
        <el-form-item label="图标">
          <IconField v-model:icon="form.icon" v-model:color="form.color" :fallback="form.name" />
          <p class="field-tip">建议方形 PNG / SVG，透明底，≤ 512KB</p>
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="form.kind">
            <el-radio-button value="crawler">爬虫</el-radio-button>
            <el-radio-button value="api">接口</el-radio-button>
            <el-radio-button value="rss">RSS</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="0" :max="9999" controls-position="right" />
          <span class="dim switch-tip">数字越小越靠前，客户端按此顺序展示</span>
        </el-form-item>
        <el-form-item label="主页">
          <el-input v-model="form.homepage" placeholder="https://" maxlength="255" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
          <span class="dim switch-tip">停用后拒绝该来源的写入</span>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="drawer = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">保存</el-button>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 14px 16px;
  border-bottom: 1px solid var(--line-soft);
}

.src-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}

.src-icon {
  width: 34px;
  height: 34px;
  border-radius: 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  flex: none;
  color: #fff;
  font-size: 14px;
  font-weight: 600;
}

.src-icon img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.src-meta {
  min-width: 0;
}

.src-name {
  font-weight: 500;
  color: var(--ink-800);
}

.field-tip {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--ink-400);
  line-height: 1.4;
}

.src-remark {
  margin-top: 2px;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.code {
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--bg-subtle);
  color: var(--ink-600);
}

.dim {
  color: var(--ink-400);
}

.nowrap {
  white-space: nowrap;
}

.field-tip {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--ink-400);
}

.switch-tip {
  margin-left: 10px;
  font-size: 12px;
}

.pager {
  display: flex;
  justify-content: flex-end;
  padding: 12px 20px;
}

.empty {
  padding: 36px 0;
  text-align: center;
  color: var(--ink-600);

  p {
    margin: 2px 0;
  }
}
</style>
