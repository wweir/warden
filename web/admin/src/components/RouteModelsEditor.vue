<template>
  <div class="route-models-editor">
    <div class="protocol-banner">
      <div class="protocol-banner-main">
        <span class="field-label">{{ $t('routeDetail.protocolLockedLabel') }}</span>
        <code>{{ routeProtocol || '-' }}</code>
      </div>
      <span class="field-hint">{{ $t('routeDetail.protocolLockedHint') }}</span>
    </div>

    <section class="model-group">
      <div class="group-head">
        <div>
          <div class="group-title">{{ $t('routeDetail.exactModelsEditor') }}</div>
          <p class="group-desc">{{ $t('routeDetail.exactModelsEditorDesc') }}</p>
        </div>
        <button class="btn btn-primary btn-sm" type="button" @click="addExactModel">
          {{ $t('routeDetail.addExactModel') }}
        </button>
      </div>

      <div v-if="exactEntries.length === 0" class="editor-empty">
        {{ $t('routeDetail.noExactModelsEditor') }}
      </div>

      <div v-for="entry in exactEntries" :key="entry.id" class="model-card exact-model-card">
        <div class="exact-card-head">
          <div class="field-row exact-model-name">
            <label class="field-label exact-model-label">{{ $t('routeDetail.modelName') }}</label>
            <div class="exact-model-input-row">
              <input
                :ref="(el) => setExactInputRef(entry.id, el)"
                :value="entry.name"
                class="form-input"
                spellcheck="false"
                placeholder="gpt-4o-mini"
                @input="renameExactModel(entry.id, $event.target.value)"
              />
              <label class="checkbox-row exact-inline-toggle">
                <input
                  :checked="!!entry.promptEnabled"
                  class="form-checkbox"
                  type="checkbox"
                  @change="toggleExactPrompt(entry.id, $event.target.checked)"
                />
                <span class="field-label">{{ $t('routeDetail.promptToggleLabel') }}</span>
              </label>
            </div>
            <span class="field-hint">{{ $t('routeDetail.modelNameHint') }}</span>
          </div>
          <div class="exact-card-actions">
            <span class="meta-chip">{{ $t('routeDetail.upstreamsCol') }} {{ entry.upstreams.length }}</span>
            <span class="meta-chip" :class="entry.promptEnabled ? 'meta-chip-active' : ''">
              {{ entry.promptEnabled ? $t('common.enabled') : $t('common.disabled') }}
            </span>
            <button class="btn btn-danger btn-sm" type="button" @click="removeExactModel(entry.id)">
              {{ $t('common.delete') }}
            </button>
          </div>
        </div>

        <div class="exact-card-body">
          <section v-if="entry.promptEnabled" class="exact-section exact-prompt-section">
            <div class="field-row prompt-shell">
              <div class="field-headline">
                <label class="field-label">{{ $t('routeDetail.promptCol') }}</label>
                <button
                  v-if="entry.systemPrompt"
                  class="btn btn-secondary btn-sm"
                  type="button"
                  @click="updateExactField(entry.id, 'systemPrompt', '')"
                >
                  {{ $t('config.clearField') }}
                </button>
              </div>
              <textarea
                :value="entry.systemPrompt"
                class="form-input prompt-input"
                rows="3"
                spellcheck="false"
                :placeholder="$t('routeDetail.systemPromptPlaceholder')"
                @input="updateExactField(entry.id, 'systemPrompt', $event.target.value)"
              ></textarea>
            </div>
          </section>

          <section class="exact-section exact-upstreams-section">
            <div class="subsection-head exact-section-head">
              <div>
                <label class="field-label">{{ $t('routeDetail.upstreamsCol') }}</label>
                <div class="field-hint">{{ upstreamHint }}</div>
              </div>
              <button
                class="btn btn-secondary btn-sm"
                type="button"
                :disabled="isStatefulRoute && entry.upstreams.length > 0"
                @click="addUpstream(entry.id)"
              >
                {{ $t('routeDetail.addUpstream') }}
              </button>
            </div>

            <div v-if="entry.upstreams.length === 0" class="editor-empty editor-empty-compact">
              {{ $t('routeDetail.noUpstreams') }}
            </div>

            <div v-else class="exact-upstream-list">
              <div class="exact-upstream-list-head" aria-hidden="true">
                <span>{{ $t('routeDetail.priorityCol') }}</span>
                <span>{{ $t('routeDetail.providersCol') }}</span>
                <span>{{ $t('routeDetail.modelCol') }}</span>
                <span class="exact-upstream-actions-label">{{ $t('common.actions') }}</span>
              </div>
              <div
                v-for="(upstream, idx) in entry.upstreams"
                :key="`${entry.id}/${idx}`"
                class="exact-upstream-item"
              >
                <span class="priority-chip">{{ $t('routeDetail.priorityValue', { n: idx + 1 }) }}</span>

                <div class="exact-upstream-field">
                  <span class="sr-only">{{ $t('routeDetail.providersCol') }}</span>
                  <select
                    :value="upstream.provider"
                    class="form-input"
                    :aria-label="$t('routeDetail.providersCol')"
                    @change="updateUpstreamProvider(entry.id, idx, $event.target.value)"
                  >
                    <option value="">{{ $t('routeDetail.selectProvider') }}</option>
                    <option
                      v-for="provider in providerOptions(upstream.provider)"
                      :key="provider"
                      :value="provider"
                    >
                      {{ provider }}
                    </option>
                  </select>
                </div>

                <div class="exact-upstream-field">
                  <span class="sr-only">{{ $t('routeDetail.modelCol') }}</span>
                  <ModelCombobox
                    :model-value="upstream.model"
                    :models="providerModelOptions(upstream.provider, upstream.model)"
                    :placeholder="upstreamModelPlaceholder(entry.name, upstream.provider)"
                    :aria-label="$t('routeDetail.modelCol')"
                    input-class="upstream-model-input"
                    @update:modelValue="updateUpstreamModel(entry.id, idx, $event)"
                  />
                </div>

                <div class="exact-upstream-actions">
                  <button
                    class="btn btn-secondary btn-sm"
                    type="button"
                    :disabled="idx === 0 || isStatefulRoute"
                    @click="moveUpstream(entry.id, idx, -1)"
                  >
                    {{ $t('common.moveUp') }}
                  </button>
                  <button
                    class="btn btn-secondary btn-sm"
                    type="button"
                    :disabled="idx === entry.upstreams.length - 1 || isStatefulRoute"
                    @click="moveUpstream(entry.id, idx, 1)"
                  >
                    {{ $t('common.moveDown') }}
                  </button>
                  <button
                    class="btn-icon upstream-delete"
                    type="button"
                    :title="$t('common.delete')"
                    :aria-label="$t('common.delete')"
                    @click="removeUpstream(entry.id, idx)"
                  >
                    &times;
                  </button>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>

    </section>

    <section class="model-group">
      <div class="group-head">
        <div>
          <div class="group-title">{{ $t('routeDetail.wildcardModelsEditor') }}</div>
          <p class="group-desc">{{ $t('routeDetail.wildcardModelsEditorDesc') }}</p>
        </div>
        <button class="btn btn-secondary btn-sm" type="button" @click="addWildcardModel">
          {{ $t('routeDetail.addWildcardModel') }}
        </button>
      </div>

      <div v-if="wildcardEntries.length === 0" class="editor-empty">
        {{ $t('routeDetail.noWildcardModelsEditor') }}
      </div>

      <div v-for="entry in wildcardEntries" :key="entry.pattern" class="model-card">
        <div class="model-toolbar">
          <div class="field-row model-identity">
            <label class="field-label">{{ $t('routeDetail.patternCol') }}</label>
            <input
              :value="entry.pattern"
              class="form-input"
              spellcheck="false"
              placeholder="gpt-*"
              @input="renameWildcardModel(entry.pattern, $event.target.value)"
            />
          </div>
          <div class="model-toolbar-side">
            <span class="meta-chip">{{ $t('routeDetail.providersCol') }} {{ entry.providers.length }}</span>
            <span class="meta-chip" :class="entry.promptEnabled ? 'meta-chip-active' : ''">
              {{ entry.promptEnabled ? $t('common.enabled') : $t('common.disabled') }}
            </span>
            <button class="btn btn-danger btn-sm" type="button" @click="removeWildcardModel(entry.pattern)">
              {{ $t('common.delete') }}
            </button>
          </div>
        </div>

        <div class="model-body">
          <div class="model-column">
            <span class="field-hint">{{ $t('routeDetail.patternHint') }}</span>
            <div class="prompt-toggle">
              <label class="checkbox-row">
                <input
                  :checked="!!entry.promptEnabled"
                  class="form-checkbox"
                  type="checkbox"
                  @change="toggleWildcardPrompt(entry.pattern, $event.target.checked)"
                />
                <span class="field-label">{{ $t('routeDetail.promptToggleLabel') }}</span>
              </label>
              <span class="field-hint">{{ $t('routeDetail.promptToggleHint') }}</span>
            </div>

            <div v-if="entry.promptEnabled" class="field-row prompt-shell">
              <div class="field-headline">
                <label class="field-label">{{ $t('routeDetail.promptCol') }}</label>
                <button
                  v-if="entry.systemPrompt"
                  class="btn btn-secondary btn-sm"
                  type="button"
                  @click="updateWildcardField(entry.pattern, 'systemPrompt', '')"
                >
                  {{ $t('config.clearField') }}
                </button>
              </div>
              <textarea
                :value="entry.systemPrompt"
                class="form-input prompt-input"
                rows="3"
                spellcheck="false"
                :placeholder="$t('routeDetail.systemPromptPlaceholder')"
                @input="updateWildcardField(entry.pattern, 'systemPrompt', $event.target.value)"
              ></textarea>
            </div>
          </div>

          <div class="model-column model-column-upstreams">
            <div class="subsection-head">
              <div>
                <label class="field-label">{{ $t('routeDetail.providersCol') }}</label>
                <div class="field-hint">{{ wildcardHint }}</div>
              </div>
            </div>
            <div class="field-row">
              <TagListEditor
                :model-value="entry.providers"
                :suggestions="providerOptions()"
                :allow-reorder="!isStatefulRoute"
                :placeholder="$t('routeDetail.providersPlaceholder')"
                @update:modelValue="updateWildcardProviders(entry.pattern, $event)"
              />
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelCombobox from './ModelCombobox.vue'
import TagListEditor from './TagListEditor.vue'

const props = defineProps({
  routeProtocol: { type: String, default: 'chat' },
  exactModels: { type: Object, default: () => ({}) },
  wildcardModels: { type: Object, default: () => ({}) },
  providerMap: { type: Object, default: () => ({}) },
  providerModelMap: { type: Object, default: () => ({}) },
})

const emit = defineEmits(['update:exactModels', 'update:wildcardModels'])
const { t } = useI18n()

const isStatefulRoute = computed(() => props.routeProtocol === 'responses_stateful')
const upstreamHint = computed(() =>
  isStatefulRoute.value ? t('routeDetail.upstreamsHintSingle') : t('routeDetail.upstreamsHint'),
)
const wildcardHint = computed(() =>
  isStatefulRoute.value ? t('routeDetail.wildcardProvidersHintSingle') : t('routeDetail.wildcardProvidersHint'),
)

const exactEntries = ref([])
const exactInputRefs = new Map()
let nextExactEntryID = 0
let exactEmitPending = false

const wildcardEntries = computed(() =>
  Object.entries(props.wildcardModels || {}).map(([pattern, cfg]) => ({
    pattern,
    promptEnabled: cfg?.prompt_enabled ?? !!cfg?.system_prompt,
    systemPrompt: cfg?.system_prompt || '',
    providers: [...(cfg?.providers || [])],
  })),
)

function normalizeText(value) {
  return String(value || '').trim()
}

function sortValues(values) {
  return [...values].sort((a, b) => a.localeCompare(b))
}

function dedupeNonEmpty(values) {
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

function supportedRouteProtocols(provider) {
  const family = String(provider?.family || provider?.protocol || '').trim().toLowerCase()
  let protocols = []
  if (family === 'anthropic') protocols = ['chat', 'anthropic']
  else if (family === 'openai') protocols = ['chat', 'responses_stateless', 'responses_stateful']
  else if (['qwen', 'copilot', 'ollama'].includes(family)) protocols = ['chat']

  const enabled = new Set((provider?.enabled_protocols || []).map(value => normalizeText(value)))
  const disabled = new Set((provider?.disabled_protocols || []).map(value => normalizeText(value)))
  return protocols.filter(protocol => (enabled.size === 0 || enabled.has(protocol)) && !disabled.has(protocol))
}

function providerOptions(currentProvider = '') {
  const candidates = Object.entries(props.providerMap || {})
    .filter(([, provider]) =>
      supportedRouteProtocols(provider).includes(props.routeProtocol),
    )
    .map(([name]) => name)
  return sortValues(dedupeNonEmpty([...candidates, currentProvider]))
}

function defaultProvider() {
  return providerOptions()[0] || ''
}

function availableProviderModels(providerName) {
  return props.providerModelMap?.[providerName] || props.providerMap?.[providerName]?.models || []
}

function providerModelOptions(providerName, currentModel = '') {
  return sortValues(dedupeNonEmpty([...(availableProviderModels(providerName) || []), currentModel]))
}

function upstreamModelPlaceholder(publicModelName, providerName) {
  if (!providerName) return t('routeDetail.selectProviderFirst')
  if (availableProviderModels(providerName).length > 0) {
    return t('routeDetail.upstreamModelPlaceholder')
  }
  return publicModelName
}

function defaultExactConfig(publicModelName = '') {
  const provider = defaultProvider()
  return {
    promptEnabled: false,
    systemPrompt: '',
    upstreams: provider ? [{ provider, model: publicModelName || '' }] : [],
  }
}

function defaultWildcardConfig() {
  const provider = defaultProvider()
  return {
    promptEnabled: false,
    systemPrompt: '',
    providers: provider ? [provider] : [],
  }
}

function serializeExactEntries(entries) {
  const out = {}
  for (const entry of entries || []) {
    const name = normalizeText(entry.name)
    if (!name) continue
    const nextCfg = {}
    if (entry?.promptEnabled) {
      nextCfg.prompt_enabled = true
      if (entry?.systemPrompt) nextCfg.system_prompt = entry.systemPrompt
    }
    const upstreams = (entry?.upstreams || [])
      .map((upstream) => ({
        provider: normalizeText(upstream.provider),
        model: normalizeText(upstream.model),
      }))
      .filter((upstream) => upstream.provider || upstream.model)
    if (upstreams.length > 0) {
      nextCfg.upstreams = isStatefulRoute.value ? upstreams.slice(0, 1) : upstreams
    }
    out[name] = nextCfg
  }
  return out
}

function serializeWildcardEntries(entries) {
  const out = {}
  for (const entry of entries || []) {
    const pattern = normalizeText(entry.pattern)
    if (!pattern) continue
    const nextCfg = {}
    if (entry?.promptEnabled) {
      nextCfg.prompt_enabled = true
      if (entry?.systemPrompt) nextCfg.system_prompt = entry.systemPrompt
    }
    const providers = isStatefulRoute.value
      ? dedupeNonEmpty(entry?.providers || []).slice(0, 1)
      : dedupeNonEmpty(entry?.providers || [])
    if (providers.length > 0) nextCfg.providers = providers
    out[pattern] = nextCfg
  }
  return out
}

function buildExactEntries(exactModels) {
  return Object.entries(exactModels || {}).map(([name, cfg]) => ({
    id: `exact-${nextExactEntryID += 1}`,
    name,
    promptEnabled: cfg?.prompt_enabled ?? !!cfg?.system_prompt,
    systemPrompt: cfg?.system_prompt || '',
    upstreams: (cfg?.upstreams || []).map((upstream) => ({
      provider: upstream?.provider || '',
      model: upstream?.model || '',
    })),
  }))
}

watch(
  () => props.exactModels,
  (nextModels) => {
    const nextEntries = buildExactEntries(nextModels)
    const serializedLocal = JSON.stringify(serializeExactEntries(exactEntries.value))
    const serializedNext = JSON.stringify(serializeExactEntries(nextEntries))
    if (exactEmitPending && serializedLocal === serializedNext) {
      exactEmitPending = false
      return
    }
    exactEmitPending = false
    exactEntries.value = nextEntries
  },
  { immediate: true, deep: true },
)

watch(
  () => props.routeProtocol,
  () => {
    const nextExactEntries = buildExactEntries(props.exactModels)
    emitExact(
      nextExactEntries.map((entry) => ({
        ...entry,
        upstreams: isStatefulRoute.value ? (entry.upstreams || []).slice(0, 1) : entry.upstreams,
      })),
    )
    emitWildcard(
      cloneWildcardEntries().map((entry) => ({
        ...entry,
        providers: isStatefulRoute.value ? dedupeNonEmpty(entry.providers || []).slice(0, 1) : entry.providers,
      })),
    )
  },
)

function emitExact(nextEntries) {
  exactEntries.value = nextEntries
  exactEmitPending = true
  emit('update:exactModels', serializeExactEntries(nextEntries))
}

function emitWildcard(nextEntries) {
  emit('update:wildcardModels', serializeWildcardEntries(nextEntries))
}

function cloneExactEntries() {
  return exactEntries.value.map((entry) => ({
    id: entry.id,
    name: entry.name,
    promptEnabled: entry.promptEnabled,
    systemPrompt: entry.systemPrompt,
    upstreams: JSON.parse(JSON.stringify(entry.upstreams || [])),
  }))
}

function cloneWildcardEntries() {
  return wildcardEntries.value.map((entry) => ({
    pattern: entry.pattern,
    promptEnabled: entry.promptEnabled,
    systemPrompt: entry.systemPrompt,
    providers: [...(entry.providers || [])],
  }))
}

function setExactInputRef(entryID, el) {
  if (el) {
    exactInputRefs.set(entryID, el)
    return
  }
  exactInputRefs.delete(entryID)
}

function moveArrayItem(list, from, to) {
  if (to < 0 || to >= list.length || from === to) return list
  const next = [...list]
  const [moved] = next.splice(from, 1)
  next.splice(to, 0, moved)
  return next
}

function uniqueWildcardPattern() {
  const taken = new Set(wildcardEntries.value.map((entry) => entry.pattern))
  let idx = 1
  let next = 'model-*'
  while (taken.has(next)) {
    idx += 1
    next = `model-${idx}-*`
  }
  return next
}

function addExactModel() {
  emitExact([
    ...cloneExactEntries(),
    {
      id: `exact-${nextExactEntryID += 1}`,
      name: '',
      ...defaultExactConfig(),
    },
  ])
}

function addWildcardModel() {
  emitWildcard([
    ...cloneWildcardEntries(),
    {
      pattern: uniqueWildcardPattern(),
      ...defaultWildcardConfig(),
    },
  ])
}

function renameExactModel(entryID, nextName) {
  const normalized = normalizeText(nextName)
  const currentEntry = exactEntries.value.find((entry) => entry.id === entryID)
  if (!currentEntry) return
  if (normalized === currentEntry.name) return
  if (normalized && exactEntries.value.some((entry) => entry.name === normalized && entry.id !== entryID)) return

  const nextEntries = cloneExactEntries().map((entry) => {
    if (entry.id !== entryID) return entry
    return {
      ...entry,
      name: normalized,
      upstreams: (entry.upstreams || []).map((upstream) => ({
        ...upstream,
        model:
          currentEntry.name && (!upstream.model || upstream.model === currentEntry.name)
            ? normalized
            : upstream.model,
      })),
    }
  })
  emitExact(nextEntries)
}

function renameWildcardModel(oldPattern, nextPattern) {
  const normalized = normalizeText(nextPattern)
  if (!normalized || normalized === oldPattern) return
  if (wildcardEntries.value.some((entry) => entry.pattern === normalized && entry.pattern !== oldPattern)) return
  emitWildcard(
    cloneWildcardEntries().map((entry) =>
      entry.pattern === oldPattern ? { ...entry, pattern: normalized } : entry,
    ),
  )
}

function removeExactModel(entryID) {
  emitExact(cloneExactEntries().filter((entry) => entry.id !== entryID))
}

function removeWildcardModel(pattern) {
  emitWildcard(cloneWildcardEntries().filter((entry) => entry.pattern !== pattern))
}

function updateExactField(entryID, field, value) {
  emitExact(
    cloneExactEntries().map((entry) =>
      entry.id === entryID ? { ...entry, [field]: value } : entry,
    ),
  )
}

function updateWildcardField(pattern, field, value) {
  emitWildcard(
    cloneWildcardEntries().map((entry) =>
      entry.pattern === pattern ? { ...entry, [field]: value } : entry,
    ),
  )
}

function toggleExactPrompt(entryID, checked) {
  emitExact(
    cloneExactEntries().map((entry) =>
      entry.id === entryID
        ? { ...entry, promptEnabled: checked, systemPrompt: checked ? entry.systemPrompt : '' }
        : entry,
    ),
  )
}

function toggleWildcardPrompt(pattern, checked) {
  emitWildcard(
    cloneWildcardEntries().map((entry) =>
      entry.pattern === pattern
        ? { ...entry, promptEnabled: checked, systemPrompt: checked ? entry.systemPrompt : '' }
        : entry,
    ),
  )
}

function addUpstream(entryID) {
  emitExact(
    cloneExactEntries().map((entry) => {
      if (entry.id !== entryID) return entry
      const nextUpstreams = isStatefulRoute.value
        ? [{ provider: defaultProvider(), model: entry.name || '' }]
        : [...(entry.upstreams || []), { provider: defaultProvider(), model: '' }]
      return { ...entry, upstreams: nextUpstreams }
    }),
  )
}

function moveUpstream(entryID, idx, delta) {
  if (isStatefulRoute.value) return
  emitExact(
    cloneExactEntries().map((entry) =>
      entry.id === entryID
        ? { ...entry, upstreams: moveArrayItem(entry.upstreams || [], idx, idx + delta) }
        : entry,
    ),
  )
}

function updateUpstreamProvider(entryID, idx, provider) {
  emitExact(
    cloneExactEntries().map((entry) => {
      if (entry.id !== entryID) return entry
      const upstreams = (entry.upstreams || []).map((upstream, upstreamIdx) =>
        upstreamIdx === idx ? { provider, model: upstream.model } : upstream,
      )
      return { ...entry, upstreams: isStatefulRoute.value ? upstreams.slice(0, 1) : upstreams }
    }),
  )
}

function updateUpstreamModel(entryID, idx, model) {
  emitExact(
    cloneExactEntries().map((entry) => {
      if (entry.id !== entryID) return entry
      return {
        ...entry,
        upstreams: (entry.upstreams || []).map((upstream, upstreamIdx) =>
          upstreamIdx === idx ? { ...upstream, model } : upstream,
        ),
      }
    }),
  )
}

function removeUpstream(entryID, idx) {
  emitExact(
    cloneExactEntries().map((entry) =>
      entry.id === entryID
        ? { ...entry, upstreams: (entry.upstreams || []).filter((_, upstreamIdx) => upstreamIdx !== idx) }
        : entry,
    ),
  )
}

function updateWildcardProviders(pattern, providers) {
  emitWildcard(
    cloneWildcardEntries().map((entry) =>
      entry.pattern === pattern
        ? {
            ...entry,
            providers: isStatefulRoute.value
              ? dedupeNonEmpty(providers).slice(0, 1)
              : [...providers],
          }
        : entry,
    ),
  )
}

async function focusExactModel(modelName) {
  const targetName = normalizeText(modelName)
  if (!targetName) return false

  const entry = exactEntries.value.find((item) => item.name === targetName)
  if (!entry) return false

  await nextTick()
  const input = exactInputRefs.get(entry.id)
  if (!input) return false

  input.scrollIntoView({ behavior: 'smooth', block: 'center' })
  input.focus()
  input.select?.()
  return true
}

defineExpose({
  focusExactModel,
})
</script>

<style scoped>
.route-models-editor {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.protocol-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: linear-gradient(180deg, color-mix(in srgb, var(--c-warning-bg) 42%, var(--c-surface)) 0%, var(--c-surface) 100%);
  flex-wrap: wrap;
}

.protocol-banner-main {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.model-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.group-head,
.subsection-head,
.field-headline {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.group-title {
  font-size: 12px;
  font-weight: 700;
  color: var(--c-text-2);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.group-desc {
  margin-top: 2px;
  font-size: 11px;
  color: var(--c-text-3);
  line-height: 1.5;
}

.model-card {
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 12px;
  background: linear-gradient(180deg, var(--c-surface) 0%, var(--c-surface-tint) 100%);
}

.model-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.model-identity {
  flex: 1;
  min-width: 0;
}

.model-toolbar-side {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.meta-chip {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 0 8px;
  border-radius: 999px;
  background: var(--c-accent-soft);
  color: var(--c-accent-text);
  font-size: 11px;
  font-weight: 600;
}

.meta-chip-active {
  background: var(--c-success-soft);
  color: var(--c-success-text);
}

.exact-model-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.exact-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.exact-model-name {
  flex: 1;
  min-width: 0;
}

.exact-model-label {
  min-height: 18px;
}

.exact-inline-toggle {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 36px;
  padding: 0 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-surface-soft);
}

.exact-model-input-row {
  display: flex;
  align-items: stretch;
  gap: 10px;
}

.exact-card-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}

.exact-card-body {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px;
  align-items: start;
}

.exact-section {
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--c-surface) 82%, var(--c-surface-soft));
  padding: 12px;
}

.exact-section-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.exact-upstream-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
}

.exact-upstream-list-head,
.exact-upstream-item {
  display: grid;
  grid-template-columns: auto minmax(148px, 190px) minmax(240px, 1fr) auto;
  align-items: center;
  gap: 8px;
}

.exact-upstream-list-head {
  padding: 0 10px;
  color: var(--c-text-3);
  font-size: 11px;
  font-weight: 600;
}

.exact-upstream-actions-label {
  text-align: right;
}

.exact-upstream-item {
  padding: 8px 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-surface);
}

.exact-upstream-actions {
  display: inline-flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  justify-content: flex-end;
}

.exact-upstream-field {
  min-width: 0;
}

.model-body {
  display: grid;
  grid-template-columns: minmax(240px, 320px) minmax(0, 1fr);
  gap: 14px;
  margin-top: 10px;
}

.model-column {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.model-column-upstreams {
  min-width: 0;
}

.editor-empty {
  border: 1px dashed var(--c-border);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
  color: var(--c-text-3);
  font-size: 12px;
  background: var(--c-surface-soft);
}

.editor-empty-compact {
  padding: 8px 10px;
}

.field-row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-label,
.column-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text-2);
}

.field-hint,
.column-desc {
  font-size: 11px;
  color: var(--c-text-3);
  line-height: 1.5;
}

.prompt-shell {
  padding: 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--c-surface) 88%, var(--c-surface-soft));
}

.prompt-toggle {
  padding: 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--c-surface-soft) 88%, var(--c-surface));
}

.checkbox-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.upstream-row {
  display: grid;
  grid-template-columns: auto minmax(120px, 180px) minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  padding: 8px 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: rgba(255, 255, 255, 0.85);
}

.priority-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 38px;
  height: 28px;
  padding: 0 8px;
  border-radius: 999px;
  background: var(--c-border-light);
  color: var(--c-text-2);
  font-size: 11px;
  font-weight: 700;
}

.upstream-provider {
  min-width: 0;
}

.upstream-actions {
  display: inline-flex;
  gap: 6px;
}

.upstream-delete {
  width: 28px;
  height: 28px;
  padding: 0;
  justify-content: center;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

.prompt-input {
  min-height: 76px;
  resize: vertical;
}

@media (max-width: 900px) {
  .exact-card-body,
  .model-body {
    grid-template-columns: 1fr;
  }

  .exact-upstream-list-head,
  .exact-upstream-item {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }

  .exact-model-input-row {
    flex-direction: column;
  }

  .exact-upstream-field:last-of-type {
    grid-column: 2 / 4;
  }

  .upstream-row {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .upstream-actions,
  .upstream-delete,
  .upstream-provider {
    grid-column: 2;
  }
}

@media (max-width: 640px) {
  .exact-upstream-list-head {
    display: none;
  }

  .exact-card-head,
  .exact-section-head,
  .model-toolbar,
  .group-head,
  .subsection-head,
  .field-headline {
    flex-direction: column;
    align-items: stretch;
  }

  .exact-inline-toggle {
    width: 100%;
    justify-content: flex-start;
  }

  .exact-card-actions,
  .exact-upstream-actions {
    justify-content: flex-start;
  }

  .exact-upstream-item {
    grid-template-columns: minmax(0, 1fr);
    align-items: stretch;
  }

  .exact-upstream-field:last-of-type,
  .exact-upstream-actions {
    grid-column: auto;
  }

  .upstream-row {
    grid-template-columns: 1fr;
  }

  .upstream-actions,
  .upstream-delete,
  .upstream-provider {
    grid-column: auto;
  }
}
</style>
