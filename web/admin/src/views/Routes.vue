<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">{{ $t('routes.title') }}</h2>
      <input
        v-model="search"
        class="form-input search-input"
        :placeholder="$t('routes.searchPlaceholder')"
      />
    </div>
    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <!-- Stats row -->
    <div v-if="metricsData" class="route-stats">
      <div class="route-stat-card">
        <div class="route-stat-title">{{ $t('routes.topTraffic') }}</div>
        <div class="route-stat-list">
          <div v-for="item in topTrafficRoutes" :key="item.route" class="stat-row-item">
            <span class="stat-name">{{ item.route }}</span>
            <span class="stat-count">{{ fmtNum(item.requests) }}</span>
          </div>
          <span v-if="topTrafficRoutes.length === 0" class="stat-empty">{{ $t('common.noData') }}</span>
        </div>
      </div>
      <div class="route-stat-card">
        <div class="route-stat-title">{{ $t('routes.topFailures') }}</div>
        <div class="route-stat-list">
          <div v-for="item in topFailureRoutes" :key="item.route" class="stat-row-item">
            <span class="stat-name">{{ item.route }}<span class="stat-sub-label"> · {{ item.successRate.toFixed(1) }}%</span></span>
            <span class="stat-count">{{ fmtNum(item.failure) }}</span>
          </div>
          <span v-if="topFailureRoutes.length === 0" class="stat-empty">{{ $t('common.noData') }}</span>
        </div>
      </div>
      <div class="route-stat-card">
        <div class="route-stat-title">{{ $t('routes.lowestSuccess') }}</div>
        <div class="route-stat-list">
          <div v-for="item in lowSuccessRoutes" :key="item.route" class="stat-row-item">
            <span class="stat-name">{{ item.route }}<span class="stat-sub-label"> · {{ fmtNum(item.failure) }} {{ $t('routes.failures') }}</span></span>
            <span class="stat-count">{{ item.successRate.toFixed(1) }}%</span>
          </div>
          <span v-if="lowSuccessRoutes.length === 0" class="stat-empty">{{ $t('common.noData') }}</span>
        </div>
      </div>
      <div class="route-stat-card">
        <div class="route-stat-title">{{ $t('routes.highestOutputRate') }}</div>
        <div class="route-stat-list">
          <div v-for="item in highOutputRateRoutes" :key="item.route" class="stat-row-item">
            <span class="stat-name">{{ item.route }}</span>
            <span class="stat-count">{{ formatTPS(item.outputRate) }}</span>
          </div>
          <span v-if="highOutputRateRoutes.length === 0" class="stat-empty">{{ $t('common.noData') }}</span>
        </div>
      </div>
    </div>

    <div v-if="status" class="panel" style="padding:18px">
      <table class="data-table">
        <thead>
          <tr>
            <th>{{ $t('routes.prefix') }}</th>
            <th>{{ $t('routes.providers') }}</th>
            <th>{{ $t('routes.tools') }}</th>
            <th>{{ $t('routes.requests') }}</th>
            <th>{{ $t('routes.failures') }}</th>
            <th>{{ $t('routes.successRate') }}</th>
            <th>{{ $t('routes.outputRate') }}</th>
            <th>{{ $t('routes.latencyP95') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in filteredRoutes" :key="r.prefix">
            <td><router-link :to="'/routes' + r.prefix" class="resource-link"><code>{{ r.prefix }}</code></router-link></td>
            <td>
              <template v-for="(p, i) in (r.providers || [])" :key="p">
                <span v-if="i > 0">, </span>
                <router-link :to="'/providers/' + encodeURIComponent(p)" class="resource-link">{{ p }}</router-link>
              </template>
            </td>
            <td>
              <span v-if="!(r.tools||[]).length" class="text-muted">-</span>
              <template v-for="(t, i) in (r.tools || [])" :key="t">
                <span v-if="i > 0">, </span>
                <router-link :to="'/mcp/' + encodeURIComponent(t)" class="resource-link">{{ t }}</router-link>
              </template>
            </td>
            <td class="metric-cell">{{ fmtNum(r.requests) }}</td>
            <td class="metric-cell">{{ fmtNum(r.failures) }}</td>
            <td class="metric-cell" :class="r.successRate >= 99 ? 'text-success' : r.successRate >= 95 ? 'text-warning' : 'text-error'">{{ r.successRate > 0 ? r.successRate.toFixed(1) + '%' : '-' }}</td>
            <td class="metric-cell">{{ formatTPS(r.outputRate) }}</td>
            <td class="metric-cell">{{ formatMs(r.latencyP95) }}</td>
          </tr>
          <tr v-if="filteredRoutes.length === 0">
            <td colspan="8" class="empty" style="padding:16px 0">{{ $t('routes.noMatch', { query: search }) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { createStatusStream, createMetricsStream } from '../api.js'
import { fmtNum } from '../utils.js'

const status = ref(null)
const metricsData = ref(null)
const error = ref('')
const search = ref('')
let statusStop = null
let metricsStop = null

const filtered = computed(() => {
  const routes = status.value?.routes ?? []
  const q = search.value.trim().toLowerCase()
  if (!q) return routes
  return routes.filter(r =>
    r.prefix.toLowerCase().includes(q) ||
    (r.providers || []).some(p => p.toLowerCase().includes(q)) ||
    (r.tools || []).some(t => t.toLowerCase().includes(q))
  )
})

function quantileFromHistogramBuckets(buckets, quantile) {
  const levels = Object.keys(buckets).map(Number).filter(Number.isFinite).sort((a, b) => a - b)
  if (!levels.length) return { value: 0, count: 0 }
  const total = buckets[levels[levels.length - 1]]
  if (!total) return { value: 0, count: 0 }
  const rank = total * quantile
  let prevLe = 0
  let prevCount = 0
  for (const le of levels) {
    const cum = buckets[le]
    if (cum >= rank) {
      const bucketCount = cum - prevCount
      if (bucketCount <= 0) return { value: le, count: total }
      const ratio = Math.max(0, Math.min(1, (rank - prevCount) / bucketCount))
      return { value: prevLe + (le - prevLe) * ratio, count: total }
    }
    prevLe = le
    prevCount = cum
  }
  return { value: levels[levels.length - 1], count: total }
}

const routeMetrics = computed(() => {
  const map = {}
  for (const row of metricsData.value?.requests_total ?? []) {
    if (!map[row.route]) {
      map[row.route] = {
        route: row.route,
        success: 0,
        failure: 0,
        requests: 0,
        latencyP95: 0,
        ttftWeighted: 0,
        ttftSamples: 0,
        throughputWeighted: 0,
        throughputSamples: 0,
        outputRate: 0,
      }
    }
    if (row.status === "failure") map[row.route].failure += row.value
    else map[row.route].success += row.value
  }

  const durationBuckets = {}
  for (const row of metricsData.value?.request_duration ?? []) {
    if (!durationBuckets[row.route]) durationBuckets[row.route] = {}
    const le = Number(row.le)
    if (!Number.isFinite(le)) continue
    durationBuckets[row.route][le] = (durationBuckets[row.route][le] ?? 0) + Number(row.value || 0)
  }

  for (const route of Object.keys(durationBuckets)) {
    if (!map[route]) {
      map[route] = {
        route,
        success: 0,
        failure: 0,
        requests: 0,
        latencyP95: 0,
        ttftWeighted: 0,
        ttftSamples: 0,
        throughputWeighted: 0,
        throughputSamples: 0,
        outputRate: 0,
      }
    }
    const q = quantileFromHistogramBuckets(durationBuckets[route], 0.95)
    map[route].latencyP95 = q.value
  }

  for (const row of metricsData.value?.stream_ttft_p95_ms ?? []) {
    if (!map[row.route]) {
      map[row.route] = {
        route: row.route,
        success: 0,
        failure: 0,
        requests: 0,
        latencyP95: 0,
        ttftWeighted: 0,
        ttftSamples: 0,
        throughputWeighted: 0,
        throughputSamples: 0,
        outputRate: 0,
      }
    }
    const count = Number(row.count || 0)
    map[row.route].ttftWeighted += Number(row.value || 0) * count
    map[row.route].ttftSamples += count
  }

  for (const row of metricsData.value?.throughput_p99_tokens ?? []) {
    if (!map[row.route]) {
      map[row.route] = {
        route: row.route,
        success: 0,
        failure: 0,
        requests: 0,
        latencyP95: 0,
        ttftWeighted: 0,
        ttftSamples: 0,
        throughputWeighted: 0,
        throughputSamples: 0,
        outputRate: 0,
      }
    }
    const count = Number(row.count || 0)
    map[row.route].throughputWeighted += Number(row.value || 0) * count
    map[row.route].throughputSamples += count
  }

  for (const row of metricsData.value?.token_rate ?? []) {
    if (row.type !== "completion") continue
    if (!row.route) continue
    if (!map[row.route]) {
      map[row.route] = {
        route: row.route,
        success: 0,
        failure: 0,
        requests: 0,
        latencyP95: 0,
        ttftWeighted: 0,
        ttftSamples: 0,
        throughputWeighted: 0,
        throughputSamples: 0,
        outputRate: 0,
      }
    }
    map[row.route].outputRate = Math.max(map[row.route].outputRate, Number(row.value || 0))
  }

  return Object.values(map).map((item) => {
    const requests = item.success + item.failure
    return {
      route: item.route,
      requests,
      successRate: requests > 0 ? (item.success / requests) * 100 : 0,
      latencyP95: item.latencyP95,
      ttftP95: item.ttftSamples > 0 ? item.ttftWeighted / item.ttftSamples : 0,
      throughputP99: item.throughputSamples > 0 ? item.throughputWeighted / item.throughputSamples : 0,
      outputRate: item.outputRate || 0,
      failure: item.failure,
      total: requests,
    }
  })
})

const routeMetricMap = computed(() => {
  const map = {}
  for (const item of routeMetrics.value) map[item.route] = item
  return map
})

const filteredRoutes = computed(() => {
  return filtered.value.map((route) => {
    const metric = routeMetricMap.value[route.prefix] || {}
    return {
      ...route,
      requests: metric.requests || 0,
      failures: metric.failure || 0,
      successRate: metric.successRate || 0,
      outputRate: metric.outputRate || 0,
      latencyP95: metric.latencyP95 || 0,
      ttftP95: metric.ttftP95 || 0,
      throughputP99: metric.throughputP99 || 0,
    }
  }).sort((a, b) => b.requests - a.requests || a.prefix.localeCompare(b.prefix))
})

const topTrafficRoutes = computed(() => {
  return routeMetrics.value
    .filter((item) => item.requests > 0)
    .sort((a, b) => b.requests - a.requests)
    .slice(0, 5)
})

const topFailureRoutes = computed(() => {
  return routeMetrics.value
    .filter((item) => item.failure > 0)
    .sort((a, b) => b.failure - a.failure || a.successRate - b.successRate)
    .slice(0, 5)
})

const lowSuccessRoutes = computed(() => {
  let list = routeMetrics.value.filter((item) => item.total >= 20 && item.failure > 0)
  if (!list.length) list = routeMetrics.value.filter((item) => item.failure > 0)
  return list.sort((a, b) => a.successRate - b.successRate || b.failure - a.failure).slice(0, 5)
})

const highOutputRateRoutes = computed(() => {
  return routeMetrics.value
    .filter((item) => item.outputRate > 0)
    .sort((a, b) => b.outputRate - a.outputRate)
    .slice(0, 5)
})

function formatMs(value) {
  if (!value || value < 0) return "-"
  return `${Math.round(value)}ms`
}

function formatTPS(value) {
  if (!value || value < 0) return "-"
  return `${value.toFixed(1)}/s`
}

onMounted(() => {
  statusStop = createStatusStream().start(
    (data) => { status.value = data; error.value = '' },
    (e) => { error.value = e.message }
  )
  metricsStop = createMetricsStream().start(
    (data) => { metricsData.value = data },
    () => { /* ignore errors */ }
  )
})

onUnmounted(() => {
  if (statusStop) statusStop()
  if (metricsStop) metricsStop()
})
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
}
.page-header .page-title {
  margin-bottom: 0;
  flex-shrink: 0;
}
.search-input {
  max-width: 280px;
  font-family: inherit;
}

.route-stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 16px;
}
.route-stat-card {
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 14px 16px;
}
.route-stat-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--c-text-3);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-bottom: 10px;
}
.route-stat-list {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.stat-row-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
}
.stat-name {
  color: var(--c-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}
.stat-sub-label {
  color: var(--c-text-3);
  font-size: 11px;
}
.stat-count {
  color: var(--c-text-3);
  font-family: monospace;
  font-size: 11px;
  flex-shrink: 0;
  margin-left: 8px;
}
.stat-empty {
  font-size: 12px;
  color: var(--c-text-3);
}
.metric-cell {
  color: var(--c-text-2);
  font-family: var(--font-mono);
  font-size: 12px;
}

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .search-input {
    max-width: 100%;
  }
  .route-stats {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 480px) {
  .route-stats {
    grid-template-columns: 1fr;
  }
}
</style>
