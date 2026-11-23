<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Refresh, Search } from '@element-plus/icons-vue'
import { logApi } from '@/api/ops'
import type { LoginLogRow } from '@/api/types'
import { fmtTime, fromNow } from '@/utils/format'

const rows = ref<LoginLogRow[]>([])
const total = ref(0)
const loading = ref(false)
const clearing = ref(false)

const query = reactive({
  page: 1,
  page_size: 20,
  keyword: '',
  status: undefined as number | undefined,
  range: [] as string[],
})

function buildQuery() {
  return {
    page: query.page,
    page_size: query.page_size,
    keyword: query.keyword || undefined,
    status: query.status,
    created_at_gte: query.range?.[0] || undefined,
    created_at_lte: query.range?.[1] || undefined,
  }
}

async function load() {
  loading.value = true
  try {
    const res = await logApi.logins(buildQuery())
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
  query.range = []
  reload()
}

async function clearLogs() {
  let days = 30
  try {
    const res = await ElMessageBox.prompt(
      '将删除该天数之前的登录记录，此操作不可恢复。请输入保留天数：',
      '清理登录日志',
      {
        confirmButtonText: '确认清理',
        cancelButtonText: '取消',
        inputValue: '30',
        inputPattern: /^\d{1,4}$/,
        inputErrorMessage: '请输入 1-9999 的整数',
        type: 'warning',
      },
    )
    days = Number(res.value)
  } catch {
    return
  }
  clearing.value = true
  try {
    const res = await logApi.clearLogins(days)
    ElMessage.success(`已清理 ${res.deleted} 条登录日志`)
    reload()
  } finally {
    clearing.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">登录日志</h1>
        <p class="page-desc">记录每次登录的 IP、终端与结果，用于排查异常登录</p>
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
            placeholder="搜索账号 / IP / 结果"
            :prefix-icon="Search"
            clearable
            style="width: 236px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select v-model="query.status" placeholder="全部结果" clearable style="width: 120px" @change="reload">
            <el-option label="登录成功" :value="1" />
            <el-option label="登录失败" :value="0" />
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
        <el-table-column label="账号" min-width="150">
          <template #default="{ row }">
            <div class="user-cell">
              <span class="avatar-dot">{{ (row.username || '?').slice(0, 1).toUpperCase() }}</span>
              <span class="u-name">{{ row.username || '—' }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="IP / 位置" min-width="150">
          <template #default="{ row }">
            <div class="num ip">{{ row.ip || '—' }}</div>
            <div class="sub">{{ row.location || '未知位置' }}</div>
          </template>
        </el-table-column>

        <el-table-column label="终端" min-width="170">
          <template #default="{ row }">
            <div class="sub">{{ row.browser || '—' }}</div>
            <div class="sub dim">{{ row.os || '—' }}</div>
          </template>
        </el-table-column>

        <el-table-column label="结果" width="110">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
              {{ row.status === 1 ? '成功' : '失败' }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="说明" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <span :class="row.status === 1 ? 'dim' : 'fail-text'">{{ row.message || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="时间" width="170">
          <template #default="{ row }">
            <div class="cell-time" :title="fmtTime(row.createdAt)">{{ fromNow(row.createdAt) }}</div>
            <div class="cell-sub num">{{ fmtTime(row.createdAt) }}</div>
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
  </div>
</template>

<style scoped lang="scss">
.user-cell {
  display: flex;
  align-items: center;
  gap: 9px;
}

.u-name {
  font-size: 13px;
  color: var(--ink-900);
}

.ip {
  font-size: 12.5px;
  color: var(--ink-800);
}

.sub {
  font-size: 11.5px;
  color: var(--ink-500);
  line-height: 1.5;
}

.fail-text {
  font-size: 12.5px;
  color: var(--danger-deep);
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
</style>
