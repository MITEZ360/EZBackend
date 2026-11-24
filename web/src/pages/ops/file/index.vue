<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Download, Refresh, Search, Upload } from '@element-plus/icons-vue'
import { fileApi } from '@/api/ops'
import type { FileRow } from '@/api/types'
import { fmtBytes, fmtTime, fromNow, saveBlob } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const auth = useAuthStore()

const rows = ref<FileRow[]>([])
const total = ref(0)
const loading = ref(false)
const query = reactive({
  page: 1,
  page_size: 20,
  keyword: '',
  ext: '',
})

async function load() {
  loading.value = true
  try {
    const res = await fileApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
      ext: query.ext || undefined,
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
  query.ext = ''
  reload()
}

/* ---------------------------------------------------------------- 上传 */
const fileInput = ref<HTMLInputElement>()
const dragging = ref(false)
const uploading = ref(false)
const uploadDone = ref(0)
const uploadFailed = ref(0)

const ACCEPT = [
  '.jpg', '.jpeg', '.png', '.gif', '.webp', '.svg', '.pdf', '.doc', '.docx',
  '.xls', '.xlsx', '.ppt', '.pptx', '.txt', '.csv', '.md', '.zip', '.rar', '.7z',
  '.mp4', '.mp3',
]
const MAX_SIZE = 20 * 1024 * 1024

function pick() {
  fileInput.value?.click()
}

function onPick(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (files.length) uploadFiles(files)
}

function onDrop(e: DragEvent) {
  dragging.value = false
  const files = Array.from(e.dataTransfer?.files ?? [])
  if (files.length) uploadFiles(files)
}

async function uploadFiles(files: File[]) {
  uploading.value = true
  uploadDone.value = 0
  uploadFailed.value = 0
  for (const f of files) {
    const ext = f.name.slice(f.name.lastIndexOf('.')).toLowerCase()
    if (!ACCEPT.includes(ext)) {
      uploadFailed.value += 1
      ElMessage.warning(t('「{name}」类型不支持', { name: f.name }))
      continue
    }
    if (f.size > MAX_SIZE) {
      uploadFailed.value += 1
      ElMessage.warning(t('「{name}」超过 20MB', { name: f.name }))
      continue
    }
    try {
      await fileApi.upload(toForm(f))
      uploadDone.value += 1
    } catch {
      uploadFailed.value += 1
    }
  }
  uploading.value = false
  if (uploadDone.value) {
    ElMessage.success(
      uploadFailed.value
        ? t('成功上传 {done} 个，{failed} 个失败', { done: uploadDone.value, failed: uploadFailed.value })
        : t('已上传 {count} 个文件', { count: uploadDone.value }),
    )
    reload()
  }
}

function toForm(f: File) {
  const form = new FormData()
  form.append('file', f)
  return form
}

/* ---------------------------------------------------------------- 操作 */
async function download(row: FileRow) {
  const blob = await fileApi.download(row.id)
  saveBlob(blob, row.name)
}

async function removeFile(row: FileRow) {
  try {
    await ElMessageBox.confirm(
      t('删除后文件将从存储中移除且不可恢复。确定删除「{name}」吗？', { name: row.name }),
      t('删除文件'),
      { confirmButtonText: t('删除'), cancelButtonText: t('取消'), type: 'error' },
    )
  } catch {
    return
  }
  await fileApi.remove(row.id)
  ElMessage.success(t('已删除'))
  if (rows.value.length === 1 && query.page > 1) query.page -= 1
  load()
}

/* ---------------------------------------------------------------- 展示 */
const EXT_TONE: Record<string, string> = {
  '.pdf': 'doc',
  '.doc': 'doc',
  '.docx': 'doc',
  '.xls': 'sheet',
  '.xlsx': 'sheet',
  '.csv': 'sheet',
  '.ppt': 'slide',
  '.pptx': 'slide',
  '.jpg': 'image',
  '.jpeg': 'image',
  '.png': 'image',
  '.gif': 'image',
  '.webp': 'image',
  '.svg': 'image',
  '.zip': 'archive',
  '.rar': 'archive',
  '.7z': 'archive',
  '.mp4': 'media',
  '.mp3': 'media',
}

function toneOf(ext: string) {
  return EXT_TONE[ext.toLowerCase()] ?? 'other'
}

function extLabel(ext: string) {
  return (ext || '').replace('.', '').toUpperCase().slice(0, 4) || 'FILE'
}

const pageSize = computed(() => rows.value.reduce((s, r) => s + r.size, 0))

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('文件管理') }}</h1>
        <p class="page-desc">{{ $t('本地磁盘存储，按日期分目录；扩展名白名单与大小限制在服务端二次校验') }}</p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'ops:file:upload'" type="primary" :icon="Upload" :loading="uploading" @click="pick">
          {{ uploading ? $t('上传中 {done}/{total}', { done: uploadDone, total: uploadDone + uploadFailed }) : $t('上传文件') }}
        </el-button>
      </div>
    </div>

    <input ref="fileInput" type="file" multiple hidden :accept="ACCEPT.join(',')" @change="onPick" />

    <div class="card">
      <div
        v-if="auth.can('ops:file:upload')"
        class="drop-zone"
        :class="{ dragging }"
        @click="pick"
        @dragover.prevent="dragging = true"
        @dragleave.prevent="dragging = false"
        @drop.prevent="onDrop"
      >
        <el-icon :size="18"><Upload /></el-icon>
        <div class="dz-text">
          <span class="dz-main">{{ $t('拖拽文件到此处，或') }}<em>{{ $t('点击选择') }}</em></span>
          <span class="dz-sub">{{ $t('支持图片、文档、表格、压缩包与音视频，单个不超过 20MB，可多选') }}</span>
        </div>
      </div>

      <div class="toolbar">
        <div class="toolbar-left">
          <el-input
            v-model="query.keyword"
            :placeholder="$t('搜索文件名')"
            :prefix-icon="Search"
            clearable
            style="width: 236px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-select v-model="query.ext" :placeholder="$t('全部类型')" clearable style="width: 130px" @change="reload">
            <el-option v-for="e in ACCEPT" :key="e" :label="e" :value="e" />
          </el-select>
          <el-button @click="reload">{{ $t('查询') }}</el-button>
          <el-button text @click="resetQuery">{{ $t('重置') }}</el-button>
        </div>
        <div class="toolbar-right">
          <span class="count-hint">
            {{ $t('共') }} <b class="num">{{ total }}</b> {{ $t('个文件 · 本页') }} <b class="num">{{ fmtBytes(pageSize) }}</b>
          </span>
          <el-button :icon="Refresh" circle :title="$t('刷新')" @click="load" />
        </div>
      </div>

      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="$t('文件名')" min-width="300">
          <template #default="{ row }">
            <div class="file-cell">
              <span class="ext-badge" :class="`t-${toneOf(row.ext)}`">{{ extLabel(row.ext) }}</span>
              <div class="f-lines">
                <span class="f-name" :title="row.name">{{ row.name }}</span>
                <span class="f-hash mono">SHA256 {{ (row.hash || '').slice(0, 12) || '—' }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('类型')" width="120">
          <template #default="{ row }">
            <span class="muted fs-12">{{ row.mime || row.ext || '—' }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('大小')" width="100" align="right">
          <template #default="{ row }">
            <span class="num size">{{ fmtBytes(row.size) }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('上传时间')" width="160">
          <template #default="{ row }">
            <div class="cell-time" :title="fmtTime(row.createdAt)">{{ fromNow(row.createdAt) }}</div>
            <div class="cell-sub num">{{ fmtTime(row.createdAt, false) }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('存储')" width="90">
          <template #default="{ row }">
            <el-tag size="small" type="info" effect="plain">{{ row.storage || 'local' }}</el-tag>
          </template>
        </el-table-column>

        <el-table-column label="" width="140" align="right" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" :icon="Download" @click="download(row)">{{ $t('下载') }}</el-button>
            <el-button v-perm="'ops:file:delete'" link type="danger" @click="removeFile(row)">{{ $t('删除') }}</el-button>
          </template>
        </el-table-column>

        <template #empty>
          <div class="table-empty">
            <p>{{ query.keyword || query.ext ? $t('没有匹配的文件') : $t('还没有上传过文件') }}</p>
            <el-button v-if="!query.keyword && !query.ext && auth.can('ops:file:upload')" size="small" type="primary" plain @click="pick">
              {{ $t('上传第一个文件') }}
            </el-button>
          </div>
        </template>
      </el-table>

      <div class="pager">
        <span class="total">{{ $t('共 {count} 个文件', { count: total }) }}</span>
        <el-pagination
          v-model:current-page="query.page"
          v-model:page-size="query.page_size"
          :total="total"
          :page-sizes="[20, 50, 100]"
          layout="sizes, prev, pager, next"
          background
          @current-change="load"
          @size-change="reload"
        />
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.drop-zone {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 16px 16px 0;
  padding: 14px 18px;
  border: 1px dashed var(--ink-200);
  border-radius: var(--r-md);
  color: var(--ink-400);
  cursor: pointer;
  transition: all 0.15s ease;

  &:hover,
  &.dragging {
    border-color: var(--brand-400);
    background: var(--brand-50);
    color: var(--brand-600);
  }
}

.dz-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.dz-main {
  font-size: 13px;
  color: var(--ink-800);

  em {
    font-style: normal;
    font-weight: 500;
    color: var(--brand-600);
  }
}

.dz-sub {
  font-size: 11.5px;
  color: var(--ink-400);
}

.count-hint {
  font-size: 12px;
  color: var(--ink-400);

  b {
    color: var(--ink-700);
    font-weight: 600;
  }
}

.file-cell {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.ext-badge {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 7px;
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: 0.02em;
  flex-shrink: 0;

  &.t-doc {
    color: var(--danger-deep);
    background: var(--danger-bg);
  }

  &.t-sheet {
    color: #0f766e;
    background: #e6f6f4;
  }

  &.t-slide {
    color: #b45309;
    background: #fef7e6;
  }

  &.t-image {
    color: var(--brand-700);
    background: var(--brand-50);
  }

  &.t-archive {
    color: #6d28d9;
    background: #f3edfd;
  }

  &.t-media {
    color: #0369a1;
    background: #e7f3fb;
  }

  &.t-other {
    color: var(--ink-600);
    background: var(--ink-100);
  }
}

.f-lines {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

.f-name {
  font-size: 13px;
  color: var(--ink-900);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.f-hash {
  font-size: 10.5px;
  color: var(--ink-300);
}

.size {
  font-size: 12.5px;
  color: var(--ink-700);
}

.cell-time {
  font-size: 12.5px;
  color: var(--ink-700);
}

.cell-sub {
  font-size: 11px;
  color: var(--ink-400);
}

.table-empty {
  padding: 44px 0;
  color: var(--ink-400);

  p {
    font-size: 13px;
    margin-bottom: 10px;
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
</style>
