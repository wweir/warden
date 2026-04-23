<template>
  <div class="route-page">
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

    <section class="route-overview panel">
      <div class="route-overview-main">
        <div class="route-kicker">{{ isCreate ? $t('routeDetail.newRouteTitle') : $t('routeDetail.configEditor') }}</div>
        <div class="route-title-row">
          <h1 class="route-title">
            <code>{{ effectivePrefix || $t('routeDetail.prefixPlaceholder') }}</code>
          </h1>
          <span class="badge badge-ok">{{ routeConfig.protocol || '-' }}</span>
        </div>
        <p class="route-overview-desc">{{ $t('routeDetail.configEditorDesc') }}</p>
      </div>

      <div class="route-overview-stats">
        <div class="overview-stat">
          <span class="overview-label">{{ $t('routeDetail.exactModels') }}</span>
          <strong>{{ routeSummary.exactCount }}</strong>
        </div>
        <div class="overview-stat">
          <span class="overview-label">{{ $t('routeDetail.wildcardModels') }}</span>
          <strong>{{ routeSummary.wildcardCount }}</strong>
        </div>
        <div class="overview-stat">
          <span class="overview-label">{{ $t('routeDetail.providersCol') }}</span>
          <strong>{{ routeSummary.providerCount }}</strong>
        </div>
        <div class="overview-stat">
          <span class="overview-label">{{ $t('routeDetail.requests') }}</span>
          <strong>{{ formatCount(runtimeSummary.totalRequests) }}</strong>
        </div>
        <div class="overview-stat">
          <span class="overview-label">{{ $t('routeDetail.failover') }}</span>
          <strong>{{ formatCount(runtimeSummary.failoverCount) }}</strong>
        </div>
        <div class="overview-stat">
          <span class="overview-label">{{ $t('routeDetail.avgLatency') }}</span>
          <strong>{{ formatLatency(runtimeSummary.avgLatencyMs, runtimeSummary.totalRequests) }}</strong>
        </div>
      </div>
    </section>

    <div class="route-workbench">
      <div class="workbench-main">
        <section v-if="configuredExactModels.length > 0" class="detail-panel panel exact-summary-panel">
          <div class="detail-panel-head">
            <div>
              <h3>{{ $t('routeDetail.exactModels') }}</h3>
              <p class="section-desc">{{ $t('routeDetail.exactModelsEditorDesc') }}</p>
            </div>
            <span class="badge badge-muted">{{ configuredExactModels.length }}</span>
          </div>
          <div class="table-scroll">
            <table class="data-table compact-table">
              <thead>
                <tr>
                  <th>{{ $t('routeDetail.modelCol') }}</th>
                  <th>{{ $t('routeDetail.upstreamsCol') }}</th>
                  <th>{{ $t('routeDetail.promptCol') }}</th>
                  <th class="table-actions-col">{{ $t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in configuredExactModels" :key="model.name">
                  <td><code>{{ model.name }}</code></td>
                  <td>{{ formatTargets(model.targets) }}</td>
                  <td><pre class="prompt-text">{{ model.prompt_enabled ? model.system_prompt || '-' : '-' }}</pre></td>
                  <td class="table-actions-cell">
                    <button
                      class="btn btn-secondary btn-sm"
                      type="button"
                      @click="focusExactModel(model.name)"
                    >
                      {{ $t('common.edit') }}
                    </button>
                    <button
                      class="btn btn-danger btn-sm"
                      type="button"
                      @click="removeExactModel(model.name)"
                    >
                      {{ $t('common.delete') }}
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section class="editor-panel panel">
          <div class="section-head">
            <div>
              <h3>{{ $t('routeDetail.configEditor') }}</h3>
              <p class="section-desc">{{ $t('routeDetail.protocolRouteHint') }}</p>
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
                <option v-for="option in routeProtocolOptions" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
              <span class="field-hint">{{ $t('routeDetail.protocolLockedHint') }}</span>
            </div>
          </div>

          <RouteModelsEditor
            ref="modelsEditorRef"
            :route-protocol="routeConfig.protocol"
            :exact-models="routeConfig.exact_models"
            :wildcard-models="routeConfig.wildcard_models"
            :provider-map="providerMap"
            :provider-model-map="providerModelMap"
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

        <section v-if="configuredWildcardModels.length > 0" class="detail-panel panel">
          <div class="detail-panel-head">
            <div>
              <h3>{{ $t('routeDetail.wildcardModels') }}</h3>
              <p class="section-desc">{{ $t('routeDetail.wildcardModelsEditorDesc') }}</p>
            </div>
            <span class="badge badge-muted">{{ configuredWildcardModels.length }}</span>
          </div>
          <div class="table-scroll">
            <table class="data-table compact-table">
              <thead>
                <tr>
                  <th>{{ $t('routeDetail.patternCol') }}</th>
                  <th>{{ $t('routeDetail.providersCol') }}</th>
                  <th>{{ $t('routeDetail.promptCol') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in configuredWildcardModels" :key="model.pattern || model.name">
                  <td><code>{{ model.pattern || model.name }}</code></td>
                  <td>{{ formatTargets(model.targets) }}</td>
                  <td><pre class="prompt-text">{{ model.prompt_enabled ? model.system_prompt || '-' : '-' }}</pre></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <aside class="detail-rail">
        <section class="rail-panel panel">
          <h3>{{ $t('routeDetail.basicInfo') }}</h3>
          <div class="info-list">
            <div class="info-item">
              <span>{{ $t('routeDetail.prefix') }}</span>
              <code>{{ effectivePrefix || '-' }}</code>
            </div>
            <div class="info-item">
              <span>{{ $t('routeDetail.protocol') }}</span>
              <code>{{ routeConfig.protocol || '-' }}</code>
            </div>
            <div class="info-item">
              <span>{{ $t('routeDetail.hookCount') }}</span>
              <strong>{{ routeSummary.hookCount }}</strong>
            </div>
            <div class="info-item">
              <span>{{ $t('routeDetail.requests') }}</span>
              <strong>{{ formatCount(runtimeSummary.totalRequests) }}</strong>
            </div>
            <div class="info-item">
              <span>{{ $t('routeDetail.avgLatency') }}</span>
              <strong>{{ formatLatency(runtimeSummary.avgLatencyMs, runtimeSummary.totalRequests) }}</strong>
            </div>
          </div>
        </section>

        <section class="rail-panel panel provider-rail-panel">
          <div class="detail-panel-head">
            <div>
              <h3>{{ $t('routeDetail.providers', { n: providerRailEntries.length }) }}</h3>
              <p class="section-desc">{{ $t('routeDetail.statusSummary', { suppressed: runtimeSummary.suppressedCount, degraded: runtimeSummary.degradedCount }) }}</p>
            </div>
            <span class="badge badge-muted">{{ formatCount(runtimeSummary.totalRequests) }}</span>
          </div>
          <div v-if="providerRailEntries.length === 0" class="empty">{{ $t('routeDetail.noProviders') }}</div>
          <div v-else class="provider-rail-list">
            <router-link
              v-for="provider in providerRailEntries"
              :key="provider.name"
              :to="'/providers/' + encodeURIComponent(provider.name)"
              class="provider-card"
            >
              <div class="provider-card-top">
                <span class="provider-card-name">{{ provider.name }}</span>
                <span
                  v-if="provider.runtime?.suppressed"
                  class="badge badge-error"
                >
                  {{ $t('common.suppressed') }}
                </span>
                <span
                  v-else-if="provider.runtime?.consecutive_failures > 0"
                  class="badge badge-warn"
                >
                  {{ $t('routeDetail.failures', { n: provider.runtime.consecutive_failures }) }}
                </span>
                <span v-else-if="provider.runtime" class="badge badge-ok">{{ $t('common.ok') }}</span>
                <span v-else class="badge badge-muted">{{ $t('common.noData') }}</span>
              </div>
              <div class="provider-card-metrics">
                <span>{{ $t('routeDetail.requests') }} {{ formatCount(provider.runtime?.total_requests) }}</span>
                <span>{{ $t('routeDetail.success') }} {{ formatCount(provider.runtime?.success_count) }}</span>
                <span>{{ $t('routeDetail.failure') }} {{ formatCount(provider.runtime?.failure_count) }}</span>
              </div>
              <div class="provider-card-metrics">
                <span>{{ $t('routeDetail.failover') }} {{ formatCount(provider.runtime?.failover_count) }}</span>
                <span>{{ $t('routeDetail.preStream') }} {{ formatCount(provider.runtime?.pre_stream_errors) }}</span>
                <span>{{ $t('routeDetail.inStream') }} {{ formatCount(provider.runtime?.in_stream_errors) }}</span>
              </div>
              <div class="provider-card-footer">
                <span>{{ $t('routeDetail.avgLatency') }} {{ formatLatency(provider.runtime?.avg_latency_ms, provider.runtime?.total_requests) }}</span>
                <span v-if="provider.runtime?.manual_suppressed">{{ $t('providerDetail.manuallySuppressed') }}</span>
              </div>
            </router-link>
          </div>
        </section>
      </aside>
    </div>

  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
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
import { fmtNum, providerRouteProtocols } from '../utils.js'
import RouteModelsEditor from '../components/RouteModelsEditor.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const routeProtocolOptions = [
  { value: 'chat', label: t('routeDetail.protocolChat') },
  { value: 'responses_stateless', label: t('routeDetail.protocolResponsesStateless') },
  { value: 'responses_stateful', label: t('routeDetail.protocolResponsesStateful') },
  { value: 'anthropic', label: t('routeDetail.protocolAnthropic') },
]

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
const modelsEditorRef = ref(null)
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
const sourceProviderProtocol = computed(() => {
  if (!isCreate.value) return ''
  const value = route.query.protocol
  if (Array.isArray(value)) return normalizeText(value[0])
  return normalizeText(value)
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
const routeProviderNames = computed(() => {
  const names = new Set()
  for (const cfg of Object.values(routeConfig.value?.exact_models || {})) {
    for (const upstream of cfg?.upstreams || []) {
      const provider = normalizeText(upstream?.provider)
      if (provider) names.add(provider)
    }
  }
  for (const cfg of Object.values(routeConfig.value?.wildcard_models || {})) {
    for (const provider of cfg?.providers || []) {
      const normalized = normalizeText(provider)
      if (normalized) names.add(normalized)
    }
  }
  return [...names].sort((a, b) => a.localeCompare(b))
})
const runtimeProviders = computed(() => detail.value?.providers || [])
const runtimeProviderMap = computed(() => {
  const out = {}
  for (const provider of runtimeProviders.value) {
    out[provider.name] = provider
  }
  return out
})
const providerRailEntries = computed(() =>
  uniqueSortedTextValues([
    ...routeProviderNames.value,
    ...runtimeProviders.value.map((provider) => provider.name),
  ]).map((name) => ({
    name,
    runtime: runtimeProviderMap.value[name] || null,
  })),
)
const configuredExactModels = computed(() =>
  Object.entries(routeConfig.value?.exact_models || {})
    .map(([name, cfg]) => ({
      name,
      prompt_enabled: !!cfg?.prompt_enabled,
      system_prompt: cfg?.system_prompt || '',
      targets: (cfg?.upstreams || []).map((upstream) => {
        const provider = normalizeText(upstream?.provider)
        const model = normalizeText(upstream?.model) || name
        return provider ? `${provider}:${model}` : model
      }),
    }))
    .sort((a, b) => a.name.localeCompare(b.name)),
)
const configuredWildcardModels = computed(() =>
  Object.entries(routeConfig.value?.wildcard_models || {})
    .map(([pattern, cfg]) => ({
      pattern,
      name: pattern,
      prompt_enabled: !!cfg?.prompt_enabled,
      system_prompt: cfg?.system_prompt || '',
      targets: dedupeOrderedTextValues(cfg?.providers || []),
    }))
    .sort((a, b) => a.pattern.localeCompare(b.pattern)),
)
const runtimeSummary = computed(() => {
  let totalRequests = 0
  let successCount = 0
  let failureCount = 0
  let failoverCount = 0
  let preStreamErrors = 0
  let inStreamErrors = 0
  let weightedLatencyMs = 0
  let suppressedCount = 0
  let degradedCount = 0

  for (const provider of runtimeProviders.value) {
    const requests = Number(provider?.total_requests || 0)
    totalRequests += requests
    successCount += Number(provider?.success_count || 0)
    failureCount += Number(provider?.failure_count || 0)
    failoverCount += Number(provider?.failover_count || 0)
    preStreamErrors += Number(provider?.pre_stream_errors || 0)
    inStreamErrors += Number(provider?.in_stream_errors || 0)
    weightedLatencyMs += Number(provider?.avg_latency_ms || 0) * requests
    if (provider?.suppressed) suppressedCount += 1
    else if (provider?.consecutive_failures > 0) degradedCount += 1
  }

  return {
    totalRequests,
    successCount,
    failureCount,
    failoverCount,
    preStreamErrors,
    inStreamErrors,
    avgLatencyMs: totalRequests > 0 ? weightedLatencyMs / totalRequests : 0,
    suppressedCount,
    degradedCount,
  }
})
const routeSummary = computed(() => ({
  exactCount: Object.keys(routeConfig.value?.exact_models || {}).length,
  wildcardCount: Object.keys(routeConfig.value?.wildcard_models || {}).length,
  providerCount: routeProviderNames.value.length,
  hookCount: detail.value?.hook_count || 0,
}))
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

function dedupeOrderedTextValues(values) {
  const out = []
  const seen = new Set()
  for (const value of values || []) {
    const normalized = normalizeText(value)
    if (!normalized || seen.has(normalized)) continue
    seen.add(normalized)
    out.push(normalized)
  }
  return out
}

function deepClone(value) {
  return JSON.parse(JSON.stringify(value))
}

function supportedRouteProtocols(provider) {
  return providerRouteProtocols(provider)
}

function defaultProviderForProtocol(protocol, providerConfigMap = {}) {
  for (const [name, provider] of Object.entries(providerConfigMap || {})) {
    if (supportedRouteProtocols(provider).includes(protocol)) {
      return name
    }
  }
  return ''
}

function createEmptyRouteConfig(providerConfigMap = {}, protocol = 'chat') {
  const provider = defaultProviderForProtocol(protocol, providerConfigMap)
  return {
    protocol,
    exact_models: {},
    wildcard_models: provider ? { '*': { providers: [provider] } } : {},
  }
}

function createProviderSeededRouteConfig(
  providerName,
  providerConfigMap = {},
  discoveredModelMap = {},
  preferredProtocol = '',
) {
  const provider = providerConfigMap?.[providerName]
  if (!provider) {
    throw new Error(t('routeDetail.sourceProviderMissing', { name: providerName }))
  }

  const supportedProtocols = supportedRouteProtocols(provider)
  const protocol = supportedProtocols.includes(preferredProtocol)
    ? preferredProtocol
    : (supportedProtocols[0] || 'chat')
  const models = uniqueSortedTextValues([
    ...(provider?.models || []),
    ...(discoveredModelMap?.[providerName] || []),
  ])
  const exactModels = {}
  for (const model of models) {
    exactModels[model] = { upstreams: [{ provider: providerName, model }] }
  }

  return {
    protocol,
    exact_models: exactModels,
    wildcard_models: {},
  }
}

function normalizeEditableRoute(route) {
  const protocol = normalizeText(route?.protocol) || 'chat'
  const normalized = createEmptyRouteConfig(providerMap.value, protocol)
  normalized.protocol = protocol
  normalized.exact_models = deepClone(route?.exact_models || {})
  normalized.wildcard_models = deepClone(route?.wildcard_models || {})
  if (
    Object.keys(normalized.exact_models).length === 0 &&
    Object.keys(normalized.wildcard_models).length === 0
  ) {
    const provider = defaultProviderForProtocol(protocol, providerMap.value)
    normalized.wildcard_models = provider ? { '*': { providers: [provider] } } : {}
  }
  return normalized
}

function sanitizeRouteModelsForProtocol(protocol, routeModelConfig = {}) {
  const nextExactModels = {}
  for (const [name, cfg] of Object.entries(routeModelConfig.exact_models || {})) {
    const promptEnabled = !!cfg?.prompt_enabled
    const upstreams = []
    const seenUpstreams = new Set()
    for (const upstream of cfg?.upstreams || []) {
      const nextUpstream = {
        provider: normalizeText(upstream?.provider),
        model: normalizeText(upstream?.model),
      }
      if (!nextUpstream.provider && !nextUpstream.model) continue
      const key = JSON.stringify(nextUpstream)
      if (seenUpstreams.has(key)) continue
      seenUpstreams.add(key)
      upstreams.push(nextUpstream)
    }
    const nextCfg = {
      upstreams: protocol === 'responses_stateful' ? upstreams.slice(0, 1) : upstreams,
    }
    if (promptEnabled) {
      nextCfg.prompt_enabled = true
      if (cfg?.system_prompt) nextCfg.system_prompt = cfg.system_prompt
    }
    nextExactModels[name] = nextCfg
  }

  const nextWildcardModels = {}
  for (const [pattern, cfg] of Object.entries(routeModelConfig.wildcard_models || {})) {
    const promptEnabled = !!cfg?.prompt_enabled
    const providers = dedupeOrderedTextValues(cfg?.providers || [])
    const nextCfg = {
      providers: protocol === 'responses_stateful' ? providers.slice(0, 1) : providers,
    }
    if (promptEnabled) {
      nextCfg.prompt_enabled = true
      if (cfg?.system_prompt) nextCfg.system_prompt = cfg.system_prompt
    }
    nextWildcardModels[pattern] = nextCfg
  }

  return {
    exact_models: nextExactModels,
    wildcard_models: nextWildcardModels,
  }
}

function buildRoutePayload(existingRoute = {}) {
  const nextRoute = deepClone(existingRoute || {})
  nextRoute.protocol = normalizeText(routeConfig.value.protocol) || 'chat'
  const normalized = sanitizeRouteModelsForProtocol(nextRoute.protocol, routeConfig.value)
  nextRoute.exact_models = normalized.exact_models
  nextRoute.wildcard_models = normalized.wildcard_models
  nextRoute.hooks = deepClone(existingRoute?.hooks || [])
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

function formatTargets(targets) {
  return (targets || []).join(', ') || '-'
}

function formatCount(value) {
  return fmtNum(Number(value || 0))
}

function formatLatency(latencyMs, totalRequests) {
  return Number(totalRequests || 0) > 0 ? `${Number(latencyMs || 0).toFixed(0)}ms` : '-'
}

async function focusExactModel(modelName) {
  await nextTick()
  await modelsEditorRef.value?.focusExactModel?.(modelName)
}

function removeExactModel(modelName) {
  const normalized = normalizeText(modelName)
  if (!normalized) return
  if (!window.confirm(t('routeDetail.confirmDeleteExactModel', { name: normalized }))) return

  const nextExactModels = {}
  for (const [name, cfg] of Object.entries(routeConfig.value?.exact_models || {})) {
    if (name === normalized) continue
    nextExactModels[name] = deepClone(cfg)
  }

  routeConfig.value = {
    ...deepClone(routeConfig.value),
    exact_models: nextExactModels,
  }
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
        sourceProviderProtocol.value,
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
  () => [props.prefix, props.create, route.query.provider, route.query.protocol],
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
.route-page {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.route-overview {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(320px, 0.9fr);
  gap: 16px;
  padding: 18px 20px;
  align-items: start;
}

.route-kicker {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--c-text-3);
}

.route-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 8px;
  flex-wrap: wrap;
}

.route-title {
  font-size: 28px;
  line-height: 1.1;
}

.route-title code {
  font-size: inherit;
  padding: 0;
  background: transparent;
}

.route-overview-desc {
  margin-top: 8px;
  max-width: 72ch;
  color: var(--c-text-2);
}

.route-overview-stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.overview-stat {
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 12px 14px;
  background: linear-gradient(180deg, var(--c-surface) 0%, var(--c-surface-soft) 100%);
}

.overview-label {
  display: block;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--c-text-3);
}

.overview-stat strong {
  display: block;
  margin-top: 6px;
  font-size: 24px;
  line-height: 1;
  color: var(--c-text);
}

.route-workbench {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: 18px;
  align-items: start;
}

.workbench-main {
  display: flex;
  flex-direction: column;
  gap: 18px;
  min-width: 0;
}

.editor-panel,
.rail-panel,
.detail-panel {
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

.editor-panel {
  padding: 18px;
}

.detail-rail {
  display: flex;
  flex-direction: column;
  gap: 14px;
  position: sticky;
  top: 18px;
}

.rail-panel,
.detail-panel {
  padding: 16px;
}

.rail-panel h3,
.detail-panel h3 {
  font-size: 14px;
  margin-bottom: 12px;
}

.section-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 16px;
}

.section-desc {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--c-text-2);
  line-height: 1.5;
}

.editor-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 14px;
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

.info-list {
  display: grid;
  gap: 10px;
}

.info-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  font-size: 12px;
  color: var(--c-text-2);
}

.info-item strong {
  color: var(--c-text);
}

.provider-rail-list {
  display: grid;
  gap: 10px;
}

.provider-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--c-surface-soft) 78%, var(--c-surface)) 0%, var(--c-surface) 100%);
  text-decoration: none;
  color: inherit;
  transition: transform var(--transition), box-shadow var(--transition), border-color var(--transition);
}

.provider-card:hover {
  transform: translateY(-1px);
  box-shadow: var(--shadow-md);
  border-color: color-mix(in srgb, var(--c-primary) 24%, var(--c-border));
  text-decoration: none;
}

.provider-card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.provider-card-name {
  min-width: 0;
  font-size: 13px;
  font-weight: 700;
  color: var(--c-text);
}

.provider-card-metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  font-size: 11px;
  color: var(--c-text-2);
}

.provider-card-metrics span,
.provider-card-footer span {
  min-width: 0;
}

.provider-card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  font-size: 11px;
  color: var(--c-text-3);
}

.exact-summary-panel {
  scroll-margin-top: 18px;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
}

.detail-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 12px;
}

.detail-panel-wide {
  grid-column: 1 / -1;
}

.compact-table th,
.compact-table td {
  padding: 8px 10px;
  vertical-align: top;
}

.table-head-metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 84px;
}

.table-head-metric span {
  font-size: 11px;
  line-height: 1.2;
}

.table-head-metric strong {
  font-size: 13px;
  line-height: 1.3;
  color: var(--c-text);
}

.table-actions-col {
  width: 1%;
  white-space: nowrap;
}

.table-actions-cell {
  white-space: nowrap;
}

.table-actions-cell .btn + .btn {
  margin-left: 8px;
}

.table-scroll {
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}

.table-scroll .data-table {
  min-width: 640px;
}

.editor-actions {
  display: flex;
  gap: 10px;
  margin-top: 16px;
}

.prompt-text {
  margin: 0;
  white-space: pre-wrap;
  font-size: 12px;
  line-height: 1.5;
  max-height: 140px;
  overflow-y: auto;
}

@media (max-width: 1100px) {
  .route-overview,
  .route-workbench,
  .detail-grid {
    grid-template-columns: 1fr;
  }

  .detail-rail {
    position: static;
  }
}

@media (max-width: 768px) {
  .section-head,
  .editor-actions,
  .route-title-row {
    flex-direction: column;
    align-items: flex-start;
  }

  .editor-grid {
    grid-template-columns: 1fr;
  }

  .route-overview,
  .editor-panel,
  .rail-panel,
  .detail-panel {
    padding: 14px;
  }

  .route-overview-stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .provider-card-metrics {
    grid-template-columns: 1fr;
  }

  .provider-card-footer {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
