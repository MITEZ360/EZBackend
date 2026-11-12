<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Download, Plus, Refresh, RefreshLeft, Upload, View } from '@element-plus/icons-vue'
import {
  downloadBackup,
  maintenanceApi,
  type BackupFile,
  type MigrationResult,
  type VerifyResult,
} from '@/api/maintenance'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const rows = ref<BackupFile[]>([])
const migration = ref<MigrationResult | null>(null)
const loading = ref(false)
const creating = ref(false)
const activeTab = ref('backups')
const busy = ref('')

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
    const [backups, mig] = await Promise.all([
      maintenanceApi.backups(),
      maintenanceApi.migrations().catch(() => null),
    ])
    rows.value = backups ?? []
    migration.value = mig
  } finally {
    loading.value = false
  }
}

onMounted(load)

async function createBackup() {
  creating.value = true
  try {
    const meta = await maintenanceApi.createBackup()
    ElMessage.success(`已生成备份 ${meta.name}`)
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
    ElMessage.error(err instanceof Error ? err.message : '下载失败')
  } finally {
    busy.value = ''
  }
}

async function doVerify(row: BackupFile) {
  busy.value = row.name
  try {
    const res: VerifyResult = await maintenanceApi.verify(row.name)
    const lines = [
      `校验结果：${res.ok ? '通过' : '失败'}`,
      `驱动：${res.driver}`,
      `完整性：${res.integrity || '—'}`,
      `元信息比对：${res.metaMatched ? '一致' : '不一致 / 缺失'}`,
      `表数量：${res.tables?.length ?? 0}${res.userCount ? `，用户 ${res.userCount} 条` : ''}`,
      `SHA-256：${res.sha256 || '—'}`,
    ]
    await ElMessageBox.alert(lines.join('\n'), `校验 ${row.name}`, { confirmButtonText: '知道了' })
    await load()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : '校验失败')
  } finally {
    busy.value = ''
  }
}

async function doRestore(row: BackupFile) {
  try {
    await ElMessageBox.confirm(
      '恢复会用该备份覆盖当前数据，属于高危操作。SQLite 需停机后用命令行恢复；MySQL / PostgreSQL 需开启 backup.allow_restore。确定继续？',
      '恢复数据库',
      { type: 'warning', confirmButtonText: '继续', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  busy.value = row.name
  try {
    const res = await maintenanceApi.restore(row.name)
    await ElMessageBox.alert(`${res.message}${res.safetyCopy ? `\n安全副本：${res.safetyCopy}` : ''}`, '恢复结果')
  } catch (err) {
    const msg = err instanceof Error ? err.message : '恢复失败'
    await ElMessageBox.alert(msg, '无法在线恢复', { confirmButtonText: '知道了' })
  } finally {
    busy.value = ''
  }
}

async function doDelete(row: BackupFile) {
  try {
    await ElMessageBox.confirm(`删除备份「${row.name}」后无法找回，确认删除？`, '删除备份', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  await maintenanceApi.remove(row.name)
  ElMessage.success('已删除')
  await load()
}

async function migrateUp() {
  await maintenanceApi.migrateUp()
  ElMessage.success('已升级到最新版本')
  await load()
}

async function migrateDown() {
  try {
    await ElMessageBox.confirm(
      '回滚会撤销最近一个版本的结构或数据变更（可能删表/删列），仅用于升级失败时应急。确定回滚？',
      '回滚数据库版本',
      { type: 'warning', confirmButtonText: '回滚', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  await maintenanceApi.migrateDown()
  ElMessage.success('已回滚一个版本')
  await load()
}
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <h1>备份与迁移</h1>
        <p class="page-desc">
          数据库快照、完整性校验与版本化结构变更。定时备份任务默认每天 02:00 执行，按配置保留最近若干份。
        </p>
      </div>
      <div class="head-actions">
        <el-button :icon="Refresh" @click="load">刷新</el-button>
        <el-button
          v-if="auth.can('ops:backup:create')"
          type="primary"
          :icon="Plus"
          :loading="creating"
          @click="createBackup"
        >
          立即备份
        </el-button>
      </div>
    </header>

    <section class="stats">
      <div class="stat">
        <span>备份份数</span>
        <strong>{{ rows.length }}</strong>
        <em>份</em>
      </div>
      <div class="stat">
        <span>最近备份</span>
        <strong class="stat-text">{{ latest ? fmt(latest) : '暂无' }}</strong>
      </div>
      <div class="stat">
        <span>数据库版本</span>
        <strong>{{ currentVersion }}<em> / {{ latestVersion }}</em></strong>
      </div>
      <div class="stat">
        <span>待应用版本</span>
        <strong :class="{ warn: pending > 0 }">{{ pending }}</strong>
        <em>个</em>
      </div>
    </section>

    <el-tabs v-model="activeTab" class="tabs">
      <el-tab-pane label="备份文件" name="backups">
        <div class="panel">
          <el-table v-loading="loading" :data="rows" row-key="name" stripe>
            <el-table-column label="文件名" min-width="260">
              <template #default="{ row }">
                <div class="file-name">{{ row.name }}</div>
                <div class="dim sha">{{ row.sha256 ? `sha256 ${row.sha256.slice(0, 16)}…` : '无校验信息' }}</div>
              </template>
            </el-table-column>
            <el-table-column label="驱动" width="110">
              <template #default="{ row }">
                <el-tag size="small" effect="plain">{{ row.driver }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="大小" width="110">
              <template #default="{ row }">{{ fmtSize(row.size) }}</template>
            </el-table-column>
            <el-table-column label="创建时间" width="180">
              <template #default="{ row }">{{ fmt(row.createdAt) }}</template>
            </el-table-column>
            <el-table-column label="压缩" width="90">
              <template #default="{ row }">
                <span :class="row.compressed ? 'yes' : 'dim'">{{ row.compressed ? 'gzip' : '原始' }}</span>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="270" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" :icon="Download" :loading="busy === row.name" @click="doDownload(row)">
                  下载
                </el-button>
                <el-button link type="primary" :icon="View" @click="doVerify(row)">校验</el-button>
                <el-button
                  v-if="auth.can('ops:backup:restore')"
                  link
                  type="warning"
                  :icon="RefreshLeft"
                  @click="doRestore(row)"
                >
                  恢复
                </el-button>
                <el-button
                  v-if="auth.can('ops:backup:delete')"
                  link
                  type="danger"
                  :icon="Delete"
                  @click="doDelete(row)"
                >
                  删除
                </el-button>
              </template>
            </el-table-column>
            <template #empty>
              <div class="empty">
                <p>还没有备份文件</p>
                <p class="dim">点击「立即备份」生成第一份，或等待定时任务执行</p>
              </div>
            </template>
          </el-table>
        </div>

        <div class="tip">
          <div class="tip-title"><el-icon><Upload /></el-icon> 离线恢复（SQLite）</div>
          <p>SQLite 需要替换数据文件，必须在服务停止后执行，恢复前会自动把当前库另存为 <code>pre-restore-*.db</code>：</p>
          <pre>keel -config configs/config.yaml restore &lt;备份文件名&gt;</pre>
          <p class="dim">MySQL / PostgreSQL 可在开启 <code>backup.allow_restore</code> 后使用页面上的「恢复」按钮在线导入。</p>
        </div>
      </el-tab-pane>

      <el-tab-pane label="数据库迁移" name="migrations">
        <div v-if="!migration?.enabled" class="disabled-note">
          {{ migration?.message || '未启用版本化迁移' }}
        </div>
        <template v-else>
          <div class="migrate-head">
            <div>
              <strong>当前版本 {{ migration.status?.current }}</strong>
              <span class="dim"> / 最新 {{ migration.status?.latest }}</span>
              <div class="dim">驱动 {{ migration.status?.driver }} · 版本表 {{ migration.status?.table }}</div>
            </div>
            <div class="head-actions" v-if="auth.can('ops:backup:restore')">
              <el-button :icon="Upload" :disabled="pending === 0" @click="migrateUp">升级到最新</el-button>
              <el-button :icon="RefreshLeft" :disabled="currentVersion === 0" @click="migrateDown">回滚一步</el-button>
            </div>
          </div>
          <div class="panel">
            <el-table :data="migration.status?.versions ?? []" stripe>
              <el-table-column label="版本" width="100">
                <template #default="{ row }"><code>v{{ row.version }}</code></template>
              </el-table-column>
              <el-table-column label="名称" min-width="200">
                <template #default="{ row }">{{ row.name }}</template>
              </el-table-column>
              <el-table-column label="状态" width="130">
                <template #default="{ row }">
                  <el-tag v-if="row.applied" type="success" size="small" effect="light">已应用</el-tag>
                  <el-tag v-else type="info" size="small" effect="plain">待应用</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="应用时间" width="200">
                <template #default="{ row }">{{ fmt(row.appliedAt) }}</template>
              </el-table-column>
            </el-table>
          </div>
          <div class="tip">
            <div class="tip-title">命令行方式</div>
            <p>生产环境建议关闭启动时自动迁移，由发布流水线显式执行：</p>
            <pre>keel -config configs/config.yaml migrate status
keel -config configs/config.yaml migrate up</pre>
          </div>
        </template>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<style scoped lang="scss">
.page {
  padding: 4px 2px 24px;
}

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
  grid-template-columns: repeat(4, 1fr);
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

@media (max-width: 900px) {
  .stats { grid-template-columns: repeat(2, 1fr); }
  .page-head, .migrate-head { flex-direction: column; }
}
</style>
