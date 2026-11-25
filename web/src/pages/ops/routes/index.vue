<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { CopyDocument, Refresh, Search } from '@element-plus/icons-vue'
import { routeApi } from '@/api/ops'
import type { RouteEntry } from '@/api/types'

const { t } = useI18n()

const rows = ref<RouteEntry[]>([])
const total = ref(0)
const loading = ref(false)
const groups = ref<{ name: string; count: number }[]>([])
const stats = reactive({ registered: 0, unregistered: 0 })

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD']

const query = reactive({
  page: 1,
  page_size: 20,
  keyword: '',
  method: '',
  group: '',
})

async function load() {
  loading.value = true
  try {
    const res = await routeApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword.trim() || undefined,
      method: query.method || undefined,
      group: query.group || undefined,
    })
    rows.value = res.list ?? []
    total.value = res.total ?? 0
    groups.value = res.groups ?? []
    stats.registered = res.registered ?? 0
    stats.unregistered = res.unregistered ?? 0
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
  query.method = ''
  query.group = ''
  reload()
}

/* -------------------------------------------------------------- 示例抽屉 */
const drawer = ref(false)
const current = ref<RouteEntry | null>(null)

function openDetail(row: RouteEntry) {
  current.value = row
  drawer.value = true
}

async function copyExample() {
  const text = current.value?.example ?? ''
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(t('示例已复制'))
  } catch {
    ElMessage.error(t('复制失败，请手动选择文本'))
  }
}

function permText(row: RouteEntry) {
  if (row.public) return t('公开')
  if (row.auth === 'api_key') return t('API 密钥')
  return row.perm || t('登录即可')
}

function rowKey(row: RouteEntry) {
  return `${row.method} ${row.path}`
}

const drawerTitle = computed(() => {
  const c = current.value
  return c ? `${c.method} ${c.path}` : ''
})

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('路由管理') }}</h1>
        <p class="page-desc">
          {{ $t('运行时实际注册的接口清单，标注作用、权限与完整请求示例，供联调与排障使用') }}
        </p>
      </div>
      <div class="page-actions">
        <span class="stat-pill">{{ $t('共 {n} 条', { n: total }) }}</span>
        <span class="stat-pill ok">{{ $t('已登记 {n}', { n: stats.registered }) }}</span>
        <span v-if="stats.unregistered" class="stat-pill warn">
          {{ $t('未登记 {n}', { n: stats.unregistered }) }}
        </span>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            :placeholder="$t('搜索路径 / 作用 / 权限')"
            :prefix-icon="Search"
            clearable
            style="width: 250px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select
            v-model="query.method"
            :placeholder="$t('全部方法')"
            clearable
            style="width: 126px"
            @change="reload"
          >
            <el-option v-for="m in METHODS" :key="m" :label="m" :value="m" />
          </el-select>
          <el-select
            v-model="query.group"
            :placeholder="$t('全部分组')"
            clearable
            filterable
            style="width: 150px"
            @change="reload"
          >
            <el-option
              v-for="g in groups"
              :key="g.name"
              :label="`${g.name} (${g.count})`"
              :value="g.name"
            />
          </el-select>
          <el-button @click="reload">{{ $t('查询') }}</el-button>
          <el-button text @click="resetQuery">{{ $t('重置') }}</el-button>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle :title="$t('刷新')" @click="load" />
        </div>
      </div>

      <el-table v-loading="loading" :data="rows" :row-key="rowKey">
        <el-table-column :label="$t('方法')" width="88">
          <template #default="{ row }">
            <span class="method" :class="`is-${row.method.toLowerCase()}`">{{ row.method }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('路径')" min-width="262">
          <template #default="{ row }">
            <code class="rpath">{{ row.path }}</code>
          </template>
        </el-table-column>
        <el-table-column :label="$t('作用')" min-width="190">
          <template #default="{ row }">
            <div class="title-cell">
              <span class="rt-title">{{ row.title }}</span>
              <span v-if="!row.registered" class="unregistered">{{ $t('未登记') }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="desc" :label="$t('备注')" min-width="240" show-overflow-tooltip />
        <el-table-column :label="$t('分组')" width="112">
          <template #default="{ row }">
            <span class="group-tag">{{ row.group }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('权限')" min-width="186">
          <template #default="{ row }">
            <code v-if="row.perm" class="perm">{{ row.perm }}</code>
            <span v-else class="perm-free">{{ permText(row) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('示例')" width="88" align="center" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">{{ $t('查看') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="sizes, prev, pager, next"
          background
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </div>

    <el-drawer v-model="drawer" :title="drawerTitle" size="620px">
      <div v-if="current" class="detail">
        <dl class="kv">
          <div><dt>{{ $t('作用') }}</dt><dd>{{ current.title }}</dd></div>
          <div><dt>{{ $t('分组') }}</dt><dd>{{ current.group }}</dd></div>
          <div><dt>{{ $t('权限') }}</dt><dd>{{ permText(current) }}</dd></div>
          <div><dt>{{ $t('处理器') }}</dt><dd class="mono">{{ current.name || '—' }}</dd></div>
        </dl>

        <div class="block">
          <div class="block-head">{{ $t('备注') }}</div>
          <p class="block-text">{{ current.desc }}</p>
        </div>

        <div class="block">
          <div class="block-head">
            <span>{{ $t('完整示例') }}</span>
            <el-button size="small" :icon="CopyDocument" @click="copyExample">
              {{ $t('复制') }}
            </el-button>
          </div>
          <pre class="code">{{ current.example }}</pre>
          <p class="code-hint">
            {{ $t('示例中的 ID、令牌与请求体为占位内容，请按实际环境替换') }}
          </p>
        </div>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.stat-pill {
  font-size: 12px;
  color: var(--ink-500);
  background: var(--ink-50);
  border: 1px solid var(--line-soft);
  border-radius: 999px;
  padding: 3px 10px;
  font-variant-numeric: tabular-nums;

  &.ok {
    color: #0f766e;
    background: #e6f6f4;
    border-color: #cbece7;
  }

  &.warn {
    color: #b45309;
    background: #fef7e6;
    border-color: #f6e3bb;
  }
}

.method {
  display: inline-block;
  min-width: 52px;
  padding: 0 6px;
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  line-height: 18px;
  text-align: center;

  &.is-get {
    color: #0f766e;
    background: #e6f6f4;
  }

  &.is-post {
    color: var(--brand-700);
    background: var(--brand-50);
  }

  &.is-put,
  &.is-patch {
    color: #b45309;
    background: #fef7e6;
  }

  &.is-delete {
    color: var(--danger-deep);
    background: var(--danger-bg);
  }

  &.is-head {
    color: var(--ink-600);
    background: var(--ink-50);
  }
}

.rpath {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink-800);
  word-break: break-all;
}

.title-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.rt-title {
  font-size: 13px;
  color: var(--ink-900);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.unregistered {
  flex-shrink: 0;
  font-size: 10.5px;
  color: #b45309;
  background: #fef7e6;
  border-radius: 4px;
  padding: 0 5px;
  line-height: 16px;
}

.group-tag {
  font-size: 11.5px;
  color: var(--ink-600);
}

.perm {
  display: block;
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--ink-600);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.perm-free {
  font-size: 11.5px;
  color: var(--ink-400);
}

.pager {
  display: flex;
  justify-content: flex-end;
  padding: 12px 16px;
  border-top: 1px solid var(--line-soft);
}

/* ---------------------------------------------------------------- 抽屉 */
.detail {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.kv {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1px;
  margin: 0;
  background: var(--line-soft);
  border: 1px solid var(--line-soft);
  border-radius: var(--r-md);
  overflow: hidden;

  > div {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 9px 12px;
    background: var(--bg-card);
  }

  dt {
    font-size: 11.5px;
    color: var(--ink-400);
    white-space: nowrap;
  }

  dd {
    margin: 0;
    font-size: 12.5px;
    color: var(--ink-800);
    text-align: right;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-700);
}

.block-text {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.7;
  color: var(--ink-600);
}

.code {
  margin: 0;
  padding: 12px 14px;
  background: var(--ink-900);
  color: #e6e8ef;
  border-radius: var(--r-md);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-all;
}

.code-hint {
  margin: 8px 0 0;
  font-size: 11.5px;
  color: var(--ink-400);
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 720px) {
  .kv {
    grid-template-columns: 1fr;
  }
}
</style>
