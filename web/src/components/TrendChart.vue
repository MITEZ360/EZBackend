<script setup lang="ts">
import { computed, ref } from 'vue'

interface Point {
  date: string
  logins: number
  ops: number
}

const props = defineProps<{ data: Point[] }>()

const W = 680
const H = 210
const PAD = { top: 16, right: 14, bottom: 28, left: 32 }

const hoverIndex = ref(-1)

const points = computed(() => props.data ?? [])

const maxValue = computed(() => {
  const values = points.value.flatMap((p) => [p.logins, p.ops])
  const max = Math.max(...values, 0)
  // 取整到 4 的倍数，保证 5 条刻度线数值均匀
  return Math.max(4, Math.ceil((max * 1.15) / 4) * 4)
})

const ticks = computed(() => {
  const step = maxValue.value / 4
  return [0, 1, 2, 3, 4].map((i) => Math.round(step * i))
})

function xAt(i: number) {
  const n = points.value.length
  if (n <= 1) return PAD.left
  return PAD.left + (i * (W - PAD.left - PAD.right)) / (n - 1)
}

function yAt(v: number) {
  const usable = H - PAD.top - PAD.bottom
  return H - PAD.bottom - (v / maxValue.value) * usable
}

/** Catmull-Rom 转三次贝塞尔，让折线柔和一些 */
function smoothPath(values: number[]) {
  if (!values.length) return ''
  const pts = values.map((v, i) => [xAt(i), yAt(v)] as const)
  if (pts.length === 1) return `M ${pts[0][0]} ${pts[0][1]}`
  let d = `M ${pts[0][0]} ${pts[0][1]}`
  for (let i = 0; i < pts.length - 1; i += 1) {
    const p0 = pts[i - 1] ?? pts[i]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[i + 2] ?? p2
    // 张力取 1/8 而非标准的 1/6，减少数据突变处的过冲
    const c1x = p1[0] + (p2[0] - p0[0]) / 8
    const c1y = p1[1] + (p2[1] - p0[1]) / 8
    const c2x = p2[0] - (p3[0] - p1[0]) / 8
    const c2y = p2[1] - (p3[1] - p1[1]) / 8
    d += ` C ${c1x.toFixed(1)} ${c1y.toFixed(1)}, ${c2x.toFixed(1)} ${c2y.toFixed(1)}, ${p2[0].toFixed(1)} ${p2[1].toFixed(1)}`
  }
  return d
}

const loginLine = computed(() => smoothPath(points.value.map((p) => p.logins)))
const opsLine = computed(() => smoothPath(points.value.map((p) => p.ops)))

function areaOf(line: string) {
  if (!line || points.value.length === 0) return ''
  const baseline = H - PAD.bottom
  const lastX = xAt(points.value.length - 1)
  return `${line} L ${lastX.toFixed(1)} ${baseline} L ${PAD.left} ${baseline} Z`
}

const loginArea = computed(() => areaOf(loginLine.value))
const opsArea = computed(() => areaOf(opsLine.value))

function onMove(e: MouseEvent) {
  const svg = e.currentTarget as SVGElement
  const rect = svg.getBoundingClientRect()
  const scale = W / rect.width
  const x = (e.clientX - rect.left) * scale
  const n = points.value.length
  if (n === 0) return
  const step = n > 1 ? (W - PAD.left - PAD.right) / (n - 1) : 1
  const idx = Math.round((x - PAD.left) / step)
  hoverIndex.value = Math.min(Math.max(idx, 0), n - 1)
}

const hoverPoint = computed(() => {
  const i = hoverIndex.value
  if (i < 0 || !points.value[i]) return null
  const p = points.value[i]
  return { i, ...p, x: xAt(i), yLogin: yAt(p.logins), yOps: yAt(p.ops) }
})

/** 短日期：10-28 */
function shortDate(d: string) {
  return d?.slice(5) ?? ''
}
</script>

<template>
  <div class="chart-wrap">
    <svg
      :viewBox="`0 0 ${W} ${H}`"
      class="chart"
      preserveAspectRatio="none"
      @mousemove="onMove"
      @mouseleave="hoverIndex = -1"
    >
      <defs>
        <linearGradient id="gradLogin" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="#6366f1" stop-opacity="0.22" />
          <stop offset="100%" stop-color="#6366f1" stop-opacity="0" />
        </linearGradient>
        <linearGradient id="gradOps" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="#12b76a" stop-opacity="0.16" />
          <stop offset="100%" stop-color="#12b76a" stop-opacity="0" />
        </linearGradient>
      </defs>

      <!-- 网格与刻度 -->
      <g class="grid">
        <template v-for="(t, i) in ticks" :key="i">
          <line :x1="PAD.left" :x2="W - PAD.right" :y1="yAt(t)" :y2="yAt(t)" />
          <text :x="PAD.left - 8" :y="yAt(t) + 3.5" text-anchor="end">{{ t }}</text>
        </template>
      </g>

      <!-- 面积 -->
      <path :d="loginArea" fill="url(#gradLogin)" />
      <path :d="opsArea" fill="url(#gradOps)" />

      <!-- 折线 -->
      <path :d="loginLine" class="line line-login" />
      <path :d="opsLine" class="line line-ops" />

      <!-- 悬浮指示 -->
      <g v-if="hoverPoint" class="hover">
        <line
          :x1="hoverPoint.x"
          :x2="hoverPoint.x"
          :y1="PAD.top"
          :y2="H - PAD.bottom"
          class="vline"
        />
        <circle :cx="hoverPoint.x" :cy="hoverPoint.yLogin" r="4" class="dot-login" />
        <circle :cx="hoverPoint.x" :cy="hoverPoint.yOps" r="4" class="dot-ops" />
      </g>

      <!-- x 轴 -->
      <g class="axis">
        <text
          v-for="(p, i) in points"
          :key="i"
          :x="xAt(i)"
          :y="H - 8"
          :text-anchor="i === points.length - 1 ? 'end' : i === 0 ? 'start' : 'middle'"
        >
          {{ shortDate(p.date) }}
        </text>
      </g>
    </svg>

    <div
      v-if="hoverPoint"
      class="tip"
      :style="{ left: `${(hoverPoint.x / W) * 100}%` }"
    >
      <div class="tip-date">{{ hoverPoint.date }}</div>
      <div class="tip-row">
        <i class="k k-login" />{{ $t('登录次数') }}<b class="num">{{ hoverPoint.logins }}</b>
      </div>
      <div class="tip-row">
        <i class="k k-ops" />{{ $t('操作次数') }}<b class="num">{{ hoverPoint.ops }}</b>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.chart-wrap {
  position: relative;
  width: 100%;
  height: 210px;
}

.chart {
  width: 100%;
  height: 100%;
  overflow: visible;
}

.grid {
  line {
    stroke: var(--line-soft);
    stroke-width: 1;
    stroke-dasharray: 3 4;
  }

  text {
    font-size: 10px;
    fill: var(--ink-400);
  }
}

.line {
  fill: none;
  stroke-width: 2;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.line-login {
  stroke: var(--brand-500);
}

.line-ops {
  stroke: var(--ok);
}

.vline {
  stroke: var(--ink-300);
  stroke-width: 1;
  stroke-dasharray: 3 3;
}

.dot-login {
  fill: #fff;
  stroke: var(--brand-500);
  stroke-width: 2;
}

.dot-ops {
  fill: #fff;
  stroke: var(--ok);
  stroke-width: 2;
}

.axis text {
  font-size: 10.5px;
  fill: var(--ink-400);
}

.tip {
  position: absolute;
  top: 4px;
  transform: translateX(-50%);
  padding: 8px 11px;
  border-radius: 8px;
  background: rgba(16, 24, 40, 0.92);
  color: #fff;
  font-size: 11.5px;
  line-height: 1.7;
  white-space: nowrap;
  pointer-events: none;
  backdrop-filter: blur(4px);
  box-shadow: var(--sh-lg);
}

.tip-date {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.6);
  margin-bottom: 2px;
}

.tip-row {
  display: flex;
  align-items: center;
  gap: 6px;

  b {
    margin-left: auto;
    padding-left: 12px;
    font-weight: 600;
  }
}

.k {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}

.k-login {
  background: #818cf8;
}

.k-ops {
  background: #32d583;
}
</style>
