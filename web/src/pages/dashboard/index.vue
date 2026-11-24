<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Connection, DataLine, Refresh, Timer, User } from '@element-plus/icons-vue'
import TrendChart from '@/components/TrendChart.vue'
import { dashboardApi, type Overview } from '@/api/dashboard'
import { fmtTime, fromNow } from '@/utils/format'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const data = ref<Overview | null>(null)
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    data.value = await dashboardApi.overview()
  } finally {
    loading.value = false
  }
}

const statCards = computed(() => {
  const s = data.value?.stats
  return [
    {
      label: t('用户总数'),
      value: s?.userCount ?? 0,
      foot: t('活跃 {count} 人', { count: s?.activeUsers ?? 0 }),
      icon: User,
      tone: 'indigo',
    },
    {
      label: t('今日登录'),
      value: s?.todayLogins ?? 0,
      foot: s?.loginFailures
        ? t('失败 {count} 次', { count: s.loginFailures })
        : t('无失败记录'),
      icon: Timer,
      tone: 'emerald',
    },
    {
      label: t('在线用户'),
      value: s?.onlineCount ?? 0,
      foot: t('会话实时统计'),
      icon: Connection,
      tone: 'amber',
    },
    {
      label: t('今日操作'),
      value: s?.todayOps ?? 0,
      foot: t('文件 {fileCount} 个 · 部门 {deptCount} 个', {
        fileCount: s?.fileCount ?? 0,
        deptCount: s?.deptCount ?? 0,
      }),
      icon: DataLine,
      tone: 'violet',
    },
  ]
})

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 6) return t('夜深了')
  if (h < 12) return t('上午好')
  if (h < 14) return t('中午好')
  if (h < 18) return t('下午好')
  return t('晚上好')
})

const today = computed(() => {
  const d = new Date()
  const week = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六'][d.getDay()]
  return t('{year} 年 {month} 月 {day} 日 · {week}', {
    year: d.getFullYear(),
    month: d.getMonth() + 1,
    day: d.getDate(),
    week: t(week),
  })
})

const deptTotal = computed(() =>
  (data.value?.userDeptDist ?? []).reduce((sum, d) => sum + d.value, 0),
)

onMounted(load)
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="welcome">
      <div>
        <h1 class="page-title">{{ $t('{greeting}，{name}', { greeting, name: auth.displayName }) }}</h1>
        <p class="page-desc">{{ today }} · {{ $t('所有服务运行正常') }}</p>
      </div>
      <div class="page-actions">
        <el-button :icon="Refresh" @click="load">{{ $t('刷新数据') }}</el-button>
      </div>
    </div>

    <!-- 指标卡 -->
    <div class="stats">
      <div v-for="card in statCards" :key="card.label" class="stat">
        <div class="stat-icon" :class="`tone-${card.tone}`">
          <el-icon :size="18"><component :is="card.icon" /></el-icon>
        </div>
        <div class="stat-body">
          <span class="stat-label">{{ card.label }}</span>
          <span class="stat-value num">{{ card.value }}</span>
          <span class="stat-foot">{{ card.foot }}</span>
        </div>
      </div>
    </div>

    <!-- 趋势 + 部门分布 -->
    <div class="grid-main">
      <section class="card chart-card">
        <div class="card-head">
          <div>
            <h3>{{ $t('近 7 日活跃趋势') }}</h3>
            <p class="sub">{{ $t('登录与写操作按天统计') }}</p>
          </div>
          <div class="legend">
            <span><i class="k k-login" />{{ $t('登录') }}</span>
            <span><i class="k k-ops" />{{ $t('操作') }}</span>
          </div>
        </div>
        <div class="card-body">
          <TrendChart :data="data?.trend ?? []" />
        </div>
      </section>

      <section class="card">
        <div class="card-head">
          <h3>{{ $t('部门人员分布') }}</h3>
          <span class="sub">{{ $t('共 {count} 人', { count: deptTotal }) }}</span>
        </div>
        <div class="card-body dept-list">
          <div v-for="d in data?.userDeptDist ?? []" :key="d.name" class="dept-row">
            <div class="dept-meta">
              <span class="dept-name">{{ d.name }}</span>
              <span class="dept-val num">{{ d.value }}</span>
            </div>
            <div class="bar">
              <span :style="{ width: `${deptTotal ? (d.value / deptTotal) * 100 : 0}%` }" />
            </div>
          </div>
          <div v-if="(data?.userDeptDist ?? []).length" class="dept-total">
            {{ $t('共 {count} 人，分布在 {deptCount} 个部门', { count: deptTotal, deptCount: (data?.userDeptDist ?? []).length }) }}
          </div>
          <el-empty v-else :description="$t('暂无部门数据')" :image-size="60" />
        </div>
      </section>
    </div>

    <!-- 最近登录 + 公告 -->
    <div class="grid-sub">
      <section class="card">
        <div class="card-head">
          <h3>{{ $t('最近登录') }}</h3>
        </div>
        <ul class="login-list">
          <li v-for="r in (data?.recent ?? []).slice(0, 6)" :key="r.id">
            <div class="avatar-dot">{{ r.username.slice(0, 1).toUpperCase() }}</div>
            <div class="login-meta">
              <span class="login-name">{{ r.username }}</span>
              <span class="login-sub">{{ r.ip }} · {{ r.browser }}</span>
            </div>
            <div class="login-right">
              <span class="tag-ok" :class="{ fail: r.status !== 1 }">
                {{ r.status === 1 ? $t('成功') : $t('失败') }}
              </span>
              <time :title="fmtTime(r.createdAt)">{{ fromNow(r.createdAt) }}</time>
            </div>
          </li>
          <li v-if="!(data?.recent ?? []).length" class="empty">{{ $t('暂无记录') }}</li>
        </ul>
      </section>

      <section class="card">
        <div class="card-head">
          <h3>{{ $t('通知公告') }}</h3>
        </div>
        <ul class="notice-list">
          <li v-for="n in (data?.notices ?? []).slice(0, 4)" :key="n.id">
            <span class="notice-dot" />
            <div>
              <p class="notice-title">{{ n.title }}</p>
              <p class="notice-desc">{{ n.content }}</p>
              <time>{{ fromNow(n.publishAt || n.createdAt) }}</time>
            </div>
          </li>
          <li v-if="!(data?.notices ?? []).length" class="empty">{{ $t('暂无公告') }}</li>
        </ul>
      </section>
    </div>
  </div>
</template>

<style scoped lang="scss">
.welcome {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 22px;
}

/* ------------------------------------------------------------ 指标卡 */
.stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 16px;
}

.stat {
  display: flex;
  gap: 14px;
  padding: 18px;
  background: var(--bg-card);
  border: 1px solid var(--line);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-xs);
  transition: border-color 0.16s ease, box-shadow 0.16s ease, transform 0.16s ease;

  &:hover {
    border-color: var(--ink-200);
    box-shadow: var(--sh-sm);
    transform: translateY(-1px);
  }
}

.stat-icon {
  width: 38px;
  height: 38px;
  flex-shrink: 0;
  border-radius: 10px;
  display: grid;
  place-items: center;

  &.tone-indigo {
    background: #eef2ff;
    color: #4f46e5;
  }

  &.tone-emerald {
    background: #ecfdf3;
    color: #039855;
  }

  &.tone-amber {
    background: #fffaeb;
    color: #dc6803;
  }

  &.tone-violet {
    background: #f5f3ff;
    color: #7c3aed;
  }
}

.stat-body {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.stat-label {
  font-size: 12.5px;
  color: var(--ink-500);
}

.stat-value {
  margin-top: 2px;
  font-size: 26px;
  font-weight: 650;
  line-height: 1.15;
  letter-spacing: -0.02em;
  color: var(--ink-900);
}

.stat-foot {
  margin-top: 3px;
  font-size: 11.5px;
  color: var(--ink-400);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* -------------------------------------------------------------- 布局 */
.grid-main {
  display: grid;
  grid-template-columns: minmax(0, 1.9fr) minmax(0, 1fr);
  gap: 14px;
  margin-bottom: 14px;
}

.grid-sub {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 14px;
}

.card-head {
  .sub {
    margin-top: 3px;
    font-size: 11.5px;
    color: var(--ink-400);
    font-weight: 400;
  }
}

.legend {
  display: flex;
  gap: 14px;
  font-size: 12px;
  color: var(--ink-500);

  span {
    display: flex;
    align-items: center;
    gap: 6px;
  }
}

.k {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.k-login {
  background: var(--brand-500);
}

.k-ops {
  background: var(--ok);
}

.chart-card .card-body {
  padding: 10px 16px 8px;
}

/* ---------------------------------------------------------- 部门分布 */
.dept-list {
  display: flex;
  flex-direction: column;
  gap: 15px;
  padding: 18px 20px;
}

.dept-total {
  margin-top: 4px;
  padding-top: 14px;
  border-top: 1px solid var(--line-soft);
  font-size: 11.5px;
  color: var(--ink-400);
}

.dept-meta {
  display: flex;
  justify-content: space-between;
  margin-bottom: 6px;
  font-size: 12.5px;
}

.dept-name {
  color: var(--ink-700);
}

.dept-val {
  color: var(--ink-500);
  font-weight: 500;
}

.bar {
  height: 6px;
  border-radius: 999px;
  background: var(--ink-100);
  overflow: hidden;

  span {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: linear-gradient(90deg, #818cf8, #4f46e5);
    transition: width 0.4s ease;
  }
}

/* ---------------------------------------------------------- 最近登录 */
.login-list {
  padding: 4px 0;

  li {
    display: flex;
    align-items: center;
    gap: 11px;
    padding: 10px 20px;
    transition: background-color 0.14s ease;

    &:hover {
      background: var(--bg-subtle);
    }
  }

  .empty {
    justify-content: center;
    color: var(--ink-400);
    font-size: 13px;
    padding: 28px;
  }
}

.login-meta {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.login-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-800);
}

.login-sub {
  font-size: 11.5px;
  color: var(--ink-400);
}

.login-right {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 10px;

  time {
    font-size: 11.5px;
    color: var(--ink-400);
    white-space: nowrap;
  }
}

.tag-ok {
  padding: 1px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 500;
  background: var(--ok-bg);
  color: var(--ok-deep);

  &.fail {
    background: var(--danger-bg);
    color: var(--danger-deep);
  }
}

/* -------------------------------------------------------------- 公告 */
.notice-list {
  padding: 8px 0;

  li {
    display: flex;
    gap: 11px;
    padding: 11px 20px;
  }

  .empty {
    justify-content: center;
    color: var(--ink-400);
    font-size: 13px;
    padding: 28px;
  }
}

.notice-dot {
  width: 6px;
  height: 6px;
  margin-top: 7px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--brand-400);
}

.notice-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-800);
}

.notice-desc {
  margin-top: 3px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--ink-500);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.notice-list time {
  display: block;
  margin-top: 4px;
  font-size: 11px;
  color: var(--ink-400);
}

@media (max-width: 1200px) {
  .stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .grid-main,
  .grid-sub {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
