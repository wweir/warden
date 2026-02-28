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
import { createStatusStream } from '../api.js'

const status = ref(null)
const error = ref('')
const search = ref('')
let statusStop = null

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

@media (max-width: 768px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }
  .search-input {
    max-width: 100%;
  }
}
</style>
