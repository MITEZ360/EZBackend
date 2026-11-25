<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, RefreshRight, VideoPlay } from '@element-plus/icons-vue'
import { cacheApi, clusterApi, logApi } from '@/api/ops'
import type { ClusterNode } from '@/api/types'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const tab = ref('restart')
const nodes = ref<ClusterNode[]>([])
const mode = ref('cluster')
const nodeId = ref('')
const picked = ref<string[]>([])
const busy = ref(false)
const ns = ref('')
const days = ref(30)

const onlineNodes = computed(() => nodes.value.filter((n) => n.online))
const canCluster = computed(() => mode.value !== 'disabled')
const modeHint = computed(() => {
  switch (mode.value) {
    case 'cluster':
      return t('多节点部署：可选择单个节点，也可以一次重启全部在线节点')
    case 'standalone':
      return t('当前使用 SQLite，只能操作本节点；多节点需改用 MySQL 或 PostgreSQL')
    default:
      return t('集群协同已关闭，仅能使用原有的单节点重启接口')
  }
})

async function load() {
  try {
    const res = await clusterApi.nodes()
    nodes.value = res.nodes ?? []
    mode.value = res.mode
    nodeId.value = res.nodeId
  } catch {
    // 请求拦截器已提示错误
  }
}

/* ------------------------------------------------------------ 重启服务 */
async function restart(scope: 'self' | 'all' | 'selected') {
  const ids =
    scope === 'all' ? onlineNodes.value.map((n) => n.id) : scope === 'selected' ? picked.value : [nodeId.value]
  if (!ids.length) {
    ElMessage.warning(t('请先选择要重启的节点'))
    return
  }
  const target =
    scope === 'all'
      ? t('全部在线节点（{n} 个）', { n: ids.length })
      : t('{n} 个节点', { n: ids.length })
  try {
    await ElMessageBox.confirm(
      t('将重启{target}。重启会中断该节点上正在处理的请求，进程内未落库的状态会丢失，请确认当前不是业务高峰。', {
        target,
      }),
      t('重启服务'),
      { type: 'error', confirmButtonText: t('确认重启'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  busy.value = true
  try {
    const cmd = await clusterApi.dispatch({
      action: 'restart',
      scope: scope === 'all' ? 'all' : 'selected',
      nodeIds: ids,
    })
    ElMessage.success(t('重启指令已下发，涉及 {n} 个节点', { n: cmd.total }))
    picked.value = []
    await load()
  } catch {
    // 请求拦截器已提示错误
  } finally {
    busy.value = false
  }
}

function restartLegacy() {
  ElMessageBox.confirm(
    t('当前部署未启用集群协同，将调用单节点重启接口。重启期间服务不可用约数秒，确定继续？'),
    t('重启服务'),
    { type: 'warning', confirmButtonText: t('确认重启'), cancelButtonText: t('取消') },
  )
    .then(async () => {
      busy.value = true
      try {
        await clusterApi.dispatch({ action: 'restart', scope: 'selected', nodeIds: [nodeId.value] })
        ElMessage.success(t('重启指令已下发'))
      } finally {
        busy.value = false
      }
    })
    .catch(() => undefined)
}

/* -------------------------------------------------------------- 缓存 */
async function flushCache() {
  const namespace = ns.value.trim()
  const name = namespace || t('全部缓存')
  try {
    const { value } = await ElMessageBox.prompt(
      t('将清空{name}，业务会在下次访问时重建；会话类缓存清空后所有用户需要重新登录。请输入 CLEAR 确认。', {
        name,
      }),
      t('清空缓存'),
      {
        type: 'error',
        confirmButtonText: t('确认清空'),
        cancelButtonText: t('取消'),
        inputPlaceholder: 'CLEAR',
        inputValidator: (v) => v === 'CLEAR' || t('请输入 CLEAR'),
      },
    )
    if (value !== 'CLEAR') return
  } catch {
    return
  }
  busy.value = true
  try {
    const res = await cacheApi.flush(namespace)
    ElMessage.success(t('已清理 {n} 个缓存键', { n: res.deleted }))
    ns.value = ''
  } catch {
    // 请求拦截器已提示错误
  } finally {
    busy.value = false
  }
}

/* -------------------------------------------------------------- 日志 */
async function clearLogs(kind: 'login' | 'operation') {
  const label = kind === 'login' ? t('登录日志') : t('操作日志')
  try {
    await ElMessageBox.confirm(
      t('将删除 {n} 天前的{label}，该操作不可恢复。确定继续？', { n: days.value, label }),
      t('清理日志'),
      { type: 'warning', confirmButtonText: t('确认清理'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  busy.value = true
  try {
    const res = kind === 'login' ? await logApi.clearLogins(days.value) : await logApi.clearOperations(days.value)
    ElMessage.success(t('已清理 {n} 条记录', { n: res.deleted }))
  } catch {
    // 请求拦截器已提示错误
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('危险操作') }}</h1>
        <p class="page-desc">
          {{ $t('这里集中了会中断服务或不可恢复的操作，均需二次确认；日常配置请使用配置中心') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="RefreshRight" :loading="busy" @click="load">{{ $t('刷新节点') }}</el-button>
      </div>
    </div>

    <el-alert
      class="danger-alert"
      type="error"
      :closable="false"
      show-icon
      :title="$t('以下操作会影响线上可用性：重启会中断请求，清空缓存会让会话失效，清理日志不可恢复。')"
    />

    <div class="card danger-card">
      <el-tabs v-model="tab">
        <el-tab-pane :label="$t('重启服务')" name="restart">
          <div class="pane">
            <p class="pane-desc">{{ modeHint }}</p>

            <div v-if="canCluster" class="restart-grid">
              <div class="restart-box">
                <div class="box-head">
                  <span class="box-title">{{ $t('当前节点') }}</span>
                  <code>{{ nodeId }}</code>
                </div>
                <p class="box-desc">{{ $t('仅重启本节点，其他节点继续提供服务') }}</p>
                <el-button type="warning" plain :icon="VideoPlay" :loading="busy" @click="restart('self')">
                  {{ $t('重启当前节点') }}
                </el-button>
              </div>

              <div class="restart-box">
                <div class="box-head">
                  <span class="box-title">{{ $t('全部节点') }}</span>
                  <span class="box-count">{{ $t('{n} 个在线', { n: onlineNodes.length }) }}</span>
                </div>
                <p class="box-desc">{{ $t('逐一下发重启指令，节点各自领取执行；服务会出现短暂抖动') }}</p>
                <el-button
                  type="danger"
                  :icon="VideoPlay"
                  :loading="busy"
                  :disabled="!onlineNodes.length"
                  @click="restart('all')"
                >
                  {{ $t('重启全部节点') }}
                </el-button>
              </div>

              <div class="restart-box wide">
                <div class="box-head">
                  <span class="box-title">{{ $t('指定节点') }}</span>
                  <span class="box-count">{{ $t('已选 {n} 个', { n: picked.length }) }}</span>
                </div>
                <el-checkbox-group v-model="picked" class="node-picks">
                  <el-checkbox v-for="n in nodes" :key="n.id" :value="n.id" :disabled="!n.online">
                    {{ n.name }}
                    <el-tag v-if="n.self" size="small" effect="plain">{{ $t('本机') }}</el-tag>
                    <el-tag v-if="!n.online" size="small" type="info" effect="plain">{{ $t('离线') }}</el-tag>
                  </el-checkbox>
                </el-checkbox-group>
                <el-button
                  type="danger"
                  plain
                  :icon="VideoPlay"
                  :loading="busy"
                  :disabled="!picked.length"
                  @click="restart('selected')"
                >
                  {{ $t('重启选中节点') }}
                </el-button>
              </div>
            </div>

            <div v-else class="restart-box wide">
              <div class="box-head">
                <span class="box-title">{{ $t('单节点重启') }}</span>
                <code>{{ nodeId }}</code>
              </div>
              <p class="box-desc">{{ $t('进程原地重启，Unix 下保持 PID；重启期间接口不可用') }}</p>
              <el-button type="danger" :icon="VideoPlay" :loading="busy" @click="restartLegacy">
                {{ $t('重启服务') }}
              </el-button>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="$t('清空缓存')" name="cache">
          <div class="pane">
            <p class="pane-desc">
              {{ $t('清空后由业务在下次访问时重建。清空全部会同时删除登录会话与权限缓存，所有用户需要重新登录。') }}
            </p>
            <div class="flush-box">
              <el-input
                v-model="ns"
                :placeholder="$t('命名空间，如 dict、captcha；留空表示全部缓存')"
                clearable
                class="flush-input"
              />
              <el-button
                type="danger"
                :icon="Delete"
                :loading="busy"
                :disabled="!auth.can('ops:cache:flush')"
                @click="flushCache"
              >
                {{ $t('清空缓存') }}
              </el-button>
            </div>
            <p class="tip">{{ $t('需要输入 CLEAR 才会执行；可先在缓存管理页查看各命名空间占用') }}</p>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="$t('清理日志')" name="logs">
          <div class="pane">
            <p class="pane-desc">{{ $t('按天数清理历史日志，减少数据量；审计要求较高的环境请先备份。') }}</p>
            <div class="flush-box">
              <span class="inline-label">{{ $t('保留最近') }}</span>
              <el-input-number v-model="days" :min="1" :max="3650" controls-position="right" />
              <span class="inline-label">{{ $t('天') }}</span>
              <el-button type="warning" plain :loading="busy" @click="clearLogs('login')">
                {{ $t('清理登录日志') }}
              </el-button>
              <el-button type="warning" plain :loading="busy" @click="clearLogs('operation')">
                {{ $t('清理操作日志') }}
              </el-button>
            </div>
            <p class="tip">{{ $t('清理范围包含该天数之前的记录，删除后不可恢复') }}</p>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<style scoped lang="scss">
.page-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.danger-alert {
  margin-bottom: 14px;
  border-radius: var(--r-md);
}

.danger-card {
  padding: 4px 16px 16px;
}

.pane {
  padding-top: 4px;
}

.pane-desc {
  margin: 0 0 16px;
  font-size: 12.5px;
  line-height: 1.7;
  color: var(--ink-500);
}

.restart-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.restart-box {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 16px;
  background: var(--ink-50);
  border: 1px solid var(--line-soft);
  border-radius: var(--r-lg);

  &.wide {
    grid-column: 1 / -1;
  }
}

.box-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.box-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-800);
}

.box-head code {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-400);
}

.box-count {
  font-size: 11.5px;
  color: var(--ink-400);
}

.box-desc {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--ink-500);
}

.box-head + .box-desc {
  min-height: 38px;
}

.node-picks {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 18px;
}

.flush-box {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.flush-input {
  max-width: 360px;
}

.inline-label {
  font-size: 13px;
  color: var(--ink-600);
}

.tip {
  margin: 10px 0 0;
  font-size: 11.5px;
  color: var(--ink-400);
}

@media (max-width: 900px) {
  .restart-grid {
    grid-template-columns: 1fr;
  }
}
</style>
