<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Download, Plus, Refresh, RefreshLeft, Upload, View } from '@element-plus/icons-vue'
import {
  downloadBackup,
  maintenanceApi,
  type BackupFile,
  type MigrationResult,
  type PrunePlan,
  type RestoreResult,
  type VerifyResult,
} from '@/api/maintenance'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const rows = ref<BackupFile[]>([])
const migration = ref<MigrationResult | null>(null)
const prune = ref<PrunePlan | null>(null)
const loading = ref(false)
const creating = ref(false)
const activeTab = ref('backups')
const busy = ref('')

const totalFiles = computed(() => rows.value.reduce((n, r) => n + (r.files ?? 0), 0))

const latest = computed(() => rows.value[0]?.createdAt ?? '')
const pending = computed(() => migration.value?.status?.pending ?? 0)
const currentVersion = computed(() => migration.value?.status?.current ?? 0)
const latestVersion = computed(() => migration.value?.status?.latest ?? 0)

function fmt(iso?: string) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-CN', { hour12: false })
}

function fmtSize(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(2)} MB`
}

async function load() {
  loading.value = true
  try {
    const [backups, mig, plan] = await Promise.all([
      maintenanceApi.backups(),
      maintenanceApi.migrations().catch(() => null),
      maintenanceApi.prunePlan().catch(() => null),
    ])
    rows.value = backups ?? []
    migration.value = mig
    prune.value = plan
  } finally {
    loading.value = false
  }
}

onMounted(load)

async function createBackup() {
  creating.value = true
  try {
    const meta = await maintenanceApi.createBackup()
    ElMessage.success(t('已生成备份 {name}', { name: meta.name }))
    await load()
  } finally {
    creating.value = false
  }
}

async function doDownload(row: BackupFile) {
  busy.value = row.name
  try {
    await downloadBackup(row.name)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('下载失败'))
  } finally {
    busy.value = ''
  }
}

async function doVerify(row: BackupFile) {
  busy.value = row.name
  try {
    const res: VerifyResult = await maintenanceApi.verify(row.name)
    const lines = [
      t('校验结果：{result}', { result: res.ok ? t('通过') : t('失败') }),
      t('驱动：{driver}', { driver: res.driver }),
      t('完整性：{value}', { value: res.integrity || '—' }),
      t('元信息比对：{result}', { result: res.metaMatched ? t('一致') : t('不一致 / 缺失') }),
      t('表数量：{count}{users}', {
        count: res.tables?.length ?? 0,
        users: res.userCount != null ? t('，用户 {count} 条', { count: res.userCount }) : '',
      }),
      t('文件资产：{count} 个{note}', {
        count: res.files ?? 0,
        note: res.fileCheck ? t('（{check}）', { check: res.fileCheck }) : '',
      }),
      t('SHA-256：{value}', { value: res.sha256 || '—' }),
    ]
    await ElMessageBox.alert(lines.join('\n'), t('校验 {name}', { name: row.name }), {
      confirmButtonText: t('知道了'),
    })
    await load()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('校验失败'))
  } finally {
    busy.value = ''
  }
}

function fmtPlan(res: RestoreResult) {
  const lines = [
    t('模式：{mode}', {
      mode: res.mode === 'offline' ? t('停机恢复（SQLite）') : t('在线恢复'),
    }),
    t('—— 演练计划（未改动任何文件）——'),
    ...(res.plan ?? []).map((p) => `· ${p}`),
  ]
  return lines.join('\n')
}

async function doRestorePlan(row: BackupFile) {
  busy.value = row.name
  try {
    const res = await maintenanceApi.restorePlan(row.name)
    await ElMessageBox.alert(fmtPlan(res), t('恢复演练 {name}', { name: row.name }), {
      confirmButtonText: t('知道了'),
    })
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : t('演练失败'))
  } finally {
    busy.value = ''
  }
}

async function doRestore(row: BackupFile) {
  // 真实恢复前先演练，展示计划并二次确认（演练不会改动文件）
  busy.value = row.name
  let preview: RestoreResult
  try {
    preview = await maintenanceApi.restorePlan(row.name)
  } catch (err) {
    busy.value = ''
    ElMessage.error(err instanceof Error ? err.message : t('演练失败，已中止恢复'))
    return
  }
  try {
    await ElMessageBox.confirm(
      `${fmtPlan(preview)}\n\n${t('恢复会用该备份覆盖当前数据，属于高危操作。SQLite 需停机后用命令行恢复；MySQL / PostgreSQL 需开启 backup.allow_restore。确定继续？')}`,
      t('恢复数据库'),
      { type: 'warning', confirmButtonText: t('继续'), cancelButtonText: t('取消') },
    )
  } catch {
    busy.value = ''
    return
  }
  try {
    const res = await maintenanceApi.restore(row.name)
    const details = [
      res.message,
      res.safetyCopy ? t('安全副本：{path}', { path: res.safetyCopy }) : '',
      res.filesRestored ? t('已还原文件资产：{count} 个', { count: res.filesRestored }) : '',
      ...(res.postCheck ?? []),
    ].filter(Boolean)
    await ElMessageBox.alert(details.join('\n'), t('恢复结果'))
  } catch (err) {
    const msg = err instanceof Error ? err.message : t('恢复失败')
    await ElMessageBox.alert(msg, t('无法在线恢复'), { confirmButtonText: t('知道了') })
  } finally {
    busy.value = ''
  }
}

async function doDelete(row: BackupFile) {
  try {
    await ElMessageBox.confirm(
      t('删除备份「{name}」后无法找回，确认删除？', { name: row.name }),
      t('删除备份'),
      {
        type: 'warning',
        confirmButtonText: t('删除'),
        cancelButtonText: t('取消'),
      },
    )
  } catch {
    return
  }
  await maintenanceApi.remove(row.name)
  ElMessage.success(t('已删除'))
  await load()
}

async function migrateUp() {
  await maintenanceApi.migrateUp()
  ElMessage.success(t('已升级到最新版本'))
  await load()
}

async function migrateDown() {
  try {
    await ElMessageBox.confirm(
      t('回滚会撤销最近一个版本的结构或数据变更（可能删表/删列），仅用于升级失败时应急。确定回滚？'),
      t('回滚数据库版本'),
      { type: 'warning', confirmButtonText: t('回滚'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  await maintenanceApi.migrateDown()
  ElMessage.success(t('已回滚一个版本'))
  await load()
}
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1>{{ $t('备份与迁移') }}</h1>
        <p class="page-desc">
          {{ $t('数据库快照 + 本地文件资产一并归档，完整性校验与版本化结构变更。定时备份任务默认每天 02:00 执行，按 GFS 分档保留。') }}
        </p>
      </div>
      <div class="head-actions">
        <el-button :icon="Refresh" @click="load">{{ $t('刷新') }}</el-button>
        <el-button
          v-if="auth.can('ops:backup:create')"
          type="primary"
          :icon="Plus"
          :loading="creating"
          @click="createBackup"
        >
          {{ $t('立即备份') }}
        </el-button>
      </div>
    </header>

    <section class="stats">
      <div class="stat">
        <span>{{ $t('备份份数') }}</span>
        <strong>{{ rows.length }}</strong>
        <em>{{ $t('份') }}</em>
      </div>
      <div class="stat">
        <span>{{ $t('最近备份') }}</span>
        <strong class="stat-text">{{ latest ? fmt(latest) : $t('暂无') }}</strong>
      </div>
      <div class="stat">
        <span>{{ $t('数据库版本') }}</span>
        <strong>{{ currentVersion }}<em> / {{ latestVersion }}</em></strong>
      </div>
      <div class="stat">
        <span>{{ $t('待应用版本') }}</span>
        <strong :class="{ warn: pending > 0 }">{{ pending }}</strong>
        <em>{{ $t('个') }}</em>
      </div>
      <div class="stat">
        <span>{{ $t('文件资产') }}</span>
        <strong>{{ totalFiles }}</strong>
        <em>{{ $t('个（最新备份）') }}</em>
      </div>
    </section>

    <el-tabs v-model="activeTab" class="tabs">
      <el-tab-pane :label="$t('备份文件')" name="backups">
        <div class="panel">
          <el-table v-loading="loading" :data="rows" row-key="name" stripe>
            <el-table-column :label="$t('文件名')" min-width="260">
              <template #default="{ row }">
                <div class="file-name">{{ row.name }}</div>
                <div class="dim sha">{{ row.sha256 ? `sha256 ${row.sha256.slice(0, 16)}…` : $t('无校验信息') }}</div>
              </template>
            </el-table-column>
            <el-table-column :label="$t('驱动')" width="110">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ row.driver }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('大小')" width="110">
              <template #default="{ row }">{{ fmtSize(row.size) }}</template>
            </el-table-column>
            <el-table-column :label="$t('文件资产')" width="100">
              <template #default="{ row }">
                <span v-if="row.files" class="yes">{{ row.files }} {{ $t('个') }}</span>
                <span v-else class="dim">—</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('创建时间')" width="180">
              <template #default="{ row }">{{ fmt(row.createdAt) }}</template>
            </el-table-column>
            <el-table-column :label="$t('压缩')" width="90">
              <template #default="{ row }">
                <span :class="row.compressed ? 'yes' : 'dim'">{{ row.compressed ? 'gzip' : $t('原始') }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('操作')" width="310" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" :icon="Download" :loading="busy === row.name" @click="doDownload(row)">
                  {{ $t('下载') }}
                </el-button>
                <el-button link type="primary" :icon="View" @click="doVerify(row)">{{ $t('校验') }}</el-button>
                <el-button
                  v-if="auth.can('ops:backup:restore')"
                  link
                  type="info"
                  :icon="View"
                  :loading="busy === row.name"
                  @click="doRestorePlan(row)"
                >
                  {{ $t('演练') }}
                </el-button>
                <el-button
                  v-if="auth.can('ops:backup:restore')"
                  link
                  type="warning"
                  :icon="RefreshLeft"
                  @click="doRestore(row)"
                >
                  {{ $t('恢复') }}
                </el-button>
                <el-button
                  v-if="auth.can('ops:backup:delete')"
                  link
                  type="danger"
                  :icon="Delete"
                  @click="doDelete(row)"
                >
                  {{ $t('删除') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <div class="empty">
                <p>{{ $t('还没有备份文件') }}</p>
                <p class="dim">{{ $t('点击「立即备份」生成第一份，或等待定时任务执行') }}</p>
              </div>
            </template>
          </el-table>
        </div>

        <div class="tip">
          <div class="tip-title"><el-icon><Upload /></el-icon> {{ $t('离线恢复（SQLite）') }}</div>
          <p v-html="$t('每份备份是 tar 容器，内含数据库快照与 <code>files/</code> 文件资产。SQLite 需要替换数据文件，必须在服务停止后执行，恢复前会自动把当前库另存为 <code>pre-restore-*.db</code>、把旧文件目录改名保留：')"></p>
          <pre>keel -config configs/config.yaml restore &lt;{{ $t('备份文件名') }}&gt;
keel -config configs/config.yaml restore --dry-run &lt;{{ $t('备份文件名') }}&gt;   # {{ $t('只看计划，不改文件') }}</pre>
          <p class="dim" v-html="$t('恢复后会自动逐表比对行数；MySQL / PostgreSQL 可在开启 <code>backup.allow_restore</code> 后使用页面上的「恢复」按钮在线导入。')"></p>
        </div>
      </el-tab-pane>

      <el-tab-pane :label="$t('保留策略')" name="prune">
        <div class="panel prune-panel" v-loading="loading">
          <div class="prune-head">
            <div>
              <strong>{{ $t('GFS 分档保留') }}</strong>
              <div class="dim">{{ $t('按小时 / 天 / 周 / 月的槽位各保留若干份，断档时不会误删窗口内唯一备份；最新一份永不删除。') }}</div>
            </div>
          </div>
          <div v-if="prune" class="prune-body">
            <div class="prune-stats">
              <div><span>{{ $t('当前备份') }}</span><strong>{{ prune.keep.length + prune.remove.length }}</strong></div>
              <div><span>{{ $t('将保留') }}</span><strong class="yes">{{ prune.keep.length }}</strong></div>
              <div><span>{{ $t('将清理') }}</span><strong class="warn">{{ prune.remove.length }}</strong></div>
              <div><span>{{ $t('预计释放') }}</span><strong>{{ fmtSize(prune.removeBytes) }}</strong></div>
            </div>
            <div v-if="prune.slots" class="slot-line">
              <el-tag v-for="(n, k) in prune.slots" :key="k" size="small" effect="plain" class="slot-tag">
                {{ k }}: {{ n }}
              </el-tag>
            </div>
            <div v-if="prune.remove.length" class="prune-list">
              <div class="dim">{{ $t('将被清理（演练结果，需 CLI 执行）：') }}</div>
              <ul>
                <li v-for="name in prune.remove.slice(0, 10)" :key="name"><code>{{ name }}</code></li>
              </ul>
              <div v-if="prune.remove.length > 10" class="dim">{{ $t('… 还有 {count} 份', { count: prune.remove.length - 10 }) }}</div>
            </div>
          </div>
          <div v-else class="disabled-note">{{ $t('未获取到保留策略演练结果') }}</div>
        </div>
        <div class="tip">
          <div class="tip-title">{{ $t('命令行演练与执行') }}</div>
          <pre>keel -config configs/config.yaml backup-prune --dry-run
keel -config configs/config.yaml backup-prune</pre>
          <p class="dim" v-html="$t('配置见 <code>backup.retain_hourly / retain_daily / retain_weekly / retain_monthly</code>；四档全为 0 时退回按 <code>retain</code> 保留最近 N 份。')"></p>
        </div>
      </el-tab-pane>

      <el-tab-pane :label="$t('数据库迁移')" name="migrations">
        <div v-if="!migration?.enabled" class="disabled-note">
          {{ migration?.message || $t('未启用版本化迁移') }}
        </div>
        <template v-else>
          <div class="migrate-head">
            <div>
              <strong>{{ $t('当前版本 {version}', { version: migration.status?.current }) }}</strong>
              <span class="dim"> / {{ $t('最新 {version}', { version: migration.status?.latest }) }}</span>
              <div class="dim">{{ $t('驱动 {driver} · 版本表 {table}', { driver: migration.status?.driver, table: migration.status?.table }) }}</div>
            </div>
            <div class="head-actions" v-if="auth.can('ops:backup:restore')">
              <el-button :icon="Upload" :disabled="pending === 0" @click="migrateUp">{{ $t('升级到最新') }}</el-button>
              <el-button :icon="RefreshLeft" :disabled="currentVersion === 0" @click="migrateDown">{{ $t('回滚一步') }}</el-button>
            </div>
          </div>
          <div class="panel">
            <el-table :data="migration.status?.versions ?? []" stripe>
              <el-table-column :label="$t('版本')" width="100">
                <template #default="{ row }"><code>v{{ row.version }}</code></template>
              </el-table-column>
              <el-table-column :label="$t('名称')" min-width="200">
                <template #default="{ row }">{{ row.name }}</template>
              </el-table-column>
              <el-table-column :label="$t('状态')" width="130">
                <template #default="{ row }">
                  <el-tag v-if="row.applied" type="success" size="small" effect="light">{{ $t('已应用') }}</el-tag>
                  <el-tag v-else type="info" size="small" effect="plain">{{ $t('待应用') }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column :label="$t('应用时间')" width="200">
                <template #default="{ row }">{{ fmt(row.appliedAt) }}</template>
              </el-table-column>
            </el-table>
          </div>
          <div class="tip">
            <div class="tip-title">{{ $t('命令行方式') }}</div>
            <p>{{ $t('生产环境建议关闭启动时自动迁移，由发布流水线显式执行：') }}</p>
            <pre>keel -config configs/config.yaml migrate status
keel -config configs/config.yaml migrate up</pre>
          </div>
        </template>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<style scoped lang="scss">
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 20px;

  h1 {
    margin: 0 0 6px;
    font-size: 21px;
    font-weight: 650;
    color: var(--ink-800);
  }
}

.page-desc {
  margin: 0;
  max-width: 720px;
  font-size: 13px;
  line-height: 1.7;
  color: var(--ink-400);
}

.head-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.stats {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 20px;
}

.stat {
  padding: 16px 18px;
  border: 1px solid var(--ink-100);
  border-radius: 10px;
  background: #fff;

  span {
    display: block;
    margin-bottom: 8px;
    font-size: 12px;
    color: var(--ink-400);
  }

  strong {
    font-size: 24px;
    font-weight: 650;
    color: var(--ink-800);
    font-variant-numeric: tabular-nums;

    &.warn { color: #d97706; }
    &.stat-text { font-size: 15px; font-weight: 550; }
  }

  em {
    margin-left: 4px;
    font-size: 12px;
    font-style: normal;
    color: var(--ink-400);
  }
}

.tabs {
  :deep(.el-tabs__item) { font-size: 14px; }
}

.panel {
  border: 1px solid var(--ink-100);
  border-radius: 10px;
  overflow: hidden;
  background: #fff;
}

.file-name {
  font-weight: 500;
  color: var(--ink-800);
  font-family: var(--font-mono);
  font-size: 12.5px;
}

.dim { color: var(--ink-400); font-size: 12px; }
.sha { margin-top: 3px; font-family: var(--font-mono); font-size: 11px; }
.yes { color: #16a34a; font-size: 12px; }

.empty {
  padding: 36px 0;
  text-align: center;
  color: var(--ink-500);

  p { margin: 0 0 6px; }
}

.tip {
  margin-top: 18px;
  padding: 16px 18px;
  border: 1px dashed var(--ink-200);
  border-radius: 10px;
  background: var(--bg-subtle);
  font-size: 12.5px;
  line-height: 1.75;
  color: var(--ink-600);

  p { margin: 6px 0; }

  code, pre {
    font-family: var(--font-mono);
    font-size: 12px;
  }

  pre {
    margin: 8px 0;
    padding: 10px 12px;
    border-radius: 6px;
    background: #fff;
    border: 1px solid var(--ink-100);
    color: var(--ink-700);
    overflow-x: auto;
  }
}

.tip-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  color: var(--ink-800);
}

.migrate-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 16px;
  padding: 16px 18px;
  border: 1px solid var(--ink-100);
  border-radius: 10px;
  background: #fff;

  strong { font-size: 16px; color: var(--ink-800); }
  .dim { margin-top: 4px; }
}

.disabled-note {
  padding: 30px;
  border: 1px dashed var(--ink-200);
  border-radius: 10px;
  text-align: center;
  color: var(--ink-400);
  background: var(--bg-subtle);
}

.prune-panel { padding: 18px; }
.prune-head {
  strong { font-size: 15px; color: var(--ink-800); }
  .dim { margin-top: 4px; }
}
.prune-body { margin-top: 16px; }
.prune-stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  div {
    padding: 12px 14px;
    border: 1px solid var(--ink-100);
    border-radius: 8px;
    background: var(--bg-subtle);
  }
  span { display: block; font-size: 12px; color: var(--ink-400); margin-bottom: 6px; }
  strong { font-size: 18px; color: var(--ink-800); font-variant-numeric: tabular-nums; }
  .warn { color: #d97706; }
}
.slot-line { margin-top: 14px; display: flex; gap: 6px; flex-wrap: wrap; }
.slot-tag { font-family: var(--font-mono); }
.prune-list {
  margin-top: 16px;
  ul { margin: 8px 0 0; padding-left: 18px; columns: 2; }
  li { font-size: 12px; line-height: 1.9; }
  code { font-family: var(--font-mono); font-size: 11.5px; color: var(--ink-600); }
}

@media (max-width: 1100px) {
  .stats { grid-template-columns: repeat(3, 1fr); }
}
@media (max-width: 900px) {
  .stats { grid-template-columns: repeat(2, 1fr); }
  .page-head, .migrate-head { flex-direction: column; }
}
</style>
