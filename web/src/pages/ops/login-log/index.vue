<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Refresh, Search } from '@element-plus/icons-vue'
import { logApi } from '@/api/ops'
import type { LoginLogRow } from '@/api/types'
import { fmtTime, fromNow } from '@/utils/format'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

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
      t('将删除该天数之前的登录记录，此操作不可恢复。请输入保留天数：'),
      t('清理登录日志'),
      {
        confirmButtonText: t('确认清理'),
        cancelButtonText: t('取消'),
        inputValue: '30',
        inputPattern: /^\d{1,4}$/,
        inputErrorMessage: t('请输入 1-9999 的整数'),
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
    ElMessage.success(t('已清理 {count} 条登录日志', { count: res.deleted }))
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
        <h1 class="page-title">{{ $t('登录日志') }}</h1>
        <p class="page-desc">{{ $t('记录每次登录的 IP、终端与结果，用于排查异常登录') }}</p>
      </div>
      <div class="page-actions">
        <el-button :icon="Delete" :loading="clearing" @click="clearLogs">{{ $t('清理历史') }}</el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            :placeholder="$t('搜索账号 / IP / 结果')"
            :prefix-icon="Search"
            clearable
            style="width: 236px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select v-model="query.status" :placeholder="$t('全部结果')" clearable style="width: 120px" @change="reload">
            <el-option :label="$t('登录成功')" :value="1" />
            <el-option :label="$t('登录失败')" :value="0" />
          </el-select>
          <el-date-picker
            v-model="query.range"
            type="daterange"
            value-format="YYYY-MM-DD"
            :range-separator="$t('至')"
            :start-placeholder="$t('开始日期')"
            :end-placeholder="$t('结束日期')"
            style="width: 240px"
            @change="reload"
          />
          <el-button @click="reload">{{ $t('查询') }}</el-button>
          <el-button text @click="resetQuery">{{ $t('重置') }}</el-button>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle :title="$t('刷新')" @click="load" />
        </div>
      </div>

      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="$t('账号')" min-width="150">
          <template #default="{ row }">
            <div class="user-cell">
              <span class="avatar-dot">{{ (row.username || '?').slice(0, 1).toUpperCase() }}</span>
              <span class="u-name">{{ row.username || '—' }}</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('IP / 位置')" min-width="150">
          <template #default="{ row }">
            <div class="num ip">{{ row.ip || '—' }}</div>
            <div class="sub">{{ row.location || $t('未知位置') }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('终端')" min-width="170">
          <template #default="{ row }">
            <div class="sub">{{ row.browser || '—' }}</div>
            <div class="sub dim">{{ row.os || '—' }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('结果')" width="110">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 1 ? 'is-on' : 'is-off'">
              {{ row.status === 1 ? $t('成功') : $t('失败') }}
            </span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('说明')" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <span :class="row.status === 1 ? 'dim' : 'fail-text'">{{ row.message || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('时间')" width="170">
          <template #default="{ row }">
            <div class="cell-time" :title="fmtTime(row.createdAt)">{{ fromNow(row.createdAt) }}</div>
            <div class="cell-sub num">{{ fmtTime(row.createdAt) }}</div>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <span class="total">{{ $t('共 {count} 条记录', { count: total }) }}</span>
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
