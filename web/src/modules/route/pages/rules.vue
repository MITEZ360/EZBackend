<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh, Upload } from '@element-plus/icons-vue'
import {
  registryApi,
  routeApi,
  type GovCompiledRoute,
  type GovRoutePayload,
  type GovRouteView,
  type GovServiceView,
} from '@/api/gov'
import { fmtTime } from '@/utils/format'

const { t } = useI18n()

const rules = ref<GovRouteView[]>([])
const services = ref<GovServiceView[]>([])
const activeRev = ref<number | null>(null)
const loading = ref(false)
const publishing = ref(false)

const METHOD_OPTIONS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']

const published = computed(() => rules.value.filter((r) => r.status === 'published').length)
const drafts = computed(() => rules.value.filter((r) => r.status === 'draft').length)

function splitList(raw?: string): string[] {
  return (raw ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function parseMap(raw?: string): Record<string, string> {
  if (!raw) return {}
  try {
    const obj = JSON.parse(raw)
    return obj && typeof obj === 'object' ? (obj as Record<string, string>) : {}
  } catch {
    return {}
  }
}

function mapPairs(raw?: string): { k: string; v: string }[] {
  const m = parseMap(raw)
  return Object.entries(m).map(([k, v]) => ({ k, v: String(v) }))
}

function toMap(pairs: { k: string; v: string }[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const p of pairs) {
    const k = p.k.trim()
    if (k) out[k] = p.v
  }
  return out
}

function matchSummary(row: GovRouteView): string {
  const parts: string[] = []
  if (row.headers) parts.push('header')
  if (row.query) parts.push('query')
  return parts.join(' + ')
}

async function load() {
  loading.value = true
  try {
    const [list, svc, rel] = await Promise.all([
      routeApi.list(),
      registryApi.services(),
      routeApi.releases(),
    ])
    rules.value = list
    services.value = svc
    const active = rel.find((r) => r.status === 'active')
    activeRev.value = active ? active.revision : null
  } finally {
    loading.value = false
  }
}

/* ---------------------------------------------------------------- 表单 */
const formVisible = ref(false)
const editingId = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const form = reactive({
  name: '',
  prefix: '',
  methods: [] as string[],
  service: '',
  version: '',
  weight: 100,
  priority: 100,
  strip: false,
  timeoutMs: 0,
  retries: 0,
  headers: [] as { k: string; v: string }[],
  query: [] as { k: string; v: string }[],
})

const rules4 = computed<FormRules>(() => ({
  prefix: [
    { required: true, message: t('请输入路径前缀'), trigger: 'blur' },
    { pattern: /^\/[^\s]*$/, message: t('前缀必须以 / 开头且不含空格'), trigger: 'blur' },
  ],
  service: [{ required: true, message: t('请选择目标服务'), trigger: 'change' }],
}))

function resetForm() {
  Object.assign(form, {
    name: '',
    prefix: '',
    methods: [],
    service: '',
    version: '',
    weight: 100,
    priority: 100,
    strip: false,
    timeoutMs: 0,
    retries: 0,
    headers: [],
    query: [],
  })
  formRef.value?.clearValidate()
}

function openCreate() {
  editingId.value = ''
  resetForm()
  formVisible.value = true
}

function openEdit(row: GovRouteView) {
  editingId.value = row.id
  Object.assign(form, {
    name: row.name,
    prefix: row.prefix,
    methods: splitList(row.methods),
    service: row.service,
    version: row.version ?? '',
    weight: row.weight || 100,
    priority: row.priority || 100,
    strip: row.strip,
    timeoutMs: row.timeoutMs || 0,
    retries: row.retries || 0,
    headers: mapPairs(row.headers),
    query: mapPairs(row.query),
  })
  formRef.value?.clearValidate()
  formVisible.value = true
}

function payloadOf(overrides: Partial<GovRoutePayload> = {}): GovRoutePayload {
  return {
    name: form.name.trim() || form.prefix.trim(),
    priority: form.priority,
    prefix: form.prefix.trim(),
    methods: form.methods,
    headers: toMap(form.headers),
    query: toMap(form.query),
    service: form.service,
    version: form.version,
    weight: form.weight,
    strip: form.strip,
    timeoutMs: form.timeoutMs,
    retries: form.retries,
    status: 'draft',
    ...overrides,
  }
}

async function submitForm() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    if (isEdit.value) {
      await routeApi.update(editingId.value, payloadOf())
      ElMessage.success(t('已保存，需重新发布才会生效'))
    } else {
      await routeApi.create(payloadOf())
      ElMessage.success(t('规则已创建，需发布后生效'))
    }
    formVisible.value = false
    await load()
  } finally {
    submitting.value = false
  }
}

async function removeRule(row: GovRouteView) {
  try {
    await ElMessageBox.confirm(
      t('删除规则「{name}」后，需要重新发布才会从数据面下线。确定继续？', { name: row.name }),
      t('删除规则'),
      { type: 'error', confirmButtonText: t('删除'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  await routeApi.remove(row.id)
  ElMessage.success(t('已删除，记得重新发布'))
  await load()
}

/* ---------------------------------------------------------------- 编译预览 */
const compileVisible = ref(false)
const compiling = ref(false)
const compiled = ref<GovCompiledRoute[]>([])

async function doCompile() {
  const payload = rules.value
    .filter((r) => r.status !== 'archived')
    .map<GovRoutePayload>((r) => ({
      name: r.name,
      priority: r.priority,
      prefix: r.prefix,
      methods: splitList(r.methods),
      headers: parseMap(r.headers),
      query: parseMap(r.query),
      service: r.service,
      version: r.version ?? '',
      weight: r.weight,
      strip: r.strip,
      timeoutMs: r.timeoutMs,
      retries: r.retries,
      status: r.status,
    }))
  if (!payload.length) {
    ElMessage.warning(t('没有可校验的规则'))
    return
  }
  compiling.value = true
  try {
    const out = await routeApi.compile(payload)
    compiled.value = out.routes ?? []
    compileVisible.value = true
  } catch {
    // 校验失败由拦截器提示具体冲突
  } finally {
    compiling.value = false
  }
}

/* ---------------------------------------------------------------- 发布 */
const publishVisible = ref(false)
const publishForm = reactive({ targets: [] as string[], note: '' })
const targetOptions = computed(() =>
  services.value.map((s) => ({ label: s.name, value: s.name })),
)

function openPublish() {
  publishForm.targets = []
  publishForm.note = ''
  publishVisible.value = true
}

async function doPublish() {
  publishing.value = true
  try {
    const rel = await routeApi.publish({
      targets: publishForm.targets,
      note: publishForm.note,
    })
    ElMessage.success(
      rel.stage === 'canary'
        ? t('已灰度发布 r{rev}，仅 {n} 个目标可见', { rev: rel.revision, n: publishForm.targets.length })
        : t('已全量发布 r{rev}', { rev: rel.revision }),
    )
    publishVisible.value = false
    await load()
  } finally {
    publishing.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('动态路由') }}</h1>
        <p class="page-desc">
          {{ $t('规则在控制面编译成路由表，发布后由数据面按版本号拉取，改配置不需要重启网关') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" circle :title="$t('刷新')" @click="load" />
        <el-button :icon="Upload" :loading="compiling" @click="doCompile">
          {{ $t('校验预览') }}
        </el-button>
        <el-button v-perm="'route:rule:publish'" type="primary" @click="openPublish">
          {{ $t('发布') }}
        </el-button>
        <el-button v-perm="'route:rule:create'" :icon="Plus" @click="openCreate">
          {{ $t('新增规则') }}
        </el-button>
      </div>
    </div>

    <div class="stat-row">
      <div class="stat-card">
        <span class="sc-label">{{ $t('规则总数') }}</span>
        <div class="sc-value">{{ rules.length }}</div>
        <span class="sc-foot">{{ $t('含草稿与已发布') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('已发布') }}</span>
        <div class="sc-value">{{ published }}</div>
        <span class="sc-foot">{{ $t('已包含在上一次发布中') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('待发布改动') }}</span>
        <div class="sc-value" :class="{ warn: drafts > 0 }">{{ drafts }}</div>
        <span class="sc-foot">{{ $t('草稿状态的规则不会进入数据面') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('当前生效版本') }}</span>
        <div class="sc-value">{{ activeRev === null ? '—' : `r${activeRev}` }}</div>
        <span class="sc-foot">{{ $t('单增序号，回滚也会产生新版本号') }}</span>
      </div>
    </div>

    <el-alert
      v-if="drafts > 0"
      class="draft-alert"
      type="warning"
      show-icon
      :closable="false"
      :title="$t('有 {n} 条规则改动还没发布，数据面仍在用上一版', { n: drafts })"
    />

    <div class="card">
      <el-table v-loading="loading" :data="rules" row-key="id">
        <el-table-column :label="$t('规则')" min-width="200">
          <template #default="{ row }">
            <div class="rule-name">{{ row.name }}</div>
            <div class="rule-sub">
              <span class="mono">{{ row.prefix }}</span>
              <el-tag v-if="matchSummary(row)" size="small" effect="plain" class="cond-tag">
                {{ matchSummary(row) }}
              </el-tag>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('方法')" width="150">
          <template #default="{ row }">
            <span v-if="!row.methods" class="dim">{{ $t('全部') }}</span>
            <template v-else>
              <el-tag v-for="m in splitList(row.methods)" :key="m" size="small" effect="plain" class="m-tag">
                {{ m }}
              </el-tag>
            </template>
          </template>
        </el-table-column>

        <el-table-column :label="$t('目标服务')" min-width="170">
          <template #default="{ row }">
            <span class="mono">{{ row.service }}</span>
            <el-tag v-if="row.version" size="small" type="warning" effect="plain" class="m-tag">
              v{{ row.version }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column :label="$t('策略')" min-width="215">
          <template #default="{ row }">
            <span class="dim fs-12">
              {{ $t('权重 {w}', { w: row.weight }) }}
              <template v-if="row.strip"> · {{ $t('剥离前缀') }}</template>
              <template v-if="row.timeoutMs"> · {{ $t('超时 {ms}ms', { ms: row.timeoutMs }) }}</template>
              <template v-if="row.retries"> · {{ $t('重试 {n}', { n: row.retries }) }}</template>
            </span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('优先级')" width="90" align="right">
          <template #default="{ row }">
            <span class="mono dim">{{ row.priority }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('状态')" width="96" align="center">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 'published' ? 'is-on' : 'is-warn'">
              {{ row.status === 'published' ? $t('已发布') : $t('草稿') }}
            </span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('更新时间')" width="160">
          <template #default="{ row }">
            <span class="dim fs-12">{{ fmtTime(row.updatedAt) }}</span>
          </template>
        </el-table-column>

        <el-table-column label="" width="130" align="right" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'route:rule:update'" link type="primary" @click="openEdit(row)">
              {{ $t('编辑') }}
            </el-button>
            <el-button v-perm="'route:rule:delete'" link type="danger" @click="removeRule(row)">
              {{ $t('删除') }}
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <el-empty :description="$t('还没有规则，新增后发布即可接管流量')" :image-size="72" />
        </template>
      </el-table>
    </div>

    <!-- 表单 -->
    <el-dialog
      v-model="formVisible"
      :title="isEdit ? $t('编辑规则') : $t('新增规则')"
      width="720px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules4" label-position="top">
        <div class="form-grid">
          <el-form-item :label="$t('路径前缀')" prop="prefix">
            <el-input v-model="form.prefix" placeholder="/api/v1/order" />
            <span class="field-hint">{{ $t('按前缀最长匹配，逐级向上泛化查找') }}</span>
          </el-form-item>
          <el-form-item :label="$t('规则名称')">
            <el-input v-model="form.name" :placeholder="$t('留空则用前缀')" />
          </el-form-item>
        </div>

        <div class="form-grid">
          <el-form-item :label="$t('目标服务')" prop="service">
            <el-select v-model="form.service" filterable :placeholder="$t('选择服务目录中的服务')" style="width: 100%">
              <el-option v-for="s in services" :key="s.id" :label="s.name" :value="s.name">
                <span class="mono">{{ s.name }}</span>
                <span class="opt-sub">{{ $t('{n} 个在线实例', { n: s.online }) }}</span>
              </el-option>
            </el-select>
          </el-form-item>
          <el-form-item :label="$t('限定版本')">
            <el-input v-model="form.version" :placeholder="$t('选填，如 v2；只转发到该版本的实例')" />
          </el-form-item>
        </div>

        <el-form-item :label="$t('请求方法')">
          <el-select v-model="form.methods" multiple collapse-tags :placeholder="$t('留空表示不限方法')" style="width: 100%">
            <el-option v-for="m in METHOD_OPTIONS" :key="m" :label="m" :value="m" />
          </el-select>
        </el-form-item>

        <div class="form-grid-4">
          <el-form-item :label="$t('优先级')">
            <el-input-number v-model="form.priority" :min="-10000" :max="10000" controls-position="right" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="$t('权重')">
            <el-input-number v-model="form.weight" :min="1" :max="1000" controls-position="right" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="$t('超时（ms）')">
            <el-input-number v-model="form.timeoutMs" :min="0" :max="60000" :step="500" controls-position="right" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="$t('重试次数')">
            <el-input-number v-model="form.retries" :min="0" :max="3" controls-position="right" style="width: 100%" />
          </el-form-item>
        </div>

        <el-form-item :label="$t('剥离前缀')">
          <el-switch v-model="form.strip" />
          <span class="field-hint inline">
            {{ $t('开启后转发时去掉匹配到的前缀，适合网关对外收敛路径的场合') }}
          </span>
        </el-form-item>

        <div class="form-grid">
          <el-form-item :label="$t('Header 条件')">
            <div class="kv-list">
              <div v-for="(p, i) in form.headers" :key="`h${i}`" class="kv-row">
                <el-input v-model="p.k" :placeholder="$t('名称')" size="small" />
                <el-input v-model="p.v" :placeholder="$t('值')" size="small" />
                <el-button link type="danger" size="small" @click="form.headers.splice(i, 1)">
                  {{ $t('移除') }}
                </el-button>
              </div>
              <el-button link type="primary" size="small" @click="form.headers.push({ k: '', v: '' })">
                + {{ $t('添加 Header') }}
              </el-button>
            </div>
          </el-form-item>
          <el-form-item :label="$t('Query 条件')">
            <div class="kv-list">
              <div v-for="(p, i) in form.query" :key="`q${i}`" class="kv-row">
                <el-input v-model="p.k" :placeholder="$t('名称')" size="small" />
                <el-input v-model="p.v" :placeholder="$t('值')" size="small" />
                <el-button link type="danger" size="small" @click="form.query.splice(i, 1)">
                  {{ $t('移除') }}
                </el-button>
              </div>
              <el-button link type="primary" size="small" @click="form.query.push({ k: '', v: '' })">
                + {{ $t('添加 Query') }}
              </el-button>
            </div>
          </el-form-item>
        </div>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">{{ $t('保存') }}</el-button>
      </template>
    </el-dialog>

    <!-- 编译预览 -->
    <el-dialog v-model="compileVisible" :title="$t('编译预览')" width="780px" append-to-body>
      <p class="dlg-desc">
        {{ $t('下面是数据面的匹配顺序：优先级高的先匹配，同优先级下前缀更具体的在前') }}
      </p>
      <el-table :data="compiled" size="small" row-key="id">
        <el-table-column type="index" width="56" :label="$t('顺序')" />
        <el-table-column :label="$t('规则')" min-width="180">
          <template #default="{ row }">
            <span class="mono">{{ row.prefix }}</span>
            <span class="dim"> · {{ row.name }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('方法')" width="140">
          <template #default="{ row }">
            <span v-if="!row.methods || !row.methods.length" class="dim">{{ $t('全部') }}</span>
            <span v-else class="mono fs-12">{{ row.methods.join(',') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('目标')" min-width="150">
          <template #default="{ row }">
            <span class="mono">{{ row.service }}</span>
            <span v-if="row.version" class="dim">@{{ row.version }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="$t('优先级')" width="90" align="right">
          <template #default="{ row }"><span class="mono dim">{{ row.priority }}</span></template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button type="primary" @click="compileVisible = false">{{ $t('知道了') }}</el-button>
      </template>
    </el-dialog>

    <!-- 发布 -->
    <el-dialog v-model="publishVisible" :title="$t('发布路由规则')" width="560px" append-to-body>
      <p class="dlg-desc">
        {{ $t('发布后数据面在一个轮询周期内完成热更新，不需要重启任何进程。') }}
      </p>
      <el-form label-position="top">
        <el-form-item :label="$t('灰度目标')">
          <el-select
            v-model="publishForm.targets"
            multiple
            filterable
            allow-create
            default-first-option
            :placeholder="$t('留空表示全量发布')"
            style="width: 100%"
          >
            <el-option v-for="o in targetOptions" :key="o.value" :label="o.label" :value="o.value" />
          </el-select>
          <span class="field-hint">
            {{ $t('填实例 ID 或任意标签值：命中的网关用新规则，其余继续用上一版') }}
          </span>
        </el-form-item>
        <el-form-item :label="$t('备注')">
          <el-input v-model="publishForm.note" :placeholder="$t('选填，写清本次改动')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="publishVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="publishing" @click="doPublish">
          {{ publishForm.targets.length ? $t('灰度发布') : $t('全量发布') }}
        </el-button>
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

  &.warn {
    color: var(--warning-600, #d97706);
  }
}

.sc-foot {
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--ink-400);
}

.draft-alert {
  margin-bottom: 14px;
  border-radius: var(--r-md);
}

.rule-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-900);
}

.rule-sub {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 1px;
}

.mono {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: 12px;
  color: var(--ink-600);
}

.dim {
  color: var(--ink-400);
}

.m-tag {
  margin-right: 4px;
}

.cond-tag {
  font-size: 10.5px;
}

.opt-sub {
  float: right;
  font-size: 11.5px;
  color: var(--ink-400);
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}

.form-grid-4 {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0 14px;
}

.kv-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
}

.kv-row {
  display: grid;
  grid-template-columns: 1fr 1fr auto;
  gap: 6px;
  align-items: center;
}

.field-hint {
  margin-top: 2px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--ink-400);

  &.inline {
    margin-left: 10px;
  }
}

.dlg-desc {
  margin: 0 0 12px;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--ink-500);
}

@media (max-width: 900px) {
  .stat-row,
  .form-grid,
  .form-grid-4 {
    grid-template-columns: 1fr;
  }
}
</style>
