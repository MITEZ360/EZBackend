<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { logApi } from '@/api/ops'
import type { OnlineSessionRow } from '@/api/types'
import { fmtDuration, fmtTime, fromNow } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()

const rows = ref<OnlineSessionRow[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive({ page: 1, page_size: 20, keyword: '' })

async function load() {
  loading.value = true
  try {
    const res = await logApi.online({
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

async function kick(row: OnlineSessionRow) {
  const self = row.id === auth.user?.sessionId
  try {
    await ElMessageBox.confirm(
      self
        ? '这是你自己当前的会话，强制下线后需要重新登录。确定继续吗？'
        : `强制下线后「${row.nickname || row.username}」需要重新登录。确定继续吗？`,
      '强制下线',
      { confirmButtonText: '下线', cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return
  }
  await logApi.kick(row.id)
  ElMessage.success('已强制下线')
  load()
}

function lasting(loginAt: string) {
  const sec = Math.max(0, (Date.now() - new Date(loginAt).getTime()) / 1000)
  return fmtDuration(sec)
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">在线用户</h1>
        <p class="page-desc">当前持有有效会话的登录用户，可强制下线以回收访问权限</p>
      </div>
      <div class="page-actions">
        <span class="live-badge">
          <span class="live-dot" />
          <b class="num">{{ total }}</b> 人在线
        </span>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            placeholder="搜索昵称 / 账号 / IP"
            :prefix-icon="Search"
            clearable
            style="width: 250px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-button @click="reload">查询</el-button>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle title="刷新" @click="load" />
        </div>
      </div>

      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column label="用户" min-width="200">
          <template #default="{ row }">
            <div class="user-cell">
              <span class="avatar-dot">{{ (row.nickname || row.username || '?').slice(0, 1) }}</span>
              <div class="u-lines">
                <span class="u-name">
                  {{ row.nickname || row.username }}
                  <em v-if="row.id === auth.user?.sessionId" class="badge">当前会话</em>
                </span>
                <span class="u-sub">@{{ row.username }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="IP" width="150">
          <template #default="{ row }">
            <span class="num muted">{{ row.ip || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="终端" min-width="180">
          <template #default="{ row }">
            <div class="term">{{ row.browser || '—' }}</div>
            <div class="term dim">{{ row.os || '—' }}</div>
          </template>
        </el-table-column>

        <el-table-column label="登录时间" width="160">
          <template #default="{ row }">
            <div class="cell-time" :title="fmtTime(row.loginAt)">已在线 {{ lasting(row.loginAt) }}</div>
            <div class="cell-sub num">{{ fmtTime(row.loginAt, false) }}</div>
          </template>
        </el-table-column>

        <el-table-column label="最后活跃" width="130">
          <template #default="{ row }">
            <span class="muted fs-12" :title="fmtTime(row.lastSeenAt)">{{ fromNow(row.lastSeenAt) }}</span>
          </template>
        </el-table-column>

        <el-table-column label="" width="110" align="right" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'ops:online:list'" link type="danger" @click="kick(row)">
              强制下线
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <div class="table-empty">当前没有在线会话</div>
        </template>
      </el-table>

      <div v-if="total > query.page_size" class="pager">
        <span class="total">共 {{ total }} 个会话</span>
        <el-pagination
          v-model:current-page="query.page"
          :page-size="query.page_size"
          :total="total"
          layout="prev, pager, next"
          background
          @current-change="load"
        />
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.live-badge {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 5px 12px;
  border: 1px solid var(--line);
  border-radius: var(--r-full);
  font-size: 12.5px;
  color: var(--ink-600);
  background: var(--bg-card);

  b {
    color: var(--ink-900);
    font-weight: 650;
  }
}

.live-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ok);
  box-shadow: 0 0 0 3px rgba(18, 183, 106, 0.14);
}

.user-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}

.u-lines {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
}

.u-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-900);
}

.u-sub {
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

.term {
  font-size: 12.5px;
  color: var(--ink-700);
  line-height: 1.5;
}

.cell-time {
  font-size: 12.5px;
  color: var(--ink-700);
}

.cell-sub {
  font-size: 11px;
  color: var(--ink-400);
}

.table-empty {
  padding: 44px 0;
  font-size: 13px;
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
