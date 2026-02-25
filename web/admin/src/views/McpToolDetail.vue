<template>
  <div>
    <div class="breadcrumb">
      <router-link to="/">Dashboard</router-link>
      <span class="sep">/</span>
      <router-link :to="'/mcp/'+encodeURIComponent(mcp)">MCP: {{ mcp }}</router-link>
      <span class="sep">/</span>
      <span class="current">{{ tool }}</span>
    </div>

    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <div v-if="toolInfo" class="detail-layout">
      <section class="info-section">
        <h3>Basic Info</h3>
        <table class="info-table">
          <tr><td>Name</td><td><strong>{{ toolInfo.name }}</strong></td></tr>
          <tr><td>Description</td><td>{{ toolInfo.description }}</td></tr>
        </table>
      </section>

      <section v-if="toolInfo.input_schema" class="info-section">
        <h3>Input Schema</h3>
        <pre class="code-block">{{ JSON.stringify(toolInfo.input_schema, null, 2) }}</pre>
      </section>

      <section class="info-section">
        <h3>Call Tool</h3>
        <div class="call-area">
          <label class="input-label">Arguments (JSON)</label>
          <textarea
            v-model="argsText"
            class="form-input args-input"
            rows="8"
            placeholder="{}"
            spellcheck="false"
          ></textarea>
          <div class="call-actions">
            <button @click="callTool" class="btn btn-primary" :disabled="calling">
              {{ calling ? 'Calling...' : 'Call Tool' }}
            </button>
          </div>
        </div>
      </section>

      <section v-if="result" class="info-section">
        <h3>Result</h3>
        <div class="result-meta">
          <span :class="['result-status', result.status]">{{ result.status }}</span>
          <span class="result-duration">{{ result.duration_ms }}ms</span>
        </div>
        <pre class="code-block">{{ result.status === 'ok' ? result.result : result.error }}</pre>
      </section>
    </div>

    <div v-if="!toolInfo && !error" class="loading">Loading...</div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { fetchMcpDetail, invokeMcpTool } from '../api.js'

const props = defineProps({
  mcp: String,
  tool: String,
})

const toolInfo = ref(null)
const error = ref('')
const argsText = ref('{}')
const calling = ref(false)
const result = ref(null)

function generateTemplate(schema) {
  if (!schema || !schema.properties) return '{}'
  const tmpl = {}
  for (const [key, prop] of Object.entries(schema.properties)) {
    if (prop.type === 'string') tmpl[key] = ''
    else if (prop.type === 'number' || prop.type === 'integer') tmpl[key] = 0
    else if (prop.type === 'boolean') tmpl[key] = false
    else if (prop.type === 'array') tmpl[key] = []
    else if (prop.type === 'object') tmpl[key] = {}
    else tmpl[key] = null
  }
  return JSON.stringify(tmpl, null, 2)
}

async function load() {
  try {
    const detail = await fetchMcpDetail(props.mcp)
    const found = detail.tools.find(t => t.name === props.tool)
    if (!found) {
      error.value = `Tool "${props.tool}" not found in MCP "${props.mcp}"`
      return
    }
    toolInfo.value = found
    argsText.value = generateTemplate(found.input_schema)
    error.value = ''
  } catch (e) {
    error.value = e.message
  }
}

async function callTool() {
  let args
  try {
    args = JSON.parse(argsText.value)
  } catch (e) {
    result.value = { status: 'error', error: 'Invalid JSON: ' + e.message, duration_ms: 0 }
    return
  }

  calling.value = true
  result.value = null
  try {
    result.value = await invokeMcpTool(props.mcp, props.tool, args)
  } catch (e) {
    result.value = { status: 'error', error: e.message, duration_ms: 0 }
  } finally {
    calling.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.call-area { display: flex; flex-direction: column; gap: 10px; }
.input-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text-2);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.args-input {
  font-family: var(--font-mono);
  resize: vertical;
  min-height: 120px;
}
.call-actions { display: flex; gap: 12px; }
.result-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.result-status {
  font-weight: 600;
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 3px;
}
.result-status.ok { color: var(--c-success); background: var(--c-success-bg); }
.result-status.error { color: var(--c-danger); background: var(--c-danger-bg); }
.result-duration { font-size: 13px; color: var(--c-text-3); }
.loading { color: var(--c-text-3); font-size: 14px; }
</style>
