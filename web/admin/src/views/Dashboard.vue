<template>
  <div>
    <div class="page-header">
      <div class="page-header-main">
        <h2 class="page-title">{{ $t('dashboard.title') }}</h2>
        <p v-if="routeCount === 0" class="page-hint">{{ $t('dashboard.emptyRoutesHint') }}</p>
      </div>
    </div>
    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <section v-if="status">
      <!-- Summary stats -->
      <div class="stat-grid">
        <router-link to="/routes" class="stat-card">
          <div class="stat-value">{{ routeCount }}</div>
          <div class="stat-label">{{ $t('dashboard.routes') }}</div>
          <div class="stat-sub">
            <span class="text-success">{{ activeRoutesCount }} {{ $t('dashboard.active') }}</span>
          </div>
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

        <div class="stat-card">
          <div class="stat-value">{{ fmtNum(tokenStats.requestTotal + tokenStats.responseTotal) }}</div>
          <div class="stat-label">{{ $t('dashboard.totalTokens') }}</div>
          <div class="stat-sub">
            <span class="text-success">{{ usageLatest.tok_per_min > 0 ? fmtNum(Math.round(usageLatest.tok_per_min)) : 0 }}/min</span>
          </div>
        </div>
      </div>

      <!-- Unified Metrics Section -->
      <div v-if="metricsData" class="metrics-section">
        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.usage') }}</span>
            <span class="metric-count">{{ realtimeUsageHistory.length }} · {{ $t('dashboard.trendWindow') }}</span>
          </div>
          <div class="trend-chart">
            <RealtimeLineChart
              :points="realtimeUsageHistory"
              :series="usageChartSeries"
              :empty-text="$t('common.noData')"
              :group="chartGroup"
              :time-range="chartTimeRange"
              :y-formatter="formatRateAxis"
            />
          </div>
          <div class="metric-stats">
            <div class="stat-row"><span class="trend-dot usage-main"></span>{{ $t('dashboard.requestsPerMin') }}: {{ usageLatest.req_per_min.toFixed(1) }}</div>
            <div class="stat-row"><span class="trend-dot usage-sub"></span>{{ $t('dashboard.tokensPerMin') }}: {{ fmtNum(Math.round(usageLatest.tok_per_min)) }}</div>
            <div class="stat-row">{{ $t('dashboard.requestTokens') }}: {{ fmtNum(tokenStats.requestTotal) }}</div>
            <div class="stat-row">{{ $t('dashboard.responseTokens') }}: {{ fmtNum(tokenStats.responseTotal) }}</div>
            <div class="stat-row">{{ $t('dashboard.cacheTokens') }}: {{ fmtNum(tokenStats.cacheTotal) }}</div>
            <div class="stat-row">{{ $t('routes.requests') }}: {{ fmtNum(requestStatusTotal) }}</div>
            <div class="stat-row">{{ $t('dashboard.completionShare') }}: {{ tokenStats.completionShare.toFixed(1) }}%</div>
          </div>
        </div>

        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.outputRate') }}</span>
            <span class="metric-count">{{ realtimeOutputHistory.length }} · {{ $t('dashboard.trendWindow') }}</span>
          </div>
          <div class="trend-chart">
            <RealtimeLineChart
              :points="outputProviderChartPoints"
              :series="outputChartSeries"
              :empty-text="$t('common.noData')"
              :group="chartGroup"
              :time-range="chartTimeRange"
              :y-formatter="formatTPSAxis"
            />
          </div>
          <div class="metric-stats">
            <div class="stat-row"><span class="trend-dot output-main"></span>{{ $t('dashboard.currentOutputRate') }}: {{ formatTPS(outputLatest.completion_tps) }}</div>
            <div class="stat-row">{{ $t('dashboard.requestTokens') }}: {{ formatTPS(outputLatest.prompt_tps) }}</div>
            <div class="stat-row">{{ $t('dashboard.responseTokens') }}: {{ formatTPS(outputLatest.completion_tps) }}</div>
            <div class="stat-row">{{ $t('dashboard.cacheTokens') }}: {{ formatTPS(outputLatest.cache_tps) }}</div>
            <div class="stat-row" v-if="peakOutputProvider">{{ $t('dashboard.peakProviderRate') }}: {{ peakOutputProvider.provider }} · {{ formatTPS(peakOutputProvider.rate) }}</div>
            <div class="stat-row" v-else>{{ $t('dashboard.peakProviderRate') }}: -</div>
          </div>
        </div>

        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.errors') }}</span>
            <span class="metric-count">{{ realtimeErrorHistory.length }} · {{ $t('dashboard.trendWindow') }}</span>
          </div>
          <div class="trend-chart">
            <RealtimeLineChart
              :points="realtimeErrorHistory"
              :series="errorChartSeries"
              :empty-text="$t('common.noData')"
              :group="chartGroup"
              :time-range="chartTimeRange"
              :y-formatter="formatRateAxis"
            />
          </div>
          <div class="metric-stats">
            <div class="stat-row"><span class="trend-dot error-main"></span>{{ $t('dashboard.errorRate') }}: {{ errorLatest.error_rate.toFixed(2) }}%</div>
            <div class="stat-row"><span class="trend-dot error-sub"></span>{{ $t('dashboard.streamErrorsPer1k') }}: {{ errorLatest.stream_err_per_1k.toFixed(2) }}</div>
            <div class="stat-row">{{ $t('dashboard.failoverPer1k') }}: {{ errorLatest.failover_per_1k.toFixed(2) }}</div>
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

	</section>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import RealtimeLineChart from '../components/RealtimeLineChart.vue'
import { createMetricsStream, createStatusStream } from '../api.js'
import { fmtNum } from '../utils.js'

const { t } = useI18n()

const status = ref(null)
const metricsData = ref(null)
const error = ref('')
const expandedAlerts = ref({})
const chartGroup = 'dashboard-time'
let statusStop = null
let metricsStop = null

const routeCount = computed(() => status.value?.routes?.length ?? 0)

const providerStats = computed(() => {
  const providers = status.value?.providers ?? []
  let ok = 0
  let warn = 0
  let err = 0
  let totalRequests = 0
  let successCount = 0
  let failoverCount = 0
  let preStreamErrors = 0
  let inStreamErrors = 0

  for (const provider of providers) {
    totalRequests += provider.total_requests ?? 0
    successCount += provider.success_count ?? 0
    failoverCount += provider.failover_count ?? 0
    preStreamErrors += provider.pre_stream_errors ?? 0
    inStreamErrors += provider.in_stream_errors ?? 0

    if (provider.suppressed) err++
    else if (provider.consecutive_failures > 0) warn++
    else ok++
  }

  return { total: providers.length, ok, warn, error: err, totalRequests, successCount, failoverCount, preStreamErrors, inStreamErrors }
})

const successRate = computed(() => {
  const { totalRequests, successCount } = providerStats.value
  return totalRequests > 0 ? (successCount / totalRequests) * 100 : 100
})

const activeRoutesCount = computed(() => {
  const routes = status.value?.routes ?? []
  return routes.filter(r => (r.total_requests ?? 0) > 0).length
})

const alerts = computed(() => {
  const items = []
  for (const provider of status.value?.providers ?? []) {
    const reasons = provider.suppress_reasons ?? []
    if (provider.suppressed) {
      items.push({
        key: 'p-' + provider.name,
        level: 'error',
        name: provider.name,
        link: '/providers/' + provider.name,
        msg: t('dashboard.suppressedUntil', { time: new Date(provider.suppress_until).toLocaleTimeString() }),
        reasons,
      })
      continue
    }
    if (provider.consecutive_failures > 0) {
      items.push({
        key: 'p-' + provider.name,
        level: 'warn',
        name: provider.name,
        link: '/providers/' + provider.name,
        msg: t('dashboard.consecutiveFailures', { n: provider.consecutive_failures }),
        reasons,
      })
      continue
    }
    if (reasons.length > 0) {
      items.push({
        key: 'p-' + provider.name,
        level: 'info',
        name: provider.name,
        link: '/providers/' + provider.name,
        msg: t('dashboard.recentErrors', { n: reasons.length }),
        reasons,
      })
    }
  }
  return items
})

const realtimeUsageHistory = computed(() => metricsData.value?.realtime?.usage ?? [])
const realtimeOutputHistory = computed(() => metricsData.value?.realtime?.output ?? [])
const realtimeErrorHistory = computed(() => metricsData.value?.realtime?.errors ?? [])
const usageLatest = computed(() => realtimeUsageHistory.value[realtimeUsageHistory.value.length - 1] ?? { req_per_min: 0, tok_per_min: 0 })
const errorLatest = computed(() => realtimeErrorHistory.value[realtimeErrorHistory.value.length - 1] ?? { error_rate: 0, stream_err_per_1k: 0, failover_per_1k: 0 })

const providerTokenRateStats = computed(() => {
  const stats = {
    prompt_tps: 0,
    completion_tps: 0,
    cache_tps: 0,
    providers: {},
  }
  for (const item of metricsData.value?.provider_token_rate ?? metricsData.value?.token_rate ?? []) {
    const value = Number(item.value || 0)
    if (item.type === 'prompt') stats.prompt_tps += value
    if (item.type === 'completion') {
      stats.completion_tps += value
      if (item.provider) {
        stats.providers[item.provider] = Number(stats.providers[item.provider] || 0) + value
      }
    }
    if (item.type === 'cache') stats.cache_tps += value
  }
  return stats
})

const outputLatest = computed(() => {
  const latest = realtimeOutputHistory.value[realtimeOutputHistory.value.length - 1]
  if (latest) {
    return {
      prompt_tps: providerTokenRateStats.value.prompt_tps,
      completion_tps: providerTokenRateStats.value.completion_tps || Number(latest.completion_tps || 0),
      cache_tps: providerTokenRateStats.value.cache_tps,
      providers: {
        ...(latest.providers ?? {}),
        ...providerTokenRateStats.value.providers,
      },
    }
  }

  return {
    prompt_tps: providerTokenRateStats.value.prompt_tps,
    completion_tps: providerTokenRateStats.value.completion_tps,
    cache_tps: providerTokenRateStats.value.cache_tps,
    providers: providerTokenRateStats.value.providers,
  }
})

const chartTimeRange = computed(() => {
  const windowSeconds = Number(metricsData.value?.realtime?.window_seconds || 0)
  const latestTs = Math.max(
    realtimeUsageHistory.value[realtimeUsageHistory.value.length - 1]?.ts || 0,
    realtimeOutputHistory.value[realtimeOutputHistory.value.length - 1]?.ts || 0,
    realtimeErrorHistory.value[realtimeErrorHistory.value.length - 1]?.ts || 0,
  )
  if (!latestTs || !windowSeconds) return null
  return {
    start: latestTs - windowSeconds * 1000,
    end: latestTs,
  }
})

const usageChartSeries = computed(() => ([
  { key: 'req_per_min', name: t('dashboard.requestsPerMin'), color: '#2563eb', area: 'rgba(37,99,235,0.18)' },
  { key: 'tok_per_min', name: t('dashboard.tokensPerMin'), color: '#0f766e', area: 'rgba(15,118,110,0.14)' },
]))

const outputProviderPalette = ['#2563eb', '#7c3aed', '#ea580c', '#0f766e', '#dc2626', '#0891b2', '#ca8a04', '#4f46e5']

const outputProviderNames = computed(() => {
  const historyNames = new Set()
  for (const point of realtimeOutputHistory.value) {
    for (const provider of Object.keys(point.providers ?? {})) {
      historyNames.add(provider)
    }
  }
  for (const provider of Object.keys(providerTokenRateStats.value.providers)) {
    historyNames.add(provider)
  }

  const ordered = []
  for (const provider of status.value?.providers ?? []) {
    if (!historyNames.has(provider.name)) continue
    ordered.push(provider.name)
    historyNames.delete(provider.name)
  }

  return ordered.concat(Array.from(historyNames).sort())
})

const outputProviderChartPoints = computed(() => realtimeOutputHistory.value.map((point) => {
  const row = { ts: point.ts }
  for (const provider of outputProviderNames.value) {
    row[provider] = Number(point.providers?.[provider] || 0)
  }
  return row
}))

const outputChartSeries = computed(() => outputProviderNames.value.map((provider, index) => ({
  key: provider,
  name: provider,
  color: outputProviderPalette[index % outputProviderPalette.length],
})))

const errorChartSeries = computed(() => ([
  { key: 'error_rate', name: t('dashboard.errorRate'), color: '#dc2626', area: 'rgba(220,38,38,0.16)' },
  { key: 'stream_err_per_1k', name: t('dashboard.streamErrorsPer1k'), color: '#d97706', area: 'rgba(217,119,6,0.14)' },
]))

const requestStatus = computed(() => {
  const totals = { success: 0, failure: 0 }
  for (const item of metricsData.value?.requests_total ?? []) {
    if (item.status === 'success') totals.success += item.value
    else totals.failure += item.value
  }
  return totals
})

const requestStatusTotal = computed(() => requestStatus.value.success + requestStatus.value.failure)

const providerTokenTotals = computed(() => (
  metricsData.value?.provider_tokens_total
  ?? metricsData.value?.tokens_total
  ?? []
))

const tokenStats = computed(() => {
  const stats = { requestTotal: 0, responseTotal: 0, cacheTotal: 0, completionShare: 0 }
  for (const item of providerTokenTotals.value) {
    if (item.type === 'prompt') stats.requestTotal += item.value
    if (item.type === 'completion') stats.responseTotal += item.value
    if (item.type === 'cache') stats.cacheTotal += item.value
  }

  const total = stats.requestTotal + stats.responseTotal
  stats.completionShare = total > 0 ? (stats.responseTotal / total) * 100 : 0
  return stats
})

const peakOutputProvider = computed(() => {
  let provider = ''
  let rate = 0

  for (const [name, value] of Object.entries(outputLatest.value.providers ?? {})) {
    const numeric = Number(value || 0)
    if (numeric <= rate) continue
    provider = name
    rate = numeric
  }

  return provider ? { provider, rate } : null
})

const streamTTFTLeaders = computed(() => {
  let list = (metricsData.value?.stream_ttft_p95_ms ?? [])
    .filter((item) => item.count > 0)
    .map((item) => ({
      key: `${item.route}\0${item.provider}\0${item.model}\0${item.endpoint}`,
      route: item.route,
      provider: item.provider,
      model: item.model || 'unknown',
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
      model: item.model || 'unknown',
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
    if (item.status === 'failure') agg[item.route].failure += item.value
    else agg[item.route].success += item.value
  }

  let list = Object.values(agg)
    .map((item) => {
      const total = item.success + item.failure
      return {
        route: item.route,
        total,
        failure: item.failure,
        failureRate: total > 0 ? (item.failure / total) * 100 : 0,
      }
    })
    .filter((item) => item.failure > 0)

  const stable = list.filter((item) => item.total >= 20)
  if (stable.length > 0) list = stable

  return list
    .sort((a, b) => b.failure - a.failure || b.failureRate - a.failureRate)
    .slice(0, 5)
})

function formatMs(value) {
  if (!value || value < 0) return '-'
  return `${Math.round(value)}ms`
}

function formatTPS(value) {
  if (!value || value < 0) return '-'
  return `${value.toFixed(1)}/s`
}

function formatTPSAxis(value) {
  return `${Number(value || 0).toFixed(0)}/s`
}

function formatRateAxis(value) {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}k`
  return value.toFixed(0)
}

onMounted(() => {
  statusStop = createStatusStream().start(
    (data) => {
      status.value = data
      error.value = ''
    },
    (streamError) => {
      error.value = streamError.message
    },
  )

  metricsStop = createMetricsStream().start(
    (data) => {
      metricsData.value = data
    },
    () => {},
  )
})

onUnmounted(() => {
  if (statusStop) statusStop()
  if (metricsStop) metricsStop()
})
</script>

<style scoped>
.page-header {
  margin-bottom: 20px;
}

.page-header-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.page-header .page-title {
  margin-bottom: 0;
}

.page-hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--c-text-3);
}

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
  height: 148px;
  margin-bottom: 8px;
  border: 1px solid var(--c-border-light);
  border-radius: 10px;
  background: linear-gradient(180deg, #ffffff 0%, #f8fbff 100%);
  overflow: hidden;
  position: relative;
}
.trend-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  flex-shrink: 0;
}
.trend-dot.usage-main { background: #2563eb; }
.trend-dot.usage-sub { background: #0f766e; }
.trend-dot.output-main { background: #7c3aed; }
.trend-dot.error-main { background: #dc2626; }
.trend-dot.error-sub { background: #d97706; }

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
