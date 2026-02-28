<template>
  <div>
    <div class="breadcrumb">
      <router-link to="/">{{ $t('dashboard.title') }}</router-link>
      <span class="sep">/</span>
      <span class="current">{{ $t('mcpDetail.breadcrumbMcp', { name }) }}</span>
    </div>

    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <div v-if="detail" class="detail-layout">
      <section class="info-section">
        <h3>{{ $t('mcpDetail.basicInfo') }}</h3>
        <table class="info-table">
          <tr><td>{{ $t('mcpDetail.name') }}</td><td>{{ detail.name }}</td></tr>
          <tr><td>{{ $t('mcpDetail.command') }}</td><td><code>{{ detail.command }} {{ (detail.args || []).join(' ') }}</code></td></tr>
          <tr v-if="detail.ssh"><td>{{ $t('mcpDetail.ssh') }}</td><td>{{ detail.ssh }}</td></tr>
          <tr><td>{{ $t('mcpDetail.status') }}</td><td :class="detail.connected ? 'text-success' : 'text-error'">{{ detail.connected ? $t('common.connected') : $t('common.disconnected') }}</td></tr>
        </table>
      </section>

      <section v-if="detail.routes && detail.routes.length > 0" class="info-section">
        <h3>{{ $t('mcpDetail.usedByRoutes') }}</h3>
        <div class="route-tags">
          <code v-for="r in detail.routes" :key="r" class="route-tag">{{ r }}</code>
        </div>
      </section>

      <section class="info-section">
        <h3>{{ $t('mcpDetail.tools', { n: detail.tools.length }) }}</h3>
        <div v-if="detail.tools.length === 0" class="empty">{{ $t('mcpDetail.noTools') }}</div>
        <div v-else class="tool-list">
          <router-link
            v-for="t in detail.tools"
            :key="t.name"
            :to="`/mcp/${encodeURIComponent(name)}/tools/${encodeURIComponent(t.name)}`"
            class="tool-card"
          >
            <div class="tool-header">
              <strong>{{ t.name }}</strong>
              <span class="tool-expand">▶</span>
            </div>
            <div class="tool-desc">{{ t.description }}</div>
          </router-link>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { fetchMcpDetail } from '../api.js'

const props = defineProps({ name: String })

const detail = ref(null)
const error = ref('')

async function load() {
  try {
    detail.value = await fetchMcpDetail(props.name)
    error.value = ''
  } catch (e) {
    error.value = e.message
  }
}

onMounted(load)
</script>

<style scoped>
.route-tags { display: flex; flex-wrap: wrap; gap: 8px; }
.route-tag {
  background: var(--c-border-light);
  padding: 4px 10px;
  border-radius: var(--radius-sm);
  font-size: 13px;
  font-family: var(--font-mono);
}
.tool-list { display: flex; flex-direction: column; gap: 8px; }
.tool-card {
  display: block;
  text-decoration: none;
  color: inherit;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 10px 14px;
  cursor: pointer;
  transition: all var(--transition);
}
.tool-card:hover { background: var(--c-border-light); border-color: #cbd5e1; }
.tool-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.tool-expand { color: var(--c-text-3); font-size: 11px; }
.tool-desc {
  font-size: 13px;
  color: var(--c-text-2);
  margin-top: 4px;
}
</style>
