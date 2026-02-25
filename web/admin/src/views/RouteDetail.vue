<template>
  <div>
    <div class="breadcrumb">
      <router-link to="/">Dashboard</router-link>
      <span class="sep">/</span>
      <span class="current">Route: {{ routePrefix }}</span>
    </div>

    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <div v-if="detail" class="detail-layout">
      <section class="info-section">
        <h3>Basic Info</h3>
        <table class="info-table">
          <tr><td>Prefix</td><td><code>{{ detail.prefix }}</code></td></tr>
        </table>
      </section>

      <section v-if="detail.system_prompts && Object.keys(detail.system_prompts).length > 0" class="info-section">
        <h3>System Prompts</h3>
        <table class="data-table">
          <thead><tr><th>Model</th><th>Prompt</th></tr></thead>
          <tbody>
            <tr v-for="(prompt, model) in detail.system_prompts" :key="model">
              <td><code>{{ model }}</code></td>
              <td><pre class="prompt-text">{{ prompt }}</pre></td>
            </tr>
          </tbody>
        </table>
      </section>

      <section class="info-section">
        <h3>Providers ({{ detail.providers.length }})</h3>
        <div v-if="detail.providers.length === 0" class="empty">No providers configured</div>
        <table v-else class="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Requests</th>
              <th>Success</th>
              <th>Failure</th>
              <th>Avg Latency</th>
              <th>Status</th>
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
                <span v-if="p.suppressed" class="badge badge-error">Suppressed</span>
                <span v-else-if="p.consecutive_failures > 0" class="badge badge-warn">{{ p.consecutive_failures }} failures</span>
                <span v-else class="badge badge-ok">OK</span>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section class="info-section">
        <h3>MCP Tools ({{ detail.tools.length }})</h3>
        <div v-if="detail.tools.length === 0" class="empty">No MCP tools configured</div>
        <table v-else class="data-table">
          <thead>
            <tr><th>Name</th><th>Connected</th><th>Tools</th></tr>
          </thead>
          <tbody>
            <tr v-for="t in detail.tools" :key="t.name">
              <td>
                <router-link :to="'/mcp/' + encodeURIComponent(t.name)" class="resource-link">{{ t.name }}</router-link>
              </td>
              <td>
                <span :class="['badge', t.connected ? 'badge-ok' : 'badge-error']">
                  {{ t.connected ? 'Connected' : 'Disconnected' }}
                </span>
              </td>
              <td>{{ t.tool_count }}</td>
            </tr>
          </tbody>
        </table>
      </section>

      <section class="info-section">
        <h3>Send Request</h3>
        <div class="send-form">
          <div class="form-row">
            <label>Endpoint:</label>
            <select v-model="endpoint">
              <option value="chat/completions">chat/completions</option>
              <option value="responses">responses</option>
            </select>
          </div>
          <div class="form-row">
            <label>Model:</label>
            <div class="model-combobox" ref="comboboxRef">
              <input
                v-model="modelQuery"
                class="model-input"
                placeholder="Search or type model name"
                @focus="showDropdown = true"
                @keydown.down.prevent="moveHighlight(1)"
                @keydown.up.prevent="moveHighlight(-1)"
                @keydown.enter.prevent="confirmHighlight"
                @keydown.escape="showDropdown = false"
              >
              <ul v-if="showDropdown && filteredModels.length > 0" class="model-dropdown">
                <li
                  v-for="(m, i) in filteredModels"
                  :key="m"
                  :class="{ highlighted: i === highlightIndex }"
                  @mousedown.prevent="selectModel(m)"
                >{{ m }}</li>
              </ul>
            </div>
          </div>
          <div class="form-row">
            <label>Stream:</label>
            <label class="toggle">
              <input type="checkbox" v-model="stream" @change="updateTemplate">
              <span>{{ stream ? 'On' : 'Off' }}</span>
            </label>
          </div>
          <div class="form-row">
            <label>Request Body:</label>
            <textarea v-model="requestBody" rows="10" class="form-input json-input" spellcheck="false"></textarea>
          </div>
          <div class="form-row">
            <button @click="send" class="btn btn-primary" :disabled="sending">
              {{ sending ? 'Sending...' : 'Send' }}
            </button>
          </div>
        </div>

        <div v-if="response" class="response-section">
          <div class="response-meta">
            <span :class="['status-code', response.ok ? 'ok' : 'error']">{{ response.status }}</span>
            <span class="latency">{{ response.done ? response.duration + 'ms' : 'streaming...' }}</span>
          </div>

          <template v-if="response.streaming">
            <div class="stream-panels">
              <div class="stream-panel">
                <h4>Content</h4>
                <pre class="code-block" ref="contentRef">{{ response.content || '(waiting...)' }}</pre>
              </div>
              <div class="stream-panel">
                <h4>Raw Events <span class="event-count">({{ response.eventCount }})</span></h4>
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
import { fetchRouteDetail, fetchRouteModels, sendRouteRequest, createLogStream } from '../api.js'

const props = defineProps({ prefix: String })

const detail = ref(null)
const error = ref('')
const endpoint = ref('chat/completions')
const modelQuery = ref('')
const models = ref([])
const showDropdown = ref(false)
const highlightIndex = ref(-1)
const comboboxRef = ref(null)
const stream = ref(false)
const requestBody = ref('')
const sending = ref(false)
const response = ref(null)
const contentRef = ref(null)
const eventsRef = ref(null)

const routePrefix = computed(() => '/' + props.prefix)

const filteredModels = computed(() => {
  const q = modelQuery.value.toLowerCase()
  if (!q) return models.value
  return models.value.filter(m => m.toLowerCase().includes(q))
})

function selectModel(m) {
  modelQuery.value = m
  showDropdown.value = false
  highlightIndex.value = -1
}

function moveHighlight(dir) {
  if (!showDropdown.value) { showDropdown.value = true; return }
  const len = filteredModels.value.length
  if (len === 0) return
  highlightIndex.value = (highlightIndex.value + dir + len) % len
}

function confirmHighlight() {
  if (highlightIndex.value >= 0 && highlightIndex.value < filteredModels.value.length) {
    selectModel(filteredModels.value[highlightIndex.value])
  } else {
    showDropdown.value = false
  }
}

// close dropdown on outside click
function onClickOutside(e) {
  if (comboboxRef.value && !comboboxRef.value.contains(e.target)) {
    showDropdown.value = false
  }
}

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
  document.addEventListener('click', onClickOutside)
})

onUnmounted(() => {
  if (stopStream) stopStream()
  document.removeEventListener('click', onClickOutside)
})
</script>

<style scoped>
.model-combobox {
  position: relative;
  flex: 1;
}
.model-input {
  width: 100%;
  padding: 6px 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  font-size: 13px;
  font-family: var(--font-mono);
  box-sizing: border-box;
}
.model-input:focus { border-color: var(--c-primary); outline: none; }
.model-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  margin: 2px 0 0;
  padding: 0;
  list-style: none;
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  max-height: 220px;
  overflow-y: auto;
  z-index: 10;
  box-shadow: var(--shadow-md);
}
.model-dropdown li {
  padding: 6px 10px;
  font-size: 13px;
  font-family: var(--font-mono);
  cursor: pointer;
}
.model-dropdown li:hover,
.model-dropdown li.highlighted {
  background: var(--c-primary-bg);
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
