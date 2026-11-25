<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { Bell, Plus, Refresh, Search } from '@element-plus/icons-vue'
import { noticeApi, type NoticePayload } from '@/api/system'
import type { NoticeRow } from '@/api/types'
import { fmtTime, fromNow } from '@/utils/format'

const { t } = useI18n()

const TYPE_META = computed<Record<string, { label: string; tone: 'primary' | 'success' | 'warning' }>>(() => ({
  notice: { label: t('通知'), tone: 'primary' },
  announcement: { label: t('公告'), tone: 'success' },
  warning: { label: t('预警'), tone: 'warning' },
}))

const rows = ref<NoticeRow[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive({
  page: 1,
  page_size: 10,
  keyword: '',
  type: '' as string,
  status: undefined as number | undefined,
})

async function load() {
  loading.value = true
  try {
    const res = await noticeApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
      type: query.type || undefined,
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
  query.type = ''
  query.status = undefined
  reload()
}

/* ---------------------------------------------------------------- 查看 */
const viewVisible = ref(false)
const viewing = ref<NoticeRow | null>(null)

async function openView(row: NoticeRow) {
  viewing.value = row
  viewVisible.value = true
  // 列表只返回摘要时补一次详情
  if (!row.content) {
    try {
      viewing.value = await noticeApi.get(row.id)
    } catch {
      /* 保留列表数据 */
    }
  }
}

/* ---------------------------------------------------------------- 表单 */
const formVisible = ref(false)
const editingId = ref('')
const isEdit = computed(() => Boolean(editingId.value))
const formRef = ref<FormInstance>()
const submitting = ref(false)

const form = reactive<NoticePayload>({
  title: '',
  content: '',
  type: 'notice',
  status: 1,
})

const rules = computed<FormRules>(() => ({
  title: [
    { required: true, message: t('请输入标题'), trigger: 'blur' },
    { max: 128, message: t('标题不超过 128 字'), trigger: 'blur' },
  ],
  content: [{ required: true, message: t('请输入正文内容'), trigger: 'blur' }],
}))

function openCreate() {
  editingId.value = ''
  Object.assign(form, { title: '', content: '', type: 'notice', status: 1 })
  formRef.value?.clearValidate()
  formVisible.value = true
}

async function openEdit(row: NoticeRow) {
  try {
    const detail = row.content ? row : await noticeApi.get(row.id)
    editingId.value = row.id
    formRef.value?.clearValidate()
    Object.assign(form, {
      title: detail.title,
      content: detail.content,
      type: detail.type,
      status: detail.status,
    })
    formVisible.value = true
  } catch {
    ElMessage.error(t('读取公告详情失败'))
  }
}

async function submitForm() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  submitting.value = true
  try {
    if (isEdit.value) {
      await noticeApi.update(editingId.value, { ...form })
      ElMessage.success(t('已保存'))
    } else {
      await noticeApi.create({ ...form })
      ElMessage.success(form.status === 1 ? t('公告已发布') : t('公告已保存为草稿'))
    }
    formVisible.value = false
    load()
  } finally {
    submitting.value = false
  }
}

async function removeNotice(row: NoticeRow) {
  try {
    await ElMessageBox.confirm(t('删除后用户将无法再看到「{title}」。确定删除吗？', { title: row.title }), t('删除公告'), {
      confirmButtonText: t('删除'),
      cancelButtonText: t('取消'),
      type: 'error',
    })
  } catch {
    return
  }
  await noticeApi.remove(row.id)
  ElMessage.success(t('已删除'))
  if (rows.value.length === 1 && query.page > 1) query.page -= 1
  load()
}

function metaOf(type: string) {
  return TYPE_META.value[type] ?? { label: type, tone: 'primary' as const }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('通知公告') }}</h1>
        <p class="page-desc">{{ $t('面向全员的消息发布，支持草稿、置顶展示与分类标签') }}</p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'system:notice:create'" type="primary" :icon="Plus" @click="openCreate">
          {{ $t('发布公告') }}
        </el-button>
      </div>
    </div>

    <div class="card">
      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            :placeholder="$t('搜索标题')"
            :prefix-icon="Search"
            clearable
            style="width: 230px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select v-model="query.type" :placeholder="$t('全部类型')" clearable style="width: 120px" @change="reload">
            <el-option :label="$t('通知')" value="notice" />
            <el-option :label="$t('公告')" value="announcement" />
            <el-option :label="$t('预警')" value="warning" />
          </el-select>
          <el-select v-model="query.status" :placeholder="$t('全部状态')" clearable style="width: 118px" @change="reload">
            <el-option :label="$t('已发布')" :value="1" />
            <el-option :label="$t('草稿')" :value="0" />
          </el-select>
          <el-button @click="reload">{{ $t('查询') }}</el-button>
          <el-button text @click="resetQuery">{{ $t('重置') }}</el-button>
        </div>
        <div class="toolbar-right">
          <el-button :icon="Refresh" circle :title="$t('刷新')" @click="load" />
        </div>
      </div>

      <div v-loading="loading" class="notice-list">
        <article v-for="row in rows" :key="row.id" class="notice-item" @click="openView(row)">
          <div class="ni-icon" :class="`is-${row.type}`">
            <el-icon :size="15"><Bell /></el-icon>
          </div>

          <div class="ni-body">
            <div class="ni-title-row">
              <h3 class="ni-title">{{ row.title }}</h3>
              <el-tag :type="metaOf(row.type).tone" size="small" effect="light">
                {{ metaOf(row.type).label }}
              </el-tag>
              <span v-if="row.status === 0" class="draft-tag">{{ $t('草稿') }}</span>
            </div>
            <p class="ni-excerpt">{{ row.content }}</p>
          </div>

          <div class="ni-side">
            <span class="ni-time" :title="fmtTime(row.publishAt || row.createdAt)">
              {{ fromNow(row.publishAt || row.createdAt) }}
            </span>
            <div class="ni-actions" @click.stop>
              <el-button v-perm="'system:notice:update'" link type="primary" @click="openEdit(row)">
                {{ $t('编辑') }}
              </el-button>
              <el-button v-perm="'system:notice:delete'" link type="danger" @click="removeNotice(row)">
                {{ $t('删除') }}
              </el-button>
            </div>
          </div>
        </article>

        <div v-if="!loading && !rows.length" class="empty">
          <el-icon :size="22"><Bell /></el-icon>
          <p>{{ $t('还没有发布过公告') }}</p>
          <el-button v-perm="'system:notice:create'" type="primary" plain size="small" @click="openCreate">
            {{ $t('发布第一条') }}
          </el-button>
        </div>
      </div>

      <div v-if="total > query.page_size" class="pager">
        <span class="total">{{ $t('共 {count} 条', { count: total }) }}</span>
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          layout="prev, pager, next"
          background
          @current-change="load"
        />
      </div>
    </div>

    <!-- 查看 -->
    <el-drawer v-model="viewVisible" :size="520" append-to-body>
      <template #header>
        <div class="view-head">
          <el-tag :type="metaOf(viewing?.type ?? 'notice').tone" size="small" effect="light">
            {{ metaOf(viewing?.type ?? 'notice').label }}
          </el-tag>
          <span v-if="viewing?.status === 0" class="draft-tag">{{ $t('草稿') }}</span>
        </div>
      </template>

      <div v-if="viewing" class="view-body">
        <h2 class="view-title">{{ viewing.title }}</h2>
        <p class="view-meta">
          {{ $t('发布于 {time}', { time: fmtTime(viewing.publishAt || viewing.createdAt) }) }}
        </p>
        <div class="view-divider" />
        <div class="view-content">{{ viewing.content }}</div>
      </div>
    </el-drawer>

    <!-- 编辑 -->
    <el-dialog
      v-model="formVisible"
      :title="isEdit ? $t('编辑公告') : $t('发布公告')"
      width="640px"
      append-to-body
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item :label="$t('标题')" prop="title">
          <el-input v-model="form.title" :placeholder="$t('一句话说明这条消息')" maxlength="128" show-word-limit />
        </el-form-item>

        <div class="form-grid">
          <el-form-item :label="$t('类型')">
            <el-radio-group v-model="form.type">
              <el-radio-button value="notice">{{ $t('通知') }}</el-radio-button>
              <el-radio-button value="announcement">{{ $t('公告') }}</el-radio-button>
              <el-radio-button value="warning">{{ $t('预警') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="$t('发布状态')">
            <el-switch v-model="form.status" :active-value="1" :inactive-value="0" :active-text="$t('立即发布')" :inactive-text="$t('存为草稿')" />
          </el-form-item>
        </div>

        <el-form-item :label="$t('正文')" prop="content">
          <el-input
            v-model="form.content"
            type="textarea"
            :rows="9"
            maxlength="5000"
            show-word-limit
            :placeholder="$t('支持换行，正文将按纯文本展示')"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="formVisible = false">{{ $t('取消') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitForm">
          {{ form.status === 1 ? $t('发布') : $t('保存草稿') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
.notice-list {
  min-height: 120px;
}

.notice-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 15px 20px;
  border-bottom: 1px solid var(--line-soft);
  cursor: pointer;
  transition: background 0.14s ease;

  &:hover {
    background: var(--ink-50);

    .ni-actions {
      opacity: 1;
    }
  }

  &:last-child {
    border-bottom: none;
  }
}

.ni-icon {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  flex-shrink: 0;
  margin-top: 1px;

  &.is-notice {
    color: var(--brand-600);
    background: var(--brand-50);
  }

  &.is-announcement {
    color: #0f766e;
    background: #e6f6f4;
  }

  &.is-warning {
    color: #b45309;
    background: #fef7e6;
  }
}

.ni-body {
  flex: 1;
  min-width: 0;
}

.ni-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ni-title {
  font-size: 13.5px;
  font-weight: 500;
  color: var(--ink-900);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.draft-tag {
  padding: 0 6px;
  border-radius: 4px;
  font-size: 10.5px;
  line-height: 17px;
  color: var(--ink-500);
  background: var(--ink-100);
  flex-shrink: 0;
}

.ni-excerpt {
  margin-top: 3px;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--ink-500);
  display: -webkit-box;
  -webkit-line-clamp: 1;
  line-clamp: 1;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.ni-side {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  flex-shrink: 0;
}

.ni-time {
  font-size: 11.5px;
  color: var(--ink-400);
  white-space: nowrap;
}

.ni-actions {
  display: flex;
  gap: 2px;
  opacity: 0;
  transition: opacity 0.14s ease;
}

.empty {
  padding: 56px 20px;
  text-align: center;
  color: var(--ink-300);

  p {
    margin: 10px 0 14px;
    font-size: 13px;
    color: var(--ink-500);
  }
}

.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-top: 1px solid var(--line-soft);
}

.total {
  font-size: 12.5px;
  color: var(--ink-500);
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}

/* ---------------------------------------------------------- 详情抽屉 */
.view-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.view-title {
  font-size: 19px;
  font-weight: 650;
  line-height: 1.45;
  color: var(--ink-900);
  letter-spacing: -0.01em;
}

.view-meta {
  margin-top: 8px;
  font-size: 12px;
  color: var(--ink-400);
}

.view-divider {
  height: 1px;
  margin: 18px 0;
  background: var(--line-soft);
}

.view-content {
  font-size: 13.5px;
  line-height: 1.9;
  color: var(--ink-700);
  white-space: pre-wrap;
  word-break: break-word;
}

@media (max-width: 720px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
