<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Link, Refresh, Search } from '@element-plus/icons-vue'
import { newsApi, newsCategoryApi, newsSourceApi } from '@/api/news'
import type { NewsArticle, NewsCategory, NewsSource, NewsStats } from '@/api/types'
import { fmtTime } from '@/utils/format'

const { t } = useI18n()

/** 列表里用紧凑时间，完整时间放在 tooltip 中 */
function fmtShort(v?: string) {
  if (!v) return '—'
  // 走 fmtTime 而不是切字符串：后端存 UTC，直接切会把时区丢掉
  const s = fmtTime(v, false)
  return s.length >= 16 ? s.slice(5) : s
}

const rows = ref<NewsArticle[]>([])
const total = ref(0)
const loading = ref(false)
const stats = ref<NewsStats | null>(null)
const categories = ref<NewsCategory[]>([])
const sources = ref<NewsSource[]>([])

const query = reactive({
  page: 1,
  page_size: 20,
  keyword: '',
  category: '',
  sourceId: '',
  range: [] as string[],
})

async function load() {
  loading.value = true
  try {
    const res = await newsApi.list({
      page: query.page,
      page_size: query.page_size,
      keyword: query.keyword || undefined,
      category: query.category || undefined,
      sourceId: query.sourceId || undefined,
      from: query.range?.[0] || undefined,
      to: query.range?.[1] || undefined,
    })
    rows.value = res.list ?? []
    total.value = res.total ?? 0
  } finally {
    loading.value = false
  }
}

async function loadStats() {
  stats.value = await newsApi.stats()
}

/** 来源与分类是配置项，筛选下拉与列表标识都取自同一份元数据。 */
async function loadOptions() {
  const [cats, srcs] = await Promise.all([
    newsCategoryApi.list(),
    newsSourceApi.list({ page: 1, page_size: 100 }),
  ])
  categories.value = cats ?? []
  sources.value = srcs.list ?? []
}

function reload() {
  query.page = 1
  load()
}

function refreshAll() {
  load()
  loadStats()
  loadOptions()
}

function resetQuery() {
  query.keyword = ''
  query.category = ''
  query.sourceId = ''
  query.range = []
  reload()
}

onMounted(() => {
  load()
  loadStats()
  loadOptions()
})

/* ------------------------------------------------------------ 详情 */
const drawer = ref(false)
const detail = ref<NewsArticle | null>(null)
const detailLoading = ref(false)

async function openDetail(row: NewsArticle) {
  drawer.value = true
  detailLoading.value = true
  detail.value = null
  try {
    detail.value = await newsApi.detail(row.id)
  } finally {
    detailLoading.value = false
  }
}

/* ------------------------------------------------------------ 操作 */
async function remove(row: NewsArticle) {
  try {
    await ElMessageBox.confirm(
      t('删除后不可恢复。确认删除「{title}」？', { title: row.title }),
      t('删除资讯'),
      {
        type: 'warning',
        confirmButtonText: t('删除'),
        cancelButtonText: t('取消'),
      },
    )
  } catch {
    return
  }
  await newsApi.remove(row.id)
  ElMessage.success(t('已删除'))
  if (rows.value.length === 1 && query.page > 1) query.page -= 1
  await Promise.all([load(), loadStats()])
}

async function cleanup() {
  try {
    await ElMessageBox.confirm(
      t('将立即删除超过 {days} 天保留期的资讯，该操作不可恢复。', { days: stats.value?.retentionDays ?? 7 }),
      t('清理过期资讯'),
      { type: 'warning', confirmButtonText: t('开始清理'), cancelButtonText: t('取消') },
    )
  } catch {
    return
  }
  const res = await newsApi.cleanup()
  ElMessage.success(
    res.deleted > 0 ? t('已清理 {count} 条', { count: res.deleted }) : t('没有需要清理的数据'),
  )
  await Promise.all([load(), loadStats()])
}

const retentionTip = computed(() =>
  t('保留 {days} 天，过期自动清理', { days: stats.value?.retentionDays ?? 7 }),
)

const categoryMap = computed(() => new Map(categories.value.map((c) => [c.code, c])))
const sourceMap = computed(() => new Map(sources.value.map((s) => [s.code, s])))

function categoryOf(code: string) {
  return categoryMap.value.get(code)
}

function categoryName(code: string) {
  return categoryOf(code)?.name ?? code
}

function sourceOf(code: string) {
  return sourceMap.value.get(code)
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">{{ $t('资讯列表') }}</h1>
        <p class="page-desc">
          {{ $t('第三方爬虫通过开放接口批量写入，这里负责检索、审阅与清理。来源与分类为白名单配置，{tip}。', { tip: retentionTip }) }}
        </p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'content:news:cleanup'" :icon="Delete" @click="cleanup">
          {{ $t('清理过期') }}
        </el-button>
        <el-button :icon="Refresh" @click="refreshAll">{{ $t('刷新') }}</el-button>
      </div>
    </div>

    <div class="stat-row">
      <div class="stat-item">
        <span>{{ $t('窗口内总量') }}</span>
        <strong>{{ (stats?.total ?? 0).toLocaleString() }}</strong>
      </div>
      <div class="stat-item">
        <span>{{ $t('今日入库') }}</span>
        <strong>{{ (stats?.today ?? 0).toLocaleString() }}</strong>
      </div>
      <div class="stat-item">
        <span>{{ $t('近 24 小时') }}</span>
        <strong>{{ (stats?.last24h ?? 0).toLocaleString() }}</strong>
      </div>
      <div class="stat-item">
        <span>{{ $t('分类数') }}</span>
        <strong>{{ categories.length }}</strong>
      </div>
      <div class="stat-hint">
        <el-icon><Link /></el-icon>
        {{ $t('数据由爬虫经') }} <code>/api/open/v1/news/batch</code>{{ $t('推送') }}
      </div>
    </div>

    <section class="card">
      <div class="toolbar">
        <el-input
          v-model="query.keyword"
          :placeholder="$t('搜索标题、摘要或作者')"
          clearable
          style="width: 260px"
          :prefix-icon="Search"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="query.sourceId" :placeholder="$t('全部来源')" clearable style="width: 160px" @change="reload">
          <el-option v-for="s in sources" :key="s.id" :label="s.name" :value="s.id">
            <span class="opt">
              <span class="opt-icon" :style="{ background: s.color || 'var(--bg-subtle)' }">
                <img v-if="s.icon" :src="s.icon" alt="" />
                <span v-else>{{ s.name.slice(0, 1) }}</span>
              </span>
              {{ s.name }}
            </span>
          </el-option>
        </el-select>
        <el-select v-model="query.category" :placeholder="$t('全部分类')" clearable style="width: 150px" @change="reload">
          <el-option v-for="c in categories" :key="c.code" :label="c.name" :value="c.code">
            <span class="opt">
              <span class="opt-icon" :style="{ background: c.color || 'var(--bg-subtle)' }">
                <img v-if="c.icon" :src="c.icon" alt="" />
                <span v-else>{{ c.name.slice(0, 1) }}</span>
              </span>
              {{ c.name }}
            </span>
          </el-option>
        </el-select>
        <el-date-picker
          v-model="query.range"
          type="daterange"
          value-format="YYYY-MM-DD"
          :start-placeholder="$t('开始日期')"
          :end-placeholder="$t('结束日期')"
          style="width: 240px"
          @change="reload"
        />
        <el-button type="primary" @click="reload">{{ $t('查询') }}</el-button>
        <el-button text @click="resetQuery">{{ $t('重置') }}</el-button>
      </div>

      <el-table :data="rows" v-loading="loading" row-key="id" class="news-table">
        <el-table-column :label="$t('标题')" min-width="380">
          <template #default="{ row }">
            <div class="title-cell" @click="openDetail(row)">
              <div class="news-title">{{ row.title }}</div>
              <div v-if="row.summary" class="news-summary">{{ row.summary }}</div>
              <div class="tag-line">
                <span class="cat-chip">
                  <i
                    v-if="categoryOf(row.category)?.color"
                    class="cat-dot"
                    :style="{ background: categoryOf(row.category)?.color }"
                  />
                  {{ categoryName(row.category) }}
                </span>
                <span v-for="t in (row.tags ?? []).slice(0, 3)" :key="t" class="tag-chip">#{{ t }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('来源')" width="150">
          <template #default="{ row }">
            <div class="source-line">
              <span
                class="opt-icon sm"
                :style="{ background: sourceOf(row.sourceCode)?.color || 'var(--bg-subtle)' }"
              >
                <img v-if="sourceOf(row.sourceCode)?.icon" :src="sourceOf(row.sourceCode)?.icon" alt="" />
                <span v-else>{{ (row.sourceName || row.sourceCode).slice(0, 1) }}</span>
              </span>
              <span class="source-name">{{ row.sourceName || row.sourceCode }}</span>
            </div>
            <div v-if="row.author" class="dim author">{{ row.author }}</div>
          </template>
        </el-table-column>

        <el-table-column :label="$t('发布时间')" width="118">
          <template #default="{ row }">
            <el-tooltip :content="fmtTime(row.publishedAt)" placement="top">
              <span class="mono dim nowrap">{{ fmtShort(row.publishedAt) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>

        <el-table-column :label="$t('字数')" width="86" align="right">
          <template #default="{ row }">
            <span class="mono dim">{{ row.wordCount }}</span>
          </template>
        </el-table-column>

        <el-table-column :label="$t('操作')" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">{{ $t('查看') }}</el-button>
            <el-button v-perm="'content:news:delete'" link type="danger" @click="remove(row)">
              {{ $t('删除') }}
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <div class="empty">
            <p>{{ $t('窗口内还没有资讯') }}</p>
            <p class="dim">{{ $t('爬虫推送后会自动出现在这里，超过保留期会被自动清理') }}</p>
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

    <el-drawer v-model="drawer" size="720px" :title="detail?.title || $t('资讯详情')" destroy-on-close>
      <div v-loading="detailLoading" class="detail">
        <template v-if="detail">
          <div class="detail-meta">
            <span class="meta-item">{{ detail.sourceName || detail.sourceCode }}</span>
            <span v-if="detail.author" class="meta-item">{{ detail.author }}</span>
            <span class="meta-item mono">{{ fmtTime(detail.publishedAt) }}</span>
            <span class="cat-chip">
              <i
                v-if="categoryOf(detail.category)?.color"
                class="cat-dot"
                :style="{ background: categoryOf(detail.category)?.color }"
              />
              {{ categoryName(detail.category) }}
            </span>
            <span v-for="t in detail.tags ?? []" :key="t" class="tag-chip">#{{ t }}</span>
          </div>

          <blockquote v-if="detail.summary" class="detail-summary">{{ detail.summary }}</blockquote>

          <article class="detail-content">{{ detail.content || $t('（该条资讯没有正文，仅有标题与摘要）') }}</article>

          <div class="detail-foot">
            <span class="dim mono">{{ $t('{words} 字 · {views} 次阅读', { words: detail.wordCount, views: detail.viewCount }) }}</span>
            <el-link
              v-if="detail.url"
              type="primary"
              :href="detail.url"
              target="_blank"
              rel="noopener noreferrer"
            >
              {{ $t('查看原文') }}
            </el-link>
          </div>
        </template>
      </div>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.stat-row {
  display: flex;
  align-items: center;
  gap: 32px;
  flex-wrap: wrap;
  padding: 16px 20px;
  margin-bottom: 16px;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-xs);
}

.stat-item {
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
    font-variant-numeric: tabular-nums;
  }
}

.stat-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
  font-size: 12px;
  color: var(--ink-400);

  code {
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--bg-subtle);
    font-family: var(--font-mono);
    color: var(--ink-600);
  }
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 14px 16px;
  border-bottom: 1px solid var(--line-soft);
}

.title-cell {
  cursor: pointer;

  &:hover .news-title {
    color: var(--brand-600);
  }
}

.news-title {
  font-weight: 550;
  color: var(--ink-900);
  line-height: 1.5;
  transition: color 0.15s ease;
}

.news-summary {
  margin-top: 3px;
  font-size: 12.5px;
  color: var(--ink-500);
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  line-clamp: 1;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.tag-line {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
}

.cat-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 1px 7px;
  border-radius: 4px;
  background: color-mix(in srgb, var(--brand-500) 8%, transparent);
  color: var(--brand-700);
  font-size: 11.5px;
  line-height: 1.7;
}

.cat-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  display: inline-block;
}

/* 下拉选项与列表共用的来源/分类图标 */
.opt {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.opt-icon {
  width: 20px;
  height: 20px;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  flex: none;
  color: #fff;
  font-size: 11px;
  font-weight: 600;
}

.opt-icon.sm {
  width: 24px;
  height: 24px;
  border-radius: 7px;
  font-size: 12px;
}

.opt-icon img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.source-line {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tag-chip {
  font-size: 11.5px;
  color: var(--ink-400);
}

.source-name {
  color: var(--ink-700);
  font-size: 13px;
}

.author {
  margin-top: 2px;
  font-size: 12px;
}

.dim {
  color: var(--ink-400);
}

.nowrap {
  white-space: nowrap;
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

/* ------------------------------------------------------------ 详情 */
.detail {
  min-height: 200px;
}

.detail-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

.meta-item {
  font-size: 12.5px;
  color: var(--ink-500);

  & + .meta-item::before {
    content: '·';
    margin-right: 10px;
    color: var(--ink-300);
  }
}

.detail-summary {
  margin: 0 0 18px;
  padding: 12px 16px;
  border-left: 3px solid var(--brand-600);
  border-radius: 0 var(--r-md) var(--r-md) 0;
  background: var(--bg-subtle);
  font-size: 13.5px;
  line-height: 1.75;
  color: var(--ink-600);
}

.detail-content {
  font-size: 14.5px;
  line-height: 1.9;
  color: var(--ink-800);
  white-space: pre-wrap;
  word-break: break-word;
}

.detail-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 24px;
  padding-top: 14px;
  border-top: 1px solid var(--line-soft);
  font-size: 12px;
}
</style>
