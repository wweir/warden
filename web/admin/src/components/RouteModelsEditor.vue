<template>
  <div class="route-models-editor">
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

      <div v-for="entry in exactEntries" :key="entry.id" class="model-card">
        <div class="model-head">
          <code>{{ entry.name }}</code>
          <button class="btn btn-danger btn-sm" type="button" @click="removeExactModel(entry.id)">
            {{ $t('common.delete') }}
          </button>
        </div>

        <div class="model-layout">
          <div class="model-column">
            <div class="field-row">
              <label class="field-label">{{ $t('routeDetail.modelName') }}</label>
              <input
                :value="entry.name"
                class="form-input"
                spellcheck="false"
                placeholder="gpt-4o-mini"
                @change="renameExactModel(entry.id, $event.target.value)"
              />
              <span class="field-hint">{{ $t('routeDetail.modelNameHint') }}</span>
            </div>

            <div class="prompt-toggle">
              <label class="checkbox-row">
                <input
                  :checked="isExactPromptEnabled(entry)"
                  class="form-checkbox"
                  type="checkbox"
                  @change="toggleExactPrompt(entry.id, $event.target.checked)"
                />
                <span class="field-label">{{ $t('routeDetail.promptToggleLabel') }}</span>
              </label>
              <span class="field-hint">{{ $t('routeDetail.promptToggleHint') }}</span>
            </div>

            <div v-if="isExactPromptEnabled(entry)" class="field-row">
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
                rows="4"
                spellcheck="false"
                :placeholder="$t('routeDetail.systemPromptPlaceholder')"
                @input="updateExactField(entry.id, 'systemPrompt', $event.target.value)"
              ></textarea>
            </div>
          </div>

          <div class="model-column model-column-upstreams">
            <div class="subsection-head">
              <div>
                <label class="field-label">{{ $t('routeDetail.upstreamsCol') }}</label>
                <div class="field-hint">{{ $t('routeDetail.upstreamsHint') }}</div>
              </div>
              <button class="btn btn-secondary btn-sm" type="button" @click="addUpstream(entry.id)">
                {{ $t('routeDetail.addUpstream') }}
              </button>
            </div>

            <div v-if="entry.upstreams.length === 0" class="editor-empty editor-empty-compact">
              {{ $t('routeDetail.noUpstreams') }}
            </div>

            <div
              v-for="(upstream, idx) in entry.upstreams"
              :key="`${entry.name}/${idx}`"
              class="upstream-row"
            >
              <div class="upstream-priority">
                <span class="priority-chip">{{ $t('routeDetail.priorityValue', { n: idx + 1 }) }}</span>
              </div>
              <select
                :value="upstream.provider"
                class="form-input"
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
              <ModelCombobox
                :model-value="upstream.model"
                :models="providerModelOptions(upstream.provider, upstream.model)"
                :placeholder="upstreamModelPlaceholder(entry.name, upstream.provider)"
                input-class="upstream-model-input"
                @update:modelValue="updateUpstreamModel(entry.id, idx, $event)"
              />
              <div class="upstream-actions">
                <button
                  class="btn btn-secondary btn-sm"
                  type="button"
                  :disabled="idx === 0"
                  :title="$t('common.moveUp')"
                  :aria-label="$t('common.moveUp')"
                  @click="moveUpstream(entry.id, idx, -1)"
                >
                  Up
                </button>
                <button
                  class="btn btn-secondary btn-sm"
                  type="button"
                  :disabled="idx === entry.upstreams.length - 1"
                  :title="$t('common.moveDown')"
                  :aria-label="$t('common.moveDown')"
                  @click="moveUpstream(entry.id, idx, 1)"
                >
                  Down
                </button>
              </div>
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
        <div class="model-head">
          <code>{{ entry.pattern }}</code>
          <button
            class="btn btn-danger btn-sm"
            type="button"
            @click="removeWildcardModel(entry.pattern)"
          >
            {{ $t('common.delete') }}
          </button>
        </div>

        <div class="model-layout">
          <div class="model-column">
            <div class="field-row">
              <label class="field-label">{{ $t('routeDetail.patternCol') }}</label>
              <input
                :value="entry.pattern"
                class="form-input"
                spellcheck="false"
                placeholder="gpt-*"
                @change="renameWildcardModel(entry.pattern, $event.target.value)"
              />
              <span class="field-hint">{{ $t('routeDetail.patternHint') }}</span>
            </div>

            <div class="prompt-toggle">
              <label class="checkbox-row">
                <input
                  :checked="isWildcardPromptEnabled(entry)"
                  class="form-checkbox"
                  type="checkbox"
                  @change="toggleWildcardPrompt(entry.pattern, $event.target.checked)"
                />
                <span class="field-label">{{ $t('routeDetail.promptToggleLabel') }}</span>
              </label>
              <span class="field-hint">{{ $t('routeDetail.promptToggleHint') }}</span>
            </div>

            <div v-if="isWildcardPromptEnabled(entry)" class="field-row">
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
                rows="4"
                spellcheck="false"
                :placeholder="$t('routeDetail.systemPromptPlaceholder')"
                @input="updateWildcardField(entry.pattern, 'systemPrompt', $event.target.value)"
              ></textarea>
            </div>
          </div>

          <div class="model-column model-column-upstreams">
            <div class="column-head">
              <div class="column-title">{{ $t('routeDetail.providersCol') }}</div>
              <p class="column-desc">{{ $t('routeDetail.wildcardProvidersHint') }}</p>
            </div>

            <div class="field-row">
              <TagListEditor
                :model-value="entry.providers"
                :suggestions="providerOptions()"
                :allow-reorder="true"
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
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelCombobox from './ModelCombobox.vue'
import TagListEditor from './TagListEditor.vue'

const props = defineProps({
  exactModels: { type: Object, default: () => ({}) },
  wildcardModels: { type: Object, default: () => ({}) },
  providerMap: { type: Object, default: () => ({}) },
  providerModelMap: { type: Object, default: () => ({}) },
  routeProtocol: { type: String, default: '' },
})

const emit = defineEmits(['update:exactModels', 'update:wildcardModels'])
const { t } = useI18n()

const exactEntries = ref([])
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

function serializeExactEntries(entries) {
  const out = {}
  for (const entry of entries || []) {
    const name = normalizeText(entry.name)
    if (!name) continue
    out[name] = { prompt_enabled: !!entry.promptEnabled }
    if (entry.promptEnabled && entry.systemPrompt) out[name].system_prompt = entry.systemPrompt
    const upstreams = (entry.upstreams || [])
      .map((upstream) => ({
        provider: normalizeText(upstream.provider),
        model: normalizeText(upstream.model),
      }))
      .filter((upstream) => upstream.provider || upstream.model)
    if (upstreams.length > 0) out[name].upstreams = upstreams
  }
  return out
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

function supportedRouteProtocols(providerProtocol) {
  if (providerProtocol === 'anthropic') return ['anthropic']
  if (['openai', 'qwen', 'copilot'].includes(providerProtocol)) return ['chat', 'responses']
  return []
}

function providerSupportsRouteProtocol(providerProtocol, routeProtocol) {
  if (!routeProtocol) return true
  return supportedRouteProtocols(providerProtocol).includes(routeProtocol)
}

function providerOptions(currentProvider = '') {
  const candidates = Object.entries(props.providerMap || {})
    .filter(([, provider]) =>
      providerSupportsRouteProtocol(provider?.protocol || 'openai', props.routeProtocol),
    )
    .map(([name]) => name)
  return sortValues(dedupeNonEmpty([...candidates, currentProvider]))
}

function defaultProvider() {
  return providerOptions()[0] || sortValues(Object.keys(props.providerMap || {}))[0] || ''
}

function availableProviderModels(providerName) {
  return props.providerModelMap?.[providerName] || props.providerMap?.[providerName]?.models || []
}

function providerModelOptions(providerName, currentModel = '') {
  return sortValues(dedupeNonEmpty([...(availableProviderModels(providerName) || []), currentModel]))
}

function isExactPromptEnabled(entry) {
  return !!entry?.promptEnabled
}

function isWildcardPromptEnabled(entry) {
  return !!entry?.promptEnabled
}

function upstreamModelPlaceholder(publicModelName, providerName) {
  if (!providerName) return t('routeDetail.selectProviderFirst')
  if (availableProviderModels(providerName).length > 0) {
    return t('routeDetail.upstreamModelPlaceholder')
  }
  return publicModelName
}

function emitExact(nextEntries) {
  exactEntries.value = nextEntries
  exactEmitPending = true
  emit('update:exactModels', serializeExactEntries(nextEntries))
}

function emitWildcard(nextEntries) {
  const out = {}
  for (const entry of nextEntries) {
    const pattern = normalizeText(entry.pattern)
    if (!pattern) continue
    out[pattern] = { prompt_enabled: !!entry.promptEnabled }
    if (entry.promptEnabled && entry.systemPrompt) out[pattern].system_prompt = entry.systemPrompt
    const providers = dedupeNonEmpty(entry.providers)
    if (providers.length > 0) out[pattern].providers = providers
  }
  emit('update:wildcardModels', out)
}

function cloneExactEntries() {
  return exactEntries.value.map((entry) => ({
    id: entry.id,
    name: entry.name,
    promptEnabled: !!entry.promptEnabled,
    systemPrompt: entry.systemPrompt,
    upstreams: entry.upstreams.map((upstream) => ({ ...upstream })),
  }))
}

function cloneWildcardEntries() {
  return wildcardEntries.value.map((entry) => ({
    pattern: entry.pattern,
    promptEnabled: !!entry.promptEnabled,
    systemPrompt: entry.systemPrompt,
    providers: [...entry.providers],
  }))
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
      promptEnabled: false,
      systemPrompt: '',
      upstreams: [{ provider: defaultProvider(), model: '' }],
    },
  ])
}

function addWildcardModel() {
  const provider = defaultProvider()
  emitWildcard([
    ...cloneWildcardEntries(),
    {
      pattern: uniqueWildcardPattern(),
      promptEnabled: false,
      systemPrompt: '',
      providers: provider ? [provider] : [],
    },
  ])
}

function renameExactModel(entryID, nextName) {
  const normalized = normalizeText(nextName)
  const currentEntry = exactEntries.value.find((entry) => entry.id === entryID)
  if (!currentEntry) return
  if (normalized === currentEntry.name) return
  if (
    normalized &&
    exactEntries.value.some((entry) => entry.name === normalized && entry.id !== entryID)
  ) {
    return
  }
  const nextEntries = cloneExactEntries().map((entry) => {
    if (entry.id !== entryID) return entry
    return {
      ...entry,
      name: normalized,
      upstreams: entry.upstreams.map((upstream) => ({
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

function updateWildcardProviders(pattern, providers) {
  emitWildcard(
    cloneWildcardEntries().map((entry) =>
      entry.pattern === pattern ? { ...entry, providers: [...providers] } : entry,
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
    cloneExactEntries().map((entry) =>
      entry.id === entryID
        ? {
            ...entry,
            upstreams: [
              ...entry.upstreams,
              { provider: defaultProvider(), model: '' },
            ],
          }
        : entry,
    ),
  )
}

function moveUpstream(entryID, idx, delta) {
  emitExact(
    cloneExactEntries().map((entry) => {
      if (entry.id !== entryID) return entry
      return {
        ...entry,
        upstreams: moveArrayItem(entry.upstreams, idx, idx + delta),
      }
    }),
  )
}

function updateUpstreamProvider(entryID, idx, provider) {
  emitExact(
    cloneExactEntries().map((entry) => {
      if (entry.id !== entryID) return entry
      return {
        ...entry,
        upstreams: entry.upstreams.map((upstream, upstreamIdx) =>
          upstreamIdx === idx
            ? { provider, model: upstream.model }
            : upstream,
        ),
      }
    }),
  )
}

function updateUpstreamModel(entryID, idx, model) {
  emitExact(
    cloneExactEntries().map((entry) => {
      if (entry.id !== entryID) return entry
      return {
        ...entry,
        upstreams: entry.upstreams.map((upstream, upstreamIdx) =>
          upstreamIdx === idx ? { ...upstream, model } : upstream,
        ),
      }
    }),
  )
}

function removeUpstream(entryID, idx) {
  emitExact(
    cloneExactEntries().map((entry) => {
      if (entry.id !== entryID) return entry
      return {
        ...entry,
        upstreams: entry.upstreams.filter((_, upstreamIdx) => upstreamIdx !== idx),
      }
    }),
  )
}

</script>

<style scoped>
.route-models-editor {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.model-group {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.group-head,
.model-head,
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
  margin-top: 4px;
  font-size: 12px;
  color: var(--c-text-3);
  line-height: 1.6;
}

.model-card {
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 14px;
  background: linear-gradient(180deg, #fff 0%, #f8fbff 100%);
}

.model-layout {
  display: grid;
  grid-template-columns: minmax(0, 320px) minmax(0, 1fr);
  gap: 16px;
  margin-top: 12px;
}

.model-column {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.model-column-upstreams {
  min-width: 0;
}

.editor-empty {
  border: 1px dashed var(--c-border);
  border-radius: var(--radius-sm);
  padding: 12px;
  color: var(--c-text-3);
  font-size: 13px;
  background: #f8fafc;
}

.editor-empty-compact {
  padding: 10px 12px;
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

.prompt-input {
  min-height: 92px;
  resize: vertical;
}

.prompt-toggle {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: rgba(255, 255, 255, 0.7);
}

.checkbox-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.form-checkbox {
  width: 16px;
  height: 16px;
  margin: 0;
}

.subsection-head {
  margin-top: 0;
}

.upstream-row {
  display: grid;
  grid-template-columns: auto minmax(0, 220px) minmax(0, 1fr) auto auto;
  gap: 8px;
  align-items: center;
  margin-top: 8px;
}

.upstream-priority {
  display: flex;
  align-items: center;
  justify-content: center;
}

.priority-chip {
  min-width: 40px;
  padding: 6px 8px;
  border-radius: 999px;
  background: var(--c-primary-bg);
  color: var(--c-primary);
  font-size: 12px;
  font-weight: 700;
  text-align: center;
}

.upstream-actions {
  display: flex;
  gap: 6px;
}

.upstream-delete {
  color: var(--c-danger);
  font-size: 16px;
  padding: 6px 8px;
}

@media (max-width: 768px) {
  .group-head,
  .model-head,
  .subsection-head,
  .field-headline {
    flex-direction: column;
    align-items: stretch;
  }

  .model-layout,
  .upstream-row {
    grid-template-columns: 1fr;
  }
}
</style>
