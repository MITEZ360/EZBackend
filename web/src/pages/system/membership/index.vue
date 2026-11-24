<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Delete, Plus, Refresh } from '@element-plus/icons-vue'
import { membershipApi, type MembershipLevelPayload } from '@/api/membership'
import type { MembershipBenefit, MembershipLevel } from '@/api/types'

const { t } = useI18n()

const levels = ref<MembershipLevel[]>([])
const loading = ref(false)

const activeCount = computed(() => levels.value.filter((l) => l.status === 1).length)
const memberTotal = computed(() => levels.value.reduce((sum, l) => sum + (l.userCount ?? 0), 0))

async function load() {
  loading.value = true
  try {
    const res = await membershipApi.list({ page: 1, page_size: 500 })
    levels.value = res.list ?? []
  } finally {
    loading.value = false
  }
}

onMounted(load)

/* ------------------------------------------------------------ 编辑抽屉 */
const drawer = ref(false)
const editingId = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const saving = ref(false)
const formRef = ref<FormInstance>()

const COLOR_PRESETS = ['#8A94A6', '#C08A2E', '#6D5BD0', '#2E90FA', '#12B76A', '#E0556B']

const form = reactive<MembershipLevelPayload>({
  code: '',
  name: '',
  rank: 10,
  status: 1,
  description: '',
  defaultDays: 365,
  color: COLOR_PRESETS[0],
  benefits: [],
})

const rules = computed<FormRules>(() => ({
  name: [{ required: true, message: t('请输入等级名称'), trigger: 'blur' }],
  code: [
    { required: true, message: t('请输入等级编码'), trigger: 'blur' },
    {
      pattern: /^[a-z][a-z0-9_]{1,31}$/,
      message: t('小写字母开头，仅含小写字母、数字与下划线'),
      trigger: 'blur',
    },
  ],
}))

function resetForm() {
  Object.assign(form, {
    code: '',
    name: '',
    rank: 10,
    status: 1,
    description: '',
    defaultDays: 365,
    color: COLOR_PRESETS[0],
    benefits: [] as MembershipBenefit[],
  })
  formRef.value?.clearValidate()
}

function openCreate() {
  editingId.value = ''
  resetForm()
  form.rank = (levels.value[0]?.rank ?? 0) + 10
  drawer.value = true
}

function openEdit(lv: MembershipLevel) {
  editingId.value = lv.id
  Object.assign(form, {
    code: lv.code,
    name: lv.name,
    rank: lv.rank,
    status: lv.status,
    description: lv.description ?? '',
    defaultDays: lv.defaultDays,
    color: lv.color || COLOR_PRESETS[0],
    benefits: (lv.benefits ?? []).map((b) => ({ ...b })),
  })
  formRef.value?.clearValidate()
  drawer.value = true
}

function addBenefit() {
  form.benefits.push({ key: '', label: '', value: '' })
}

function removeBenefit(index: number) {
  form.benefits.splice(index, 1)
}

/** 布尔型权益用符号展示，避免卡片里出现裸的 true/false */
function benefitValue(v: string) {
  const s = (v ?? '').trim().toLowerCase()
  if (s === 'true') return '✓'
  if (s === 'false' || s === '') return '—'
  return v
}

async function submit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return

  const filled = form.benefits.filter((b) => b.key.trim() || b.label.trim() || b.value.trim())
  const keys = new Set<string>()
  for (const b of filled) {
    if (!b.key.trim()) {
      ElMessage.warning(t('权益标识不能为空'))
      return
    }
    if (keys.has(b.key.trim())) {
      ElMessage.warning(t('权益标识「{key}」重复', { key: b.key }))
      return
    }
    keys.add(b.key.trim())
  }

  saving.value = true
  try {
    const payload: MembershipLevelPayload = { ...form, benefits: filled }
    if (isEdit.value) {
      await membershipApi.update(editingId.value, payload)
      ElMessage.success(t('已保存'))
    } else {
      await membershipApi.create(payload)
      ElMessage.success(t('等级已创建'))
    }
    drawer.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function remove(lv: MembershipLevel) {
  try {
    await ElMessageBox.confirm(
      t('删除「{name}」后，该等级的权益配置将不可恢复。仍有用户属于该等级时无法删除，可改为停用。确定删除吗？', { name: lv.name }),
      t('删除会员等级'),
      { confirmButtonText: t('删除'), cancelButtonText: t('取消'), type: 'error' },
    )
  } catch {
    return
  }
  await membershipApi.remove(lv.id)
  ElMessage.success(t('已删除'))
  await load()
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('会员等级') }}</h1>
        <p class="page-desc">{{ $t('配置等级名称、标识与权益；用户没有等级即非会员') }}</p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" @click="load">{{ $t('刷新') }}</el-button>
        <el-button v-perm="'system:membership:create'" type="primary" :icon="Plus" @click="openCreate">
          {{ $t('新增等级') }}
        </el-button>
      </div>
    </div>

    <div class="summary">
      <div class="summary-item">
        <span>{{ $t('等级数量') }}</span>
        <strong class="num">{{ levels.length }}</strong>
      </div>
      <div class="summary-item">
        <span>{{ $t('启用中') }}</span>
        <strong class="num">{{ activeCount }}</strong>
      </div>
      <div class="summary-item">
        <span>{{ $t('有效会员') }}</span>
        <strong class="num">{{ memberTotal }}</strong>
      </div>
      <p class="summary-tip">
        {{ $t('等级停用或会员到期后，用户自动按非会员处理；数据保留，便于查看开通历史。') }}
      </p>
    </div>

    <div v-loading="loading" class="level-grid">
      <article
        v-for="lv in levels"
        :key="lv.id"
        class="level-card"
        :class="{ 'is-off': lv.status === 0 }"
        :style="{ '--lv-color': lv.color || '#6366f1' }"
      >
        <header class="lv-head">
          <div class="lv-mark">{{ lv.name.slice(0, 1) }}</div>
          <div class="lv-title">
            <h3>{{ lv.name }}</h3>
            <span class="lv-code">{{ lv.code }}</span>
          </div>
          <span class="status-dot" :class="lv.status === 1 ? 'is-on' : 'is-off'">
            {{ lv.status === 1 ? $t('启用') : $t('停用') }}
          </span>
        </header>

        <p class="lv-desc">{{ lv.description || $t('暂无说明') }}</p>

        <div class="lv-stats">
          <div>
            <strong class="num">{{ lv.userCount ?? 0 }}</strong>
            <span>{{ $t('有效会员') }}</span>
          </div>
          <div>
            <strong class="num">{{ lv.defaultDays > 0 ? $t('{days} 天', { days: lv.defaultDays }) : $t('长期') }}</strong>
            <span>{{ $t('默认时长') }}</span>
          </div>
          <div>
            <strong class="num">{{ lv.benefits?.length ?? 0 }}</strong>
            <span>{{ $t('项权益') }}</span>
          </div>
        </div>

        <div class="lv-benefits">
          <span v-for="b in (lv.benefits ?? []).slice(0, 4)" :key="b.key" class="benefit-chip">
            <em>{{ b.label }}</em>
            {{ benefitValue(b.value) }}
          </span>
          <span v-if="!(lv.benefits ?? []).length" class="dim">{{ $t('尚未配置权益') }}</span>
          <span v-else-if="(lv.benefits ?? []).length > 4" class="dim">
            {{ $t('等 {count} 项', { count: lv.benefits.length }) }}
          </span>
        </div>

        <footer class="lv-foot">
          <span class="lv-rank">Lv.{{ lv.rank }}</span>
          <div class="lv-actions">
            <el-button v-perm="'system:membership:update'" link type="primary" @click="openEdit(lv)">
              {{ $t('编辑') }}
            </el-button>
            <el-button
              v-perm="'system:membership:delete'"
              link
              type="danger"
              :icon="Delete"
              @click="remove(lv)"
            >
              {{ $t('删除') }}
            </el-button>
          </div>
        </footer>
      </article>

      <button
        v-if="levels.length"
        v-perm="'system:membership:create'"
        class="level-card add-card"
        @click="openCreate"
      >
        <el-icon :size="20"><Plus /></el-icon>
        <span>{{ $t('新增等级') }}</span>
      </button>

      <div v-if="!loading && !levels.length" class="empty">
        <p>{{ $t('还没有配置会员等级') }}</p>
        <el-button v-perm="'system:membership:create'" type="primary" :icon="Plus" @click="openCreate">
          {{ $t('创建第一个等级') }}
        </el-button>
      </div>
    </div>

    <!-- 编辑抽屉 -->
    <el-drawer
      v-model="drawer"
      :title="isEdit ? $t('编辑会员等级') : $t('新增会员等级')"
      size="560px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" class="lv-form">
        <div class="form-row">
          <el-form-item :label="$t('等级名称')" prop="name">
            <el-input v-model="form.name" maxlength="64" :placeholder="$t('如：黄金会员')" />
          </el-form-item>
          <el-form-item :label="$t('等级编码')" prop="code">
            <el-input v-model="form.code" maxlength="32" :placeholder="$t('如：gold')" />
          </el-form-item>
        </div>

        <div class="form-row">
          <el-form-item :label="$t('等级权重')">
            <el-input-number v-model="form.rank" :min="0" :max="9999" :step="10" controls-position="right" style="width: 100%" />
            <p class="field-tip">{{ $t('数值越大等级越高，仅用于排序与展示') }}</p>
          </el-form-item>
          <el-form-item :label="$t('默认时长（天）')">
            <el-input-number v-model="form.defaultDays" :min="0" :max="36500" controls-position="right" style="width: 100%" />
            <p class="field-tip">{{ $t('开通时默认带出的天数，0 表示长期有效') }}</p>
          </el-form-item>
        </div>

        <el-form-item :label="$t('标识色')">
          <div class="color-row">
            <button
              v-for="c in COLOR_PRESETS"
              :key="c"
              type="button"
              class="color-dot"
              :class="{ active: form.color === c }"
              :style="{ background: c }"
              @click="form.color = c"
            />
            <el-input v-model="form.color" placeholder="#RRGGBB" style="width: 130px" />
          </div>
        </el-form-item>

        <el-form-item :label="$t('状态')">
          <el-switch v-model="form.status" :active-value="1" :inactive-value="0" />
          <span class="switch-text">{{ form.status === 1 ? $t('启用（可开通）') : $t('停用（不可新开通）') }}</span>
        </el-form-item>

        <el-form-item :label="$t('等级说明')">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="2"
            maxlength="255"
            show-word-limit
            :placeholder="$t('一句话描述该等级适合的人群')"
          />
        </el-form-item>

        <div class="benefit-head">
          <div>
            <label>{{ $t('会员权益') }}</label>
            <p class="field-tip">
              {{ $t('标识供业务判断（如 report.export），值为文案或开关；值为 false / 0 / off 视为未开通') }}
            </p>
          </div>
          <el-button link type="primary" :icon="Plus" @click="addBenefit">{{ $t('添加权益') }}</el-button>
        </div>

        <div class="benefit-editor">
          <div v-for="(b, i) in form.benefits" :key="i" class="benefit-row">
            <el-input v-model="b.key" :placeholder="$t('标识，如 report.export')" />
            <el-input v-model="b.label" :placeholder="$t('名称，如 导出报表')" />
            <el-input v-model="b.value" :placeholder="$t('值，如 true / 50 GB')" />
            <el-button link type="danger" :icon="Delete" @click="removeBenefit(i)" />
          </div>
          <p v-if="!form.benefits.length" class="dim benefit-empty">
            {{ $t('暂未配置权益，点击右上角「添加权益」') }}
          </p>
        </div>
      </el-form>

      <template #footer>
        <el-button @click="drawer = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submit">{{ $t('保存') }}</el-button>
      </template>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.summary {
  display: flex;
  align-items: center;
  gap: 28px;
  padding: 14px 20px;
  margin-bottom: 16px;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-xs);
  flex-wrap: wrap;
}

.summary-item {
  display: flex;
  align-items: baseline;
  gap: 8px;

  span {
    font-size: 12px;
    color: var(--ink-500);
  }

  strong {
    font-size: 20px;
    font-weight: 650;
    color: var(--ink-900);
    letter-spacing: -0.02em;
  }
}

.summary-tip {
  margin-left: auto;
  font-size: 12px;
  color: var(--ink-400);
}

.level-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
  min-height: 120px;
}

.level-card {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 18px;
  text-align: left;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-xs);
  overflow: hidden;
  transition: box-shadow 0.18s ease, transform 0.18s ease;

  &::before {
    content: '';
    position: absolute;
    inset: 0 0 auto 0;
    height: 3px;
    background: var(--lv-color);
  }

  &:hover {
    box-shadow: var(--sh-md);
    transform: translateY(-1px);
  }

  &.is-off {
    background: var(--bg-subtle);

    &::before {
      opacity: 0.35;
    }

    .lv-mark {
      filter: grayscale(1);
      opacity: 0.6;
    }
  }
}

.lv-head {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.lv-mark {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  flex: none;
  border-radius: 10px;
  background: color-mix(in srgb, var(--lv-color) 12%, white);
  color: var(--lv-color);
  font-size: 15px;
  font-weight: 700;
}

.lv-title {
  flex: 1;
  min-width: 0;

  h3 {
    margin: 0;
    font-size: 15px;
    font-weight: 650;
    color: var(--ink-900);
    line-height: 1.3;
  }
}

.lv-code {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-400);
}

.lv-desc {
  margin: 0;
  min-height: 34px;
  font-size: 12.5px;
  line-height: 1.55;
  color: var(--ink-500);
}

.lv-stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  padding: 12px 0;
  border-top: 1px solid var(--line-soft);
  border-bottom: 1px solid var(--line-soft);

  div {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  strong {
    font-size: 15px;
    font-weight: 650;
    color: var(--ink-900);
  }

  span {
    font-size: 11px;
    color: var(--ink-400);
  }
}

.lv-benefits {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  min-height: 22px;
}

.benefit-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: var(--r-full);
  background: var(--ink-100);
  font-size: 11px;
  color: var(--ink-600);
  white-space: nowrap;

  em {
    font-style: normal;
    color: var(--ink-400);
  }
}

.lv-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: auto;
}

.lv-rank {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-400);
}

.lv-actions {
  display: flex;
  gap: 4px;
}

.add-card {
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 160px;
  border-style: dashed;
  color: var(--ink-400);
  font-size: 13px;
  cursor: pointer;

  &:hover {
    color: var(--brand-600);
    border-color: var(--brand-200);
    background: var(--brand-50);
  }
}

.empty {
  grid-column: 1 / -1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 60px 0;
  color: var(--ink-400);
  font-size: 13px;
}

/* ------------------------------------------------------------ 抽屉表单 */
.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.field-tip {
  margin: 4px 0 0;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--ink-400);
}

.color-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.color-dot {
  width: 24px;
  height: 24px;
  border: 2px solid transparent;
  border-radius: 50%;
  cursor: pointer;
  padding: 0;
  transition: transform 0.15s ease;

  &:hover {
    transform: scale(1.08);
  }

  &.active {
    border-color: var(--bg-card);
    box-shadow: 0 0 0 2px var(--ink-400);
  }
}

.switch-text {
  margin-left: 10px;
  font-size: 12px;
  color: var(--ink-500);
}

.benefit-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin: 4px 0 10px;
  padding-top: 14px;
  border-top: 1px solid var(--line-soft);

  label {
    font-size: 13px;
    font-weight: 550;
    color: var(--ink-700);
  }
}

.benefit-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.benefit-row {
  display: grid;
  grid-template-columns: 1.15fr 1fr 1fr 28px;
  gap: 8px;
  align-items: center;
}

.benefit-empty {
  margin: 2px 0 0;
  font-size: 12px;
}
</style>
