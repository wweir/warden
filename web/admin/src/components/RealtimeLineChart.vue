<template>
  <div class="chart-shell">
    <div v-if="hoverState" class="chart-tooltip">
      <div class="chart-tooltip-time">{{ hoverState.time }}</div>
      <div
        v-for="row in hoverState.rows"
        :key="row.name"
        class="chart-tooltip-row"
      >
        <span class="chart-tooltip-dot" :style="{ background: row.color }"></span>
        <span class="chart-tooltip-name">{{ row.name }}</span>
        <strong class="chart-tooltip-value">{{ row.value }}</strong>
      </div>
    </div>
    <div ref="chartRef" class="chart-root"></div>
    <div v-if="!hasData" class="chart-empty">{{ emptyText }}</div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'

const props = defineProps({
  points: {
    type: Array,
    default: () => [],
  },
  series: {
    type: Array,
    default: () => [],
  },
  emptyText: {
    type: String,
    default: '',
  },
  yFormatter: {
    type: Function,
    default: (value) => String(value),
  },
  group: {
    type: String,
    default: '',
  },
  timeRange: {
    type: Object,
    default: null,
  },
})

const chartRef = ref(null)
const hoverState = ref(null)
const hasData = computed(() => Array.isArray(props.points) && props.points.length > 0)

let chart = null
let resizeObserver = null

function formatAxisTime(ts) {
  return new Date(ts).toLocaleTimeString([], {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function buildData() {
  const points = Array.isArray(props.points) ? props.points : []
  const xValues = points.map((point) => Number(point.ts || 0))
  const values = props.series.map((entry) =>
    points.map((point) => Number(point[entry.key] || 0)),
  )
  return [xValues, ...values]
}

function updateHover(idx, data) {
  if (idx == null || idx < 0 || !data[0]?.length) {
    hoverState.value = null
    return
  }

  hoverState.value = {
    time: formatAxisTime(data[0][idx]),
    rows: props.series.map((entry, seriesIdx) => ({
      name: entry.name,
      color: entry.color,
      value: props.yFormatter(data[seriesIdx + 1][idx]),
    })),
  }
}

function buildOptions(data) {
  return {
    width: chartRef.value?.clientWidth || 320,
    height: chartRef.value?.clientHeight || 148,
    ms: 1,
    legend: {
      show: false,
    },
    select: {
      show: false,
    },
    cursor: {
      x: true,
      y: false,
      drag: {
        setScale: false,
        x: false,
        y: false,
      },
      points: {
        show: false,
      },
      sync: props.group
        ? {
            key: props.group,
            scales: ['x', null],
          }
        : undefined,
    },
    hooks: {
      setCursor: [
        (self) => {
          updateHover(self.cursor.idx, data)
        },
      ],
    },
    scales: {
      x: {
        time: true,
        auto: false,
        min: props.timeRange?.start ?? data[0]?.[0] ?? 0,
        max: props.timeRange?.end ?? data[0]?.[data[0].length - 1] ?? 1,
      },
      y: {
        auto: true,
      },
    },
    axes: [
      {
        stroke: '#94a3b8',
        grid: {
          show: false,
        },
        ticks: {
          show: false,
        },
        values: (self, splits) => splits.map((value) => formatAxisTime(value)),
      },
      {
        stroke: '#94a3b8',
        size: 54,
        ticks: {
          show: false,
        },
        grid: {
          show: true,
          stroke: '#e2e8f0',
          width: 1,
          dash: [4, 4],
        },
        values: (self, splits) => splits.map((value) => props.yFormatter(value)),
      },
    ],
    series: [
      {},
      ...props.series.map((entry) => ({
        label: entry.name,
        stroke: entry.color,
        width: 2,
        fill: entry.area || undefined,
        points: {
          show: false,
        },
      })),
    ],
  }
}

function destroyChart() {
  if (!chart) return
  chart.destroy()
  chart = null
}

function renderChart() {
  destroyChart()
  hoverState.value = null

  if (!chartRef.value || !hasData.value) {
    return
  }

  const data = buildData()
  chart = new uPlot(buildOptions(data), data, chartRef.value)
}

function resizeChart() {
  if (!chart || !chartRef.value) return
  chart.setSize({
    width: chartRef.value.clientWidth || 320,
    height: chartRef.value.clientHeight || 148,
  })
}

onMounted(() => {
  renderChart()

  resizeObserver = new ResizeObserver(() => {
    resizeChart()
  })
  resizeObserver.observe(chartRef.value)
})

watch(
  () => [props.points, props.series, props.emptyText, props.yFormatter, props.group, props.timeRange],
  () => {
    renderChart()
  },
  { deep: true },
)

onUnmounted(() => {
  if (resizeObserver) resizeObserver.disconnect()
  destroyChart()
  resizeObserver = null
})
</script>

<style scoped>
.chart-shell {
  position: relative;
  width: 100%;
  height: 100%;
}

.chart-root {
  width: 100%;
  height: 100%;
}

.chart-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #94a3b8;
  font-size: 12px;
  pointer-events: none;
}

.chart-tooltip {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 3;
  min-width: 128px;
  padding: 8px 10px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.86);
  color: #f8fafc;
  backdrop-filter: blur(10px);
  pointer-events: none;
}

.chart-tooltip-time {
  margin-bottom: 6px;
  color: #cbd5e1;
  font-size: 11px;
}

.chart-tooltip-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
}

.chart-tooltip-row + .chart-tooltip-row {
  margin-top: 4px;
}

.chart-tooltip-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  flex-shrink: 0;
}

.chart-tooltip-name {
  color: #e2e8f0;
}

.chart-tooltip-value {
  margin-left: auto;
}
</style>

<style>
.chart-root .uplot {
  font-family: inherit;
}

.chart-root .u-wrap {
  border: 0;
  background: transparent;
}

.chart-root .u-over {
  background: transparent;
}

.chart-root .u-axis {
  color: #94a3b8;
}

.chart-root .u-cursor-x {
  border-right: 1px solid rgba(37, 99, 235, 0.45);
}

.chart-root .u-cursor-y {
  border-top: 1px solid rgba(148, 163, 184, 0.35);
}
</style>
