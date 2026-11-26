<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh, RefreshRight, VideoPlay } from '@element-plus/icons-vue'
import { clusterApi } from '@/api/ops'
import type { ClusterCommand, ClusterNodesResult } from '@/api/types'
import { fmtTime, fromNow } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const data = ref<ClusterNodesResult | null>(null)
const commands = ref<ClusterCommand[]>([])
const loading = ref(false)
const busy = ref(false)

let timer: number | undefined

const onlineCount = computed(() => data.value?.nodes.filter((n) => n.online).length ?? 0)
const staleCount = computed(() => data.value?.nodes.filter((n) => n.lagging).length ?? 0)
const modeLabel = computed(() => {
  switch (data.value?.mode) {
    case 'cluster':
      return t('集群模式')
    default:
      return t('未启用')
  }
})
const modeHint = computed(() => {
  switch (data.value?.mode) {
    case 'cluster':
      return t('节点通过共享数据库协调，指令由各节点自行领取')
    default:
      return t('cluster.enabled=false，节点不会登记心跳')
  }
})

async function load() {
  loading.value = true
  try {
    const [nodes, cmds] = await Promise.all([clusterApi.nodes(), clusterApi.commands()])
    data.value = nodes
    commands.value = cmds.list ?? []
  } finally {
    loading.value = false
  }
}

const statusType = (s: string): 'success' | 'warning' | 'danger' | 'info' => {
  switch (s) {
    case 'online':
    case 'success':
      return 'success'
    case 'running':
    case 'pending':
      return 'warning'
    case 'failed':
      return 'danger'
    case 'partial':
      return 'warning'
    default:
      return 'info'
  }
}

const statusLabel = (s: string) => {
  const map: Record<string, string> = {
    online: '在线',
    offline: '离线',
    pending: '等待领取',
    running: '执行中',
    success: '成功',
    failed: '失败',
    partial: '部分成功',
  }
  return map[s] ? t(map[s]) : s
}

const actionLabel = (a: string) => (a === 'restart' ? t('重启节点') : t('重新加载配置'))

async function dispatch(action: 'restart' | 'reload-config', scope: 'all' | 'selected', nodeIds?: string[]) {
  if (!auth.can('ops:cluster:operate')) return
  const target =
    scope === 'all'
      ? t('全部在线节点')
      : t('选中的 {n} 个节点', { n: nodeIds?.length ?? 0 })
  const tip =
    action === 'restart'
      ? t('将重启{target}，重启期间该节点上的请求会中断，未保存的进程内状态会丢失。确定继续？', { target })
      : t('将向{target}下发配置重载，已保存的配置会立即生效。确定继续？', { target })
  try {
    await ElMessageBox.confirm(tip, action === 'restart' ? t('重启确认') : t('配置下发'), {
      type: action === 'restart' ? 'warning' : 'info',
      confirmButtonText: action === 'restart' ? t('确认重启') : t('确认下发'),
      cancelButtonText: t('取消'),
    })
  } catch {
    return
  }
  busy.value = true
  try {
    const cmd = await clusterApi.dispatch({ action, scope, nodeIds })
    ElMessage.success(t('指令已下发，涉及 {n} 个节点', { n: cmd.total }))
    await load()
  } catch {
    // 请求拦截器已提示错误
  } finally {
    busy.value = false
  }
}

function restartNode(id: string, name: string) {
  ElMessageBox.confirm(
    t('确定重启节点「{name}」？该节点上的请求会立即中断。', { name }),
    t('重启确认'),
    { type: 'warning', confirmButtonText: t('确认重启'), cancelButtonText: t('取消') },
  )
    .then(() => dispatch('restart', 'selected', [id]))
    .catch(() => undefined)
}

function restartAll() {
  dispatch('restart', 'all')
}

function reloadAll() {
  dispatch('reload-config', 'all')
}

onMounted(() => {
  load()
  timer = window.setInterval(() => {
    // 有未完成指令时轮询更勤，便于看到节点逐个完成。
    load()
  }, 5000)
})

onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('集群管理') }}</h1>
        <p class="page-desc">
          {{ $t('查看节点存活与配置版本，向单个或全部节点下发重启、配置重载指令') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="loading" @click="load">{{ $t('刷新') }}</el-button>
        <el-button
          v-if="auth.can('ops:cluster:operate')"
          :icon="RefreshRight"
          :loading="busy"
          @click="reloadAll"
        >
          {{ $t('配置下发') }}
        </el-button>
        <el-button
          v-if="auth.can('ops:cluster:operate')"
          type="danger"
          :icon="VideoPlay"
          :loading="busy"
          @click="restartAll"
        >
          {{ $t('重启全部节点') }}
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="data && data.mode !== 'cluster'"
      class="mode-alert"
      type="warning"
      show-icon
      :closable="false"
      :title="modeHint"
    />

    <div class="stat-row">
      <div class="stat-card">
        <span class="sc-label">{{ $t('运行模式') }}</span>
        <div class="sc-value">{{ modeLabel }}</div>
        <span class="sc-foot">{{ modeHint }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('节点总数') }}</span>
        <div class="sc-value">{{ data?.nodes.length ?? '—' }}</div>
        <span class="sc-foot">{{ $t('含已离线节点') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('在线节点') }}</span>
        <div class="sc-value">{{ onlineCount }}</div>
        <span class="sc-foot">{{ $t('心跳在阈值内视为在线') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('配置待同步') }}</span>
        <div class="sc-value">{{ staleCount }}</div>
        <span class="sc-foot">{{ $t('配置版本落后于最新的节点数') }}</span>
      </div>
    </div>

    <div class="card">
      <div class="card-head">
        <h3>{{ $t('节点列表') }}</h3>
        <span class="hint">{{ $t('本节点：{id}', { id: data?.nodeId ?? '—' }) }}</span>
      </div>
      <el-table v-loading="loading" :data="data?.nodes ?? []" row-key="id">
        <el-table-column :label="$t('状态')" width="96">
          <template #default="{ row }">
            <span class="status">
              <i class="dot" :class="row.online ? 'on' : 'off'" />
              {{ row.online ? $t('在线') : $t('离线') }}
            </span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('节点')" min-width="220">
          <template #default="{ row }">
            <div class="node-name">
              <span>{{ row.name }}</span>
              <el-tag v-if="row.self" size="small" effect="plain">{{ $t('本机') }}</el-tag>
              <el-tag v-if="row.leader" size="small" type="success" effect="plain">
                {{ $t('协调节点') }}
              </el-tag>
            </div>
            <code class="node-id">{{ row.id }}</code>
          </template>
        </el-table-column>
        <el-table-column :label="$t('地址')" width="180">
          <template #default="{ row }">{{ row.host }}:{{ row.port }}</template>
        </el-table-column>
        <el-table-column prop="version" :label="$t('版本')" width="110" />
        <el-table-column :label="$t('配置版本')" width="140">
          <template #default="{ row }">
            <span class="mono">v{{ row.configVersion }}</span>
            <el-tag v-if="row.lagging" size="small" type="warning" effect="plain" class="lag">
              {{ $t('待同步') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('最后心跳')" width="150">
          <template #default="{ row }">
            <span :title="fmtTime(row.lastSeen)">{{ fromNow(row.lastSeen) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('启动时间')" width="170">
          <template #default="{ row }">{{ fmtTime(row.startedAt) }}</template>
        </el-table-column>
        <el-table-column
          v-if="auth.can('ops:cluster:operate')"
          :label="$t('操作')"
          width="110"
          align="center"
          fixed="right"
        >
          <template #default="{ row }">
            <el-button link type="danger" :disabled="!row.online" @click="restartNode(row.id, row.name)">
              {{ $t('重启') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty :description="$t('还没有节点登记，确认数据库可写且 cluster.enabled=true')" :image-size="72" />
        </template>
      </el-table>
    </div>

    <div class="card">
      <div class="card-head">
        <h3>{{ $t('指令记录') }}</h3>
        <span class="hint">{{ $t('节点每 3 秒领取一次，重启类指令会先落状态再重启') }}</span>
      </div>
      <el-table :data="commands" row-key="id">
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="targets">
              <div v-for="tg in row.targets" :key="tg.nodeId" class="target">
                <span class="target-name">{{ tg.nodeName }}</span>
                <el-tag size="small" :type="statusType(tg.status)" effect="plain">
                  {{ statusLabel(tg.status) }}
                </el-tag>
                <span class="target-msg">{{ tg.message || '—' }}</span>
                <span class="target-time">{{ tg.finishedAt ? fmtTime(tg.finishedAt) : '' }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="$t('动作')" width="130">
          <template #default="{ row }">{{ actionLabel(row.action) }}</template>
        </el-table-column>
        <el-table-column :label="$t('范围')" width="110">
          <template #default="{ row }">
            {{ row.scope === 'all' ? $t('全部节点') : $t('指定节点') }}
          </template>
        </el-table-column>
        <el-table-column :label="$t('状态')" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)" effect="plain">
              {{ statusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="$t('进度')" width="130">
          <template #default="{ row }">
            <span class="mono">{{ row.succeeded }}/{{ row.total }}</span>
            <span v-if="row.failed" class="failed"> · {{ $t('失败 {n}', { n: row.failed }) }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="operator" :label="$t('操作人')" width="120" />
        <el-table-column :label="$t('发起时间')" width="170">
          <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
        </el-table-column>
        <template #empty>
          <el-empty :description="$t('还没有下发过指令')" :image-size="64" />
        </template>
      </el-table>
    </div>
  </div>
</template>

<style scoped lang="scss">
.page-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mode-alert {
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
  line-height: 1.5;
  color: var(--ink-400);
}

.card + .card {
  margin-top: 14px;
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

.hint {
  font-size: 11.5px;
  color: var(--ink-400);
}

.status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
}

.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--ink-300);

  &.on {
    background: var(--success-500, #18a058);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--success-500, #18a058) 18%, transparent);
  }

  &.off {
    background: var(--ink-300);
  }
}

.node-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  color: var(--ink-800);

  > span:first-child {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.node-id {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-400);
}

.mono {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}

.lag {
  margin-left: 6px;
}

.failed {
  color: var(--danger-500, #d03050);
  font-size: 12px;
}

.targets {
  padding: 4px 18px 8px 48px;
}

.target {
  display: grid;
  grid-template-columns: 180px 100px minmax(0, 1fr) 160px;
  gap: 10px;
  align-items: center;
  padding: 6px 0;
  font-size: 12px;
  border-top: 1px dashed var(--line-soft);

  &:first-child {
    border-top: 0;
  }
}

.target-name {
  color: var(--ink-800);
}

.target-msg {
  color: var(--ink-500);
}

.target-time {
  color: var(--ink-400);
  text-align: right;
}

@media (max-width: 900px) {
  .stat-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .target {
    grid-template-columns: 1fr;
  }
}
</style>
