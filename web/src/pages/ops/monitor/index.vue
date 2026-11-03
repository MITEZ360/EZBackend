<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { monitorApi } from '@/api/ops'
import type { RouteInfo, SystemInfo } from '@/api/types'
import { fmtDuration, fmtTime } from '@/utils/format'

const info = ref<SystemInfo | null>(null)
const routes = ref<RouteInfo[]>([])
const loading = ref(false)
const autoRefresh = ref(true)
const lastUpdated = ref<Date | null>(null)
const routeKeyword = ref('')

let timer: number | undefined

async function load() {
  loading.value = true
  try {
    const [sys, rt] = await Promise.all([monitorApi.system(), monitorApi.routes()])
    info.value = sys
    routes.value = rt ?? []
    lastUpdated.value = new Date()
  } finally {
    loading.value = false
  }
}

function setupTimer() {
  if (timer) window.clearInterval(timer)
  if (!autoRefresh.value) return
  timer = window.setInterval(load, 10000)
}

function toggleAuto() {
  setupTimer()
}

const fresh = computed(() => (lastUpdated.value ? fmtTime(lastUpdated.value) : '—'))

/** 堆内存占系统申请内存的比例，用于判断是否有明显的内存滞留 */
const heapRatio = computed(() => {
  const r = info.value?.runtime
  if (!r || !r.memSysMB) return 0
  return Math.min(100, Math.round((r.heapMB / r.memSysMB) * 100))
})

const poolRatio = computed(() => {
  const p = info.value?.database.pool
  if (!p || !p.max_open) return 0
  return Math.min(100, Math.round((p.open / p.max_open) * 100))
})

const filteredRoutes = computed(() => {
  const kw = routeKeyword.value.trim().toLowerCase()
  if (!kw) return routes.value
  return routes.value.filter(
    (r) => r.path.toLowerCase().includes(kw) || (r.name ?? '').toLowerCase().includes(kw),
  )
})

const methodGroups = computed(() => {
  const map: Record<string, number> = {}
  for (const r of routes.value) {
    map[r.method] = (map[r.method] ?? 0) + 1
  }
  return Object.entries(map).sort((a, b) => b[1] - a[1])
})

const methodSummary = computed(() => {
  const list = methodGroups.value
  if (!list.length) return '暂无路由'
  if (list.length <= 2) return list.map(([m, n]) => `${m} ${n}`).join(' · ')
  const rest = list.slice(1).reduce((s, [, n]) => s + n, 0)
  return `${list[0][0]} ${list[0][1]} · 其余 ${rest}`
})

function methodTone(m: string) {
  const map: Record<string, string> = {
    GET: 'is-get',
    POST: 'is-post',
    PUT: 'is-put',
    DELETE: 'is-del',
    PATCH: 'is-put',
  }
  return map[m] ?? 'is-get'
}

onMounted(async () => {
  await load()
  setupTimer()
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">服务监控</h1>
        <p class="page-desc">运行时指标与路由清单，每 10 秒自动刷新</p>
      </div>
      <div class="page-actions">
        <div class="auto-switch">
          <el-switch v-model="autoRefresh" size="small" @change="toggleAuto" />
          <span class="as-label">自动刷新</span>
          <span class="as-time">更新于 {{ fresh }}</span>
        </div>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
      </div>
    </div>

    <div v-if="info" class="stat-row">
      <div class="stat-card">
        <span class="sc-label">运行时长</span>
        <div class="sc-value">{{ fmtDuration(info.app.uptime) }}</div>
        <span class="sc-foot">启动于 {{ fmtTime(info.app.startedAt, false) }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">内存占用</span>
        <div class="sc-value">
          {{ info.runtime.memAllocMB.toFixed(1) }}<small> MB</small>
        </div>
        <span class="sc-foot">系统申请 {{ info.runtime.memSysMB.toFixed(1) }} MB</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">Goroutine</span>
        <div class="sc-value">{{ info.runtime.goroutines }}</div>
        <span class="sc-foot">GC 累计 {{ info.runtime.gcCount }} 次</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">REST 路由</span>
        <div class="sc-value">{{ info.http.routes }}</div>
        <span class="sc-foot">{{ methodSummary }}</span>
      </div>
    </div>

    <div v-if="info" class="info-grid">
      <div class="card panel">
        <div class="card-head"><h3>应用信息</h3></div>
        <dl class="kv">
          <div><dt>应用名称</dt><dd>{{ info.app.name }}</dd></div>
          <div><dt>版本</dt><dd class="mono">{{ info.app.version }}</dd></div>
          <div><dt>运行环境</dt><dd><el-tag size="small" :type="info.app.env === 'prod' ? 'danger' : 'warning'" effect="light">{{ info.app.env }}</el-tag></dd></div>
          <div><dt>部署形态</dt><dd>{{ info.app.mode }}</dd></div>
          <div><dt>监听端口</dt><dd class="num">{{ info.http.port }}</dd></div>
          <div><dt>缓存类型</dt><dd class="mono">{{ info.cache.kind }}</dd></div>
        </dl>
      </div>

      <div class="card panel">
        <div class="card-head"><h3>运行时</h3></div>
        <dl class="kv">
          <div><dt>Go 版本</dt><dd class="mono">{{ info.runtime.goVersion }}</dd></div>
          <div><dt>系统 / 架构</dt><dd class="mono">{{ info.runtime.os }} / {{ info.runtime.arch }}</dd></div>
          <div><dt>CPU 核心</dt><dd class="num">{{ info.runtime.cpu }}</dd></div>
          <div><dt>堆内存</dt><dd class="num">{{ info.runtime.heapMB.toFixed(1) }} MB</dd></div>
          <div><dt>GC 次数</dt><dd class="num">{{ info.runtime.gcCount }}</dd></div>
          <div><dt>堆占比</dt><dd class="num">{{ heapRatio }}%</dd></div>
        </dl>
        <div class="bar-row">
          <div class="bar"><span class="bar-fill is-brand" :style="{ width: `${heapRatio}%` }" /></div>
          <span class="bar-hint">堆内存 / 系统申请</span>
        </div>
      </div>

      <div class="card panel">
        <div class="card-head"><h3>数据库</h3></div>
        <dl class="kv">
          <div><dt>驱动</dt><dd class="mono">{{ info.database.driver }}</dd></div>
          <div><dt>版本</dt><dd class="mono">{{ info.database.version }}</dd></div>
          <div><dt>打开连接</dt><dd class="num">{{ info.database.pool.open }} / {{ info.database.pool.max_open }}</dd></div>
          <div><dt>使用中</dt><dd class="num">{{ info.database.pool.in_use }}</dd></div>
          <div><dt>空闲</dt><dd class="num">{{ info.database.pool.idle }}</dd></div>
          <div><dt>等待次数</dt><dd class="num">{{ info.database.pool.wait_count }}</dd></div>
        </dl>
        <div class="bar-row">
          <div class="bar"><span class="bar-fill is-teal" :style="{ width: `${poolRatio}%` }" /></div>
          <span class="bar-hint">连接池使用率 · 累计等待 {{ info.database.pool.wait_duration }}</span>
        </div>
      </div>

      <div class="card panel">
        <div class="card-head"><h3>健康检查</h3></div>
        <div class="health-list">
          <a class="health-item" href="/healthz" target="_blank" rel="noopener">
            <code>/healthz</code>
            <span>存活探针，进程可用即返回 200</span>
          </a>
          <a class="health-item" href="/readyz" target="_blank" rel="noopener">
            <code>/readyz</code>
            <span>就绪探针，依赖可用才返回 200</span>
          </a>
          <a class="health-item" href="/metrics" target="_blank" rel="noopener">
            <code>/metrics</code>
            <span>Prometheus 指标，含请求量、耗时与业务计数</span>
          </a>
        </div>
      </div>
    </div>

    <div class="card routes-card">
      <div class="card-head">
        <h3>路由清单</h3>
        <div class="rh-right">
          <el-input
            v-model="routeKeyword"
            placeholder="筛选路径"
            size="small"
            clearable
            style="width: 200px"
          />
          <span class="rh-count">{{ filteredRoutes.length }} / {{ routes.length }}</span>
        </div>
      </div>

      <div class="route-grid">
        <div v-for="r in filteredRoutes" :key="`${r.method}-${r.path}`" class="route-item">
          <span class="method" :class="methodTone(r.method)">{{ r.method }}</span>
          <span class="rpath mono">{{ r.path }}</span>
        </div>
      </div>

      <div v-if="!filteredRoutes.length" class="route-empty">没有匹配的路由</div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.auto-switch {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-right: 4px;
}

.as-label {
  font-size: 12px;
  color: var(--ink-600);
}

.as-time {
  font-size: 11.5px;
  color: var(--ink-400);
  font-variant-numeric: tabular-nums;
}

/* ---------------------------------------------------------------- 指标 */
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
  line-height: 1.25;

  small {
    font-size: 12px;
    font-weight: 500;
    color: var(--ink-400);
  }
}

.sc-foot {
  font-size: 11.5px;
  color: var(--ink-400);
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 16px;
}

.panel {
  padding-bottom: 14px;
}

.kv {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1px;
  margin: 0 16px;
  background: var(--line-soft);
  border: 1px solid var(--line-soft);
  border-radius: var(--r-md);
  overflow: hidden;

  > div {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 8px 11px;
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

.bar-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 12px 16px 0;
}

.bar {
  flex: 1;
  height: 5px;
  border-radius: 999px;
  background: var(--ink-100);
  overflow: hidden;
}

.bar-fill {
  display: block;
  height: 100%;
  border-radius: 999px;
  transition: width 0.4s ease;

  &.is-brand {
    background: linear-gradient(90deg, var(--brand-400), var(--brand-600));
  }

  &.is-teal {
    background: linear-gradient(90deg, #5eead4, #0d9488);
  }
}

.bar-hint {
  font-size: 11px;
  color: var(--ink-400);
  white-space: nowrap;
}

.health-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 12px 16px 0;
}

.health-item {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 9px 12px;
  border: 1px solid var(--line-soft);
  border-radius: var(--r-md);
  transition: border-color 0.15s ease, background 0.15s ease;

  &:hover {
    border-color: var(--brand-200, #dcd9fb);
    background: var(--brand-50);
  }

  code {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--brand-700);
    flex-shrink: 0;
  }

  span {
    font-size: 11.5px;
    color: var(--ink-500);
  }
}

/* ------------------------------------------------------------ 路由清单 */
.routes-card {
  margin-bottom: 8px;
}

.rh-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.rh-count {
  font-size: 11.5px;
  color: var(--ink-400);
  font-variant-numeric: tabular-nums;
}

.route-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 1px;
  max-height: 420px;
  overflow-y: auto;
  background: var(--line-soft);
}

.route-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 14px;
  background: var(--bg-card);
  min-width: 0;
}

.method {
  flex-shrink: 0;
  width: 52px;
  padding: 0 5px;
  border-radius: 4px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  line-height: 17px;
  text-align: center;

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
    color: #b42318;
    background: #fdecea;
  }
}

.rpath {
  font-size: 11.5px;
  color: var(--ink-600);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.route-empty {
  padding: 32px;
  text-align: center;
  font-size: 12.5px;
  color: var(--ink-400);
}

@media (max-width: 1100px) {
  .stat-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .info-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .stat-row {
    grid-template-columns: 1fr;
  }
}
</style>
