<template>
  <div>
    <h2 class="page-title">Dashboard</h2>
    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <section v-if="status">
      <h3 class="section-title">Routes</h3>
      <section class="panel" style="padding:18px">
        <table class="data-table">
          <thead>
            <tr><th>Prefix</th><th>Providers</th><th>Tools</th></tr>
          </thead>
          <tbody>
            <tr v-for="r in status.routes" :key="r.prefix">
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
          </tbody>
        </table>
      </section>

      <h3 class="section-title">Providers</h3>
      <div class="card-grid">
        <StatusCard
          v-for="p in status.providers"
          :key="p.name"
          :name="p.name"
          :status="providerStatus(p)"
          class="clickable-card"
          @click="$router.push('/providers/' + p.name)"
        >
          <div>Models: {{ p.model_count }}</div>
          <div>Requests: {{ p.total_requests }} ({{ p.success_count }} ok / {{ p.failure_count }} fail)</div>
          <div v-if="p.total_requests > 0">Avg Latency: {{ p.avg_latency_ms.toFixed(0) }}ms</div>
          <div>Failures: {{ p.consecutive_failures }}</div>
          <div v-if="p.suppressed">Suppressed until {{ formatTime(p.suppress_until) }}</div>
          <div class="card-actions">
            <button class="btn btn-secondary btn-sm" @click.stop="ping(p.name)">
              {{ pinging[p.name] ? '...' : 'Ping' }}
            </button>
            <span v-if="pingResults[p.name]" :class="pingResults[p.name].status === 'ok' ? 'text-success' : 'text-error'" style="font-size:12px">
              {{ pingResults[p.name].status === 'ok'
                ? pingResults[p.name].latency_ms + 'ms'
                : pingResults[p.name].error }}
            </span>
          </div>
        </StatusCard>
      </div>

      <h3 class="section-title">MCP Servers</h3>
      <div class="card-grid">
        <StatusCard
          v-for="m in status.mcp"
          :key="m.name"
          :name="m.name"
          :status="m.connected ? 'ok' : 'error'"
          class="clickable-card"
          @click="$router.push('/mcp/' + m.name)"
        >
          <div>Tools: {{ m.tool_count }}</div>
          <div>Status: {{ m.connected ? 'Connected' : 'Disconnected' }}</div>
        </StatusCard>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import StatusCard from '../components/StatusCard.vue'
import { fetchStatus, healthCheck } from '../api.js'

const status = ref(null)
const error = ref('')
const pinging = reactive({})
const pingResults = reactive({})
let timer = null

async function load() {
  try {
    status.value = await fetchStatus()
    error.value = ''
  } catch (e) {
    error.value = e.message
  }
}

function providerStatus(p) {
  if (p.suppressed) return 'error'
  if (p.consecutive_failures > 0) return 'warn'
  return 'ok'
}

function formatTime(t) {
  if (!t) return ''
  return new Date(t).toLocaleTimeString()
}

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

onMounted(() => {
  load()
  timer = setInterval(load, 5000)
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.section-title {
  margin: 20px 0 10px;
  font-size: 1.1rem;
  font-weight: 600;
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

@media (max-width: 768px) {
  .card-grid {
    grid-template-columns: 1fr;
  }

  .panel:has(.data-table) {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }
}
</style>
