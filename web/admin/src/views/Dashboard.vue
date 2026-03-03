<template>
  <div>
    <h2 class="page-title">{{ $t('dashboard.title') }}</h2>
    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <section v-if="status">
      <!-- Summary stats -->
      <div class="stat-grid">
        <router-link to="/routes" class="stat-card">
          <div class="stat-value">{{ status.routes?.length ?? 0 }}</div>
          <div class="stat-label">{{ $t('dashboard.routes') }}</div>
        </router-link>

        <router-link to="/providers" class="stat-card">
          <div class="stat-value">{{ providerStats.total }}</div>
          <div class="stat-label">{{ $t('dashboard.providers') }}</div>
          <div class="stat-sub">
            <span class="text-success">{{ providerStats.ok }} {{ $t('common.ok') }}</span>
            <template v-if="providerStats.warn"> · <span class="text-warning">{{ providerStats.warn }} {{ $t('dashboard.warn') }}</span></template>
            <template v-if="providerStats.error"> · <span class="text-error">{{ providerStats.error }} {{ $t('dashboard.error') }}</span></template>
          </div>
        </router-link>

        <div class="stat-card">
          <div class="stat-value">{{ mcpStats.connected }}<span class="stat-denom">/{{ mcpStats.total }}</span></div>
          <div class="stat-label">{{ $t('dashboard.mcpConnected') }}</div>
        </div>

        <div class="stat-card">
          <div class="stat-value">{{ fmtNum(providerStats.totalRequests) }}</div>
          <div class="stat-label">{{ $t('dashboard.totalRequests') }}</div>
          <div class="stat-sub" v-if="providerStats.totalRequests > 0">
            <span :class="successRate >= 99 ? 'text-success' : successRate >= 90 ? 'text-warning' : 'text-error'">
              {{ successRate.toFixed(1) }}% {{ $t('dashboard.success') }}
            </span>
            <template v-if="providerStats.failoverCount > 0"> · <span class="text-warning">{{ providerStats.failoverCount }} {{ $t('dashboard.failover', providerStats.failoverCount) }}</span></template>
            <template v-if="providerStats.preStreamErrors > 0"> · <span class="text-error">{{ providerStats.preStreamErrors }} {{ $t('dashboard.preStream') }}</span></template>
            <template v-if="providerStats.inStreamErrors > 0"> · <span class="text-error">{{ providerStats.inStreamErrors }} {{ $t('dashboard.inStream') }}</span></template>
          </div>
        </div>
      </div>

      <!-- Unified Metrics Section -->
      <div v-if="metricsData" class="metrics-section">
        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.usage') }}</span>
            <span class="metric-count">{{ $t('dashboard.trendWindow') }}</span>
          </div>
          <div class="trend-chart">
            <svg viewBox="0 0 280 96" preserveAspectRatio="none">
              <path class="trend-grid" d="M0 80 H280" />
              <path v-if="usageHistory.length > 1" class="trend-line usage-main" :d="sparklinePath(usageHistory, 'reqPerMin', 280, 96)" />
              <path v-if="usageHistory.length > 1" class="trend-line usage-sub" :d="sparklinePath(usageHistory, 'tokPerMin', 280, 96)" />
            </svg>
            <div v-if="usageHistory.length <= 1" class="trend-empty">{{ $t('common.noData') }}</div>
          </div>
          <div class="metric-stats">
            <div class="stat-row"><span class="trend-dot usage-main"></span>{{ $t('dashboard.requestsPerMin') }}: {{ usageLatest.reqPerMin.toFixed(1) }}</div>
            <div class="stat-row"><span class="trend-dot usage-sub"></span>{{ $t('dashboard.tokensPerMin') }}: {{ fmtNum(Math.round(usageLatest.tokPerMin)) }}</div>
            <div class="stat-row">{{ $t('routes.requests') }}: {{ fmtNum(requestStatusTotal) }}</div>
            <div class="stat-row">{{ $t('dashboard.completionShare') }}: {{ tokenStats.completionShare.toFixed(1) }}%</div>
          </div>
        </div>

        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.outputRate') }}</span>
            <span class="metric-count">{{ tokenStats.rates.length }}</span>
          </div>
          <div class="top-routes">
            <div v-for="r in tokenStats.rates.slice(0, 5)" :key="r.key" class="route-mini">
              <div class="route-mini-name">{{ r.provider }}<span class="route-mini-provider"> · {{ r.model }}</span></div>
              <div class="route-mini-bar"><div class="route-mini-fill" :style="{ width: r.percent + '%' }"></div></div>
              <div class="route-mini-count">{{ formatTPS(r.completionRate) }}</div>
            </div>
            <div v-if="tokenStats.rates.length === 0" class="empty-mini">{{ $t('common.noData') }}</div>
          </div>
        </div>

        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.errors') }}</span>
            <span class="metric-count">{{ $t('dashboard.trendWindow') }}</span>
          </div>
          <div class="trend-chart">
            <svg viewBox="0 0 280 96" preserveAspectRatio="none">
              <path class="trend-grid" d="M0 80 H280" />
              <path v-if="errorHistory.length > 1" class="trend-line error-main" :d="sparklinePath(errorHistory, 'errorRate', 280, 96)" />
              <path v-if="errorHistory.length > 1" class="trend-line error-sub" :d="sparklinePath(errorHistory, 'streamErrPer1k', 280, 96)" />
            </svg>
            <div v-if="errorHistory.length <= 1" class="trend-empty">{{ $t('common.noData') }}</div>
          </div>
          <div class="metric-stats">
            <div class="stat-row"><span class="trend-dot error-main"></span>{{ $t('dashboard.errorRate') }}: {{ errorLatest.errorRate.toFixed(2) }}%</div>
            <div class="stat-row"><span class="trend-dot error-sub"></span>{{ $t('dashboard.streamErrorsPer1k') }}: {{ errorLatest.streamErrPer1k.toFixed(2) }}</div>
            <div class="stat-row">{{ $t('dashboard.failoverPer1k') }}: {{ errorLatest.failoverPer1k.toFixed(2) }}</div>
            <div class="stat-row">{{ $t('dashboard.failureLabel') }}: {{ fmtNum(requestStatus.failure) }}</div>
          </div>
        </div>

        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.routeRisk') }}</span>
            <span class="metric-count">{{ riskyRoutesByTraffic.length }}</span>
          </div>
          <div class="top-routes">
            <div v-for="item in riskyRoutesByTraffic" :key="item.route" class="route-mini">
              <div class="route-mini-name">{{ item.route }}<span class="route-mini-provider"> · {{ fmtNum(item.total) }} {{ $t('routes.requests') }}</span></div>
              <div class="route-mini-bar"><div class="route-mini-fill bad" :style="{ width: item.failureRate + '%' }"></div></div>
              <div class="route-mini-count">{{ item.failureRate.toFixed(1) }}%</div>
            </div>
            <div v-if="riskyRoutesByTraffic.length === 0" class="empty-mini">{{ $t('common.noData') }}</div>
          </div>
        </div>

        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.ttftP95') }}</span>
            <span class="metric-count">{{ streamTTFTLeaders.length }}</span>
          </div>
          <div class="top-routes">
            <div v-for="item in streamTTFTLeaders" :key="item.key" class="route-mini">
              <div class="route-mini-name">{{ item.route }}<span class="route-mini-provider"> · {{ item.provider }} · {{ item.model }}</span></div>
              <div class="route-mini-bar"><div class="route-mini-fill warn" :style="{ width: item.percent + '%' }"></div></div>
              <div class="route-mini-count">{{ formatMs(item.value) }}</div>
            </div>
            <div v-if="streamTTFTLeaders.length === 0" class="empty-mini">{{ $t('common.noData') }}</div>
          </div>
        </div>

        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.throughputP99') }}</span>
            <span class="metric-count">{{ throughputLaggers.length }}</span>
          </div>
          <div class="top-routes">
            <div v-for="item in throughputLaggers" :key="item.key" class="route-mini">
              <div class="route-mini-name">{{ item.route }}<span class="route-mini-provider"> · {{ item.provider }} · {{ item.model }}</span></div>
              <div class="route-mini-bar"><div class="route-mini-fill bad" :style="{ width: item.percent + '%' }"></div></div>
              <div class="route-mini-count">{{ formatTPS(item.value) }}</div>
            </div>
            <div v-if="throughputLaggers.length === 0" class="empty-mini">{{ $t('common.noData') }}</div>
          </div>
        </div>
      </div>

      <!-- Alerts: only unhealthy items -->
      <div v-if="alerts.length" class="alerts-section">
        <h3 class="section-title">{{ $t('dashboard.alerts') }}</h3>
        <div class="alert-list panel">
          <div v-for="a in alerts" :key="a.key" class="alert-row-wrap">
            <div class="alert-row">
              <span :class="'alert-dot dot-' + a.level"></span>
              <router-link :to="a.link" class="resource-link">{{ a.name }}</router-link>
              <span class="alert-msg">{{ a.msg }}</span>
              <button v-if="a.reasons?.length" class="alert-toggle" @click="expandedAlerts[a.key] = !expandedAlerts[a.key]">
                {{ expandedAlerts[a.key] ? $t('dashboard.hide') : $t('dashboard.show') }} {{ a.reasons.length }} {{ $t('dashboard.reason') }}
              </button>
            </div>
            <div v-if="expandedAlerts[a.key] && a.reasons?.length" class="alert-reasons">
              <div v-for="(r, i) in a.reasons" :key="i" class="alert-reason">
                <span class="reason-time">{{ new Date(r.time).toLocaleTimeString() }}</span>
                <span class="reason-text">{{ r.reason }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- MCP Servers compact table -->
      <h3 class="section-title">{{ $t('dashboard.mcpServers') }}</h3>
      <div class="panel" style="padding:18px">
        <table class="data-table">
          <thead>
            <tr><th>{{ $t('dashboard.name') }}</th><th>{{ $t('dashboard.tools') }}</th><th>{{ $t('dashboard.status') }}</th></tr>
          </thead>
          <tbody>
            <tr
              v-for="m in status.mcp"
              :key="m.name"
              class="clickable-row"
              @click="$router.push('/mcp/' + m.name)"
            >
              <td class="resource-link">{{ m.name }}</td>
              <td>{{ m.tool_count }}</td>
              <td>
                <span :class="m.connected ? 'badge badge-ok' : 'badge badge-error'">
                  {{ m.connected ? $t('common.connected') : $t('common.disconnected') }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { createStatusStream, createMetricsStream } from '../api.js'
import { fmtNum } from '../utils.js'

const { t } = useI18n()

const status = ref(null)
const metricsData = ref(null)
const error = ref('')
const expandedAlerts = ref({}) // track expanded state by alert key
const usageHistory = ref([])
const errorHistory = ref([])
const metricsBaseline = ref(null)
let statusStop = null
let metricsStop = null

const metricSampleIntervalMs = 5000
const metricHistoryLimit = 72

const providerStats = computed(() => {
  const providers = status.value?.providers ?? []
  let ok = 0, warn = 0, err = 0, totalRequests = 0, successCount = 0
  let failoverCount = 0, preStreamErrors = 0, inStreamErrors = 0
  for (const p of providers) {
    totalRequests += p.total_requests ?? 0
    successCount += p.success_count ?? 0
    failoverCount += p.failover_count ?? 0
    preStreamErrors += p.pre_stream_errors ?? 0
    inStreamErrors += p.in_stream_errors ?? 0
    if (p.suppressed) err++
    else if (p.consecutive_failures > 0) warn++
    else ok++
  }
  return { total: providers.length, ok, warn, error: err, totalRequests, successCount, failoverCount, preStreamErrors, inStreamErrors }
})

const mcpStats = computed(() => {
  const mcp = status.value?.mcp ?? []
  return { total: mcp.length, connected: mcp.filter(m => m.connected).length }
})

const successRate = computed(() => {
  const { totalRequests, successCount } = providerStats.value
  return totalRequests > 0 ? (successCount / totalRequests) * 100 : 100
})

const alerts = computed(() => {
  const items = []
  for (const p of status.value?.providers ?? []) {
    const reasons = p.suppress_reasons ?? []
    if (p.suppressed) {
      items.push({
        key: 'p-' + p.name, level: 'error', name: p.name,
        link: '/providers/' + p.name,
        msg: t('dashboard.suppressedUntil', { time: new Date(p.suppress_until).toLocaleTimeString() }),
        reasons,
      })
    } else if (p.consecutive_failures > 0) {
      items.push({
        key: 'p-' + p.name, level: 'warn', name: p.name,
        link: '/providers/' + p.name,
        msg: t('dashboard.consecutiveFailures', { n: p.consecutive_failures }),
        reasons,
      })
    } else if (reasons.length > 0) {
      items.push({
        key: 'p-' + p.name, level: 'info', name: p.name,
        link: '/providers/' + p.name,
        msg: t('dashboard.recentErrors', { n: reasons.length }),
        reasons,
      })
    }
  }
  for (const m of status.value?.mcp ?? []) {
    if (!m.connected) {
      items.push({ key: 'm-' + m.name, level: 'error', name: m.name, link: '/mcp/' + m.name, msg: t('common.disconnected') })
    }
  }
  return items
})

// Metrics computed properties
const requestStatus = computed(() => {
  const totals = { success: 0, failure: 0 }
  if (!metricsData.value?.requests_total) return totals
  for (const item of metricsData.value.requests_total) {
    if (item.status === 'success') totals.success += item.value
    else totals.failure += item.value
  }
  return totals
})

const requestStatusTotal = computed(() => requestStatus.value.success + requestStatus.value.failure)

const tokenStats = computed(() => {
  const stats = { promptTotal: 0, completionTotal: 0, completionShare: 0, rates: [] }
  for (const item of metricsData.value?.tokens_total ?? []) {
    if (item.type === "prompt") stats.promptTotal += item.value
    if (item.type === "completion") stats.completionTotal += item.value
  }

  const total = stats.promptTotal + stats.completionTotal
  stats.completionShare = total > 0 ? (stats.completionTotal / total) * 100 : 0

  const rateMap = {}
  for (const item of metricsData.value?.token_rate ?? []) {
    if (item.type !== "completion") continue
    const key = `${item.provider || "unknown"}\0${item.model || "unknown"}`
    const value = Number(item.value || 0)
    if (!rateMap[key] || value > rateMap[key].completionRate) {
      rateMap[key] = {
        key,
        provider: item.provider || "unknown",
        model: item.model || "unknown",
        completionRate: value,
      }
    }
  }

  const rates = Object.values(rateMap).filter((item) => item.completionRate > 0)
    .sort((a, b) => b.completionRate - a.completionRate)
    .slice(0, 5)
  const maxRate = rates[0]?.completionRate || 1
  stats.rates = rates.map((item) => ({ ...item, percent: (item.completionRate / maxRate) * 100 }))
  return stats
})

const usageLatest = computed(() => usageHistory.value[usageHistory.value.length - 1] ?? { reqPerMin: 0, tokPerMin: 0 })
const errorLatest = computed(() => errorHistory.value[errorHistory.value.length - 1] ?? { errorRate: 0, streamErrPer1k: 0, failoverPer1k: 0 })

const streamTTFTLeaders = computed(() => {
  let list = (metricsData.value?.stream_ttft_p95_ms ?? [])
    .filter((item) => item.count > 0)
    .map((item) => ({
      key: `${item.route}\0${item.provider}\0${item.model}\0${item.endpoint}`,
      route: item.route,
      provider: item.provider,
      model: item.model || "unknown",
      value: Number(item.value || 0),
      count: Number(item.count || 0),
    }))

  const stable = list.filter((item) => item.count >= 5)
  if (stable.length > 0) list = stable

  list = list
    .sort((a, b) => b.value - a.value || b.count - a.count)
    .slice(0, 5)

  const max = list[0]?.value || 1
  return list.map((item) => ({ ...item, percent: (item.value / max) * 100 }))
})

const throughputLaggers = computed(() => {
  let list = (metricsData.value?.throughput_p99_tokens ?? [])
    .filter((item) => item.count > 0 && Number(item.value || 0) > 0)
    .map((item) => ({
      key: `${item.route}\0${item.provider}\0${item.model}\0${item.endpoint}`,
      route: item.route,
      provider: item.provider,
      model: item.model || "unknown",
      value: Number(item.value || 0),
      count: Number(item.count || 0),
    }))

  const stable = list.filter((item) => item.count >= 5)
  if (stable.length > 0) list = stable

  list = list
    .sort((a, b) => a.value - b.value || b.count - a.count)
    .slice(0, 5)

  const max = Math.max(...list.map((item) => item.value), 1)
  return list.map((item) => ({ ...item, percent: (item.value / max) * 100 }))
})

const riskyRoutesByTraffic = computed(() => {
  const agg = {}
  for (const item of metricsData.value?.requests_total ?? []) {
    if (!agg[item.route]) agg[item.route] = { route: item.route, success: 0, failure: 0 }
    if (item.status === "failure") agg[item.route].failure += item.value
    else agg[item.route].success += item.value
  }

  let list = Object.values(agg).map((item) => {
    const total = item.success + item.failure
    return {
      route: item.route,
      total,
      failure: item.failure,
      failureRate: total > 0 ? (item.failure / total) * 100 : 0,
    }
  }).filter((item) => item.failure > 0)

  const stable = list.filter((item) => item.total >= 20)
  if (stable.length > 0) list = stable

  return list
    .sort((a, b) => b.failure - a.failure || b.failureRate - a.failureRate)
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

function sparklinePath(points, key, width = 280, height = 96) {
  if (!Array.isArray(points) || points.length < 2) return ""
  const values = points.map((p) => Number(p[key] || 0))
  let min = Math.min(...values)
  let max = Math.max(...values)
  if (max === min) {
    max += 1
    min -= 1
  }
  const pad = 8
  return points.map((point, idx) => {
    const x = pad + idx * ((width - pad * 2) / (points.length - 1))
    const y = height - pad - ((Number(point[key] || 0) - min) / (max - min)) * (height - pad * 2)
    return `${idx === 0 ? "M" : "L"}${x.toFixed(2)},${y.toFixed(2)}`
  }).join(" ")
}

function collectCounters(metricsSnapshot) {
  let requests = 0
  let failures = 0
  for (const item of metricsSnapshot?.requests_total ?? []) {
    requests += Number(item.value || 0)
    if (item.status === "failure") failures += Number(item.value || 0)
  }

  let tokens = 0
  for (const item of metricsSnapshot?.tokens_total ?? []) {
    tokens += Number(item.value || 0)
  }

  const providers = status.value?.providers ?? []
  let failovers = 0
  let streamErrors = 0
  for (const p of providers) {
    failovers += Number(p.failover_count || 0)
    streamErrors += Number(p.pre_stream_errors || 0) + Number(p.in_stream_errors || 0)
  }

  return { ts: Date.now(), requests, failures, tokens, failovers, streamErrors }
}

function pushHistory(listRef, point) {
  listRef.value.push(point)
  if (listRef.value.length > metricHistoryLimit) {
    listRef.value.splice(0, listRef.value.length - metricHistoryLimit)
  }
}

function updateMetricTrends(metricsSnapshot) {
  const current = collectCounters(metricsSnapshot)
  if (!metricsBaseline.value) {
    metricsBaseline.value = current
    return
  }

  const elapsedMs = current.ts - metricsBaseline.value.ts
  if (elapsedMs < metricSampleIntervalMs) {
    return
  }

  const deltaRequests = current.requests - metricsBaseline.value.requests
  const deltaFailures = current.failures - metricsBaseline.value.failures
  const deltaTokens = current.tokens - metricsBaseline.value.tokens
  const deltaFailovers = current.failovers - metricsBaseline.value.failovers
  const deltaStreamErrors = current.streamErrors - metricsBaseline.value.streamErrors

  if (deltaRequests < 0 || deltaFailures < 0 || deltaTokens < 0 || deltaFailovers < 0 || deltaStreamErrors < 0) {
    metricsBaseline.value = current
    usageHistory.value = []
    errorHistory.value = []
    return
  }

  const perMinute = 60000 / elapsedMs
  const reqPerMin = deltaRequests * perMinute
  const tokPerMin = deltaTokens * perMinute

  const errorRate = deltaRequests > 0 ? (deltaFailures / deltaRequests) * 100 : 0
  const failoverPer1k = deltaRequests > 0 ? (deltaFailovers / deltaRequests) * 1000 : 0
  const streamErrPer1k = deltaRequests > 0 ? (deltaStreamErrors / deltaRequests) * 1000 : 0

  pushHistory(usageHistory, { ts: current.ts, reqPerMin, tokPerMin })
  pushHistory(errorHistory, { ts: current.ts, errorRate, failoverPer1k, streamErrPer1k })
  metricsBaseline.value = current
}

onMounted(() => {
  statusStop = createStatusStream().start(
    (data) => { status.value = data; error.value = '' },
    (e) => { error.value = e.message }
  )
  metricsStop = createMetricsStream().start(
    (data) => {
      metricsData.value = data
      updateMetricTrends(data)
    },
    () => { /* ignore errors */ }
  )
})

onUnmounted(() => {
  if (statusStop) statusStop()
  if (metricsStop) metricsStop()
})
</script>

<style scoped>
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 4px;
}
.stat-card {
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 16px 20px;
  text-decoration: none;
  color: var(--c-text);
  transition: box-shadow var(--transition);
  display: block;
}
a.stat-card:hover {
  box-shadow: var(--shadow-md);
  text-decoration: none;
}
.stat-value {
  font-size: 28px;
  font-weight: 700;
  line-height: 1.1;
  color: var(--c-text);
}
.stat-denom {
  font-size: 16px;
  font-weight: 400;
  color: var(--c-text-3);
}
.stat-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text-3);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  margin-top: 4px;
}
.stat-sub {
  font-size: 12px;
  margin-top: 4px;
}

.section-title {
  margin: 20px 0 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--c-text-2);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.alert-list {
  display: flex;
  flex-direction: column;
}
.alert-row-wrap {
  border-bottom: 1px solid var(--c-border-light);
}
.alert-row-wrap:last-child {
  border-bottom: none;
}
.alert-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  font-size: 13px;
}
.alert-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.dot-error { background: var(--c-danger); }
.dot-warn  { background: var(--c-warning); }
.dot-info  { background: var(--c-primary, #3b82f6); }
.alert-msg {
  color: var(--c-text-3);
}
.alert-toggle {
  margin-left: auto;
  background: none;
  border: 1px solid var(--c-border);
  border-radius: 4px;
  padding: 2px 8px;
  font-size: 11px;
  color: var(--c-text-3);
  cursor: pointer;
  white-space: nowrap;
}
.alert-toggle:hover {
  background: var(--c-border-light, var(--c-border));
}
.alert-reasons {
  padding: 0 16px 10px 34px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.alert-reason {
  display: flex;
  gap: 10px;
  font-size: 12px;
  color: var(--c-text-3);
  line-height: 1.4;
}
.reason-time {
  flex-shrink: 0;
  color: var(--c-text-3);
  font-family: monospace;
  font-size: 11px;
}
.reason-text {
  word-break: break-all;
}

.clickable-row {
  cursor: pointer;
  transition: background var(--transition);
}
.clickable-row:hover {
  background: #f8fafc;
}

@media (max-width: 768px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 480px) {
  .stat-grid {
    grid-template-columns: 1fr;
  }
}

/* Unified Metrics Section */
.metrics-section {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-top: 16px;
}
.metric-card {
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 16px;
}
.metric-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.metric-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text-3);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.metric-badge {
  font-size: 13px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 10px;
}
.metric-badge.good { background: #d1fae5; color: #065f46; }
.metric-badge.warn { background: #fef3c7; color: #92400e; }
.metric-badge.bad { background: #fee2e2; color: #991b1b; }
.metric-value {
  font-size: 18px;
  font-weight: 700;
  color: var(--c-text);
}
.metric-count {
  font-size: 12px;
  color: var(--c-text-3);
}
.trend-chart {
  height: 96px;
  margin-bottom: 8px;
  border: 1px solid var(--c-border-light);
  border-radius: 6px;
  background: #fbfdff;
  overflow: hidden;
  position: relative;
}
.trend-chart svg {
  width: 100%;
  height: 100%;
}
.trend-grid {
  fill: none;
  stroke: var(--c-border);
  stroke-width: 1;
}
.trend-line {
  fill: none;
  stroke-width: 2;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.trend-line.usage-main { stroke: #2563eb; }
.trend-line.usage-sub { stroke: #0ea5e9; }
.trend-line.error-main { stroke: #dc2626; }
.trend-line.error-sub { stroke: #f59e0b; }
.trend-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--c-text-3);
  font-size: 12px;
}
.trend-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  flex-shrink: 0;
}
.trend-dot.usage-main { background: #2563eb; }
.trend-dot.usage-sub { background: #0ea5e9; }
.trend-dot.error-main { background: #dc2626; }
.trend-dot.error-sub { background: #f59e0b; }

.metric-stats { font-size: 12px; color: var(--c-text-2); }
.stat-row { display: flex; align-items: center; gap: 6px; margin-top: 4px; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot.success { background: var(--c-success, #10b981); }
.dot.error { background: var(--c-danger); }

/* Top routes mini */
.top-routes { display: flex; flex-direction: column; gap: 6px; }
.route-mini {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 4px;
  font-size: 12px;
}
.route-mini-name {
  color: var(--c-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  grid-column: 1;
}
.route-mini-bar {
  grid-column: 1 / -1;
  height: 4px;
  background: var(--c-border-light, var(--c-border));
  border-radius: 2px;
  overflow: hidden;
}
.route-mini-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--c-primary, #3b82f6), var(--c-primary-light, #60a5fa));
  border-radius: 2px;
  transition: width 0.5s ease;
}
.route-mini-fill.warn {
  background: linear-gradient(90deg, #f59e0b, #fbbf24);
}
.route-mini-fill.bad {
  background: linear-gradient(90deg, #ef4444, #f87171);
}
.route-mini-count { color: var(--c-text-3); text-align: right; }
.route-mini-provider { color: var(--c-text-3); font-size: 11px; }
.empty-mini { text-align: center; padding: 16px; color: var(--c-text-3); font-size: 12px; }
.rate-mini { display: flex; justify-content: space-between; font-size: 11px; }
.rate-provider { color: var(--c-text-2); font-weight: 500; }
.rate-value { color: var(--c-text-3); font-family: monospace; }

@media (max-width: 1024px) {
  .metrics-section { grid-template-columns: repeat(2, 1fr); }
}
@media (max-width: 480px) {
  .metrics-section { grid-template-columns: 1fr; }
}
</style>
