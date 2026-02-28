<template>
  <div>
    <div class="breadcrumb">
      <router-link to="/">{{ $t('dashboard.title') }}</router-link>
      <span class="sep">/</span>
      <span class="current">{{ $t('routeDetail.breadcrumbRoute', { prefix: routePrefix }) }}</span>
    </div>

    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <div v-if="detail" class="detail-layout">
      <section class="info-section">
        <h3>{{ $t('routeDetail.basicInfo') }}</h3>
        <table class="info-table">
          <tr><td>{{ $t('routeDetail.prefix') }}</td><td><code>{{ detail.prefix }}</code></td></tr>
        </table>
      </section>

      <section v-if="detail.system_prompts && Object.keys(detail.system_prompts).length > 0" class="info-section">
        <h3>{{ $t('routeDetail.systemPrompts') }}</h3>
        <table class="data-table">
          <thead><tr><th>{{ $t('routeDetail.modelCol') }}</th><th>{{ $t('routeDetail.promptCol') }}</th></tr></thead>
          <tbody>
            <tr v-for="(prompt, model) in detail.system_prompts" :key="model">
              <td><code>{{ model }}</code></td>
              <td><pre class="prompt-text">{{ prompt }}</pre></td>
            </tr>
          </tbody>
        </table>
      </section>

      <section class="info-section">
        <h3>{{ $t('routeDetail.providers', { n: detail.providers.length }) }}</h3>
        <div v-if="detail.providers.length === 0" class="empty">{{ $t('routeDetail.noProviders') }}</div>
        <table v-else class="data-table">
          <thead>
            <tr>
              <th>{{ $t('routeDetail.name') }}</th>
              <th>{{ $t('routeDetail.requests') }}</th>
              <th>{{ $t('routeDetail.success') }}</th>
              <th>{{ $t('routeDetail.failure') }}</th>
              <th>{{ $t('routeDetail.avgLatency') }}</th>
              <th>{{ $t('routeDetail.status') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in detail.providers" :key="p.name">
              <td>
                <router-link :to="'/providers/' + encodeURIComponent(p.name)" class="resource-link">{{ p.name }}</router-link>
              </td>
              <td>{{ p.total_requests }}</td>
              <td>{{ p.success_count }}</td>
              <td>{{ p.failure_count }}</td>
              <td>{{ p.total_requests > 0 ? p.avg_latency_ms.toFixed(0) + 'ms' : '-' }}</td>
              <td>
                <span v-if="p.suppressed" class="badge badge-error">{{ $t('common.suppressed') }}</span>
                <span v-else-if="p.consecutive_failures > 0" class="badge badge-warn">{{ $t('routeDetail.failures', { n: p.consecutive_failures }) }}</span>
                <span v-else class="badge badge-ok">{{ $t('common.ok') }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section class="info-section">
        <h3>{{ $t('routeDetail.mcpTools', { n: detail.tools.length }) }}</h3>
        <div v-if="detail.tools.length === 0" class="empty">{{ $t('routeDetail.noMcpTools') }}</div>
        <table v-else class="data-table">
          <thead>
            <tr><th>{{ $t('routeDetail.name') }}</th><th>{{ $t('routeDetail.connected') }}</th><th>{{ $t('routeDetail.tools') }}</th></tr>
          </thead>
          <tbody>
            <tr v-for="mc in detail.tools" :key="mc.name">
              <td>
                <router-link :to="'/mcp/' + encodeURIComponent(mc.name)" class="resource-link">{{ mc.name }}</router-link>
              </td>
              <td>
                <span :class="['badge', mc.connected ? 'badge-ok' : 'badge-error']">
                  {{ mc.connected ? $t('routeDetail.connected') : $t('common.disconnected') }}
                </span>
              </td>
              <td>{{ mc.tool_count }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <section class="info-section">
        <h3>{{ $t('routeDetail.sendRequest') }}</h3>
        <div class="send-form">
          <div class="form-row">
            <label>{{ $t('routeDetail.endpoint') }}</label>
            <select v-model="endpoint">
              <option value="chat/completions">chat/completions</option>
              <option value="responses">responses</option>
            </select>
          </div>
          <div class="form-row">
            <label>{{ $t('routeDetail.model') }}</label>
            <ModelCombobox
              v-model="modelQuery"
              :models="models"
              :placeholder="$t('routeDetail.searchModel')"
              @update:modelValue="updateTemplate"
            />
          </div>
          <div class="form-row">
            <label>{{ $t('routeDetail.stream') }}</label>
            <label class="toggle">
              <input type="checkbox" v-model="stream" @change="updateTemplate">
              <span>{{ stream ? $t('common.on') : $t('common.off') }}</span>
            </label>
          </div>
          <div class="form-row">
            <label>{{ $t('routeDetail.requestBody') }}</label>
            <textarea v-model="requestBody" rows="10" class="form-input json-input" spellcheck="false"></textarea>
          </div>
          <div class="form-row">
            <button @click="send" class="btn btn-primary" :disabled="sending">
              {{ sending ? $t('routeDetail.sending') : $t('routeDetail.send') }}
            </button>
          </div>
        </div>

        <div v-if="response" class="response-section">
          <div class="response-meta">
            <span :class="['status-code', response.ok ? 'ok' : 'error']">{{ response.status }}</span>
            <span class="latency">{{ response.done ? response.duration + 'ms' : $t('routeDetail.streaming') }}</span>
          </div>

          <template v-if="response.streaming">
            <div class="stream-panels">
              <div class="stream-panel">
                <h4>{{ $t('routeDetail.contentLabel') }}</h4>
                <pre class="code-block" ref="contentRef">{{ response.content || $t('routeDetail.waiting') }}</pre>
              </div>
              <div class="stream-panel">
                <h4>{{ $t('routeDetail.rawEvents') }} <span class="event-count">({{ response.eventCount }})</span></h4>
                <pre class="code-block raw-events" ref="eventsRef">{{ response.events }}</pre>
              </div>
            </div>
          </template>
          <template v-else>
            <pre class="code-block">{{ response.body }}</pre>
          </template>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchRouteDetail, fetchRouteModels, sendRouteRequest, createLogStream } from '../api.js'
import ModelCombobox from '../components/ModelCombobox.vue'

const { t } = useI18n()

const props = defineProps({ prefix: String })

const detail = ref(null)
const error = ref('')
const endpoint = ref('chat/completions')
const modelQuery = ref('')
const models = ref([])
const stream = ref(false)
const requestBody = ref('')
const sending = ref(false)
const response = ref(null)
const contentRef = ref(null)
const eventsRef = ref(null)

const routePrefix = computed(() => '/' + props.prefix)

function buildTemplate() {
  const m = modelQuery.value
  const s = stream.value
  if (endpoint.value === 'responses') {
    return JSON.stringify({ model: m, input: 'Hello', stream: s }, null, 2)
  }
  return JSON.stringify({
    model: m,
    messages: [{ role: 'user', content: 'Hello' }],
    stream: s,
  }, null, 2)
}

function updateTemplate() {
  requestBody.value = buildTemplate()
}

watch(endpoint, updateTemplate)
watch(modelQuery, updateTemplate)

async function loadDetail() {
  try {
    detail.value = await fetchRouteDetail(routePrefix.value)
    error.value = ''
  } catch (e) {
    error.value = e.message
  }
}

async function load() {
  await loadDetail()
  models.value = await fetchRouteModels(routePrefix.value)
  if (models.value.length > 0) {
    modelQuery.value = ''
  }
  updateTemplate()
}

// Extract text content from a single SSE data line based on endpoint type.
function extractContent(dataStr, ep) {
  try {
    const obj = JSON.parse(dataStr)
    if (ep === 'chat/completions') {
      return obj.choices?.[0]?.delta?.content || ''
    }
    // responses endpoint
    if (obj.type === 'response.output_text.delta') {
      return obj.delta || ''
    }
  } catch { /* ignore */ }
  return ''
}

async function send() {
  sending.value = true
  response.value = null
  const start = performance.now()

  let body
  try {
    body = JSON.parse(requestBody.value)
  } catch (e) {
    response.value = { status: 'Parse Error', ok: false, done: true, duration: 0, body: 'Invalid JSON: ' + e.message, streaming: false }
    sending.value = false
    return
  }

  const isStream = !!body.stream
  let res
  try {
    res = await sendRouteRequest(routePrefix.value, endpoint.value, body)
  } catch (e) {
    response.value = { status: 'Error', ok: false, done: true, duration: Math.round(performance.now() - start), body: e.message, streaming: false }
    sending.value = false
    return
  }

  if (!isStream) {
    const duration = Math.round(performance.now() - start)
    const text = await res.text()
    let formatted = text
    try { formatted = JSON.stringify(JSON.parse(text), null, 2) } catch { /* keep raw */ }
    response.value = { status: res.status, ok: res.ok, done: true, duration, body: formatted, streaming: false }
    sending.value = false
    return
  }

  // streaming mode
  response.value = {
    status: res.status,
    ok: res.ok,
    done: false,
    duration: 0,
    streaming: true,
    content: '',
    events: '',
    eventCount: 0,
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const ep = endpoint.value

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed) continue
        if (trimmed.startsWith('data: ')) {
          const dataStr = trimmed.slice(6)
          if (dataStr === '[DONE]') continue
          response.value.events += trimmed + '\n'
          response.value.eventCount++
          response.value.content += extractContent(dataStr, ep)
        } else if (trimmed.startsWith('event: ')) {
          response.value.events += trimmed + '\n'
        } else {
          response.value.events += trimmed + '\n'
        }
      }
    }
  } catch (e) {
    response.value.content += '\n[Stream error: ' + e.message + ']'
  }

  response.value.done = true
  response.value.duration = Math.round(performance.now() - start)
  sending.value = false
}

// SSE subscription: refresh stats when a request on this route completes
let stopStream = null

function startStream() {
  const logStream = createLogStream()
  stopStream = logStream.start(
    (record) => {
      if (record.route === routePrefix.value) {
        loadDetail()
      }
    },
    () => {
      // reconnect after 3s on error
      setTimeout(startStream, 3000)
    },
  )
}

onMounted(() => {
  load()
  startStream()
})

onUnmounted(() => {
  if (stopStream) stopStream()
})
</script>

<style scoped>
.model-combobox {
  flex: 1;
}
.toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  cursor: pointer;
  font-weight: normal;
  padding-top: 0;
}
.json-input {
  min-height: 120px;
  resize: vertical;
}
.response-section {
  margin-top: 16px;
  border-top: 1px solid var(--c-border);
  padding-top: 12px;
}
.response-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.stream-panels {
  display: grid;
  grid-template-columns: 1fr 4fr;
  gap: 12px;
}
.stream-panel h4 {
  margin: 0 0 6px;
  font-size: 13px;
  color: var(--c-text-2);
}
.event-count {
  font-weight: normal;
  color: var(--c-text-3);
}
.status-code {
  font-weight: 700;
  font-size: 14px;
}
.status-code.ok { color: var(--c-success); }
.status-code.error { color: var(--c-danger); }
.latency { font-size: 13px; color: var(--c-text-2); }
.raw-events {
  font-size: 11px;
  color: var(--c-text-2);
  max-height: 400px;
}
.prompt-text {
  margin: 0;
  white-space: pre-wrap;
  font-size: 13px;
  max-height: 200px;
  overflow-y: auto;
}
.send-form { display: flex; flex-direction: column; gap: 12px; }
.form-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.form-row > label:first-child {
  width: 100px;
  font-weight: 600;
  font-size: 13px;
  color: var(--c-text-2);
  padding-top: 6px;
  flex-shrink: 0;
}
.form-row select {
  padding: 6px 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  font-size: 13px;
}

@media (max-width: 768px) {
  .stream-panels {
    grid-template-columns: 1fr;
  }

  .form-row {
    flex-direction: column;
    gap: 4px;
  }

  .form-row > label:first-child {
    width: auto;
    padding-top: 0;
  }

  .model-input,
  .form-row select,
  .json-input {
    width: 100%;
  }
}
</style>
