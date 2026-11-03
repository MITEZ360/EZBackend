<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Link, Refresh, Search } from '@element-plus/icons-vue'
import { newsApi } from '@/api/news'
import type { NewsArticle, NewsStats } from '@/api/types'
import { fmtTime } from '@/utils/format'

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
const categories = ref<string[]>([])

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
  categories.value = (stats.value?.categories ?? []).map((c) => c.name)
}

function reload() {
  query.page = 1
  load()
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
  await ElMessageBox.confirm(`删除后不可恢复。确认删除「${row.title}」？`, '删除资讯', {
    type: 'warning',
    confirmButtonText: '删除',
    cancelButtonText: '取消',
  })
  await newsApi.remove(row.id)
  ElMessage.success('已删除')
  await Promise.all([load(), loadStats()])
}

async function cleanup() {
  await ElMessageBox.confirm(
    `将立即删除超过 ${stats.value?.retentionDays ?? 7} 天保留期的资讯，该操作不可恢复。`,
    '清理过期资讯',
    { type: 'warning', confirmButtonText: '开始清理', cancelButtonText: '取消' },
  )
  const res = await newsApi.cleanup()
  ElMessage.success(res.deleted > 0 ? `已清理 ${res.deleted} 条` : '没有需要清理的数据')
  await Promise.all([load(), loadStats()])
}

const retentionTip = computed(() => `保留 ${stats.value?.retentionDays ?? 7} 天，过期自动清理`)

function categoryLabel(name: string) {
  const found = stats.value?.categories.find((c) => c.name === name)
  return found ? `${name}` : name
}
</script>

<template>
  <div class="page">
    <div class="page-head">
      <div>
        <h1 class="page-title">资讯列表</h1>
        <p class="page-desc">
          第三方爬虫通过开放接口批量写入，这里负责检索、审阅与清理。{{ retentionTip }}。
        </p>
      </div>
      <div class="page-actions">
        <el-button v-perm="'content:news:cleanup'" :icon="Delete" @click="cleanup">
          清理过期
        </el-button>
        <el-button :icon="Refresh" @click="(() => { load(); loadStats() })()">刷新</el-button>
      </div>
    </div>

    <div class="stat-row">
      <div class="stat-item">
        <span>窗口内总量</span>
        <strong>{{ (stats?.total ?? 0).toLocaleString() }}</strong>
      </div>
      <div class="stat-item">
        <span>今日入库</span>
        <strong>{{ (stats?.today ?? 0).toLocaleString() }}</strong>
      </div>
      <div class="stat-item">
        <span>近 24 小时</span>
        <strong>{{ (stats?.last24h ?? 0).toLocaleString() }}</strong>
      </div>
      <div class="stat-item">
        <span>分类数</span>
        <strong>{{ stats?.categories.length ?? 0 }}</strong>
      </div>
      <div class="stat-hint">
        <el-icon><Link /></el-icon>
        数据由爬虫经 <code>/api/open/v1/news/batch</code> 推送
      </div>
    </div>

    <section class="card">
      <div class="toolbar">
        <el-input
          v-model="query.keyword"
          placeholder="搜索标题、摘要或作者"
          clearable
          style="width: 260px"
          :prefix-icon="Search"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="query.category" placeholder="全部分类" clearable style="width: 140px" @change="reload">
          <el-option v-for="c in categories" :key="c" :label="categoryLabel(c)" :value="c" />
        </el-select>
        <el-date-picker
          v-model="query.range"
          type="daterange"
          value-format="YYYY-MM-DD"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          style="width: 240px"
          @change="reload"
        />
        <el-button type="primary" @click="reload">查询</el-button>
        <el-button text @click="resetQuery">重置</el-button>
      </div>

      <el-table :data="rows" v-loading="loading" row-key="id" class="news-table">
        <el-table-column label="标题" min-width="380">
          <template #default="{ row }">
            <div class="title-cell" @click="openDetail(row)">
              <div class="news-title">{{ row.title }}</div>
              <div v-if="row.summary" class="news-summary">{{ row.summary }}</div>
              <div class="tag-line">
                <span class="cat-chip">{{ row.category }}</span>
                <span v-for="t in (row.tags ?? []).slice(0, 3)" :key="t" class="tag-chip">#{{ t }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="来源" width="140">
          <template #default="{ row }">
            <div class="source-name">{{ row.sourceName || row.sourceCode }}</div>
            <div v-if="row.author" class="dim author">{{ row.author }}</div>
          </template>
        </el-table-column>

        <el-table-column label="发布时间" width="118">
          <template #default="{ row }">
            <el-tooltip :content="fmtTime(row.publishedAt)" placement="top">
              <span class="mono dim nowrap">{{ fmtShort(row.publishedAt) }}</span>
            </el-tooltip>
          </template>
        </el-table-column>

        <el-table-column label="字数" width="86" align="right">
          <template #default="{ row }">
            <span class="mono dim">{{ row.wordCount }}</span>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">查看</el-button>
            <el-button v-perm="'content:news:delete'" link type="danger" @click="remove(row)">
              删除
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <div class="empty">
            <p>窗口内还没有资讯</p>
            <p class="dim">爬虫推送后会自动出现在这里，超过保留期会被自动清理</p>
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

    <el-drawer v-model="drawer" size="720px" :title="detail?.title || '资讯详情'" destroy-on-close>
      <div v-loading="detailLoading" class="detail">
        <template v-if="detail">
          <div class="detail-meta">
            <span class="meta-item">{{ detail.sourceName || detail.sourceCode }}</span>
            <span v-if="detail.author" class="meta-item">{{ detail.author }}</span>
            <span class="meta-item mono">{{ fmtTime(detail.publishedAt) }}</span>
            <span class="cat-chip">{{ detail.category }}</span>
            <span v-for="t in detail.tags ?? []" :key="t" class="tag-chip">#{{ t }}</span>
          </div>

          <blockquote v-if="detail.summary" class="detail-summary">{{ detail.summary }}</blockquote>

          <article class="detail-content">{{ detail.content || '（该条资讯没有正文，仅有标题与摘要）' }}</article>

          <div class="detail-foot">
            <span class="dim mono">{{ detail.wordCount }} 字 · {{ detail.viewCount }} 次阅读</span>
            <el-link
              v-if="detail.url"
              type="primary"
              :href="detail.url"
              target="_blank"
              rel="noopener noreferrer"
            >
              查看原文
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
  padding: 16px 20px;
  border-bottom: 1px solid var(--line-soft);
}

.title-cell {
  cursor: pointer;

  &:hover .news-title {
    color: var(--brand);
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
  padding: 1px 7px;
  border-radius: 4px;
  background: color-mix(in srgb, var(--brand) 8%, transparent);
  color: var(--brand-700);
  font-size: 11.5px;
  line-height: 1.7;
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
  border-left: 3px solid var(--brand);
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
