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
          </div>
        </div>

        <div class="stat-card" v-if="providerStats.failoverCount > 0 || providerStats.preStreamErrors > 0 || providerStats.inStreamErrors > 0">
          <div class="stat-value text-warning">{{ fmtNum(providerStats.failoverCount + providerStats.preStreamErrors + providerStats.inStreamErrors) }}</div>
          <div class="stat-label">{{ $t('dashboard.errorEvents') }}</div>
          <div class="stat-sub">
            <template v-if="providerStats.failoverCount > 0"><span class="text-warning">{{ providerStats.failoverCount }} {{ $t('dashboard.failover', providerStats.failoverCount) }}</span></template>
            <template v-if="providerStats.preStreamErrors > 0"> · <span class="text-error">{{ providerStats.preStreamErrors }} {{ $t('dashboard.preStream') }}</span></template>
            <template v-if="providerStats.inStreamErrors > 0"> · <span class="text-error">{{ providerStats.inStreamErrors }} {{ $t('dashboard.inStream') }}</span></template>
          </div>
        </div>
      </div>

      <!-- Unified Metrics Section -->
      <div v-if="metricsData" class="metrics-section">
        <!-- Request Status -->
        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.successRate') }}</span>
            <span class="metric-badge" :class="requestSuccessPercent >= 99 ? 'good' : requestSuccessPercent >= 90 ? 'warn' : 'bad'">
              {{ requestSuccessPercent.toFixed(0) }}%
            </span>
          </div>
          <div class="donut-mini">
            <svg viewBox="0 0 36 36">
              <circle cx="18" cy="18" r="15.9" fill="none" stroke="var(--c-border)" stroke-width="3"/>
              <circle
                v-if="requestStatusTotal > 0"
                cx="18" cy="18" r="15.9" fill="none"
                stroke="var(--c-success, #10b981)"
                stroke-width="3"
                stroke-linecap="round"
                :stroke-dasharray="`${requestSuccessPercent} ${100 - requestSuccessPercent}`"
                transform="rotate(-90 18 18)"
              />
            </svg>
          </div>
          <div class="metric-stats">
            <div class="stat-row"><span class="dot success"></span>{{ $t('dashboard.successLabel') }}: {{ fmtNum(requestStatus.success) }}</div>
            <div class="stat-row"><span class="dot error"></span>{{ $t('dashboard.failureLabel') }}: {{ fmtNum(requestStatus.failure) }}</div>
          </div>
        </div>

        <!-- Latency -->
        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.latency') }}</span>
            <span class="metric-value">{{ avgLatency }}ms</span>
          </div>
          <div class="latency-bars">
            <div v-for="b in latencyBucketsCompact" :key="b.le" class="latency-bar">
              <div class="latency-bar-fill" :style="{ height: b.percent + '%' }"></div>
            </div>
          </div>
          <div class="latency-labels">
            <span v-for="b in latencyBucketsCompact" :key="b.le">{{ b.le }}ms</span>
          </div>
        </div>

        <!-- Top Routes -->
        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.topRoutes') }}</span>
            <span class="metric-count">{{ topRoutes.length }}</span>
          </div>
          <div class="top-routes">
            <div v-for="r in topRoutes" :key="r.route" class="route-mini">
              <div class="route-mini-name">{{ r.route }}</div>
              <div class="route-mini-bar"><div class="route-mini-fill" :style="{ width: r.percent + '%' }"></div></div>
              <div class="route-mini-count">{{ fmtNum(r.count) }}</div>
            </div>
            <div v-if="topRoutes.length === 0" class="empty-mini">{{ $t('common.noData') }}</div>
          </div>
        </div>

        <!-- Tokens -->
        <div class="metric-card">
          <div class="metric-header">
            <span class="metric-title">{{ $t('dashboard.tokens') }}</span>
          </div>
          <div class="token-inline">
            <div class="token-item">
              <span class="token-num">{{ fmtNum(tokenStats.promptTotal) }}</span>
              <span class="token-lbl">{{ $t('dashboard.prompt') }}</span>
            </div>
            <div class="token-divider-v"></div>
            <div class="token-item">
              <span class="token-num">{{ fmtNum(tokenStats.completionTotal) }}</span>
              <span class="token-lbl">{{ $t('dashboard.completion') }}</span>
            </div>
          </div>
          <div v-if="tokenStats.rates.length" class="token-rates-mini">
            <div v-for="r in tokenStats.rates.slice(0, 3)" :key="r.provider + r.model" class="rate-mini">
              <span class="rate-provider">{{ r.provider }}</span>
              <span class="rate-value">{{ (r.promptRate + r.completionRate).toFixed(1) }}/s</span>
            </div>
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
let statusStop = null
let metricsStop = null

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

const requestSuccessPercent = computed(() => {
  const total = requestStatusTotal.value
  return total > 0 ? (requestStatus.value.success / total) * 100 : 0
})

const latencyBuckets = computed(() => {
  if (!metricsData.value?.request_duration?.length) return []

  const buckets = {}
  for (const item of metricsData.value.request_duration) {
    const le = parseFloat(item.le)
    if (!buckets[le]) buckets[le] = 0
    buckets[le] += item.value
  }

  const sortedBuckets = Object.keys(buckets)
    .map(k => parseFloat(k))
    .sort((a, b) => a - b)

  let prevCount = 0
  const result = []
  let maxCount = 0

  for (const le of sortedBuckets) {
    const count = buckets[le] - prevCount
    if (count > maxCount) maxCount = count
    result.push({ le: Math.round(le), count })
    prevCount = buckets[le]
  }

  for (const bucket of result) {
    bucket.percent = maxCount > 0 ? (bucket.count / maxCount) * 100 : 0
  }

  return result.filter(b => b.count > 0).slice(0, 6)
})

// Compact latency buckets for inline display
const latencyBucketsCompact = computed(() => {
  const buckets = latencyBuckets.value.slice(0, 4)
  const max = buckets.reduce((m, b) => Math.max(m, b.count), 0)
  return buckets.map(b => ({ ...b, percent: max > 0 ? (b.count / max) * 100 : 0 }))
})

// Average latency estimate (using histogram median approximation)
const avgLatency = computed(() => {
  const buckets = latencyBuckets.value
  if (!buckets.length) return 0
  const total = buckets.reduce((s, b) => s + b.count, 0)
  if (total === 0) return 0
  const mid = total / 2
  let sum = 0
  for (const b of buckets) {
    sum += b.count
    if (sum >= mid) return b.le
  }
  return buckets[buckets.length - 1]?.le ?? 0
})

const topRoutes = computed(() => {
  if (!metricsData.value?.requests_total?.length) return []

  // Aggregate by route
  const routeCounts = {}
  for (const item of metricsData.value.requests_total) {
    if (!routeCounts[item.route]) routeCounts[item.route] = 0
    routeCounts[item.route] += item.value
  }

  // Convert to array and sort
  const routes = Object.entries(routeCounts)
    .map(([route, count]) => ({ route, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 5)

  // Calculate percentages
  const maxCount = routes[0]?.count || 1
  for (const r of routes) {
    r.percent = (r.count / maxCount) * 100
  }

  return routes
})

// Token metrics computed properties
const tokenStats = computed(() => {
  const stats = { promptTotal: 0, completionTotal: 0, rates: [] }
  if (!metricsData.value?.tokens_total) return stats

  for (const item of metricsData.value.tokens_total) {
    if (item.type === 'prompt') {
      stats.promptTotal += item.value
    } else if (item.type === 'completion') {
      stats.completionTotal += item.value
    }
  }

  // Group token rates by provider-model
  if (metricsData.value?.token_rate) {
    const rateMap = {}
    for (const item of metricsData.value.token_rate) {
      const key = `${item.provider}/${item.model}`
      if (!rateMap[key]) {
        rateMap[key] = { provider: item.provider, model: item.model, promptRate: 0, completionRate: 0 }
      }
      if (item.type === 'prompt') {
        rateMap[key].promptRate = item.value
      } else if (item.type === 'completion') {
        rateMap[key].completionRate = item.value
      }
    }
    stats.rates = Object.values(rateMap)
      .filter(r => r.promptRate > 0 || r.completionRate > 0)
      .sort((a, b) => (b.promptRate + b.completionRate) - (a.promptRate + a.completionRate))
      .slice(0, 5)
  }

  return stats
})

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
  grid-template-columns: repeat(4, 1fr);
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

/* Mini donut */
.donut-mini {
  width: 64px;
  height: 64px;
  margin: 0 auto 8px;
}
.donut-mini svg {
  width: 100%;
  height: 100%;
  stroke-dashoffset: 0;
  pathLength: 100;
}

/* Metric stats */
.metric-stats { font-size: 12px; color: var(--c-text-2); }
.stat-row { display: flex; align-items: center; gap: 6px; margin-top: 4px; }
.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.dot.success { background: var(--c-success, #10b981); }
.dot.error { background: var(--c-danger); }

/* Latency bars */
.latency-bars {
  display: flex;
  gap: 4px;
  align-items: flex-end;
  height: 48px;
  margin-bottom: 4px;
}
.latency-bar {
  flex: 1;
  background: var(--c-border-light, var(--c-border));
  border-radius: 2px;
  height: 100%;
  display: flex;
  align-items: flex-end;
}
.latency-bar-fill {
  width: 100%;
  background: linear-gradient(0deg, var(--c-primary, #3b82f6), var(--c-primary-light, #60a5fa));
  border-radius: 2px;
  transition: height 0.5s ease;
}
.latency-labels {
  display: flex;
  gap: 4px;
  font-size: 10px;
  color: var(--c-text-3);
}
.latency-labels span { flex: 1; text-align: center; }

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
.route-mini-count { color: var(--c-text-3); text-align: right; }
.empty-mini { text-align: center; padding: 16px; color: var(--c-text-3); font-size: 12px; }

/* Token inline */
.token-inline {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 20px;
  padding: 8px 0;
}
.token-item { display: flex; flex-direction: column; align-items: center; gap: 2px; }
.token-num { font-size: 20px; font-weight: 700; color: var(--c-text); }
.token-lbl { font-size: 11px; color: var(--c-text-3); text-transform: uppercase; }
.token-divider-v { width: 1px; height: 36px; background: var(--c-border); }
.token-rates-mini { display: flex; flex-direction: column; gap: 4px; margin-top: 8px; border-top: 1px solid var(--c-border-light, var(--c-border)); padding-top: 8px; }
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
