<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Refresh, Search } from '@element-plus/icons-vue'
import { logApi } from '@/api/ops'
import type { OperationLogRow } from '@/api/types'
import { fmtTime, fromNow } from '@/utils/format'

const rows = ref<OperationLogRow[]>([])
const total = ref(0)
const loading = ref(false)
const clearing = ref(false)

const query = reactive({
  page: 1,
  page_size: 20,
  keyword: '',
  module: '',
  method: '',
  status: '' as '' | 'success' | 'fail',
  range: [] as string[],
})

const MODULES = [
  '用户管理', '角色管理', '菜单管理', '部门管理', '字典管理',
  '参数配置', '通知公告', '定时任务', '文件管理', '登录日志', '操作日志', '在线用户',
]

async function load() {
  loading.value = true
  try {
    const res = await logApi.operations({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
      module: query.module || undefined,
      status_in: statusCodes(query.status),
      created_at_gte: query.range?.[0] || undefined,
      created_at_lte: query.range?.[1] || undefined,
    })
    rows.value = res.list ?? []
    total.value = res.total ?? 0
  } finally {
    loading.value = false
  }
}

/** 后端按 HTTP 状态码存储，成功/失败各自映射为状态码集合 */
function statusCodes(v: '' | 'success' | 'fail') {
  if (v === 'success') return '200,201,204'
  if (v === 'fail') return '400,401,403,404,409,429,500,501,503'
  return undefined
}

function reload() {
  query.page = 1
  load()
}

function resetQuery() {
  query.keyword = ''
  query.module = ''
  query.method = ''
  query.status = ''
  query.range = []
  reload()
}

async function clearLogs() {
  let days = 30
  try {
    const res = await ElMessageBox.prompt('将删除该天数之前的操作记录，此操作不可恢复。请输入保留天数：', '清理操作日志', {
      confirmButtonText: '确认清理',
      cancelButtonText: '取消',
      inputValue: '90',
      inputPattern: /^\d{1,4}$/,
      inputErrorMessage: '请输入 1-9999 的整数',
      type: 'warning',
    })
    days = Number(res.value)
  } catch {
    return
  }
  clearing.value = true
  try {
    const res = await logApi.clearOperations(days)
    ElMessage.success(`已清理 ${res.deleted} 条操作日志`)
    reload()
  } finally {
    clearing.value = false
  }
}

/* ---------------------------------------------------------------- 详情 */
const detailVisible = ref(false)
const detail = ref<OperationLogRow | null>(null)
const detailLoading = ref(false)

async function openDetail(row: OperationLogRow) {
  detail.value = row
  detailVisible.value = true
  detailLoading.value = true
  try {
    detail.value = await logApi.operation(row.id)
  } finally {
    detailLoading.value = false
  }
}

/** 体量可能很大，格式化失败时原样展示 */
function pretty(raw?: string) {
  if (!raw) return ''
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function shortTime(v: string) {
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return fmtTime(v, false)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function methodTone(m: string) {
  const map: Record<string, string> = { GET: 'is-get', POST: 'is-post', PUT: 'is-put', DELETE: 'is-del', PATCH: 'is-put' }
  return map[m?.toUpperCase()] ?? 'is-get'
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">操作日志</h1>
        <p class="page-desc">记录写操作的行为、耗时与请求响应，敏感字段已在落库前脱敏</p>
      </div>
      <div class="page-actions">
        <el-button :icon="Delete" :loading="clearing" @click="clearLogs">清理历史</el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            placeholder="搜索账号 / 路径 / 动作"
            :prefix-icon="Search"
            clearable
            style="width: 236px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select v-model="query.module" placeholder="全部模块" clearable filterable style="width: 130px" @change="reload">
            <el-option v-for="m in MODULES" :key="m" :label="m" :value="m" />
          </el-select>
          <el-select v-model="query.status" placeholder="全部结果" clearable style="width: 118px" @change="reload">
            <el-option label="成功" value="success" />
            <el-option label="失败" value="fail" />
          </el-select>
          <el-date-picker
            v-model="query.range"
            type="daterange"
            value-format="YYYY-MM-DD"
            range-separator="至"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
            style="width: 240px"
            @change="reload"
          />
          <el-button @click="reload">查询</el-button>
          <el-button text @click="resetQuery">重置</el-button>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle title="刷新" @click="load" />
        </div>
      </div>

      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column label="操作内容" min-width="185">
          <template #default="{ row }">
            <div class="act-cell">
              <span class="module">{{ row.module || '未归类' }}</span>
              <span class="action">{{ row.action || '—' }}</span>
            </div>
            <div class="actor">
              <span :class="row.username ? 'muted' : 'dim'">{{ row.username || '—' }}</span>
              <span class="num dim">{{ row.ip || '' }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="请求" min-width="230">
          <template #default="{ row }">
            <div class="req-line">
              <span class="method" :class="methodTone(row.method)">{{ row.method }}</span>
              <span class="path mono">{{ row.path }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="结果" width="78" align="center">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status < 400 ? 'is-on' : 'is-off'">
              {{ row.status }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="耗时" width="80" align="right">
          <template #default="{ row }">
            <span class="num cost" :class="{ slow: row.costMs > 500 }">{{ row.costMs }} ms</span>
          </template>
        </el-table-column>

        <el-table-column label="时间" width="116">
          <template #default="{ row }">
            <div class="cell-time" :title="fmtTime(row.createdAt)">{{ fromNow(row.createdAt) }}</div>
            <div class="cell-sub num">{{ shortTime(row.createdAt) }}</div>
          </template>
        </el-table-column>

        <el-table-column label="" width="70" align="right" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <span class="total">共 {{ total }} 条记录</span>
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[20, 50, 100]"
          layout="sizes, prev, pager, next, jumper"
          background
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </div>

    <!-- 详情 -->
    <el-drawer v-model="detailVisible" :size="640" append-to-body>
      <template #header>
        <div class="d-head">
          <h3>{{ detail?.action || '操作详情' }}</h3>
          <p>
            <span class="method" :class="methodTone(detail?.method ?? '')">{{ detail?.method }}</span>
            <code class="mono">{{ detail?.path }}</code>
          </p>
        </div>
      </template>

      <div v-loading="detailLoading" v-if="detail" class="d-body">
        <dl class="meta-grid">
          <div><dt>操作人</dt><dd>{{ detail.username || '—' }}</dd></div>
          <div><dt>所属模块</dt><dd>{{ detail.module || '—' }}</dd></div>
          <div><dt>来源 IP</dt><dd class="num">{{ detail.ip || '—' }}</dd></div>
          <div><dt>响应状态</dt><dd>{{ detail.status }}</dd></div>
          <div><dt>耗时</dt><dd class="num">{{ detail.costMs }} ms</dd></div>
          <div><dt>发生时间</dt><dd class="num">{{ fmtTime(detail.createdAt) }}</dd></div>
          <div class="span-2">
            <dt>Trace ID</dt>
            <dd class="mono">{{ detail.traceId || '—' }}</dd>
          </div>
        </dl>

        <div v-if="detail.error" class="block">
          <div class="block-head">错误信息</div>
          <pre class="code-block is-error">{{ detail.error }}</pre>
        </div>

        <div class="block">
          <div class="block-head">
            请求参数
            <span class="bh-hint">敏感字段已脱敏，超长内容已截断</span>
          </div>
          <pre class="code-block">{{ pretty(detail.request) || '（无请求体）' }}</pre>
        </div>

        <div class="block">
          <div class="block-head">响应内容</div>
          <pre class="code-block">{{ pretty(detail.response) || '（无响应体）' }}</pre>
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.act-cell {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.module {
  font-size: 13px;
  color: var(--ink-900);
}

.action {
  font-size: 11.5px;
  color: var(--ink-500);
}

.actor {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 3px;
  font-size: 11px;

  .num {
    color: var(--ink-300);
  }
}

.req-line {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}

.method {
  flex-shrink: 0;
  padding: 0 5px;
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  line-height: 17px;

  &.is-get {
    color: #0f766e;
    background: #e6f6f4;
  }

  &.is-post {
    color: var(--brand-700);
    background: var(--brand-50);
  }

  &.is-put {
    color: #b45309;
    background: #fef7e6;
  }

  &.is-del {
    color: var(--danger-deep);
    background: var(--danger-bg);
  }
}

.path {
  font-size: 11.5px;
  color: var(--ink-600);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sub {
  font-size: 11px;
  color: var(--ink-400);
}

.cost {
  font-size: 12.5px;
  color: var(--ink-700);

  &.slow {
    color: #b45309;
    font-weight: 600;
  }
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

/* ------------------------------------------------------------ 详情抽屉 */
.d-head {
  h3 {
    font-size: 15px;
    font-weight: 600;
    color: var(--ink-900);
  }

  p {
    display: flex;
    align-items: center;
    gap: 7px;
    margin-top: 5px;
    font-size: 11.5px;
    color: var(--ink-500);
  }
}

.d-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.meta-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1px;
  margin: 0;
  background: var(--line-soft);
  border: 1px solid var(--line-soft);
  border-radius: var(--r-md);
  overflow: hidden;

  > div {
    padding: 9px 12px;
    background: var(--bg-card);
  }

  .span-2 {
    grid-column: span 2;
  }

  dt {
    font-size: 11px;
    color: var(--ink-400);
    margin-bottom: 2px;
  }

  dd {
    margin: 0;
    font-size: 12.5px;
    color: var(--ink-800);
    word-break: break-all;
  }
}

.block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
  font-size: 12px;
  font-weight: 500;
  color: var(--ink-700);
}

.bh-hint {
  font-size: 11px;
  font-weight: 400;
  color: var(--ink-400);
}

.code-block {
  margin: 0;
  padding: 12px;
  max-height: 320px;
  overflow: auto;
  border: 1px solid var(--line);
  border-radius: var(--r-md);
  background: var(--ink-50);
  font-family: var(--font-mono);
  font-size: 11.5px;
  line-height: 1.7;
  color: var(--ink-700);
  white-space: pre-wrap;
  word-break: break-all;

  &.is-error {
    color: var(--danger-deep);
    background: var(--danger-bg);
    border-color: var(--danger-border);
  }
}
</style>
