<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { CopyDocument, Plus, Refresh } from '@element-plus/icons-vue'
import { apikeyApi, type APIKeyPayload, type IssuedKey } from '@/api/apikey'
import type { APIKeyRow } from '@/api/types'
import { fmtTime as formatTime } from '@/utils/format'

const rows = ref<APIKeyRow[]>([])
const total = ref(0)
const loading = ref(false)
const scopeOptions = ref<string[]>([])
/** 调用示例里展示当前站点地址 */
const origin = window.location.origin

const query = reactive({
  page: 1,
  page_size: 20,
  keyword: '',
  kind: '' as '' | 'service' | 'client',
  status: undefined as number | undefined,
})

async function load() {
  loading.value = true
  try {
    const res = await apikeyApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
      kind: query.kind || undefined,
      status: query.status,
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

function resetQuery() {
  query.keyword = ''
  query.kind = ''
  query.status = undefined
  reload()
}

onMounted(async () => {
  await Promise.all([
    load(),
    apikeyApi.scopes().then((s) => (scopeOptions.value = s ?? [])).catch(() => undefined),
  ])
})

/* ------------------------------------------------------------ 编辑抽屉 */
const drawer = ref(false)
const editingId = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const saving = ref(false)
const formRef = ref<FormInstance>()

const form = reactive<APIKeyPayload>({
  name: '',
  kind: 'service',
  scopes: [],
  ipAllow: [],
  rateRps: 0,
  rateBurst: 0,
  dailyQuota: 0,
  expireAt: null,
  remark: '',
})

const rules: FormRules = {
  name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
  scopes: [{ required: true, type: 'array', min: 1, message: '至少选择一个权限范围', trigger: 'change' }],
}

/** 常用范围：签发时一键带入，避免在长列表里翻找 */
const PRESETS = [
  { label: '只读资讯', scopes: ['news:read'] },
  { label: '资讯写入', scopes: ['news:ingest'] },
  { label: '资讯读写', scopes: ['news:read', 'news:ingest'] },
]

function applyPreset(scopes: string[]) {
  form.scopes = [...scopes]
}

function resetForm() {
  Object.assign(form, {
    name: '',
    kind: 'service',
    scopes: [],
    ipAllow: [],
    rateRps: 0,
    rateBurst: 0,
    dailyQuota: 0,
    expireAt: null,
    remark: '',
  })
  formRef.value?.clearValidate()
}

function openCreate() {
  editingId.value = ''
  resetForm()
  drawer.value = true
}

function openEdit(row: APIKeyRow) {
  editingId.value = row.id
  Object.assign(form, {
    name: row.name,
    kind: row.kind,
    scopes: [...(row.scopes ?? [])],
    ipAllow: [...(row.ipAllow ?? [])],
    rateRps: row.rateRps,
    rateBurst: row.rateBurst,
    dailyQuota: row.dailyQuota,
    expireAt: row.expireAt ?? null,
    remark: row.remark ?? '',
  })
  formRef.value?.clearValidate()
  drawer.value = true
}

async function submit() {
  const ok = await formRef.value?.validate().catch(() => false)
  if (!ok) return
  saving.value = true
  try {
    if (isEdit.value) {
      await apikeyApi.update(editingId.value, { ...form })
      ElMessage.success('已保存')
      drawer.value = false
      await load()
    } else {
      const res = await apikeyApi.create({ ...form })
      drawer.value = false
      showIssued(res, '密钥已签发')
      await load()
    }
  } finally {
    saving.value = false
  }
}

/* ------------------------------------------------------------ 明文展示 */
const issuedOpen = ref(false)
const issued = ref<IssuedKey | null>(null)
const issuedTitle = ref('')
const copied = ref(false)

function showIssued(res: IssuedKey, title: string) {
  issued.value = res
  issuedTitle.value = title
  copied.value = false
  issuedOpen.value = true
}

async function copyPlain() {
  if (!issued.value) return
  try {
    await navigator.clipboard.writeText(issued.value.plain)
    copied.value = true
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('浏览器拒绝访问剪贴板，请手动选中复制')
  }
}

/* ------------------------------------------------------------ 行内操作 */
async function rotate(row: APIKeyRow) {
  try {
    await ElMessageBox.confirm(
      `轮换后旧密钥立即失效，正在使用它的调用方会中断。确认轮换「${row.name}」？`,
      '轮换密钥',
      { type: 'warning', confirmButtonText: '确认轮换', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  const res = await apikeyApi.rotate(row.id)
  showIssued(res, '新密钥已生成')
  await load()
}

async function toggle(row: APIKeyRow) {
  const next = row.status === 1 ? 0 : 1
  await apikeyApi.setStatus(row.id, next)
  row.status = next
  ElMessage.success(next === 1 ? '已启用' : '已停用')
}

async function remove(row: APIKeyRow) {
  try {
    await ElMessageBox.confirm(
      `删除后该密钥立即失效，且不可恢复。确认删除「${row.name}」？`,
      '删除密钥',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  await apikeyApi.remove(row.id)
  ElMessage.success('已删除')
  if (rows.value.length === 1 && query.page > 1) query.page -= 1
  await load()
}

/* ------------------------------------------------------------ 展示辅助 */
function kindLabel(kind: string) {
  return kind === 'client' ? '客户端' : '服务端'
}

function quotaText(row: APIKeyRow) {
  return row.dailyQuota > 0 ? `${row.todayUsage} / ${row.dailyQuota}` : `${row.todayUsage} / 不限`
}

function quotaPercent(row: APIKeyRow) {
  if (!row.dailyQuota) return 0
  return Math.min(100, Math.round((row.todayUsage / row.dailyQuota) * 100))
}

function quotaTone(row: APIKeyRow) {
  const p = quotaPercent(row)
  if (p >= 100) return 'exception'
  if (p >= 80) return 'warning'
  return undefined
}

function fmtTime(v?: string) {
  return v ? formatTime(v, false) : '—'
}

function expireText(row: APIKeyRow) {
  return row.expireAt ? fmtTime(row.expireAt) : '长期'
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">API 密钥</h1>
        <p class="page-desc">
          面向程序调用方的机器身份。爬虫、批处理、移动端与桌面端通过它访问开放接口，
          与后台登录账号完全隔离。
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" @click="load">刷新</el-button>
        <el-button v-perm="'system:apikey:create'" type="primary" :icon="Plus" @click="openCreate">
          签发密钥
        </el-button>
      </div>
    </div>

    <section class="card">
      <div class="toolbar">
        <el-input
          v-model="query.keyword"
          placeholder="搜索名称、前缀或备注"
          clearable
          style="width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="query.kind" placeholder="全部类型" clearable style="width: 128px" @change="reload">
          <el-option label="服务端" value="service" />
          <el-option label="客户端" value="client" />
        </el-select>
        <el-select
          v-model="query.status"
          placeholder="全部状态"
          clearable
          style="width: 118px"
          @change="reload"
        >
          <el-option label="启用" :value="1" />
          <el-option label="停用" :value="0" />
        </el-select>
        <el-button @click="reload">查询</el-button>
        <el-button text @click="resetQuery">重置</el-button>
      </div>

      <el-table :data="rows" v-loading="loading" row-key="id" class="key-table">
        <el-table-column label="名称" min-width="180">
          <template #default="{ row }">
            <div class="key-name">{{ row.name }}</div>
            <div v-if="row.remark" class="key-remark">{{ row.remark }}</div>
          </template>
        </el-table-column>

        <el-table-column label="密钥前缀" width="140">
          <template #default="{ row }">
            <span class="mono prefix">{{ row.prefix }}</span>
          </template>
        </el-table-column>

        <el-table-column label="类型" width="92">
          <template #default="{ row }">
            <el-tag size="small" :type="row.kind === 'client' ? 'warning' : 'info'" effect="plain">
              {{ kindLabel(row.kind) }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column label="权限范围" min-width="200">
          <template #default="{ row }">
            <div class="scope-cell">
              <span v-for="s in (row.scopes ?? []).slice(0, 2)" :key="s" class="scope-chip">{{ s }}</span>
              <el-tooltip
                v-if="(row.scopes ?? []).length > 2"
                :content="(row.scopes ?? []).join('、')"
                placement="top"
              >
                <span class="scope-chip more">+{{ row.scopes.length - 2 }}</span>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="今日用量" width="150">
          <template #default="{ row }">
            <div class="quota-cell">
              <span class="mono">{{ quotaText(row) }}</span>
              <el-progress
                v-if="row.dailyQuota > 0"
                :percentage="quotaPercent(row)"
                :status="quotaTone(row)"
                :show-text="false"
                :stroke-width="3"
                :style="{ width: '76px' }"
              />
            </div>
          </template>
        </el-table-column>

        <el-table-column label="最后使用" width="150">
          <template #default="{ row }">
            <div class="mono dim">{{ fmtTime(row.lastUsedAt) }}</div>
            <div v-if="row.lastUsedIp" class="mono dim ip">{{ row.lastUsedIp }}</div>
          </template>
        </el-table-column>

        <el-table-column label="有效期" width="140">
          <template #default="{ row }">
            <span class="mono dim" :class="{ expired: row.expireAt && new Date(row.expireAt) < new Date() }">
              {{ expireText(row) }}
            </span>
          </template>
        </el-table-column>

        <el-table-column label="状态" width="76">
          <template #default="{ row }">
            <el-switch
              v-perm="'system:apikey:update'"
              :model-value="row.status"
              :active-value="1"
              :inactive-value="0"
              @change="toggle(row)"
            />
          </template>
        </el-table-column>

        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-perm="'system:apikey:update'" link type="primary" @click="openEdit(row)">
              编辑
            </el-button>
            <el-button v-perm="'system:apikey:rotate'" link type="primary" @click="rotate(row)">
              轮换
            </el-button>
            <el-button v-perm="'system:apikey:delete'" link type="danger" @click="remove(row)">
              删除
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <div class="empty">
            <p>还没有任何密钥</p>
            <p class="dim">签发一条服务端密钥，爬虫就可以通过 X-API-Key 请求头写入数据</p>
          </div>
        </template>
      </el-table>

      <div class="pager">
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </section>

    <!-- 签发 / 编辑 -->
    <el-drawer
      v-model="drawer"
      :title="isEdit ? '编辑密钥' : '签发密钥'"
      size="520px"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="96px" label-position="left">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="如：资讯爬虫-生产" maxlength="64" />
        </el-form-item>

        <el-form-item label="类型">
          <el-radio-group v-model="form.kind" :disabled="isEdit">
            <el-radio-button value="service">服务端</el-radio-button>
            <el-radio-button value="client">客户端</el-radio-button>
          </el-radio-group>
          <p class="field-tip">
            {{
              form.kind === 'service'
                ? '密钥保存在服务端，泄露即等于身份被盗，请勿写入前端代码或安装包'
                : '会随安装包分发，只能当作来源标识；请只授予只读范围并设置严格配额'
            }}
          </p>
        </el-form-item>

        <el-form-item label="权限范围" prop="scopes">
          <el-select
            v-model="form.scopes"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="选择或输入权限点，如 news:read"
            style="width: 100%"
          >
            <el-option v-for="s in scopeOptions" :key="s" :label="s" :value="s" />
          </el-select>
          <div class="preset-row">
            <span class="dim">常用：</span>
            <el-button v-for="p in PRESETS" :key="p.label" link type="primary" @click="applyPreset(p.scopes)">
              {{ p.label }}
            </el-button>
          </div>
          <p class="field-tip">非超管只能授予自己已有的权限点，防止签发出比自己更大的凭证</p>
        </el-form-item>

        <el-form-item label="IP 白名单">
          <el-select
            v-model="form.ipAllow"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="留空表示不限制，支持 IP 与 CIDR"
            style="width: 100%"
          >
            <el-option v-for="s in form.ipAllow" :key="s" :label="s" :value="s" />
          </el-select>
        </el-form-item>

        <el-form-item label="速率限制">
          <div class="inline-fields">
            <el-input-number v-model="form.rateRps" :min="0" :max="10000" controls-position="right" />
            <span class="dim">次/秒，0 使用默认值</span>
          </div>
        </el-form-item>

        <el-form-item label="每日配额">
          <div class="inline-fields">
            <el-input-number v-model="form.dailyQuota" :min="0" :max="100000000" :step="1000" controls-position="right" />
            <span class="dim">次，0 表示不限</span>
          </div>
        </el-form-item>

        <el-form-item label="过期时间">
          <el-date-picker
            v-model="form.expireAt"
            type="datetime"
            placeholder="留空表示长期有效"
            style="width: 100%"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
          />
        </el-form-item>

        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="drawer = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">
          {{ isEdit ? '保存' : '签发' }}
        </el-button>
      </template>
    </el-drawer>

    <!-- 明文只展示一次 -->
    <el-dialog
      v-model="issuedOpen"
      :title="issuedTitle"
      width="560px"
      :close-on-click-modal="false"
      @closed="issued = null"
    >
      <el-alert
        type="warning"
        :closable="false"
        show-icon
        title="请立即保存，关闭后无法再次查看完整密钥"
        description="数据库只保存哈希，忘记只能重新轮换。"
      />
      <div class="secret-box">
        <code>{{ issued?.plain }}</code>
        <el-button :icon="CopyDocument" @click="copyPlain">{{ copied ? '已复制' : '复制' }}</el-button>
      </div>
      <div class="usage-hint">
        <p>把密钥放进环境变量后调用：</p>
        <pre>export KEEL_API_KEY={{ issued?.plain }}
curl -H "X-API-Key: $KEEL_API_KEY" \
  {{ origin }}/api/open/v1/whoami</pre>
      </div>
      <template #footer>
        <el-button type="primary" @click="issuedOpen = false">我已保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 14px 16px;
  border-bottom: 1px solid var(--line-soft);
}

.key-table {
  width: 100%;
}

.key-name {
  font-weight: 500;
  color: var(--ink-800);
  line-height: 1.4;
}

.key-remark {
  margin-top: 2px;
  font-size: 12px;
  color: var(--ink-400);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.prefix {
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--bg-subtle);
  color: var(--ink-600);
}

.scope-cell {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.scope-chip {
  padding: 1px 6px;
  border-radius: 4px;
  background: color-mix(in srgb, var(--brand-500) 8%, transparent);
  color: var(--brand-700, var(--brand-600));
  font-family: var(--font-mono);
  font-size: 11.5px;
  line-height: 1.6;

  &.more {
    background: var(--bg-subtle);
    color: var(--ink-500);
    cursor: default;
  }
}

.quota-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;

  span {
    font-size: 12px;
    color: var(--ink-600);
  }
}

.dim {
  color: var(--ink-400);
}

.ip {
  font-size: 11.5px;
}

.expired {
  color: var(--danger);
}

.pager {
  display: flex;
  justify-content: flex-end;
  padding: 12px 20px;
}

.empty {
  padding: 36px 0;
  text-align: center;
  color: var(--ink-600);

  p {
    margin: 2px 0;
  }
}

.field-tip {
  margin: 6px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--ink-400);
}

.preset-row {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: 4px;
  font-size: 12px;
}

.inline-fields {
  display: flex;
  align-items: center;
  gap: 10px;
}

.secret-box {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 16px;
  padding: 12px 14px;
  border: 1px dashed var(--line);
  border-radius: var(--r-md);
  background: var(--bg-subtle);

  code {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--ink-900);
    word-break: break-all;
  }
}

.usage-hint {
  margin-top: 14px;

  p {
    margin: 0 0 6px;
    font-size: 12px;
    color: var(--ink-500);
  }

  pre {
    margin: 0;
    padding: 10px 12px;
    border-radius: var(--r-md);
    background: var(--bg-subtle);
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.7;
    color: var(--ink-700);
    white-space: pre-wrap;
    word-break: break-all;
  }
}
</style>
