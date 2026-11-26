<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { routeApi, type GovReleaseView } from '@/api/gov'
import { fmtTime } from '@/utils/format'

const { t } = useI18n()

const releases = ref<GovReleaseView[]>([])
const loading = ref(false)
const acting = ref('')

const current = computed(() => releases.value.find((r) => r.status === 'active'))
const canary = computed(() => current.value?.stage === 'canary')

async function load() {
  loading.value = true
  try {
    releases.value = await routeApi.releases()
  } finally {
    loading.value = false
  }
}

function stageLabel(s: string) {
  return s === 'canary' ? t('灰度') : t('全量')
}

function statusLabel(s: string) {
  const map: Record<string, string> = { active: '生效中', done: '已被替代', rolled_back: '已回滚' }
  return map[s] ? t(map[s]) : s
}

function targetCount(raw?: string): number {
  if (!raw) return 0
  try {
    const arr = JSON.parse(raw)
    return Array.isArray(arr) ? arr.length : 0
  } catch {
    return 0
  }
}

async function promote(row: GovReleaseView) {
  try {
    await ElMessageBox.confirm(
      t('把 r{rev} 的灰度范围扩大为全量？所有网关都会切到这一版。', { rev: row.revision }),
      t('转为全量'),
      { type: 'info', confirmButtonText: t('确认'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  acting.value = row.id
  try {
    await routeApi.promote(row.id)
    ElMessage.success(t('已转为全量'))
    await load()
  } finally {
    acting.value = ''
  }
}

async function rollback(row: GovReleaseView) {
  try {
    await ElMessageBox.confirm(
      t('按 r{rev} 的内容生成一个新版本并立即生效。旧版本记录不会删除，回滚本身也能再回滚。', {
        rev: row.revision,
      }),
      t('回滚到该版本'),
      { type: 'warning', confirmButtonText: t('确认回滚'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  acting.value = row.id
  try {
    const rel = await routeApi.rollback(row.id)
    ElMessage.success(t('已生成 r{rev} 并生效', { rev: rel.revision }))
    await load()
  } finally {
    acting.value = ''
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('发布记录') }}</h1>
        <p class="page-desc">
          {{ $t('每次发布都会生成一个递增的版本号，数据面只接受比本地更新的版本，因此回滚也会产生新号') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="loading" @click="load">{{ $t('刷新') }}</el-button>
      </div>
    </div>

    <div class="stat-row">
      <div class="stat-card">
        <span class="sc-label">{{ $t('当前生效') }}</span>
        <div class="sc-value">{{ current ? `r${current.revision}` : '—' }}</div>
        <span class="sc-foot">{{ current ? fmtTime(current.createdAt) : $t('还没有发布过') }}</span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('发布阶段') }}</span>
        <div class="sc-value">{{ current ? stageLabel(current.stage) : '—' }}</div>
        <span class="sc-foot">
          {{
            canary
              ? $t('仅 {n} 个目标在用新版', { n: targetCount(current?.targets) })
              : $t('全量网关都用这一版')
          }}
        </span>
      </div>
      <div class="stat-card">
        <span class="sc-label">{{ $t('历史版本') }}</span>
        <div class="sc-value">{{ releases.length }}</div>
        <span class="sc-foot">{{ $t('最多保留最近 50 条') }}</span>
      </div>
    </div>

    <div class="card">
      <el-table v-loading="loading" :data="releases" row-key="id">
        <el-table-column :label="$t('版本')" width="100">
          <template #default="{ row }">
            <span class="rev mono">r{{ row.revision }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('阶段')" width="96">
          <template #default="{ row }">
            <el-tag size="small" :type="row.stage === 'canary' ? 'warning' : 'info'" effect="plain">
              {{ stageLabel(row.stage) }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column :label="$t('状态')" width="110">
          <template #default="{ row }">
            <span class="status-dot" :class="row.status === 'active' ? 'is-on' : 'is-off'">
              {{ statusLabel(row.status) }}
            </span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('灰度范围')" width="120">
          <template #default="{ row }">
            <span v-if="row.stage === 'canary'" class="mono">{{ targetCount(row.targets) }} {{ $t('个目标') }}</span>
            <span v-else class="dim">{{ $t('全量') }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('备注')" min-width="220">
          <template #default="{ row }">
            <span :class="row.note ? '' : 'dim'">{{ row.note || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('校验和')" width="130">
          <template #default="{ row }">
            <span class="mono dim" :title="row.checksum">{{ row.checksum.slice(0, 10) }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('操作人')" width="110">
          <template #default="{ row }">
            <span :class="row.operator ? '' : 'dim'">{{ row.operator || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('发布时间')" width="165">
          <template #default="{ row }">
            <span class="dim fs-12">{{ fmtTime(row.createdAt) }}</span>
          </template>
        </el-table-column>

        <el-table-column label="" width="180" align="right" fixed="right">
          <template #default="{ row }">
            <el-button
              v-if="row.status === 'active' && row.stage === 'canary'"
              v-perm="'route:rule:publish'"
              link
              type="primary"
              :loading="acting === row.id"
              @click="promote(row)"
            >
              {{ $t('转全量') }}
            </el-button>
            <el-button
              v-if="row.status !== 'active'"
              v-perm="'route:release:rollback'"
              link
              type="warning"
              :loading="acting === row.id"
              @click="rollback(row)"
            >
              {{ $t('回滚到此版本') }}
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <el-empty :description="$t('还没有发布过路由规则')" :image-size="72" />
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

.stat-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
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

.rev {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-900);
}

.mono {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}

.dim {
  color: var(--ink-400);
}

@media (max-width: 900px) {
  .stat-row {
    grid-template-columns: 1fr;
  }
}
</style>
