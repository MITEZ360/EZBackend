<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { jobApi, type JobPayload } from '@/api/ops'
import type { JobLogRow, JobRow } from '@/api/types'
import { fmtTime, fromNow } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const auth = useAuthStore()

const CRON_PRESETS = computed(() => [
  { label: t('每分钟'), expr: '* * * * *' },
  { label: t('每 5 分钟'), expr: '*/5 * * * *' },
  { label: t('每小时整点'), expr: '0 * * * *' },
  { label: t('每天 02:00'), expr: '0 2 * * *' },
  { label: t('每周一 09:00'), expr: '0 9 * * 1' },
  { label: t('每月 1 日 03:00'), expr: '0 3 1 * *' },
])

const rows = ref<JobRow[]>([])
const total = ref(0)
const loading = ref(false)
const running = ref<Set<string>>(new Set())

const query = reactive({
  page: 1,
  page_size: 10,
  keyword: '',
  status: undefined as number | undefined,
})

const handlers = ref<string[]>([])

async function load() {
  loading.value = true
  try {
    const res = await jobApi.list({
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

/* ---------------------------------------------------------------- 表单 */
const formVisible = ref(false)
const editingId = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const PARAMS_EXAMPLE = '{"days": 30}'

const form = reactive<JobPayload>({
  name: '',
  cron: '*/5 * * * *',
  handler: '',
  params: '',
  status: 1,
  remark: '',
})

const rules = computed<FormRules>(() => ({
  name: [{ required: true, message: t('请输入任务名称'), trigger: 'blur' }],
  cron: [
    { required: true, message: t('请输入 cron 表达式'), trigger: 'blur' },
    {
      validator: (_r, v: string, cb) => {
        const parts = String(v ?? '').trim().split(/\s+/)
        if (parts.length !== 5) cb(new Error(t('需为标准 5 段格式：分 时 日 月 周')))
        else cb()
      },
      trigger: 'blur',
    },
  ],
  handler: [{ required: true, message: t('请选择执行器'), trigger: 'change' }],
  params: [
    {
      validator: (_r, v: string, cb) => {
        if (!String(v ?? '').trim()) return cb()
        try {
          const parsed = JSON.parse(String(v))
          if (typeof parsed !== 'object' || Array.isArray(parsed)) cb(new Error(t('参数需为 JSON 对象')))
          else cb()
        } catch {
          cb(new Error(t('参数不是合法的 JSON')))
        }
      },
      trigger: 'blur',
    },
  ],
}))

function resetForm() {
  Object.assign(form, {
    name: '',
    cron: '*/5 * * * *',
    handler: handlers.value[0] ?? '',
    params: '',
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

function openEdit(row: JobRow) {
  editingId.value = row.id
  Object.assign(form, {
    name: row.name,
    cron: row.cron,
    handler: row.handler,
    params: row.params ?? '',
    status: row.status,
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
      await jobApi.update(editingId.value, { ...form })
      ElMessage.success(t('已保存'))
    } else {
      await jobApi.create({ ...form })
      ElMessage.success(t('任务已创建'))
    }
    formVisible.value = false
    load()
  } finally {
    submitting.value = false
  }
}

async function toggleStatus(row: JobRow) {
  const next = row.status === 1 ? 0 : 1
  const action = next === 1 ? t('任务已启用') : t('任务已停用')
  await jobApi.setStatus(row.id, next)
  ElMessage.success(action)
  load()
}

async function runNow(row: JobRow) {
  try {
    await ElMessageBox.confirm(
      t('立即执行「{name}」？本次执行不影响原有调度计划。', { name: row.name }),
      t('手动执行'),
      {
        confirmButtonText: t('执行'),
        cancelButtonText: t('取消'),
        type: 'info',
      },
    )
  } catch {
    return
  }
  running.value = new Set(running.value).add(row.id)
  try {
    await jobApi.run(row.id)
    ElMessage.success(t('已触发执行，可在执行日志中查看结果'))
    setTimeout(load, 600)
  } finally {
    const next = new Set(running.value)
    next.delete(row.id)
    running.value = next
  }
}

async function removeJob(row: JobRow) {
  try {
    await ElMessageBox.confirm(
      t('删除后该调度计划将停止且不会恢复。确定删除「{name}」吗？', { name: row.name }),
      t('删除任务'),
      {
        confirmButtonText: t('删除'),
        cancelButtonText: t('取消'),
        type: 'error',
      },
    )
  } catch {
    return
  }
  await jobApi.remove(row.id)
  ElMessage.success(t('已删除'))
  if (rows.value.length === 1 && query.page > 1) query.page -= 1
  load()
}

/* -------------------------------------------------------------- 执行日志 */
const logVisible = ref(false)
const logJob = ref<JobRow | null>(null)
const logs = ref<JobLogRow[]>([])
const logTotal = ref(0)
const logLoading = ref(false)
const logQuery = reactive({ page: 1, page_size: 10 })
const logDetail = ref<JobLogRow | null>(null)

async function openLogs(row: JobRow) {
  logJob.value = row
  logVisible.value = true
  logQuery.page = 1
  logDetail.value = null
  await loadLogs()
}

async function loadLogs() {
  if (!logJob.value) return
  logLoading.value = true
  try {
    const res = await jobApi.logs({
      jobId: logJob.value.id,
      page: logQuery.page,
      page_size: logQuery.page_size,
    })
    logs.value = res.list ?? []
    logTotal.value = res.total ?? 0
  } finally {
    logLoading.value = false
  }
}

function successRate(row: JobRow) {
  if (!row.runCount) return '—'
  return `${Math.round(((row.runCount - row.failCount) / row.runCount) * 100)}%`
}

function showLogDetail(row: JobLogRow) {
  logDetail.value = row
}

onMounted(async () => {
  await Promise.all([
    load(),
    jobApi.handlers().then((h) => (handlers.value = h ?? [])).catch(() => undefined),
  ])
})
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('定时任务') }}</h1>
        <p class="page-desc">{{ $t('基于 cron 的周期调度，支持跳过并发执行与手动触发') }}</p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'ops:job:create'" type="primary" :icon="Plus" @click="openCreate">
          {{ $t('新增任务') }}
        </el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            :placeholder="$t('搜索任务名 / 执行器')"
            :prefix-icon="Search"
            clearable
            style="width: 236px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select v-model="query.status" :placeholder="$t('全部状态')" clearable style="width: 118px" @change="reload">
            <el-option :label="$t('运行中')" :value="1" />
            <el-option :label="$t('已停用')" :value="0" />
          </el-select>
          <el-button @click="reload">{{ $t('查询') }}</el-button>
          <el-button text @click="resetQuery">{{ $t('重置') }}</el-button>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle :title="$t('刷新')" @click="load" />
        </div>
      </div>

      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="$t('任务')" min-width="220">
          <template #default="{ row }">
            <div class="job-cell">
              <div class="job-dot" :class="row.status === 1 ? 'on' : 'off'" />
              <div class="job-lines">
                <span class="j-name">{{ row.name }}</span>
                <code class="j-handler">{{ row.handler }}</code>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('执行计划')" min-width="150">
          <template #default="{ row }">
            <code class="cron">{{ row.cron }}</code>
            <div v-if="row.nextRunAt" class="next-run">{{ $t('下次 {time}', { time: fromNow(row.nextRunAt) }) }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('最近执行')" width="170">
          <template #default="{ row }">
            <template v-if="row.lastRunAt">
              <div class="last-run" :title="fmtTime(row.lastRunAt)">
                {{ fromNow(row.lastRunAt) }}
                <span class="result" :class="row.lastResult === 'success' ? 'ok' : 'fail'">
                  {{ row.lastResult === 'success' ? $t('成功') : $t('失败') }}
                </span>
              </div>
            </template>
            <span v-else class="dim">{{ $t('尚未执行') }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('成功 / 失败')" width="130">
          <template #default="{ row }">
            <span class="num stats">
              <b class="ok">{{ row.runCount - row.failCount }}</b>
              <span class="sep">/</span>
              <b :class="row.failCount ? 'fail' : 'dim'">{{ row.failCount }}</b>
            </span>
            <div class="rate">{{ $t('成功率 {rate}', { rate: successRate(row) }) }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('状态')" width="96" align="center">
          <template #default="{ row }">
            <el-switch
              :model-value="row.status"
              :active-value="1"
              :inactive-value="0"
              :disabled="!auth.can('ops:job:update')"
              size="small"
              @change="toggleStatus(row)"
            />
          </template>
        </el-table-column>

        <template #empty>
          <div class="table-empty">
            <p>{{ query.keyword || query.status !== undefined ? $t('没有匹配的任务') : $t('还没有配置定时任务') }}</p>
            <span>{{ $t('任务由「cron 表达式 + 已注册执行器」组成，支持手动触发与执行日志追溯') }}</span>
            <el-button v-perm="'ops:job:create'" size="small" type="primary" plain @click="openCreate">
              {{ $t('创建第一个任务') }}
            </el-button>
          </div>
        </template>

        <el-table-column label="" width="196" align="right" fixed="right">
          <template #default="{ row }">
            <el-button
              v-perm="'ops:job:run'"
              link
              type="primary"
              :loading="running.has(row.id)"
              @click="runNow(row)"
            >
              {{ $t('执行') }}
            </el-button>
            <el-button link type="primary" @click="openLogs(row)">{{ $t('日志') }}</el-button>
            <el-button v-perm="'ops:job:update'" link type="primary" @click="openEdit(row)">{{ $t('编辑') }}</el-button>
            <el-button v-perm="'ops:job:delete'" link type="danger" @click="removeJob(row)">{{ $t('删除') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <span class="total">{{ $t('共 {count} 个任务', { count: total }) }}</span>
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[10, 20, 50]"
          layout="sizes, prev, pager, next"
          background
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </div>

    <!-- 任务表单 -->
    <el-dialog
      v-model="formVisible"
      :title="isEdit ? $t('编辑任务') : $t('新增任务')"
      width="620px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="form-grid">
          <el-form-item :label="$t('任务名称')" prop="name">
            <el-input v-model="form.name" :placeholder="$t('如 清理过期日志')" />
          </el-form-item>
          <el-form-item :label="$t('执行器')" prop="handler">
            <el-select v-model="form.handler" :placeholder="$t('选择已注册的执行器')" style="width: 100%">
              <el-option v-for="h in handlers" :key="h" :label="h" :value="h" />
            </el-select>
          </el-form-item>
        </div>

        <el-form-item :label="$t('cron 表达式')" prop="cron">
          <el-input v-model="form.cron" :placeholder="$t('分 时 日 月 周，如 */5 * * * *')" class="mono-input" />
          <div class="cron-presets">
            <span class="cp-label">{{ $t('常用：') }}</span>
            <button
              v-for="p in CRON_PRESETS"
              :key="p.expr"
              type="button"
              class="cp-item"
              :class="{ active: form.cron === p.expr }"
              @click="form.cron = p.expr"
            >
              {{ p.label }}
            </button>
          </div>
        </el-form-item>

        <el-form-item :label="$t('执行参数')" prop="params">
          <el-input
            v-model="form.params"
            type="textarea"
            :rows="3"
            :placeholder="$t('JSON 对象，如 {example}；无需参数可留空', { example: PARAMS_EXAMPLE })"
            class="mono-input"
          />
        </el-form-item>

        <div class="form-grid">
          <el-form-item :label="$t('状态')">
            <el-switch v-model="form.status" :active-value="1" :inactive-value="0" :active-text="$t('启用')" :inactive-text="$t('停用')" />
          </el-form-item>
          <el-form-item :label="$t('备注')">
            <el-input v-model="form.remark" maxlength="255" :placeholder="$t('选填')" />
          </el-form-item>
        </div>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ $t('保存') }}</el-button>
      </template>
    </el-dialog>

    <!-- 执行日志 -->
    <el-drawer v-model="logVisible" :size="620" append-to-body>
      <template #header>
        <div class="log-head">
          <h3>{{ $t('执行日志') }}</h3>
          <p v-if="logJob">{{ logJob.name }} · <code class="mono">{{ logJob.cron }}</code></p>
        </div>
      </template>

      <div class="log-body">
        <el-table
          v-loading="logLoading"
          :data="logs"
          row-key="id"
          size="small"
          @row-click="showLogDetail"
        >
          <el-table-column :label="$t('开始时间')" min-width="150">
            <template #default="{ row }">
              <span class="num fs-12">{{ fmtTime(row.startedAt) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('结果')" width="80">
            <template #default="{ row }">
              <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
                {{ row.status === 1 ? $t('成功') : $t('失败') }}
              </span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('耗时')" width="86" align="right">
            <template #default="{ row }">
              <span class="num dim fs-12">{{ row.costMs }} ms</span>
            </template>
          </el-table-column>
          <el-table-column :label="$t('输出')" min-width="180" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="dim fs-12">{{ row.output || '—' }}</span>
            </template>
          </el-table-column>
        </el-table>

        <div v-if="logTotal > logQuery.page_size" class="log-pager">
          <el-pagination
            v-model:current-page="logQuery.page"
            :page-size="logQuery.page_size"
            :total="logTotal"
            layout="prev, pager, next"
            small
            background
            @current-change="loadLogs"
          />
        </div>

        <div v-if="logDetail" class="log-output">
          <div class="lo-head">
            <span>{{ $t('执行输出') }}</span>
            <span class="dim fs-12">{{ fmtTime(logDetail.startedAt) }}</span>
          </div>
          <pre class="lo-body">{{ logDetail.output || $t('（无输出）') }}</pre>
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.job-cell {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  min-width: 0;
}

.job-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  margin-top: 6px;
  flex-shrink: 0;

  &.on {
    background: var(--ok);
    box-shadow: 0 0 0 3px rgba(18, 183, 106, 0.14);
  }

  &.off {
    background: var(--ink-300);
  }
}

.job-lines {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.j-name {
  font-size: 13px;
  color: var(--ink-900);
}

.j-handler {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-400);
}

.cron {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--ink-700);
  background: var(--ink-100);
}

.next-run {
  margin-top: 3px;
  font-size: 11px;
  color: var(--ink-400);
}

.last-run {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--ink-700);
}

.result {
  padding: 0 5px;
  border-radius: 4px;
  font-size: 10.5px;
  line-height: 16px;

  &.ok {
    color: #0f766e;
    background: #e6f6f4;
  }

  &.fail {
    color: var(--danger-deep);
    background: var(--danger-bg);
  }
}

.stats {
  font-size: 13px;

  .ok {
    color: var(--ok);
  }

  .fail {
    color: var(--danger);
  }

  .sep {
    margin: 0 3px;
    color: var(--ink-300);
  }
}

.rate {
  font-size: 11px;
  color: var(--ink-400);
}

.table-empty {
  padding: 40px 0;
  color: var(--ink-400);

  p {
    font-size: 13px;
    color: var(--ink-600);
    margin-bottom: 4px;
  }

  span {
    display: block;
    font-size: 11.5px;
    margin-bottom: 14px;
  }
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

.cron-presets {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}

.cp-label {
  font-size: 11.5px;
  color: var(--ink-400);
}

.cp-item {
  padding: 2px 8px;
  border: 1px solid var(--line);
  border-radius: var(--r-full);
  font-size: 11.5px;
  color: var(--ink-600);
  background: var(--bg-card);
  cursor: pointer;
  transition: all 0.14s ease;

  &:hover {
    border-color: var(--brand-200);
    color: var(--brand-700);
  }

  &.active {
    border-color: var(--brand-500);
    color: var(--brand-700);
    background: var(--brand-50);
  }
}

.mono-input :deep(.el-input__inner),
.mono-input :deep(.el-textarea__inner) {
  font-family: var(--font-mono);
  font-size: 12.5px;
}

/* ------------------------------------------------------------ 日志抽屉 */
.log-head {
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

.log-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  height: 100%;
  min-height: 0;
}

.log-pager {
  display: flex;
  justify-content: flex-end;
}

.log-output {
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  overflow: hidden;
}

.lo-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  font-size: 12px;
  font-weight: 500;
  color: var(--ink-700);
  background: var(--ink-50);
  border-bottom: 1px solid var(--line-soft);
}

.lo-body {
  margin: 0;
  padding: 12px;
  max-height: 240px;
  overflow: auto;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.7;
  color: var(--ink-700);
  white-space: pre-wrap;
  word-break: break-all;
}

@media (max-width: 720px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
