<template>
  <div>
    <div class="breadcrumb">
      <router-link to="/">{{ $t('dashboard.title') }}</router-link>
      <span class="sep">/</span>
      <router-link to="/routes">{{ $t('routes.title') }}</router-link>
      <span class="sep">/</span>
      <span class="current">{{ pageTitle }}</span>
    </div>

    <div v-if="configSource && !configSource.source_type?.file" class="msg warning">
      {{ $t('config.nonFileWarning', { path: configSource.config_path || 'remote' }) }}
    </div>
    <div v-if="message" :class="['msg', messageType]">{{ message }}</div>
    <div v-if="error" class="msg msg-error">{{ error }}</div>

    <div class="detail-layout">
      <section class="info-section">
        <div class="section-head">
          <div>
            <h3>{{ $t('routeDetail.configEditor') }}</h3>
            <p class="section-desc">{{ $t('routeDetail.configEditorDesc') }}</p>
          </div>
          <router-link
            v-if="effectivePrefix"
            :to="{ path: '/tool-hooks', query: { route: effectivePrefix } }"
            class="btn btn-secondary btn-sm"
          >
            {{ $t('routeDetail.editHooks') }}
          </router-link>
        </div>

        <div class="editor-grid">
          <div class="field-row">
            <label class="field-label">{{ $t('routeDetail.prefix') }}</label>
            <input
              v-model="editablePrefix"
              class="form-input"
              :readonly="!isCreate"
              :placeholder="$t('routeDetail.prefixPlaceholder')"
              spellcheck="false"
            />
            <span class="field-hint">{{ $t('routeDetail.prefixHint') }}</span>
          </div>

          <div class="field-row">
            <label class="field-label">{{ $t('routeDetail.protocol') }}</label>
            <select v-model="routeConfig.protocol" class="form-input">
              <option value="chat">chat</option>
              <option value="responses">responses</option>
              <option value="anthropic">anthropic</option>
            </select>
            <span class="field-hint">{{ $t('routeDetail.protocolHint') }}</span>
          </div>
        </div>

        <RouteModelsEditor
          :exact-models="routeConfig.exact_models"
          :wildcard-models="routeConfig.wildcard_models"
          :provider-map="providerMap"
          :route-protocol="routeConfig.protocol"
          @update:exactModels="routeConfig.exact_models = $event"
          @update:wildcardModels="routeConfig.wildcard_models = $event"
        />

        <div class="editor-actions">
          <button
            class="btn btn-primary"
            :disabled="busy || (configSource && !configSource.source_type?.file)"
            @click="saveAndApply"
          >
            {{
              busy
                ? waitingAlive
                  ? $t('config.waitingService', { n: waitingElapsed })
                  : $t('routeDetail.saving')
                : $t('routeDetail.saveApply')
            }}
          </button>
          <button
            v-if="!isCreate"
            class="btn btn-danger"
            :disabled="busy || (configSource && !configSource.source_type?.file)"
            @click="deleteRoute"
          >
            {{ $t('routeDetail.deleteRoute') }}
          </button>
        </div>
      </section>

      <section v-if="detail" class="info-section">
        <h3>{{ $t('routeDetail.basicInfo') }}</h3>
        <table class="info-table">
          <tr><td>{{ $t('routeDetail.prefix') }}</td><td><code>{{ detail.prefix }}</code></td></tr>
          <tr><td>{{ $t('routeDetail.protocol') }}</td><td><code>{{ detail.protocol || 'legacy' }}</code></td></tr>
          <tr><td>{{ $t('routeDetail.hookCount') }}</td><td>{{ detail.hook_count || 0 }}</td></tr>
        </table>
      </section>

      <section v-if="detail && (detail.exact_models || []).length > 0" class="info-section">
        <h3>{{ $t('routeDetail.exactModels') }}</h3>
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ $t('routeDetail.modelCol') }}</th>
              <th>{{ $t('routeDetail.upstreamsCol') }}</th>
              <th>{{ $t('routeDetail.promptCol') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in detail.exact_models" :key="model.name">
              <td><code>{{ model.name }}</code></td>
              <td>{{ (model.upstreams || []).join(', ') || '-' }}</td>
              <td><pre class="prompt-text">{{ model.system_prompt || '-' }}</pre></td>
            </tr>
          </tbody>
        </table>
      </section>

      <section v-if="detail && (detail.wildcard_models || []).length > 0" class="info-section">
        <h3>{{ $t('routeDetail.wildcardModels') }}</h3>
        <table class="data-table">
          <thead>
            <tr>
              <th>{{ $t('routeDetail.patternCol') }}</th>
              <th>{{ $t('routeDetail.providersCol') }}</th>
              <th>{{ $t('routeDetail.promptCol') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="model in detail.wildcard_models" :key="model.pattern || model.name">
              <td><code>{{ model.pattern || model.name }}</code></td>
              <td>{{ (model.upstreams || []).join(', ') || '-' }}</td>
              <td><pre class="prompt-text">{{ model.system_prompt || '-' }}</pre></td>
            </tr>
          </tbody>
        </table>
      </section>

      <section v-if="detail" class="info-section">
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
            <tr v-for="provider in detail.providers" :key="provider.name">
              <td>
                <router-link
                  :to="'/providers/' + encodeURIComponent(provider.name)"
                  class="resource-link"
                >
                  {{ provider.name }}
                </router-link>
              </td>
              <td>{{ provider.total_requests }}</td>
              <td>{{ provider.success_count }}</td>
              <td>{{ provider.failure_count }}</td>
              <td>{{ provider.total_requests > 0 ? provider.avg_latency_ms.toFixed(0) + 'ms' : '-' }}</td>
              <td>
                <span v-if="provider.suppressed" class="badge badge-error">{{ $t('common.suppressed') }}</span>
                <span
                  v-else-if="provider.consecutive_failures > 0"
                  class="badge badge-warn"
                >
                  {{ $t('routeDetail.failures', { n: provider.consecutive_failures }) }}
                </span>
                <span v-else class="badge badge-ok">{{ $t('common.ok') }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section v-if="detail" class="info-section">
        <h3>{{ $t('routeDetail.sendRequest') }}</h3>
        <div class="send-form">
          <div class="form-row">
            <label>{{ $t('routeDetail.endpoint') }}</label>
            <select v-model="endpoint" class="form-input form-select">
              <option v-for="ep in endpointOptions" :key="ep" :value="ep">{{ ep }}</option>
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
            <textarea
              v-model="requestBody"
              rows="10"
              class="form-input json-input"
              spellcheck="false"
            ></textarea>
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
            <span class="latency">
              {{ response.done ? response.duration + 'ms' : $t('routeDetail.streaming') }}
            </span>
          </div>

          <template v-if="response.streaming">
            <div class="stream-panels">
              <div class="stream-panel">
                <h4>{{ $t('routeDetail.contentLabel') }}</h4>
                <pre class="code-block" ref="contentRef">{{ response.content || $t('routeDetail.waiting') }}</pre>
              </div>
              <div class="stream-panel">
                <h4>
                  {{ $t('routeDetail.rawEvents') }}
                  <span class="event-count">({{ response.eventCount }})</span>
                </h4>
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
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  createLogStream,
  fetchConfig,
  fetchConfigSource,
  fetchRouteDetail,
  fetchRouteModels,
  fetchStatus,
  restartGateway,
  saveConfig,
  sendRouteRequest,
  validateConfig,
} from '../api.js'
import ModelCombobox from '../components/ModelCombobox.vue'
import RouteModelsEditor from '../components/RouteModelsEditor.vue'

const { t } = useI18n()
const router = useRouter()

const props = defineProps({
  prefix: { type: String, default: '' },
  create: { type: Boolean, default: false },
})

const detail = ref(null)
const error = ref('')
const message = ref('')
const messageType = ref('msg-success')
const configSource = ref(null)
const configDoc = ref(null)
const routeConfig = ref(createEmptyRouteConfig())
const editablePrefix = ref('')
const models = ref([])
const endpoint = ref('chat/completions')
const modelQuery = ref('')
const stream = ref(false)
const requestBody = ref('')
const sending = ref(false)
const response = ref(null)
const contentRef = ref(null)
const eventsRef = ref(null)
const applying = ref(false)
const deleting = ref(false)
const waitingAlive = ref(false)
const waitingElapsed = ref(0)

const isCreate = computed(() => !!props.create)
const existingPrefix = computed(() => (isCreate.value ? '' : normalizeRoutePrefix(props.prefix)))
const effectivePrefix = computed(() =>
  isCreate.value ? normalizeRoutePrefix(editablePrefix.value) : existingPrefix.value,
)
const pageTitle = computed(() =>
  isCreate.value ? t('routeDetail.newRouteTitle') : t('routeDetail.breadcrumbRoute', { prefix: effectivePrefix.value }),
)
const providerMap = computed(() => configDoc.value?.provider || {})
const busy = computed(() => applying.value || deleting.value)
const endpointOptions = computed(() => {
  if (routeConfig.value.protocol === 'anthropic') return ['messages']
  if (routeConfig.value.protocol === 'responses') return ['responses']
  return ['chat/completions']
})

function normalizeRoutePrefix(prefix) {
  const value = String(prefix || '').trim()
  if (!value) return ''
  return value.startsWith('/') ? value : `/${value}`
}

function deepClone(value) {
  return JSON.parse(JSON.stringify(value))
}

function supportedRouteProtocols(providerProtocol) {
  if (providerProtocol === 'anthropic') return ['anthropic']
  if (['openai', 'ollama', 'qwen', 'copilot'].includes(providerProtocol)) return ['chat', 'responses']
  return []
}

function defaultProviderForProtocol(protocol, providerConfigMap = {}) {
  for (const [name, provider] of Object.entries(providerConfigMap || {})) {
    if (!protocol || supportedRouteProtocols(provider?.protocol || 'openai').includes(protocol)) {
      return name
    }
  }
  return ''
}

function createEmptyRouteConfig(providerConfigMap = {}) {
  const protocol = 'chat'
  const provider = defaultProviderForProtocol(protocol, providerConfigMap)
  return {
    protocol,
    exact_models: {},
    wildcard_models: provider ? { '*': { providers: [provider] } } : {},
  }
}

function normalizeEditableRoute(route) {
  const normalized = createEmptyRouteConfig(providerMap.value)
  normalized.protocol = route?.protocol || 'chat'
  normalized.exact_models = deepClone(route?.exact_models || {})
  normalized.wildcard_models = deepClone(route?.wildcard_models || {})
  if (
    Object.keys(normalized.exact_models).length === 0 &&
    Object.keys(normalized.wildcard_models).length === 0
  ) {
    const provider = defaultProviderForProtocol(normalized.protocol, providerMap.value)
    normalized.wildcard_models = provider ? { '*': { providers: [provider] } } : {}
  }
  return normalized
}

function buildRoutePayload(existingRoute = {}) {
  const nextRoute = deepClone(existingRoute || {})
  nextRoute.protocol = routeConfig.value.protocol
  nextRoute.exact_models = deepClone(routeConfig.value.exact_models || {})
  nextRoute.wildcard_models = deepClone(routeConfig.value.wildcard_models || {})
  nextRoute.hooks = deepClone(existingRoute?.hooks || [])
  delete nextRoute.models
  delete nextRoute.providers
  delete nextRoute.system_prompts
  return nextRoute
}

function buildTemplate() {
  const model = modelQuery.value
  const useStream = stream.value
  if (endpoint.value === 'messages') {
    return JSON.stringify(
      {
        model,
        messages: [{ role: 'user', content: 'Hello' }],
        stream: useStream,
        max_tokens: 1024,
      },
      null,
      2,
    )
  }
  return JSON.stringify(
    endpoint.value === 'responses'
      ? { model, input: 'Hello', stream: useStream }
      : { model, messages: [{ role: 'user', content: 'Hello' }], stream: useStream },
    null,
    2,
  )
}

function updateTemplate() {
  requestBody.value = buildTemplate()
}

watch(endpoint, updateTemplate)
watch(modelQuery, updateTemplate)
watch(endpointOptions, (options) => {
  if (!options.includes(endpoint.value)) {
    endpoint.value = options[0] || 'chat/completions'
  }
})

async function loadConfigDoc() {
  const [cfg, source] = await Promise.all([fetchConfig(), fetchConfigSource()])
  configDoc.value = cfg
  configSource.value = source

  if (isCreate.value) {
    editablePrefix.value = ''
    routeConfig.value = createEmptyRouteConfig(cfg.provider || {})
    return
  }

  editablePrefix.value = existingPrefix.value
  const existingRoute = cfg.route?.[existingPrefix.value]
  if (!existingRoute) {
    throw new Error(t('routeDetail.routeConfigMissing', { prefix: existingPrefix.value }))
  }
  routeConfig.value = normalizeEditableRoute(existingRoute)
}

async function loadDetail() {
  if (isCreate.value || !effectivePrefix.value) {
    detail.value = null
    models.value = []
    updateTemplate()
    return
  }

  detail.value = await fetchRouteDetail(effectivePrefix.value)
  models.value = await fetchRouteModels(effectivePrefix.value)
  modelQuery.value = ''
  updateTemplate()
}

async function load() {
  try {
    error.value = ''
    await loadConfigDoc()
    await loadDetail()
  } catch (e) {
    error.value = e.message
  }
}

function setMessage(type, text) {
  messageType.value = type
  message.value = text
}

async function pollUntilAlive(timeoutMs = 60000, intervalMs = 1500) {
  const deadline = Date.now() + timeoutMs
  waitingAlive.value = true
  waitingElapsed.value = 0
  const startMs = Date.now()
  const ticker = setInterval(() => {
    waitingElapsed.value = Math.floor((Date.now() - startMs) / 1000)
  }, 500)
  try {
    await new Promise((resolve) => setTimeout(resolve, 800))
    while (Date.now() < deadline) {
      try {
        await fetchStatus()
        return true
      } catch {
        await new Promise((resolve) => setTimeout(resolve, intervalMs))
      }
    }
    return false
  } finally {
    clearInterval(ticker)
    waitingAlive.value = false
    waitingElapsed.value = 0
  }
}

async function applyConfig(nextConfig) {
  const result = await validateConfig(nextConfig)
  if (!result.valid) {
    throw new Error(t('config.validationFailed', { error: result.error }))
  }
  await saveConfig(nextConfig)
  const restart = await restartGateway()
  if (restart.status !== 'ok') {
    throw new Error(t('config.savedButRestartFailed', { error: restart.error || 'unknown error' }))
  }
  const alive = await pollUntilAlive()
  if (!alive) {
    throw new Error(t('config.serviceTimeout'))
  }
}

async function saveAndApply() {
  if (busy.value) return
  applying.value = true
  error.value = ''
  message.value = ''

  try {
    if (!configSource.value?.source_type?.file) {
      throw new Error(t('config.savingDisabled'))
    }

    const prefix = normalizeRoutePrefix(editablePrefix.value)
    if (!prefix) {
      throw new Error(t('routeDetail.prefixRequired'))
    }

    const nextConfig = deepClone(configDoc.value || {})
    nextConfig.route = nextConfig.route || {}

    if (isCreate.value && nextConfig.route[prefix]) {
      throw new Error(t('routeDetail.routeExists', { prefix }))
    }

    const existingRoute = !isCreate.value ? nextConfig.route[existingPrefix.value] || {} : {}
    nextConfig.route[prefix] = buildRoutePayload(existingRoute)

    await applyConfig(nextConfig)

    if (isCreate.value) {
      await router.replace('/routes' + prefix)
      return
    }

    await load()
    setMessage('msg-success', t('routeDetail.savedMsg', { prefix }))
  } catch (e) {
    error.value = e.message
  } finally {
    applying.value = false
  }
}

async function deleteRoute() {
  if (busy.value || isCreate.value) return
  if (!window.confirm(t('routeDetail.confirmDeleteRoute', { prefix: existingPrefix.value }))) return

  deleting.value = true
  error.value = ''
  message.value = ''
  try {
    if (!configSource.value?.source_type?.file) {
      throw new Error(t('config.savingDisabled'))
    }

    const nextConfig = deepClone(configDoc.value || {})
    nextConfig.route = nextConfig.route || {}
    delete nextConfig.route[existingPrefix.value]

    await applyConfig(nextConfig)
    await router.push('/routes')
  } catch (e) {
    error.value = e.message
  } finally {
    deleting.value = false
  }
}

function extractContent(dataStr, ep) {
  try {
    const obj = JSON.parse(dataStr)
    if (ep === 'chat/completions') {
      return obj.choices?.[0]?.delta?.content || ''
    }
    if (obj.type === 'response.output_text.delta') {
      return obj.delta || ''
    }
  } catch {
    // ignore parse errors
  }
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
    response.value = {
      status: 'Parse Error',
      ok: false,
      done: true,
      duration: 0,
      body: 'Invalid JSON: ' + e.message,
      streaming: false,
    }
    sending.value = false
    return
  }

  let res
  try {
    res = await sendRouteRequest(effectivePrefix.value, endpoint.value, body)
  } catch (e) {
    response.value = {
      status: 'Error',
      ok: false,
      done: true,
      duration: Math.round(performance.now() - start),
      body: e.message,
      streaming: false,
    }
    sending.value = false
    return
  }

  if (!body.stream) {
    const duration = Math.round(performance.now() - start)
    const text = await res.text()
    let formatted = text
    try {
      formatted = JSON.stringify(JSON.parse(text), null, 2)
    } catch {
      // keep raw
    }
    response.value = {
      status: res.status,
      ok: res.ok,
      done: true,
      duration,
      body: formatted,
      streaming: false,
    }
    sending.value = false
    return
  }

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
          response.value.eventCount += 1
          response.value.content += extractContent(dataStr, ep)
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

let stopStream = null

function startStream() {
  if (isCreate.value) return
  const logStream = createLogStream()
  stopStream = logStream.start(
    (record) => {
      if (record.route === existingPrefix.value) {
        loadDetail().catch(() => {})
      }
    },
    () => {
      setTimeout(startStream, 3000)
    },
  )
}

watch(
  () => [props.prefix, props.create],
  () => {
    message.value = ''
    if (stopStream) {
      stopStream()
      stopStream = null
    }
    load()
    startStream()
  },
)

onMounted(() => {
  editablePrefix.value = existingPrefix.value
  message.value = ''
  load()
  startStream()
})

onUnmounted(() => {
  if (stopStream) stopStream()
})
</script>

<style scoped>
.section-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 14px;
}

.section-desc {
  margin: 4px 0 0;
  font-size: 13px;
  color: var(--c-text-2);
  line-height: 1.6;
}

.editor-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.field-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text-2);
}

.field-hint {
  font-size: 11px;
  color: var(--c-text-3);
  line-height: 1.5;
}

.editor-actions {
  display: flex;
  gap: 10px;
  margin-top: 18px;
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

.form-select,
.json-input {
  width: 100%;
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

.status-code.ok {
  color: var(--c-success);
}

.status-code.error {
  color: var(--c-danger);
}

.latency {
  font-size: 13px;
  color: var(--c-text-2);
}

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

.send-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

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

@media (max-width: 768px) {
  .section-head,
  .editor-actions,
  .form-row {
    flex-direction: column;
  }

  .editor-grid,
  .stream-panels {
    grid-template-columns: 1fr;
  }

  .form-row {
    gap: 4px;
  }

  .form-row > label:first-child {
    width: auto;
    padding-top: 0;
  }
}
</style>
