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
        <div class="route-stat-title">{{ $t('routes.activeProviders') }}</div>
        <div class="route-stat-list">
          <div v-for="p in activeProviderRoutes" :key="p.key" class="stat-row-item">
            <span class="stat-name">{{ p.provider }}<span class="stat-sub-label"> · {{ p.route }}</span></span>
            <span class="stat-count">{{ fmtNum(p.count) }}</span>
          </div>
          <span v-if="activeProviderRoutes.length === 0" class="stat-empty">{{ $t('common.noData') }}</span>
        </div>
      </div>
      <div class="route-stat-card">
        <div class="route-stat-title">{{ $t('routes.topModels') }}</div>
        <div class="route-stat-list">
          <div v-for="m in topModels" :key="m.key" class="stat-row-item">
            <span class="stat-name">{{ m.model }}<span class="stat-sub-label"> · {{ m.provider }}</span></span>
            <span class="stat-count">{{ fmtNum(m.count) }}</span>
          </div>
          <span v-if="topModels.length === 0" class="stat-empty">{{ $t('common.noData') }}</span>
        </div>
      </div>
      <div class="route-stat-card">
        <div class="route-stat-title">{{ $t('routes.topEndpoints') }}</div>
        <div class="route-stat-list">
          <div v-for="e in topEndpoints" :key="e.key" class="stat-row-item">
            <span class="stat-name"><code>{{ e.endpoint }}</code><span class="stat-sub-label"> · {{ e.route }}</span></span>
            <span class="stat-count">{{ fmtNum(e.count) }}</span>
          </div>
          <span v-if="topEndpoints.length === 0" class="stat-empty">{{ $t('common.noData') }}</span>
        </div>
      </div>
    </div>

    <div v-if="status" class="panel" style="padding:18px">
      <table class="data-table">
        <thead>
          <tr><th>{{ $t('routes.prefix') }}</th><th>{{ $t('routes.providers') }}</th><th>{{ $t('routes.tools') }}</th></tr>
        </thead>
        <tbody>
          <tr v-for="r in filtered" :key="r.prefix">
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
          </tr>
          <tr v-if="filtered.length === 0">
            <td colspan="3" class="empty" style="padding:16px 0">{{ $t('routes.noMatch', { query: search }) }}</td>
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

// Active provider+route combinations by request count
const activeProviderRoutes = computed(() => {
  if (!metricsData.value?.requests_total?.length) return []
  const counts = {}
  for (const item of metricsData.value.requests_total) {
    if (!item.provider || !item.route) continue
    const key = `${item.provider}\0${item.route}`
    if (!counts[key]) counts[key] = { provider: item.provider, route: item.route, count: 0 }
    counts[key].count += item.value
  }
  return Object.values(counts)
    .sort((a, b) => b.count - a.count || `${a.provider}\0${a.route}`.localeCompare(`${b.provider}\0${b.route}`))
    .slice(0, 5)
    .map(p => ({ ...p, key: `${p.provider}\0${p.route}` }))
})

// Top models by request count (model+provider)
const topModels = computed(() => {
  if (!metricsData.value?.requests_total?.length) return []
  const counts = {}
  for (const item of metricsData.value.requests_total) {
    const model = item.model || 'unknown'
    const key = `${model}\0${item.provider}`
    if (!counts[key]) counts[key] = { model, provider: item.provider, count: 0 }
    counts[key].count += item.value
  }
  return Object.values(counts)
    .sort((a, b) => b.count - a.count || `${a.model}\0${a.provider}`.localeCompare(`${b.model}\0${b.provider}`))
    .slice(0, 5)
    .map(m => ({ ...m, key: `${m.model}\0${m.provider}` }))
})

// Top endpoints by request count (endpoint+route)
const topEndpoints = computed(() => {
  if (!metricsData.value?.requests_total?.length) return []
  const counts = {}
  for (const item of metricsData.value.requests_total) {
    if (!item.endpoint) continue
    const key = `${item.endpoint}\0${item.route}`
    if (!counts[key]) counts[key] = { endpoint: item.endpoint, route: item.route, count: 0 }
    counts[key].count += item.value
  }
  return Object.values(counts)
    .sort((a, b) => b.count - a.count || `${a.endpoint}\0${a.route}`.localeCompare(`${b.endpoint}\0${b.route}`))
    .slice(0, 5)
    .map(e => ({ ...e, key: `${e.endpoint}\0${e.route}` }))
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
  grid-template-columns: repeat(3, 1fr);
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
    grid-template-columns: 1fr;
  }
}
</style>
