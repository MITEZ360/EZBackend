<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, Refresh, RefreshLeft } from '@element-plus/icons-vue'
import { clusterApi, configApi } from '@/api/ops'
import type { ConfigApplyResult, ConfigDeploy, ConfigGroup, ConfigItem } from '@/api/types'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()

const groups = ref<ConfigGroup[]>([])
const deploy = ref<ConfigDeploy | null>(null)
const active = ref('')
const pending = ref<Record<string, string>>({})
const loading = ref(false)
const saving = ref(false)

const current = computed(() => groups.value.find((g) => g.name === active.value))
const changedCount = computed(() => Object.keys(pending.value).length)

async function load() {
  loading.value = true
  try {
    const res = await configApi.items()
    groups.value = res.groups ?? []
    deploy.value = res.deploy
    if (!groups.value.some((g) => g.name === active.value)) {
      active.value = groups.value[0]?.name ?? 'deploy'
    }
    pending.value = {}
  } finally {
    loading.value = false
  }
}

function groupChanged(g: ConfigGroup) {
  return g.items.filter((i) => pending.value[i.key] !== undefined).length
}

/** 正在编辑的值：敏感项不回显，避免把密文或明文带到前端。 */
function shown(item: ConfigItem) {
  if (pending.value[item.key] !== undefined) return pending.value[item.key]
  return item.secret ? '' : item.value
}

function setValue(item: ConfigItem, value: string) {
  if (value === item.value) {
    delete pending.value[item.key]
    return
  }
  pending.value = { ...pending.value, [item.key]: value }
}

function discard(item: ConfigItem) {
  const next = { ...pending.value }
  delete next[item.key]
  pending.value = next
}

function discardAll() {
  pending.value = {}
}

function numValue(item: ConfigItem) {
  const n = Number(shown(item))
  return Number.isFinite(n) ? n : undefined
}

/**
 * 多选项当前勾选值。
 * 空值表示"不限制"（全部启用），因此展示为全选；常驻项永远在集合内，
 * 否则界面上会出现一个无法保存的空白名单。
 */
function chosen(item: ConfigItem): string[] {
  const choices = item.choices ?? []
  const raw = shown(item)
  if (!raw.trim()) return choices.map((c) => c.value)
  const picked = new Set(raw.split(',').map((s) => s.trim()).filter(Boolean))
  choices.filter((c) => c.locked).forEach((c) => picked.add(c.value))
  return choices.filter((c) => picked.has(c.value)).map((c) => c.value)
}

/**
 * 勾选时递归补齐依赖，取消时级联移除依赖它的模块。
 * 与后端 resolveModules 的规则保持一致，避免"我明明取消了它，重启后它又回来了"。
 */
function toggleChoice(item: ConfigItem, value: string, checked: boolean) {
  const choices = item.choices ?? []
  const byName = new Map(choices.map((c) => [c.value, c]))
  const picked = new Set(chosen(item))
  if (checked) {
    const add = (name: string) => {
      if (picked.has(name)) return
      picked.add(name)
      byName.get(name)?.dependsOn?.forEach(add)
    }
    add(value)
  } else {
    picked.delete(value)
    let pruned = true
    while (pruned) {
      pruned = false
      for (const name of [...picked]) {
        if (byName.get(name)?.dependsOn?.some((d) => !picked.has(d))) {
          picked.delete(name)
          pruned = true
        }
      }
    }
  }
  choices.filter((c) => c.locked).forEach((c) => picked.add(c.value))
  setValue(
    item,
    choices
      .filter((c) => picked.has(c.value))
      .map((c) => c.value)
      .join(','),
  )
}

function runningSourceLabel(source?: string) {
  if (source === 'micro.modules') return '微服务模块清单'
  if (source === 'modules.enabled') return '按白名单启用'
  return '全部启用'
}

const sourceLabel = (item: ConfigItem) => {
  if (pending.value[item.key] !== undefined) return t('待保存')
  switch (item.source) {
    case 'db':
      return t('管理端覆盖')
    case 'file':
      return t('配置文件')
    default:
      return t('默认值')
  }
}

async function save() {
  const items = Object.entries(pending.value).map(([key, value]) => ({ key, value }))
  if (!items.length) return
  saving.value = true
  try {
    const res: ConfigApplyResult = await configApi.save(items)
    ElMessage.success(t(res.message))
    await load()
    if (res.needRestart?.length) {
      await promptRestart(res.needRestart)
    }
  } catch {
    // 请求拦截器已提示错误
  } finally {
    saving.value = false
  }
}

async function promptRestart(keys: string[]) {
  const label = keys
    .map((k) => groups.value.flatMap((g) => g.items).find((i) => i.key === k)?.name ?? k)
    .join('、')
  try {
    await ElMessageBox.confirm(
      t('以下配置已保存，但需要重启服务才能生效：{list}。是否立即重启全部节点？', { list: label }),
      t('需要重启'),
      {
        type: 'warning',
        confirmButtonText: t('立即重启全部节点'),
        cancelButtonText: t('稍后手动重启'),
      },
    )
  } catch {
    return
  }
  try {
    await clusterApi.dispatch({ action: 'restart', scope: 'all' })
    ElMessage.success(t('重启指令已下发，节点会在数秒内依次重启'))
  } catch {
    // 请求拦截器已提示错误
  }
}

async function reload() {
  try {
    const res = await configApi.reload()
    ElMessage.success(res.message || t('配置已重新加载'))
    await load()
  } catch {
    // 请求拦截器已提示错误
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('配置中心') }}</h1>
        <p class="page-desc">
          {{ $t('部署时只需配置端口、数据库与密钥；其余参数在此调整。热生效项保存即生效，需重启项会明确提示') }}
        </p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" :loading="loading" @click="load">{{ $t('重新读取') }}</el-button>
        <el-button
          v-if="auth.can('ops:config:reload')"
          :icon="RefreshLeft"
          @click="reload"
        >
          {{ $t('刷新配置') }}
        </el-button>
      </div>
    </div>

    <div v-loading="loading" class="cfg-layout">
      <aside class="cfg-nav card">
        <button
          v-for="g in groups"
          :key="g.name"
          type="button"
          class="cfg-nav-item"
          :class="{ active: active === g.name }"
          @click="active = g.name"
        >
          <span class="nav-label">{{ $t(g.label) }}</span>
          <span v-if="groupChanged(g)" class="nav-dot">{{ groupChanged(g) }}</span>
        </button>
        <div class="cfg-nav-sep" />
        <button
          type="button"
          class="cfg-nav-item"
          :class="{ active: active === 'deploy' }"
          @click="active = 'deploy'"
        >
          <span class="nav-label">{{ $t('部署参数') }}</span>
          <el-tag size="small" type="info" effect="plain">{{ $t('只读') }}</el-tag>
        </button>
      </aside>

      <section class="card cfg-body">
        <template v-if="active === 'deploy'">
          <header class="cfg-head">
            <div>
              <h3>{{ $t('部署参数') }}</h3>
              <p>{{ $t(deploy?.note ?? '') }}</p>
            </div>
          </header>
          <div class="deploy-list">
            <div v-for="d in deploy?.items ?? []" :key="d.key" class="deploy-row">
              <div class="deploy-meta">
                <span class="deploy-name">{{ $t(d.name) }}</span>
                <code>{{ d.key }}</code>
                <p>{{ $t(d.description) }}</p>
              </div>
              <code class="deploy-value">{{ d.value }}</code>
            </div>
          </div>
        </template>

        <template v-else-if="current">
          <header class="cfg-head">
            <div>
              <h3>{{ $t(current.label) }}</h3>
              <p>{{ $t(current.description) }}</p>
            </div>
          </header>

          <div
            v-for="item in current.items"
            :key="item.key"
            class="cfg-row"
            :class="{ wide: item.choices?.length }"
          >
            <div class="cfg-meta">
              <div class="cfg-name">
                <span>{{ $t(item.name) }}</span>
                <el-tag v-if="item.hot" size="small" type="success" effect="plain">
                  {{ $t('热生效') }}
                </el-tag>
                <el-tag v-else size="small" type="warning" effect="plain">
                  {{ $t('需重启') }}
                </el-tag>
                <span class="cfg-source">{{ sourceLabel(item) }}</span>
              </div>
              <code class="cfg-key">{{ item.key }}</code>
              <p class="cfg-desc">{{ $t(item.description) }}</p>
              <div v-if="item.running?.length" class="running">
                <div class="running-head">
                  <span>
                    {{ $t('当前运行 {n} 个模块', { n: item.running.length }) }}
                    <em>{{ $t(runningSourceLabel(item.runningSource)) }}</em>
                  </span>
                  <span class="running-tip">{{ $t('与下方勾选不一致时，说明改动尚未重启') }}</span>
                </div>
                <div class="running-tags">
                  <el-tag v-for="m in item.running" :key="m" size="small" effect="plain">
                    {{ m }}
                  </el-tag>
                </div>
              </div>
            </div>

            <div class="cfg-control">
              <div v-if="item.choices?.length" class="module-grid">
                <div
                  v-for="c in item.choices"
                  :key="c.value"
                  class="module-item"
                  :class="{ locked: c.locked, on: chosen(item).includes(c.value) }"
                  @click="!c.locked && toggleChoice(item, c.value, !chosen(item).includes(c.value))"
                >
                  <el-checkbox
                    class="module-check"
                    :model-value="chosen(item).includes(c.value)"
                    :disabled="c.locked"
                  />
                  <div class="module-meta">
                    <div class="module-name">
                      <span>{{ $t(c.label) }}</span>
                      <code>{{ c.value }}</code>
                      <el-tag v-if="c.locked" size="small" type="info" effect="plain">
                        {{ $t('常驻') }}
                      </el-tag>
                      <el-tag v-else-if="c.dependsOn?.length" size="small" effect="plain">
                        {{ $t('依赖 {d}', { d: c.dependsOn.join('、') }) }}
                      </el-tag>
                    </div>
                    <p>{{ $t(c.description ?? '') }}</p>
                  </div>
                </div>
              </div>

              <template v-else>
                <el-switch
                  v-if="item.type === 'bool'"
                  :model-value="shown(item) === 'true'"
                  @change="(v: any) => setValue(item, String(v))"
                />
                <el-input-number
                  v-else-if="item.type === 'int' || item.type === 'float'"
                  :model-value="numValue(item)"
                  :min="item.min"
                  :max="item.max"
                  :step="item.type === 'float' ? 1 : 1"
                  controls-position="right"
                  @change="(v: any) => setValue(item, v === undefined ? '' : String(v))"
                />
                <el-select
                  v-else-if="item.options?.length"
                  :model-value="shown(item)"
                  class="w-full"
                  @change="(v: any) => setValue(item, v)"
                >
                  <el-option v-for="o in item.options" :key="o" :label="o" :value="o" />
                </el-select>
                <el-input
                  v-else
                  :model-value="shown(item)"
                  :type="item.secret ? 'password' : 'text'"
                  :show-password="item.secret"
                  :placeholder="item.secret ? $t('已配置时留空表示不修改') : item.default"
                  @input="(v: any) => setValue(item, v)"
                />
                <div class="cfg-hint">
                  <span v-if="item.unit">{{ $t('单位：{u}', { u: item.unit }) }}</span>
                  <span v-else-if="item.type === 'duration'">{{ $t('格式如 15m、2h、168h') }}</span>
                  <span v-else-if="item.type === 'list'">{{ $t('逗号分隔') }}</span>
                  <span v-else />
                  <el-button
                    v-if="pending[item.key] !== undefined"
                    link
                    size="small"
                    @click="discard(item)"
                  >
                    {{ $t('撤销') }}
                  </el-button>
                  <el-button
                    v-else-if="item.overridden && auth.can('ops:config:update')"
                    link
                    size="small"
                    @click="setValue(item, '')"
                  >
                    {{ $t('恢复配置文件值') }}
                  </el-button>
                </div>
              </template>
            </div>
          </div>
        </template>
      </section>
    </div>

    <div v-if="changedCount" class="save-bar">
      <span class="save-text">{{ $t('{n} 项待保存', { n: changedCount }) }}</span>
      <div class="save-actions">
        <el-button @click="discardAll">{{ $t('放弃修改') }}</el-button>
        <el-button
          type="primary"
          :icon="Check"
          :loading="saving"
          :disabled="!auth.can('ops:config:update')"
          @click="save"
        >
          {{ $t('保存并应用') }}
        </el-button>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.page-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.cfg-layout {
  display: grid;
  grid-template-columns: 196px minmax(0, 1fr);
  gap: 14px;
  align-items: start;
  padding-bottom: 64px;
}

.cfg-nav {
  display: flex;
  flex-direction: column;
  padding: 8px;
  position: sticky;
  top: 12px;
}

.cfg-nav-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 9px 11px;
  border: 0;
  border-radius: var(--r-md);
  background: transparent;
  color: var(--ink-700);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;

  &:hover {
    background: var(--ink-50);
  }

  &.active {
    background: var(--brand-50);
    color: var(--brand-700);
    font-weight: 600;
  }
}

.nav-dot {
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--brand-500);
  color: #fff;
  font-size: 11px;
  line-height: 18px;
  text-align: center;
}

.cfg-nav-sep {
  height: 1px;
  margin: 8px 6px;
  background: var(--line-soft);
}

.cfg-body {
  padding: 4px 0 8px;
}

.cfg-head {
  padding: 14px 18px 6px;

  h3 {
    margin: 0 0 4px;
    font-size: 14px;
    font-weight: 650;
    color: var(--ink-900);
  }

  p {
    margin: 0;
    font-size: 12px;
    color: var(--ink-500);
  }
}

.cfg-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  gap: 20px;
  padding: 14px 18px;
  border-top: 1px solid var(--line-soft);
  align-items: start;
}

.cfg-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-800);
}

.cfg-source {
  font-size: 11px;
  font-weight: 400;
  color: var(--ink-400);
}

.cfg-key {
  display: inline-block;
  margin-top: 3px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-400);
}

.cfg-desc {
  margin: 6px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--ink-500);
}

.cfg-control {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

/* 模块开关需要整行铺开：一列说明、一列勾选会挤成窄条 */
.cfg-row.wide {
  grid-template-columns: 1fr;
  gap: 12px;
}

.module-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 8px 12px;
}

.module-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--line-soft);
  border-radius: var(--r-md);
  background: var(--bg-card);
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;

  &:hover {
    border-color: var(--brand-200);
  }

  &.on {
    border-color: var(--brand-200);
    background: var(--brand-50);
  }

  &.locked {
    background: var(--ink-50);
    cursor: default;

    &:hover {
      border-color: var(--line-soft);
    }
  }
}

/* 点击整块切换，勾选框只负责显示，避免一次点击触发两次切换 */
.module-check {
  flex: none;
  height: auto;
  margin-top: 1px;
  pointer-events: none;
}

.module-meta {
  min-width: 0;
}

.module-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-800);

  code {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 400;
    color: var(--ink-400);
  }
}

.module-meta p {
  margin: 4px 0 0;
  font-size: 12px;
  line-height: 1.55;
  color: var(--ink-500);
}

.running {
  margin-top: 10px;
  padding: 8px 10px;
  border-radius: var(--r-md);
  background: var(--ink-50);
}

.running-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 4px 12px;
  font-size: 12px;
  color: var(--ink-600);

  em {
    margin-left: 6px;
    font-style: normal;
    color: var(--ink-400);
  }
}

.running-tip {
  font-size: 11px;
  color: var(--ink-400);
}

.running-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 6px;
}

.cfg-hint {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 20px;
  font-size: 11px;
  color: var(--ink-400);
}

.w-full {
  width: 100%;
}

.deploy-list {
  padding: 0 18px 8px;
}

.deploy-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding: 12px 0;
  border-top: 1px solid var(--line-soft);
}

.deploy-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-800);
}

.deploy-meta {
  code {
    margin-left: 8px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--ink-400);
  }

  p {
    margin: 4px 0 0;
    font-size: 12px;
    color: var(--ink-500);
  }
}

.deploy-value {
  flex: none;
  max-width: 46%;
  padding: 4px 8px;
  border-radius: var(--r-sm);
  background: var(--ink-50);
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink-700);
  word-break: break-all;
}

.save-bar {
  position: fixed;
  right: 24px;
  bottom: 20px;
  left: calc(var(--sidebar-w, 220px) + 24px);
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 16px;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-md);
}

.save-text {
  font-size: 13px;
  color: var(--ink-700);
}

.save-actions {
  display: flex;
  gap: 8px;
}

@media (max-width: 900px) {
  .cfg-layout {
    grid-template-columns: 1fr;
  }

  .cfg-nav {
    position: static;
    flex-direction: row;
    flex-wrap: wrap;
  }

  .cfg-row {
    grid-template-columns: 1fr;
    gap: 8px;
  }

  .save-bar {
    left: 16px;
    right: 16px;
  }
}
</style>
