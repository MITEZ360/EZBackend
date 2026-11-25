<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Refresh, Search } from '@element-plus/icons-vue'
import { cacheApi } from '@/api/ops'
import type { CacheKey, CacheOverview } from '@/api/types'
import { fmtBytes, fmtDuration } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const overview = ref<CacheOverview | null>(null)
const rows = ref<CacheKey[]>([])
const total = ref(0)
const truncated = ref(false)
const loading = ref(false)
const selected = ref<CacheKey[]>([])

const query = reactive({
  page: 1,
  page_size: 20,
  keyword: '',
  namespace: '',
})

/** 已知命名空间的作用说明；未登记的命名空间只显示名称，不影响清理。 */
const NS_META: Record<string, { label: string; desc: string }> = {
  sess: { label: '登录会话', desc: '会话令牌校验用；删除后对应用户需重新登录' },
  authz: { label: '权限缓存', desc: '用户权限点缓存；删除后按需从数据库重建' },
  dict: { label: '字典缓存', desc: '字典项列表；删除后下次访问回源' },
  config: { label: '配置缓存', desc: '公开配置参数；删除后下次访问回源' },
  apikey: { label: '密钥配额', desc: 'API 密钥当日调用计数；删除后配额从零开始' },
  rl: { label: '限流计数', desc: '接口限流窗口；删除后配额立即恢复' },
  lock: { label: '分布式锁', desc: '定时任务等互斥锁；任务执行期间不要清理' },
  oauth: { label: '第三方登录', desc: 'OAuth state 临时数据；删除后未完成的授权会失败' },
  captcha: { label: '图形验证码', desc: '验证码答案；删除后当前验证码立即失效，需刷新重试' },
}

function nsLabel(name: string) {
  if (!name) return t('默认')
  return NS_META[name] ? t(NS_META[name].label) : name
}

function nsDesc(name: string) {
  return NS_META[name] ? t(NS_META[name].desc) : t('业务模块自定义的缓存命名空间')
}

async function loadOverview() {
  overview.value = await cacheApi.overview()
}

async function loadKeys() {
  loading.value = true
  try {
    const res = await cacheApi.keys({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword.trim() || undefined,
      namespace: query.namespace || undefined,
    })
    rows.value = res.list ?? []
    total.value = res.total ?? 0
    truncated.value = Boolean(res.truncated)
  } finally {
    loading.value = false
  }
}

async function loadAll() {
  await Promise.all([loadOverview(), loadKeys()])
}

function reload() {
  query.page = 1
  loadKeys()
}

function resetQuery() {
  query.keyword = ''
  query.namespace = ''
  reload()
}

function pickNamespace(name: string) {
  query.namespace = query.namespace === name ? '' : name
  reload()
}

function onSelection(list: CacheKey[]) {
  selected.value = list
}

const ttlText = (ttl: number) => {
  if (ttl === -2) return t('已失效')
  if (ttl < 0) return t('永不过期')
  return fmtDuration(ttl)
}

const hitRate = computed(() => {
  const o = overview.value
  if (!o) return '—'
  const sum = o.hits + o.misses
  if (!sum) return '—'
  return `${((o.hits / sum) * 100).toFixed(1)}%`
})

const memoryValue = computed(() => {
  const o = overview.value
  if (!o) return '—'
  return fmtBytes(o.kind === 'redis' ? o.usedMemory : o.keyBytes)
})

const memoryHint = computed(() => {
  const o = overview.value
  if (!o) return ''
  return o.kind === 'redis' ? t('Redis 实例已用内存') : t('已扫描键体积')
})

const kindLabel = computed(() => {
  const o = overview.value
  if (!o) return '—'
  return o.kind === 'redis' ? 'Redis' : t('进程内 LRU')
})

const kindHint = computed(() => {
  const o = overview.value
  if (!o) return ''
  if (o.kind === 'redis') return o.version ? `Redis ${o.version}` : t('远端缓存')
  return t('容量 {n} 个键', { n: o.capacity })
})

const totalHint = computed(() => {
  const o = overview.value
  if (!o) return ''
  return t('过期 {expiring} · 常驻 {persistent}', {
    expiring: o.expiring,
    persistent: o.persistent,
  })
})

/* ------------------------------------------------------------ 删除与清空 */
async function removeKeys(keys: string[]) {
  if (!keys.length) return
  try {
    await ElMessageBox.confirm(
      t('确定删除这 {n} 个缓存键？业务会在下次访问时重建；会话类键会导致对应用户退出登录。', {
        n: keys.length,
      }),
      t('删除缓存键'),
      { type: 'warning', confirmButtonText: t('删除'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  try {
    const res = await cacheApi.remove(keys)
    ElMessage.success(t('已删除 {n} 个缓存键', { n: res.deleted }))
    selected.value = []
    await loadAll()
  } catch {
    // 请求拦截器已提示错误
  }
}

function removeSelected() {
  removeKeys(selected.value.map((r) => r.key))
}

function removeRow(row: CacheKey) {
  removeKeys([row.key])
}

async function flushNamespace(namespace: string, count: number) {
  const label = namespace ? nsLabel(namespace) : t('全部缓存')
  const tip = namespace
    ? t('将清空「{name}」下的 {n} 个缓存键，业务会在下次访问时重建。确定继续？', {
        name: label,
        n: count,
      })
    : t('将清空当前应用前缀下的全部缓存键（含登录会话与权限缓存），所有用户需要重新登录。该操作不可恢复，确定继续？')
  try {
    await ElMessageBox.confirm(tip, t('清空 {name}', { name: label }), {
      type: 'warning',
      confirmButtonText: t('确认清空'),
      cancelButtonText: t('取消'),
    })
  } catch {
    return
  }
  if (!namespace) {
    try {
      await ElMessageBox.confirm(
        t('再次确认：清空全部缓存会立即让所有会话失效，请确保当前不是业务高峰。'),
        t('高危操作'),
        { type: 'error', confirmButtonText: t('仍然清空'), cancelButtonText: t('取消') },
      )
    } catch {
      return
    }
  }
  try {
    const res = await cacheApi.flush(namespace)
    ElMessage.success(t('已清理 {n} 个缓存键', { n: res.deleted }))
    await loadAll()
  } catch {
    // 请求拦截器已提示错误
  }
}

onMounted(loadAll)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('缓存管理') }}</h1>
        <p class="page-desc">
          {{ $t('查看缓存后端、键分布与剩余有效期，按命名空间清理；删除后由业务在下次访问时重建') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="loading" @click="loadAll">{{ $t('刷新') }}</el-button>
        <el-button
          v-if="auth.can('ops:cache:flush')"
          type="danger"
          plain
          @click="flushNamespace('', overview?.keys ?? 0)"
        >
          {{ $t('清空全部缓存') }}
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="overview?.truncated || truncated"
      class="limit-alert"
      type="warning"
      show-icon
      :closable="false"
      :title="
        $t('键数量超过单次扫描上限（{n}），列表与统计仅覆盖前 {n} 个键，请用前缀或命名空间缩小范围', {
          n: 5000,
        })
      "
    />

    <div class="stat-row">
      <div class="stat-card">
        <span class="sc-label">{{ $t('缓存后端') }}</span>
        <div class="sc-value">{{ kindLabel }}</div>
        <span class="sc-foot">{{ kindHint }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('键数量') }}</span>
        <div class="sc-value">{{ overview?.keys ?? '—' }}</div>
        <span class="sc-foot">{{ totalHint }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('内存占用') }}</span>
        <div class="sc-value">{{ memoryValue }}</div>
        <span class="sc-foot">{{ memoryHint }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('命中率') }}</span>
        <div class="sc-value">{{ hitRate }}</div>
        <span class="sc-foot">
          {{ $t('命中 {hits} · 未命中 {misses}', { hits: overview?.hits ?? 0, misses: overview?.misses ?? 0 }) }}
        </span>
      </div>
    </div>

    <div class="card ns-panel">
      <div class="card-head">
        <h3>{{ $t('命名空间') }}</h3>
        <span class="ns-hint">
          {{ $t('点击筛选；按命名空间清理只会影响该前缀下的键') }}
        </span>
      </div>
      <div class="ns-list">
        <button
          v-for="ns in overview?.namespaces ?? []"
          :key="ns.name || 'default'"
          type="button"
          class="ns-chip"
          :class="{ active: query.namespace === ns.name }"
          :title="nsDesc(ns.name)"
          @click="pickNamespace(ns.name)"
        >
          <span class="ns-name">{{ nsLabel(ns.name) }}</span>
          <code class="ns-key">{{ ns.name || '—' }}</code>
          <span class="ns-count">{{ ns.count }}</span>
          <el-button
            v-if="auth.can('ops:cache:flush')"
            link
            type="danger"
            :icon="Delete"
            :title="$t('清空该命名空间')"
            @click.stop="flushNamespace(ns.name, ns.count)"
          />
        </button>
        <span v-if="!overview?.namespaces?.length" class="ns-empty">{{ $t('暂无缓存键') }}</span>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            :placeholder="$t('按键名前缀搜索，支持 * 通配')"
            :prefix-icon="Search"
            clearable
            style="width: 260px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select
            v-model="query.namespace"
            :placeholder="$t('全部命名空间')"
            clearable
            style="width: 168px"
            @change="reload"
          >
            <el-option
              v-for="ns in overview?.namespaces ?? []"
              :key="ns.name || 'default'"
              :label="`${nsLabel(ns.name)} (${ns.count})`"
              :value="ns.name"
            />
          </el-select>
          <el-button @click="reload">{{ $t('查询') }}</el-button>
          <el-button text @click="resetQuery">{{ $t('重置') }}</el-button>
        </div>
        <div class="toolbar-right">
          <el-button
            v-if="auth.can('ops:cache:delete') && selected.length"
            type="danger"
            plain
            :icon="Delete"
            @click="removeSelected"
          >
            {{ $t('删除选中（{n}）', { n: selected.length }) }}
          </el-button>
        </div>
      </div>

      <el-table
        v-loading="loading"
        :data="rows"
        row-key="key"
        @selection-change="onSelection"
      >
        <el-table-column v-if="auth.can('ops:cache:delete')" type="selection" width="44" />
        <el-table-column :label="$t('键名')" min-width="320">
          <template #default="{ row }">
            <code class="ckey">{{ row.key }}</code>
          </template>
        </el-table-column>
        <el-table-column :label="$t('命名空间')" width="150">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ nsLabel(row.namespace) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="type" :label="$t('类型')" width="100" />
        <el-table-column :label="$t('体积')" width="100" align="right">
          <template #default="{ row }">{{ fmtBytes(row.size) }}</template>
        </el-table-column>
        <el-table-column :label="$t('剩余有效期')" width="120">
          <template #default="{ row }">
            <span :class="row.ttl < 0 ? 'ttl-forever' : 'ttl-live'">{{ ttlText(row.ttl) }}</span>
          </template>
        </el-table-column>
        <el-table-column
          v-if="auth.can('ops:cache:delete')"
          :label="$t('操作')"
          width="80"
          align="center"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button link type="danger" @click="removeRow(row)">{{ $t('删除') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="$t('没有匹配的缓存键')" :image-size="72" />
        </template>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          background
          @current-change="loadKeys"
          @size-change="reload"
        />
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.page-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.limit-alert {
  margin-bottom: 14px;
  border-radius: var(--r-md);
}

.stat-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 16px;
}

.stat-card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 16px 18px;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-xs);
}

.sc-label {
  font-size: 12px;
  color: var(--ink-500);
}

.sc-value {
  font-size: 22px;
  font-weight: 650;
  letter-spacing: -0.02em;
  color: var(--ink-900);
  font-variant-numeric: tabular-nums;
}

.sc-foot {
  font-size: 11.5px;
  color: var(--ink-400);
}

.ns-panel {
  margin-bottom: 16px;
  padding-bottom: 14px;
}

.card-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 14px 16px 10px;

  h3 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--ink-800);
  }
}

.ns-hint {
  font-size: 11.5px;
  color: var(--ink-400);
}

.ns-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 0 16px;
}

.ns-chip {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 5px 8px 5px 11px;
  background: var(--ink-50);
  border: 1px solid var(--line-soft);
  border-radius: 999px;
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;

  &:hover {
    border-color: var(--brand-300);
  }

  &.active {
    background: var(--brand-50);
    border-color: var(--brand-400);
  }
}

.ns-name {
  font-size: 12px;
  color: var(--ink-800);
}

.ns-key {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-400);
}

.ns-count {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--ink-600);
  font-variant-numeric: tabular-nums;
}

.ns-empty {
  font-size: 12px;
  color: var(--ink-400);
}

.ckey {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink-800);
  word-break: break-all;
}

.ttl-live {
  font-size: 12px;
  color: #0f766e;
}

.ttl-forever {
  font-size: 12px;
  color: var(--ink-400);
}

.pager {
  display: flex;
  justify-content: flex-end;
  padding: 12px 16px;
  border-top: 1px solid var(--line-soft);
}

@media (max-width: 1100px) {
  .stat-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .stat-row {
    grid-template-columns: 1fr;
  }
}
</style>
