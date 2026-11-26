<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Plus, Refresh, Upload } from '@element-plus/icons-vue'
import {
  configApi,
  registryApi,
  type GovConfigItem,
  type GovConfigItemInput,
  type GovNamespace,
  type GovReleaseView,
  type GovServiceView,
} from '@/api/gov'
import { fmtTime } from '@/utils/format'

const { t } = useI18n()

const namespaces = ref<GovNamespace[]>([])
const selected = ref('')
const items = ref<GovConfigItem[]>([])
const services = ref<GovServiceView[]>([])
const loading = ref(false)
const itemLoading = ref(false)

const encryptedCount = computed(() => items.value.filter((i) => i.encrypted).length)
const scopedCount = computed(() => items.value.filter((i) => i.scope !== 'global').length)

async function loadNamespaces() {
  loading.value = true
  try {
    namespaces.value = await configApi.namespaces()
    if (!selected.value || !namespaces.value.some((n) => n.namespace === selected.value)) {
      selected.value = namespaces.value[0]?.namespace ?? ''
    }
    await loadItems()
  } finally {
    loading.value = false
  }
}

async function loadItems() {
  if (!selected.value) {
    items.value = []
    return
  }
  itemLoading.value = true
  try {
    items.value = await configApi.items(selected.value)
    await loadReleases()
  } finally {
    itemLoading.value = false
  }
}

function pick(ns: GovNamespace) {
  if (ns.namespace === selected.value) return
  selected.value = ns.namespace
  void loadItems()
}

async function loadServices() {
  try {
    services.value = await registryApi.services()
  } catch {
    services.value = []
  }
}

/* ---------------------------------------------------------------- 命名空间 */
const nsVisible = ref(false)
const nsForm = reactive({ namespace: '', env: '', desc: '' })
const nsSubmitting = ref(false)

function openNamespace() {
  Object.assign(nsForm, { namespace: '', env: '', desc: '' })
  nsVisible.value = true
}

async function submitNamespace() {
  if (!nsForm.namespace.trim()) {
    ElMessage.warning(t('请输入命名空间'))
    return
  }
  nsSubmitting.value = true
  try {
    await configApi.createNamespace({ ...nsForm })
    ElMessage.success(t('已创建'))
    nsVisible.value = false
    await loadNamespaces()
    selected.value = nsForm.namespace.trim()
    await loadItems()
  } finally {
    nsSubmitting.value = false
  }
}

async function removeNamespace(ns: GovNamespace) {
  try {
    await ElMessageBox.confirm(
      t('删除命名空间「{ns}」会连同其中的配置项一起移除，已发布的版本记录保留。确定继续？', {
        ns: ns.namespace,
      }),
      t('删除命名空间'),
      { type: 'error', confirmButtonText: t('删除'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  await configApi.removeNamespace(ns.id)
  ElMessage.success(t('已删除'))
  if (selected.value === ns.namespace) selected.value = ''
  await loadNamespaces()
}

/* ---------------------------------------------------------------- 配置项 */
const itemVisible = ref(false)
const editingKey = ref('')
const itemRef = ref<FormInstance>()
const itemSubmitting = ref(false)
const itemForm = reactive<GovConfigItemInput>({
  key: '',
  value: '',
  type: 'string',
  encrypted: false,
  scope: 'global',
  service: '',
})

const itemRules = computed<FormRules>(() => ({
  key: [
    { required: true, message: t('请输入配置键'), trigger: 'blur' },
    { pattern: /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/, message: t('只能用字母、数字、点、下划线、短横线'), trigger: 'blur' },
  ],
}))

function openCreateItem() {
  if (!selected.value) {
    ElMessage.warning(t('请先创建命名空间'))
    return
  }
  editingKey.value = ''
  Object.assign(itemForm, { key: '', value: '', type: 'string', encrypted: false, scope: 'global', service: '' })
  itemRef.value?.clearValidate()
  itemVisible.value = true
}

function openEditItem(row: GovConfigItem) {
  editingKey.value = row.key
  Object.assign(itemForm, {
    key: row.key,
    value: row.value,
    type: row.type || 'string',
    encrypted: row.encrypted,
    scope: row.scope || 'global',
    service: row.service ?? '',
  })
  itemRef.value?.clearValidate()
  itemVisible.value = true
}

async function submitItem() {
  const valid = await itemRef.value?.validate().catch(() => false)
  if (!valid) return
  if (itemForm.scope === 'service' && !itemForm.service) {
    ElMessage.warning(t('作用域为 service 时必须选择服务'))
    return
  }
  itemSubmitting.value = true
  try {
    await configApi.saveItems(selected.value, [{ ...itemForm }])
    ElMessage.success(t('已保存，需发布后才会下发'))
    itemVisible.value = false
    await loadItems()
  } finally {
    itemSubmitting.value = false
  }
}

async function removeItem(row: GovConfigItem) {
  try {
    await ElMessageBox.confirm(t('删除配置项「{key}」？发布后会从数据面移除。', { key: row.key }), t('删除配置'), {
      type: 'error',
      confirmButtonText: t('删除'),
      cancelButtonText: t('取消'),
    })
  } catch {
    return
  }
  await configApi.removeItem(row.id)
  ElMessage.success(t('已删除，记得重新发布'))
  await loadItems()
}

/* ---------------------------------------------------------------- 发布 */
const publishVisible = ref(false)
const publishNote = ref('')
const publishing = ref(false)
const releases = ref<GovReleaseView[]>([])

const activeRelease = computed(() => releases.value.find((r) => r.status === 'active'))

async function loadReleases() {
  if (!selected.value) {
    releases.value = []
    return
  }
  try {
    releases.value = await configApi.releases(selected.value)
  } catch {
    releases.value = []
  }
}

function openPublish() {
  publishNote.value = ''
  publishVisible.value = true
}

async function doPublish() {
  publishing.value = true
  try {
    const rel = await configApi.publish({ namespace: selected.value, note: publishNote.value })
    ElMessage.success(t('已发布 r{rev}', { rev: rel.revision }))
    publishVisible.value = false
    await loadReleases()
  } finally {
    publishing.value = false
  }
}

async function rollback(row: GovReleaseView) {
  try {
    await ElMessageBox.confirm(
      t('按 r{rev} 的内容生成新版本并生效，配置项本身也会回到旧值。确定继续？', { rev: row.revision }),
      t('回滚配置'),
      { type: 'warning', confirmButtonText: t('确认回滚'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  const rel = await configApi.rollback(row.id)
  ElMessage.success(t('已生成 r{rev} 并生效', { rev: rel.revision }))
  await loadItems()
}

onMounted(async () => {
  await loadServices()
  await loadNamespaces()
})
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('配置中心') }}</h1>
        <p class="page-desc">
          {{ $t('按命名空间管理业务配置，发布后由数据面按版本号增量拉取；密钥类配置加密存储，下发前解密') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" circle :title="$t('刷新')" @click="loadNamespaces" />
        <el-button v-perm="'configcenter:config:publish'" type="primary" :icon="Upload" :disabled="!selected" @click="openPublish">
          {{ $t('发布') }}
        </el-button>
        <el-button v-perm="'configcenter:config:update'" :icon="Plus" @click="openNamespace">
          {{ $t('新建命名空间') }}
        </el-button>
      </div>
    </div>

    <div class="layout">
      <!-- 命名空间 -->
      <div class="card ns-card" v-loading="loading">
        <div class="card-head">
          <h3>{{ $t('命名空间') }}</h3>
          <span class="hint">{{ namespaces.length }}</span>
        </div>
        <div class="ns-list">
          <div
            v-for="ns in namespaces"
            :key="ns.id"
            class="ns-item"
            :class="{ active: ns.namespace === selected }"
            @click="pick(ns)"
          >
            <div class="ns-main">
              <span class="ns-name mono">{{ ns.namespace }}</span>
              <el-tag v-if="ns.env" size="small" effect="plain">{{ ns.env }}</el-tag>
            </div>
            <span class="ns-desc">{{ ns.desc || $t('无描述') }}</span>
            <el-button
              v-perm="'configcenter:config:update'"
              class="ns-del"
              link
              type="danger"
              size="small"
              @click.stop="removeNamespace(ns)"
            >
              {{ $t('删除') }}
            </el-button>
          </div>
          <el-empty v-if="!namespaces.length" :description="$t('还没有命名空间')" :image-size="56" />
        </div>
      </div>

      <!-- 配置项 -->
      <div class="right">
        <div class="card">
          <div class="toolbar">
            <div class="toolbar-left">
              <span class="ns-current mono">{{ selected || $t('未选择') }}</span>
              <span class="hint">
                {{ $t('{n} 项配置 · {enc} 项加密 · {scoped} 项限定作用域', {
                  n: items.length,
                  enc: encryptedCount,
                  scoped: scopedCount,
                }) }}
              </span>
            </div>
            <div class="toolbar-right">
              <span v-if="activeRelease" class="rev-badge mono">
                {{ $t('生效 r{rev}', { rev: activeRelease.revision }) }}
              </span>
              <el-button v-perm="'configcenter:config:update'" :icon="Plus" size="small" :disabled="!selected" @click="openCreateItem">
                {{ $t('新增配置') }}
              </el-button>
            </div>
          </div>

          <el-table v-loading="itemLoading" :data="items" row-key="id">
            <el-table-column :label="$t('键')" min-width="200">
              <template #default="{ row }">
                <span class="mono key">{{ row.key }}</span>
                <el-tag v-if="row.encrypted" size="small" type="warning" effect="plain" class="tag">
                  {{ $t('加密') }}
                </el-tag>
              </template>
            </el-table-column>

            <el-table-column :label="$t('值')" min-width="240">
              <template #default="{ row }">
                <span class="mono val" :class="{ masked: row.encrypted }">{{ row.value || '—' }}</span>
              </template>
            </el-table-column>

            <el-table-column :label="$t('类型')" width="90">
              <template #default="{ row }">
                <span class="dim fs-12">{{ row.type }}</span>
              </template>
            </el-table-column>

            <el-table-column :label="$t('作用域')" width="130">
              <template #default="{ row }">
                <template v-if="row.scope === 'global'">
                  <span class="dim">{{ $t('全局') }}</span>
                </template>
                <template v-else>
                  <el-tag size="small" effect="plain">{{ row.scope }}</el-tag>
                  <span v-if="row.service" class="mono fs-12 svc">{{ row.service }}</span>
                </template>
              </template>
            </el-table-column>

            <el-table-column :label="$t('更新人')" width="110">
              <template #default="{ row }">
                <span :class="row.updatedBy ? '' : 'dim'">{{ row.updatedBy || '—' }}</span>
              </template>
            </el-table-column>

            <el-table-column label="" width="130" align="right" fixed="right">
              <template #default="{ row }">
                <el-button v-perm="'configcenter:config:update'" link type="primary" @click="openEditItem(row)">
                  {{ $t('编辑') }}
                </el-button>
                <el-button v-perm="'configcenter:config:update'" link type="danger" @click="removeItem(row)">
                  {{ $t('删除') }}
                </el-button>
              </template>
            </el-table-column>

            <template #empty>
              <el-empty :description="$t('该命名空间还没有配置项')" :image-size="64" />
            </template>
          </el-table>
        </div>

        <div class="card">
          <div class="card-head">
            <h3>{{ $t('发布历史') }}</h3>
            <span class="hint">{{ $t('回滚会生成新版本号，不会让数据面拒绝应用') }}</span>
          </div>
          <el-table :data="releases" size="small" row-key="id">
            <el-table-column :label="$t('版本')" width="90">
              <template #default="{ row }">
                <span class="mono rev">r{{ row.revision }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('状态')" width="100">
              <template #default="{ row }">
                <span class="status-dot" :class="row.status === 'active' ? 'is-on' : 'is-off'">
                  {{ row.status === 'active' ? $t('生效中') : $t('已被替代') }}
                </span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('备注')" min-width="200">
              <template #default="{ row }">
                <span :class="row.note ? '' : 'dim'">{{ row.note || '—' }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('操作人')" width="110">
              <template #default="{ row }">
                <span :class="row.operator ? '' : 'dim'">{{ row.operator || '—' }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('发布时间')" width="160">
              <template #default="{ row }">
                <span class="dim fs-12">{{ fmtTime(row.createdAt) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="" width="110" align="right">
              <template #default="{ row }">
                <el-button
                  v-if="row.status !== 'active'"
                  v-perm="'configcenter:config:rollback'"
                  link
                  type="warning"
                  @click="rollback(row)"
                >
                  {{ $t('回滚') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <el-empty :description="$t('还没有发布过')" :image-size="56" />
            </template>
          </el-table>
        </div>
      </div>
    </div>

    <!-- 命名空间表单 -->
    <el-dialog v-model="nsVisible" :title="$t('新建命名空间')" width="480px" append-to-body>
      <el-form label-position="top">
        <el-form-item :label="$t('命名空间')" required>
          <el-input v-model="nsForm.namespace" :placeholder="$t('如 order / payment，不含空格与斜杠')" />
        </el-form-item>
        <el-form-item :label="$t('环境')">
          <el-input v-model="nsForm.env" :placeholder="$t('如 prod / staging')" />
        </el-form-item>
        <el-form-item :label="$t('描述')">
          <el-input v-model="nsForm.desc" :placeholder="$t('选填')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="nsVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="nsSubmitting" @click="submitNamespace">{{ $t('创建') }}</el-button>
      </template>
    </el-dialog>

    <!-- 配置项表单 -->
    <el-dialog
      v-model="itemVisible"
      :title="editingKey ? $t('编辑配置项') : $t('新增配置项')"
      width="600px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="itemRef" :model="itemForm" :rules="itemRules" label-position="top">
        <div class="form-grid">
          <el-form-item :label="$t('键')" prop="key">
            <el-input v-model="itemForm.key" :disabled="Boolean(editingKey)" :placeholder="$t('如 db.maxOpen')" />
          </el-form-item>
          <el-form-item :label="$t('类型')">
            <el-select v-model="itemForm.type" style="width: 100%">
              <el-option label="string" value="string" />
              <el-option label="number" value="number" />
              <el-option label="bool" value="bool" />
              <el-option label="json" value="json" />
            </el-select>
          </el-form-item>
        </div>

        <el-form-item :label="$t('值')">
          <el-input
            v-model="itemForm.value"
            type="textarea"
            :rows="itemForm.type === 'json' ? 6 : 3"
            :placeholder="itemForm.encrypted ? $t('留空或保持 ****** 表示不修改原值') : $t('配置值')"
          />
        </el-form-item>

        <div class="form-grid">
          <el-form-item :label="$t('作用域')">
            <el-select v-model="itemForm.scope" style="width: 100%">
              <el-option :label="$t('全局')" value="global" />
              <el-option :label="$t('按服务')" value="service" />
              <el-option :label="$t('按实例')" value="instance" />
            </el-select>
          </el-form-item>
          <el-form-item v-if="itemForm.scope === 'service'" :label="$t('服务')">
            <el-select v-model="itemForm.service" filterable style="width: 100%" :placeholder="$t('选择服务')">
              <el-option v-for="s in services" :key="s.id" :label="s.name" :value="s.name" />
            </el-select>
          </el-form-item>
        </div>

        <el-form-item :label="$t('加密存储')">
          <el-switch v-model="itemForm.encrypted" />
          <span class="field-hint inline">
            {{ $t('开启后值以 AES-256-GCM 落库，页面只显示掩码；服务端需配置 TENON_MASTER_KEY') }}
          </span>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="itemVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="itemSubmitting" @click="submitItem">{{ $t('保存') }}</el-button>
      </template>
    </el-dialog>

    <!-- 发布 -->
    <el-dialog v-model="publishVisible" :title="$t('发布配置')" width="520px" append-to-body>
      <p class="dlg-desc">
        {{ $t('把命名空间「{ns}」的当前内容发布为一个新版本，数据面在一个轮询周期内完成热更新。', {
          ns: selected,
        }) }}
      </p>
      <el-form label-position="top">
        <el-form-item :label="$t('备注')">
          <el-input v-model="publishNote" :placeholder="$t('选填，写清本次改动')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="publishVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="publishing" @click="doPublish">{{ $t('发布') }}</el-button>
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

.layout {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}

.ns-card {
  position: sticky;
  top: 0;
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

.ns-list {
  padding: 0 8px 10px;
}

.ns-item {
  position: relative;
  padding: 9px 10px;
  border-radius: var(--r-md);
  cursor: pointer;
  transition: background 0.15s ease;

  &:hover {
    background: var(--ink-50, #f6f7f9);
  }

  &.active {
    background: var(--brand-50);
  }
}

.ns-main {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ns-name {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--ink-900);
}

.ns-desc {
  display: block;
  margin-top: 2px;
  font-size: 11.5px;
  color: var(--ink-400);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ns-del {
  position: absolute;
  right: 6px;
  bottom: 6px;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.ns-item:hover .ns-del {
  opacity: 1;
}

.right {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
}

.ns-current {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-900);
}

.rev-badge {
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11.5px;
  color: var(--brand-700);
  background: var(--brand-50);
}

.key {
  font-size: 12.5px;
  color: var(--ink-900);
}

.val {
  font-size: 12px;
  color: var(--ink-700);
  word-break: break-all;

  &.masked {
    color: var(--ink-400);
    letter-spacing: 1px;
  }
}

.tag {
  margin-left: 6px;
}

.svc {
  margin-left: 6px;
  color: var(--ink-500);
}

.mono {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}

.dim {
  color: var(--ink-400);
}

.rev {
  font-weight: 600;
  color: var(--ink-900);
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
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

@media (max-width: 1000px) {
  .layout {
    grid-template-columns: 1fr;
  }

  .ns-card {
    position: static;
  }

  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
