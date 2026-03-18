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
          :provider-model-map="providerModelMap"
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

    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  createLogStream,
  fetchConfig,
  fetchConfigSource,
  fetchProviderDetail,
  fetchRouteDetail,
  fetchStatus,
  restartGateway,
  saveConfig,
  validateConfig,
} from '../api.js'
import RouteModelsEditor from '../components/RouteModelsEditor.vue'

const { t } = useI18n()
const route = useRoute()
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
const providerDiscoveredModels = ref({})
const routeConfig = ref(createEmptyRouteConfig())
const editablePrefix = ref('')
const applying = ref(false)
const deleting = ref(false)
const waitingAlive = ref(false)
const waitingElapsed = ref(0)
let providerSuggestionLoadID = 0

const isCreate = computed(() => !!props.create)
const existingPrefix = computed(() => (isCreate.value ? '' : normalizeRoutePrefix(props.prefix)))
const effectivePrefix = computed(() =>
  isCreate.value ? normalizeRoutePrefix(editablePrefix.value) : existingPrefix.value,
)
const sourceProviderName = computed(() => {
  if (!isCreate.value) return ''
  const provider = route.query.provider
  return normalizeText(Array.isArray(provider) ? provider[0] : provider)
})
const pageTitle = computed(() =>
  isCreate.value ? t('routeDetail.newRouteTitle') : t('routeDetail.breadcrumbRoute', { prefix: effectivePrefix.value }),
)
const providerMap = computed(() => configDoc.value?.provider || {})
const providerModelMap = computed(() => {
  const names = new Set([
    ...Object.keys(providerMap.value || {}),
    ...Object.keys(providerDiscoveredModels.value || {}),
  ])
  const out = {}
  for (const name of names) {
    out[name] = uniqueSortedTextValues([
      ...(providerMap.value?.[name]?.models || []),
      ...(providerDiscoveredModels.value?.[name] || []),
    ])
  }
  return out
})
const busy = computed(() => applying.value || deleting.value)

function normalizeRoutePrefix(prefix) {
  const value = String(prefix || '').trim()
  if (!value) return ''
  return value.startsWith('/') ? value : `/${value}`
}

function normalizeText(value) {
  return String(value || '').trim()
}

function uniqueSortedTextValues(values) {
  const out = []
  const seen = new Set()
  for (const value of values || []) {
    const normalized = normalizeText(value)
    if (!normalized || seen.has(normalized)) continue
    seen.add(normalized)
    out.push(normalized)
  }
  return out.sort((a, b) => a.localeCompare(b))
}

function deepClone(value) {
  return JSON.parse(JSON.stringify(value))
}

function supportedRouteProtocols(providerProtocol) {
  if (providerProtocol === 'anthropic') return ['anthropic']
  if (['openai', 'ollama', 'qwen', 'copilot'].includes(providerProtocol)) return ['chat', 'responses']
  return []
}

function preferredRouteProtocol(providerProtocol) {
  return supportedRouteProtocols(providerProtocol)[0] || 'chat'
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

function createProviderSeededRouteConfig(providerName, providerConfigMap = {}, discoveredModelMap = {}) {
  const provider = providerConfigMap?.[providerName]
  if (!provider) {
    throw new Error(t('routeDetail.sourceProviderMissing', { name: providerName }))
  }

  const protocol = preferredRouteProtocol(provider?.protocol || 'openai')
  const models = uniqueSortedTextValues([
    ...(provider?.models || []),
    ...(discoveredModelMap?.[providerName] || []),
  ])
  const exactModels = {}
  for (const model of models) {
    exactModels[model] = {
      upstreams: [{ provider: providerName, model }],
    }
  }

  return {
    protocol,
    exact_models: exactModels,
    wildcard_models: {},
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

function extractProviderModelIDs(models) {
  return uniqueSortedTextValues(
    (models || []).map((model) => {
      if (typeof model === 'string') return model
      if (typeof model?.id === 'string') return model.id
      return ''
    }),
  )
}

async function loadProviderModelSuggestions(providerConfigMap = {}) {
  const loadID = ++providerSuggestionLoadID
  const providerNames = Object.keys(providerConfigMap || {})
  if (providerNames.length === 0) {
    if (loadID === providerSuggestionLoadID) {
      providerDiscoveredModels.value = {}
    }
    return
  }

  const results = await Promise.allSettled(
    providerNames.map((name) => fetchProviderDetail(name)),
  )

  if (loadID !== providerSuggestionLoadID) return

  const nextSuggestions = {}
  providerNames.forEach((name, index) => {
    const configured = providerConfigMap?.[name]?.models || []
    const discovered =
      results[index]?.status === 'fulfilled'
        ? extractProviderModelIDs(results[index].value?.models)
        : []
    nextSuggestions[name] = uniqueSortedTextValues([...configured, ...discovered])
  })
  providerDiscoveredModels.value = nextSuggestions
}

async function loadConfigDoc() {
  const [cfg, source] = await Promise.all([fetchConfig(), fetchConfigSource()])
  configDoc.value = cfg
  configSource.value = source
  await loadProviderModelSuggestions(cfg.provider || {})

  if (isCreate.value) {
    editablePrefix.value = ''
    if (sourceProviderName.value) {
      routeConfig.value = createProviderSeededRouteConfig(
        sourceProviderName.value,
        cfg.provider || {},
        providerDiscoveredModels.value,
      )
      if (Object.keys(routeConfig.value.exact_models || {}).length === 0) {
        setMessage(
          'msg-warning',
          t('routeDetail.sourceProviderNoModels', { name: sourceProviderName.value }),
        )
      }
      return
    }
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
    return
  }

  detail.value = await fetchRouteDetail(effectivePrefix.value)
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
  () => [props.prefix, props.create, route.query.provider],
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

.prompt-text {
  margin: 0;
  white-space: pre-wrap;
  font-size: 13px;
  max-height: 200px;
  overflow-y: auto;
}

@media (max-width: 768px) {
  .section-head,
  .editor-actions {
    flex-direction: column;
  }

  .editor-grid {
    grid-template-columns: 1fr;
  }
}
</style>
