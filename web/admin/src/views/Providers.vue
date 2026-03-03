<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">{{ $t('providers.title') }}</h2>
      <input
        v-model="search"
        class="form-input search-input"
        :placeholder="$t('providers.searchPlaceholder')"
      />
    </div>
    <div v-if="error" class="msg msg-error">{{ error }}</div>
    <div v-if="status">
      <div class="card-grid">
        <StatusCard
          v-for="p in filtered"
          :key="p.name"
          :name="p.name"
          :status="providerStatus(p)"
          class="clickable-card"
          @click="$router.push('/providers/' + p.name)"
        >
          <div>{{ $t('providers.models') }}: {{ p.model_count }}</div>
          <div>{{ $t('providers.requests') }}: {{ fmtNum(p.total_requests) }} ({{ fmtNum(p.success_count) }} ok / {{ fmtNum(p.failure_count) }} fail)</div>
          <div v-if="p.total_requests > 0">
            <span :class="successRate(p) >= 99 ? 'text-success' : successRate(p) >= 90 ? 'text-warning' : 'text-error'">
              {{ successRate(p).toFixed(1) }}%
            </span>
            {{ $t('providers.successRate') }} · {{ p.avg_latency_ms.toFixed(0) }}{{ $t('providers.msAvg') }}
          </div>
          <div v-if="p.failover_count > 0 || p.pre_stream_errors > 0 || p.in_stream_errors > 0" class="error-stats">
            <span v-if="p.failover_count > 0" class="stat-badge warn">{{ $t('providers.failoverCount', { n: p.failover_count }) }}</span>
            <span v-if="p.pre_stream_errors > 0" class="stat-badge error">{{ $t('providers.preStreamErrors', { n: p.pre_stream_errors }) }}</span>
            <span v-if="p.in_stream_errors > 0" class="stat-badge error">{{ $t('providers.inStreamErrors', { n: p.in_stream_errors }) }}</span>
          </div>
          <div v-if="p.manual_suppressed" class="text-error">{{ $t('providers.manuallySuppressed') }}</div>
          <div v-else-if="p.suppressed" class="text-error">{{ $t('common.suppressed') }} {{ $t('providers.suppressedUntil', { time: formatTime(p.suppress_until) }) }}</div>
          <div class="card-actions">
            <button class="btn btn-secondary btn-sm" @click.stop="ping(p.name)">
              {{ pinging[p.name] ? '...' : $t('providers.ping') }}
            </button>
            <span v-if="pingResults[p.name]" :class="pingResults[p.name].status === 'ok' ? 'text-success' : 'text-error'" style="font-size:12px">
              {{ pingResults[p.name].status === 'ok'
                ? pingResults[p.name].latency_ms + 'ms'
                : pingResults[p.name].error }}
            </span>
            <button
              v-if="!p.manual_suppressed"
              class="btn btn-error btn-sm"
              @click.stop="suppressProvider(p.name)"
            >{{ $t('providers.suppress') }}</button>
            <button
              v-else
              class="btn btn-success btn-sm"
              @click.stop="unsuppressProvider(p.name)"
            >{{ $t('providers.unsuppress') }}</button>
          </div>
        </StatusCard>
      </div>
      <p v-if="filtered.length === 0" class="empty" style="margin-top:16px">{{ $t('providers.noMatch', { query: search }) }}</p>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, reactive, onMounted, onUnmounted } from 'vue'
import StatusCard from '../components/StatusCard.vue'
import { createStatusStream, healthCheck, setProviderSuppress } from '../api.js'
import { fmtNum } from '../utils.js'

const status = ref(null)
const error = ref('')
const search = ref('')
const pinging = reactive({})
const pingResults = reactive({})
let statusStop = null

function providerStatus(p) {
  if (p.manual_suppressed || p.suppressed) return 'error'
  if (p.consecutive_failures > 0) return 'warn'
  return 'ok'
}

function formatTime(t) {
  if (!t) return ''
  return new Date(t).toLocaleTimeString()
}

function successRate(p) {
  if (p.total_requests === 0) return 100
  return (p.success_count / p.total_requests) * 100
}

const filtered = computed(() => {
  const providers = status.value?.providers ?? []
  const q = search.value.trim().toLowerCase()
  if (!q) return providers
  return providers.filter(p =>
    p.name.toLowerCase().includes(q) ||
    providerStatus(p).includes(q)
  )
})

async function ping(name) {
  pinging[name] = true
  delete pingResults[name]
  try {
    const result = await healthCheck(name)
    pingResults[name] = result
  } catch (e) {
    pingResults[name] = { status: 'error', error: e.message }
  } finally {
    pinging[name] = false
  }
}

async function suppressProvider(name) {
  try {
    await setProviderSuppress(name, true)
  } catch (e) {
    console.error('Failed to suppress provider:', e)
  }
}

async function unsuppressProvider(name) {
  try {
    await setProviderSuppress(name, false)
  } catch (e) {
    console.error('Failed to unsuppress provider:', e)
  }
}

onMounted(() => {
  statusStop = createStatusStream().start(
    (data) => { status.value = data; error.value = '' },
    (e) => { error.value = e.message }
  )
})

onUnmounted(() => {
  if (statusStop) statusStop()
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
.card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
}
.clickable-card {
  cursor: pointer;
  transition: box-shadow var(--transition);
}
.clickable-card:hover {
  box-shadow: var(--shadow-md);
}
.card-actions {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.error-stats {
  margin-top: 4px;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.stat-badge {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  font-weight: 500;
}
.stat-badge.warn {
  background: #fef3c7;
  color: #92400e;
}
.stat-badge.error {
  background: #fee2e2;
  color: #991b1b;
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
  .card-grid {
    grid-template-columns: 1fr;
  }
}
</style>
