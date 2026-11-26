<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import {
  registryApi,
  type GovInstanceView,
  type GovServicePayload,
  type GovServiceView,
  type GovVersionStat,
} from '@/api/gov'
import { fmtTime, fromNow } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const services = ref<GovServiceView[]>([])
const versions = ref<GovVersionStat[]>([])
const loading = ref(false)
const keyword = ref('')

/** 实例按服务懒加载：服务可能上百个，进页面就全量拉会拖慢首屏 */
const instances = ref<Record<string, GovInstanceView[]>>({})
const instanceLoading = ref<Record<string, boolean>>({})

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return services.value
  return services.value.filter(
    (s) =>
      s.name.toLowerCase().includes(kw) ||
      (s.group ?? '').toLowerCase().includes(kw) ||
      (s.owner ?? '').toLowerCase().includes(kw),
  )
})

const totalInstances = computed(() => services.value.reduce((n, s) => n + s.instances, 0))
const totalOnline = computed(() => services.value.reduce((n, s) => n + s.online, 0))
const versionCount = computed(() => new Set(versions.value.map((v) => v.version)).size)

/** 版本分布按服务分组，灰度期间一眼能看出新旧版本占比 */
const versionGroups = computed(() => {
  const map = new Map<string, GovVersionStat[]>()
  for (const v of versions.value) {
    const list = map.get(v.service) ?? []
    list.push(v)
    map.set(v.service, list)
  }
  return [...map.entries()].map(([service, list]) => ({
    service,
    total: list.reduce((n, v) => n + v.count, 0),
    list,
  }))
})

async function load() {
  loading.value = true
  try {
    const [svc, ver] = await Promise.all([registryApi.services(), registryApi.versions()])
    services.value = svc
    versions.value = ver
    // 已经展开过的服务顺手刷新，避免收起后数据过期
    for (const name of Object.keys(instances.value)) {
      if (svc.some((s) => s.name === name)) void loadInstances(name)
    }
  } finally {
    loading.value = false
  }
}

async function loadInstances(service: string) {
  instanceLoading.value = { ...instanceLoading.value, [service]: true }
  try {
    const list = await registryApi.instances({ service })
    instances.value = { ...instances.value, [service]: list }
  } finally {
    instanceLoading.value = { ...instanceLoading.value, [service]: false }
  }
}

function onExpand(row: GovServiceView, expanded: GovServiceView[]) {
  if (expanded.some((r) => r.id === row.id) && !instances.value[row.name]) {
    void loadInstances(row.name)
  }
}

function instanceTone(s: GovInstanceView) {
  if (s.status === 'online') return 'is-on'
  if (s.status === 'draining') return 'is-warn'
  return 'is-off'
}

function instanceLabel(status: string) {
  const map: Record<string, string> = { online: '在线', draining: '摘流中', offline: '离线' }
  return map[status] ? t(map[status]) : status
}

async function setInstanceStatus(ins: GovInstanceView, status: 'online' | 'draining' | 'offline') {
  const tip =
    status === 'draining'
      ? t('将把实例「{id}」置为摘流：新请求不再分配给它，存量连接自然结束。', { id: ins.instanceId })
      : status === 'offline'
        ? t('将把实例「{id}」置为离线：立即从可选列表中移除，正在进行的请求可能失败。', {
            id: ins.instanceId,
          })
        : t('将把实例「{id}」恢复为可分配状态。', { id: ins.instanceId })
  try {
    await ElMessageBox.confirm(tip, t('变更实例状态'), {
      type: status === 'offline' ? 'warning' : 'info',
      confirmButtonText: t('确认'),
      cancelButtonText: t('取消'),
    })
  } catch {
    return
  }
  await registryApi.setInstanceStatus(ins.id, status)
  ElMessage.success(t('已更新'))
  void loadInstances(ins.serviceId)
  void load()
}

/* ---------------------------------------------------------------- 表单 */
const formVisible = ref(false)
const editingId = ref('')
const editingName = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const form = reactive<GovServicePayload>({
  name: '',
  owner: '',
  group: '',
  desc: '',
  meta: '',
  status: 'enabled',
  checkType: 'none',
  checkPath: '',
  checkInterval: 10,
  checkTimeout: 3,
  checkFailThreshold: 3,
})

const rules = computed<FormRules>(() => ({
  name: [
    { required: true, message: t('请输入服务名'), trigger: 'blur' },
    {
      pattern: /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$/,
      message: t('只能包含字母、数字、点、下划线和短横线'),
      trigger: 'blur',
    },
  ],
}))

function resetForm() {
  Object.assign(form, {
    name: '',
    owner: '',
    group: '',
    desc: '',
    meta: '',
    status: 'enabled',
    checkType: 'none',
    checkPath: '',
    checkInterval: 10,
    checkTimeout: 3,
    checkFailThreshold: 3,
  })
  formRef.value?.clearValidate()
}

function openCreate() {
  editingId.value = ''
  editingName.value = ''
  resetForm()
  formVisible.value = true
}

function openEdit(row: GovServiceView) {
  editingId.value = row.id
  editingName.value = row.name
  Object.assign(form, {
    name: row.name,
    owner: row.owner ?? '',
    group: row.group ?? '',
    desc: row.desc ?? '',
    meta: row.meta ?? '',
    status: row.status || 'enabled',
    checkType: row.checkType || 'none',
    checkPath: row.checkPath ?? '',
    checkInterval: row.checkInterval || 10,
    checkTimeout: row.checkTimeout || 3,
    checkFailThreshold: row.checkFailThreshold || 3,
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
      await registryApi.updateService(editingId.value, { ...form })
      ElMessage.success(t('已保存'))
    } else {
      await registryApi.createService({ ...form })
      ElMessage.success(t('服务已创建'))
    }
    formVisible.value = false
    await load()
  } finally {
    submitting.value = false
  }
}

async function removeService(row: GovServiceView) {
  try {
    await ElMessageBox.confirm(
      t('删除服务「{name}」会同时移除它的 {n} 个实例登记，指向它的路由将无法转发。确定继续？', {
        name: row.name,
        n: row.instances,
      }),
      t('删除服务'),
      { type: 'error', confirmButtonText: t('删除'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  await registryApi.removeService(row.id)
  ElMessage.success(t('已删除'))
  await load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('服务目录') }}</h1>
        <p class="page-desc">
          {{ $t('实例自行上报注册与心跳，控制面负责健康检查与摘流；数据面按这份目录做负载均衡') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" circle :title="$t('刷新')" @click="load" />
        <el-button v-perm="'registry:service:create'" type="primary" :icon="Plus" @click="openCreate">
          {{ $t('新增服务') }}
        </el-button>
      </div>
    </div>

    <div class="stat-row">
      <div class="stat-card">
        <span class="sc-label">{{ $t('服务数') }}</span>
        <div class="sc-value">{{ services.length }}</div>
        <span class="sc-foot">{{ $t('含已停用的服务') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('实例总数') }}</span>
        <div class="sc-value">{{ totalInstances }}</div>
        <span class="sc-foot">{{ $t('按服务分别登记，重复上报不会重复计数') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('在线实例') }}</span>
        <div class="sc-value">
          {{ totalOnline }}
          <span class="sc-sub">/ {{ totalInstances }}</span>
        </div>
        <span class="sc-foot">{{ $t('只有在线实例参与流量分配') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('在跑版本') }}</span>
        <div class="sc-value">{{ versionCount }}</div>
        <span class="sc-foot">{{ $t('大于 1 说明还有实例未完成升级') }}</span>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="keyword"
            :placeholder="$t('按服务名、分组或负责人搜索')"
            clearable
            style="width: 260px"
          />
        </div>
        <div class="toolbar-right">
          <span class="hint">{{ $t('共 {n} 项', { n: filtered.length }) }}</span>
        </div>
      </div>

      <el-table
        v-loading="loading"
        :data="filtered"
        row-key="id"
        @expand-change="onExpand"
      >
        <el-table-column type="expand">
          <template #default="{ row }">
            <div v-loading="instanceLoading[row.name]" class="expand">
              <el-table :data="instances[row.name] ?? []" size="small" row-key="id">
                <el-table-column :label="$t('实例')" min-width="200">
                  <template #default="{ row: ins }">
                    <span class="mono">{{ ins.instanceId }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="$t('地址')" width="170">
                  <template #default="{ row: ins }">
                    <span class="mono">{{ ins.host }}:{{ ins.port }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="$t('版本')" width="110">
                  <template #default="{ row: ins }">
                    <span :class="ins.version ? 'mono' : 'dim'">{{ ins.version || '—' }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="$t('权重')" width="80" align="right">
                  <template #default="{ row: ins }">
                    <span class="mono dim">{{ ins.weight }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="$t('状态')" width="110">
                  <template #default="{ row: ins }">
                    <span class="status-dot" :class="instanceTone(ins)">
                      {{ instanceLabel(ins.status) }}
                    </span>
                  </template>
                </el-table-column>
                <el-table-column :label="$t('配置版本')" width="110" align="right">
                  <template #default="{ row: ins }">
                    <span class="mono dim">v{{ ins.configVersion }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="$t('最后心跳')" width="130">
                  <template #default="{ row: ins }">
                    <span :title="fmtTime(ins.lastSeen)">{{ fromNow(ins.lastSeen) }}</span>
                  </template>
                </el-table-column>
                <el-table-column
                  v-if="auth.can('registry:instance:update')"
                  :label="$t('操作')"
                  width="170"
                  align="right"
                  fixed="right"
                >
                  <template #default="{ row: ins }">
                    <el-button
                      v-if="ins.status !== 'draining'"
                      link
                      type="warning"
                      @click="setInstanceStatus(ins, 'draining')"
                    >
                      {{ $t('摘流') }}
                    </el-button>
                    <el-button
                      v-if="ins.status !== 'online'"
                      link
                      type="primary"
                      @click="setInstanceStatus(ins, 'online')"
                    >
                      {{ $t('恢复') }}
                    </el-button>
                    <el-button
                      v-if="ins.status !== 'offline'"
                      link
                      type="danger"
                      @click="setInstanceStatus(ins, 'offline')"
                    >
                      {{ $t('下线') }}
                    </el-button>
                  </template>
                </el-table-column>
                <template #empty>
                  <div class="expand-empty">
                    {{ $t('还没有实例上报，确认进程已接入注册接口') }}
                  </div>
                </template>
              </el-table>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('服务')" min-width="220">
          <template #default="{ row }">
            <div class="svc-cell">
              <span class="svc-mark">{{ row.name.slice(0, 1).toUpperCase() }}</span>
              <div class="svc-meta">
                <span class="svc-name">{{ row.name }}</span>
                <span class="svc-sub">
                  {{ row.group || $t('未分组') }}
                  <template v-if="row.owner"> · {{ row.owner }}</template>
                </span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('实例')" width="120">
          <template #default="{ row }">
            <div class="ins-count">
              <span class="num">{{ row.online }}</span>
              <span class="dim">/ {{ row.instances }}</span>
            </div>
            <div class="bar">
              <i :style="{ width: row.instances ? `${(row.online / row.instances) * 100}%` : '0%' }" />
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('版本数')" width="90" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ warn: row.versions > 1 }">{{ row.versions }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('健康检查')" min-width="180">
          <template #default="{ row }">
            <template v-if="row.checkType && row.checkType !== 'none'">
              <el-tag size="small" effect="plain">{{ row.checkType.toUpperCase() }}</el-tag>
              <span v-if="row.checkType === 'http'" class="mono check-path">
                {{ row.checkPath || '/healthz' }}
              </span>
              <span class="dim check-every">{{ $t('每 {s}s', { s: row.checkInterval }) }}</span>
            </template>
            <span v-else class="dim">{{ $t('靠心跳与被动摘除') }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('状态')" width="88" align="center">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 'enabled' ? 'is-on' : 'is-off'">
              {{ row.status === 'enabled' ? $t('启用') : $t('停用') }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="" width="150" align="right" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'registry:service:update'" link type="primary" @click="openEdit(row)">
              {{ $t('编辑') }}
            </el-button>
            <el-button v-perm="'registry:service:delete'" link type="danger" @click="removeService(row)">
              {{ $t('删除') }}
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <el-empty :description="$t('还没有服务，实例首次上报会自动建档，也可以手动新增')" :image-size="72" />
        </template>
      </el-table>
    </div>

    <div class="card">
      <div class="card-head">
        <h3>{{ $t('版本分布') }}</h3>
        <span class="hint">{{ $t('只统计在线实例；灰度期间与新版本占比对照') }}</span>
      </div>
      <div class="ver-list">
        <div v-for="g in versionGroups" :key="g.service" class="ver-row">
          <span class="ver-svc mono">{{ g.service }}</span>
          <div class="ver-bars">
            <div v-for="v in g.list" :key="v.version" class="ver-item">
              <span class="ver-tag mono">{{ v.version || '—' }}</span>
              <div class="ver-track">
                <i :style="{ width: `${(v.count / g.total) * 100}%` }" />
              </div>
              <span class="ver-num num">{{ v.count }}</span>
            </div>
          </div>
        </div>
        <el-empty v-if="!versionGroups.length" :description="$t('暂无在线实例')" :image-size="56" />
      </div>
    </div>

    <el-dialog
      v-model="formVisible"
      :title="isEdit ? $t('编辑服务') : $t('新增服务')"
      width="640px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <div class="form-grid">
          <el-form-item :label="$t('服务名')" prop="name">
            <el-input v-model="form.name" :disabled="isEdit" :placeholder="$t('如 order-svc')" />
            <span v-if="isEdit" class="field-hint">
              {{ $t('服务名是实例上报时的标识，创建后不可修改') }}
            </span>
          </el-form-item>
          <el-form-item :label="$t('分组')">
            <el-input v-model="form.group" :placeholder="$t('如 交易域')" />
          </el-form-item>
        </div>

        <div class="form-grid">
          <el-form-item :label="$t('负责人')">
            <el-input v-model="form.owner" :placeholder="$t('选填')" />
          </el-form-item>
          <el-form-item :label="$t('状态')">
            <el-select v-model="form.status" style="width: 100%">
              <el-option :label="$t('启用')" value="enabled" />
              <el-option :label="$t('停用')" value="disabled" />
            </el-select>
            <span class="field-hint">{{ $t('停用后仍保留实例登记，但不参与流量分配') }}</span>
          </el-form-item>
        </div>

        <el-form-item :label="$t('描述')">
          <el-input v-model="form.desc" :placeholder="$t('选填')" />
        </el-form-item>

        <div class="section-label">{{ $t('主动健康检查') }}</div>
        <div class="form-grid">
          <el-form-item :label="$t('检查方式')">
            <el-select v-model="form.checkType" style="width: 100%">
              <el-option :label="$t('不探测')" value="none" />
              <el-option label="HTTP" value="http" />
              <el-option label="TCP" value="tcp" />
            </el-select>
          </el-form-item>
          <el-form-item v-if="form.checkType === 'http'" :label="$t('检查路径')">
            <el-input v-model="form.checkPath" placeholder="/healthz" />
          </el-form-item>
        </div>

        <div v-if="form.checkType !== 'none'" class="form-grid-3">
          <el-form-item :label="$t('间隔（秒）')">
            <el-input-number v-model="form.checkInterval" :min="1" :max="300" controls-position="right" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="$t('超时（秒）')">
            <el-input-number v-model="form.checkTimeout" :min="1" :max="60" controls-position="right" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="$t('失败阈值')">
            <el-input-number
              v-model="form.checkFailThreshold"
              :min="1"
              :max="20"
              controls-position="right"
              style="width: 100%"
            />
          </el-form-item>
        </div>
        <span class="field-hint">
          {{ $t('连续失败达到阈值后控制面把实例置为离线，恢复探测通过后自动回到在线') }}
        </span>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ $t('保存') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.page-actions {
  display: flex;
  align-items: center;
  gap: 8px;
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

.sc-sub {
  font-size: 13px;
  font-weight: 400;
  color: var(--ink-400);
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

.svc-cell {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.svc-mark {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border-radius: 7px;
  font-size: 12px;
  font-weight: 600;
  color: var(--brand-700);
  background: var(--brand-50);
  flex-shrink: 0;
}

.svc-meta {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.svc-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-900);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.svc-sub {
  font-size: 11.5px;
  color: var(--ink-400);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ins-count {
  display: flex;
  align-items: baseline;
  gap: 3px;
  font-size: 13px;
}

.bar {
  margin-top: 4px;
  height: 3px;
  width: 72px;
  border-radius: 2px;
  background: var(--ink-100);
  overflow: hidden;

  i {
    display: block;
    height: 100%;
    background: var(--success-500, #18a058);
    transition: width 0.3s ease;
  }
}

.num {
  font-variant-numeric: tabular-nums;
  color: var(--ink-800);
  font-weight: 600;

  &.warn {
    color: var(--warning-600, #d97706);
  }
}

.mono {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}

.dim {
  color: var(--ink-400);
  font-weight: 400;
}

.check-path {
  margin-left: 6px;
  font-size: 11.5px;
  color: var(--ink-600);
}

.check-every {
  margin-left: 6px;
  font-size: 11px;
}

.expand {
  padding: 2px 16px 10px 44px;
}

.expand-empty {
  padding: 10px 4px 6px;
  font-size: 12px;
  color: var(--ink-400);
}

.ver-list {
  padding: 4px 16px 14px;
}

.ver-row {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
  padding: 8px 0;
  border-top: 1px dashed var(--line-soft);

  &:first-child {
    border-top: 0;
  }
}

.ver-svc {
  font-size: 12px;
  color: var(--ink-700);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ver-bars {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.ver-item {
  display: grid;
  grid-template-columns: 96px minmax(0, 1fr) 40px;
  gap: 10px;
  align-items: center;
}

.ver-tag {
  font-size: 11.5px;
  color: var(--ink-600);
}

.ver-track {
  height: 6px;
  border-radius: 3px;
  background: var(--ink-100);
  overflow: hidden;

  i {
    display: block;
    height: 100%;
    background: var(--brand-500, #4f7cff);
    opacity: 0.75;
  }
}

.ver-num {
  font-size: 12px;
  text-align: right;
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}

.form-grid-3 {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0 16px;
}

.section-label {
  margin: 4px 0 10px;
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-600);
}

.field-hint {
  margin-top: 2px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--ink-400);
}

@media (max-width: 900px) {
  .stat-row,
  .form-grid,
  .form-grid-3,
  .ver-row {
    grid-template-columns: 1fr;
  }
}
</style>
