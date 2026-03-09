<template>
  <div>
    <div class="page-title">{{ $t('hooks.title') }}</div>

    <div v-if="saveMsg" :class="['msg', saveMsg.type]">{{ saveMsg.text }}</div>

    <section class="info-section" style="margin-bottom:20px">
      <div class="section-header section-toggle" @click="quickStartOpen = !quickStartOpen">
        <div>
          <h3>{{ $t('hooks.quickStartTitle') }}</h3>
          <p class="section-subtitle">{{ $t('hooks.quickStartDesc') }}</p>
        </div>
        <span class="chevron">{{ quickStartOpen ? '▼' : '▶' }}</span>
      </div>

      <div v-show="quickStartOpen" class="quick-start-stack">
        <article class="guide-overview">
          <div class="guide-overview-head">
            <div class="guide-card-title">{{ quickStartOverview.title }}</div>
            <code class="guide-overview-example">{{ quickStartOverview.example }}</code>
          </div>
          <p class="guide-card-desc">{{ quickStartOverview.desc }}</p>
        </article>

        <div class="guide-grid">
        <article v-for="card in quickStartCards" :key="card.key" class="guide-card">
          <div class="guide-card-title">{{ card.title }}</div>
          <p class="guide-card-desc">{{ card.desc }}</p>
          <div class="guide-example">
            <span class="guide-label">{{ $t('hooks.exampleLabel') }}</span>
            <code>{{ card.example }}</code>
          </div>
        </article>
        </div>
      </div>
    </section>

    <section class="info-section" style="margin-bottom:20px">
      <div class="section-header">
        <div class="section-toggle section-toggle-split" @click="suggestionsOpen = !suggestionsOpen">
          <div>
            <h3>{{ $t('hooks.suggestionsTitle', { n: suggestions.length }) }}</h3>
            <p class="section-subtitle">{{ $t('hooks.suggestionsDesc', { n: recentLogs }) }}</p>
          </div>
          <span class="chevron">{{ suggestionsOpen ? '▼' : '▶' }}</span>
        </div>
        <button class="btn btn-secondary btn-sm" @click="loadSuggestions" :disabled="loadingSuggestions">
          {{ loadingSuggestions ? $t('common.loading') : $t('hooks.refreshSuggestions') }}
        </button>
      </div>

      <div v-if="suggestionsOpen && suggestionError" class="msg msg-error" style="margin-bottom:12px">
        {{ suggestionError }}
      </div>

      <div v-if="suggestionsOpen && loadingSuggestions" class="empty">{{ $t('hooks.loadingSuggestions') }}</div>
      <div v-else-if="suggestionsOpen && suggestions.length === 0" class="empty">{{ $t('hooks.noSuggestions') }}</div>

      <div v-else-if="suggestionsOpen" class="suggestion-grid">
        <article v-for="suggestion in suggestions" :key="suggestion.match" class="suggestion-card">
          <div class="suggestion-head">
            <div>
              <div class="suggestion-name">
                <code>{{ suggestion.match }}</code>
              </div>
              <div class="meta-chip-group" style="margin-top:8px">
                <span class="meta-chip">{{ $t('hooks.toolNameLabel') }} {{ suggestion.base_tool_name || suggestion.tool_name }}</span>
                <span v-if="suggestion.mcp_name" class="meta-chip">{{ $t('hooks.mcpNameLabel') }} {{ suggestion.mcp_name }}</span>
              </div>
              <div class="suggestion-meta">
                <span>{{ $t('hooks.recentCount', { n: suggestion.count }) }}</span>
                <span>·</span>
                <span>{{ $t('hooks.lastSeen') }} {{ formatDateTime(suggestion.last_seen) }}</span>
              </div>
            </div>
            <span v-if="hasExactRule(suggestion.match)" class="status-badge status-configured">
              {{ $t('common.configured') }}
            </span>
          </div>

          <div v-if="suggestion.sample_arguments" class="field-row">
            <label class="field-label">{{ $t('hooks.sampleArgs') }}</label>
            <pre class="suggestion-code">{{ formatArguments(suggestion.sample_arguments) }}</pre>
          </div>

          <div v-if="(suggestion.routes || []).length" class="field-row">
            <label class="field-label">{{ $t('hooks.routeHints') }}</label>
            <div class="route-hints">
              <div v-for="routeHint in suggestion.routes" :key="suggestion.match + routeHint.route" class="route-hint">
                <div class="route-hint-main">
                  <code>{{ routeHint.route }}</code>
                  <span class="route-hint-meta">
                    {{ $t('hooks.recentCount', { n: routeHint.count }) }}
                    <template v-if="routeHint.primary_model"> · {{ routeHint.primary_model }}</template>
                  </span>
                  <div v-if="(routeHint.providers || []).length" class="hint-chip-row">
                    <span v-for="provider in routeHint.providers.slice(0, 3)" :key="routeHint.route + provider" class="mini-chip">
                      {{ provider }}
                    </span>
                  </div>
                </div>
                <button class="btn btn-primary btn-sm" @click="addSuggestedAIRule(suggestion, routeHint)">
                  {{ suggestionActionLabel('ai', suggestion, routeHint) }}
                </button>
              </div>
            </div>
          </div>

          <div class="suggestion-actions">
            <button class="btn btn-secondary btn-sm" @click="addSuggestedExecRule(suggestion)">
              {{ suggestionActionLabel('exec', suggestion) }}
            </button>
            <button class="btn btn-secondary btn-sm" @click="addSuggestedHTTPRule(suggestion)">
              {{ suggestionActionLabel('http', suggestion) }}
            </button>
          </div>
        </article>
      </div>
    </section>

    <datalist id="hook-webhook-options">
      <option v-for="webhook in webhookOptions" :key="webhook" :value="webhook"></option>
    </datalist>

    <section class="info-section">
      <div class="section-header">
        <h3>{{ $t('hooks.hookRules', { n: rules.length }) }}</h3>
        <button class="btn btn-primary btn-sm" @click="addRule">{{ $t('hooks.addRule') }}</button>
      </div>

      <div v-if="rules.length === 0" class="empty" style="margin-top:12px">
        {{ $t('hooks.noRules') }}
      </div>

      <div v-for="(rule, idx) in rules" :key="idx" class="rule-card">
        <div class="rule-header">
          <div class="rule-summary">
            <div class="rule-title-line">
              <span class="rule-index">#{{ idx + 1 }}</span>
              <code class="rule-match">{{ rule.match || '*' }}</code>
            </div>
            <div class="meta-chip-group">
              <span class="meta-chip">{{ rule.hook.type }}</span>
              <span class="meta-chip">{{ rule.hook.when }}</span>
              <span v-if="rule.hook.timeout" class="meta-chip">{{ rule.hook.timeout }}</span>
            </div>
          </div>
          <button class="btn btn-danger btn-sm" @click="removeRule(idx)">{{ $t('hooks.remove') }}</button>
        </div>

        <div class="rule-fields">
          <div class="field-row">
            <label class="field-label">{{ $t('hooks.matchPattern') }}</label>
            <input
              v-model="rule.match"
              class="form-input"
              :placeholder="$t('hooks.matchPlaceholder')"
              spellcheck="false"
            />
            <span class="field-hint">{{ $t('hooks.matchHint') }}</span>
          </div>

          <div class="field-grid">
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.type') }}</label>
              <select v-model="rule.hook.type" class="form-input" @change="applyRuleTypeDefaults(rule)">
                <option value="exec">exec</option>
                <option value="ai">ai</option>
                <option value="http">http</option>
              </select>
              <span class="field-hint">{{ $t('hooks.typeHint') }}</span>
            </div>
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.when') }}</label>
              <select v-model="rule.hook.when" class="form-input">
                <option value="pre">{{ $t('hooks.preBlock') }}</option>
                <option value="post">{{ $t('hooks.postAudit') }}</option>
              </select>
              <span class="field-hint">{{ $t('hooks.whenHint') }}</span>
            </div>
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.timeout') }}</label>
              <input v-model="rule.hook.timeout" class="form-input" placeholder="5s" />
              <span class="field-hint">{{ $t('hooks.timeoutHint') }}</span>
            </div>
          </div>

          <template v-if="rule.hook.type === 'exec'">
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.command') }}</label>
              <input v-model="rule.hook.command" class="form-input" placeholder="/usr/bin/guard-check" spellcheck="false" />
              <span class="field-hint">{{ $t('hooks.commandHint') }}</span>
            </div>
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.args') }}</label>
              <input
                :value="(rule.hook.args || []).join(' ')"
                @input="rule.hook.args = $event.target.value.split(' ').filter(Boolean)"
                class="form-input"
                placeholder="--flag value"
                spellcheck="false"
              />
              <span class="field-hint">{{ $t('hooks.argsHint') }}</span>
            </div>
          </template>

          <template v-if="rule.hook.type === 'ai'">
            <div class="field-grid">
              <div class="field-row">
                <label class="field-label">{{ $t('hooks.route') }}</label>
                <select
                  v-model="rule.hook.route"
                  class="form-input"
                  @change="handleRuleRouteChange(rule)"
                >
                  <option value="">{{ $t('hooks.selectRoute') }}</option>
                  <option v-for="route in routeOptionsForRule(rule)" :key="route" :value="route">{{ route }}</option>
                </select>
                <span class="field-hint">{{ $t('hooks.routeHint') }}</span>
              </div>
              <div class="field-row">
                <label class="field-label">{{ $t('hooks.model') }}</label>
                <select
                  v-model="rule.hook.model"
                  class="form-input"
                >
                  <option value="">{{ $t('hooks.selectModel') }}</option>
                  <option v-for="model in modelOptionsForRule(rule)" :key="model" :value="model">{{ model }}</option>
                </select>
                <span class="field-hint">{{ $t('hooks.modelHint') }}</span>
              </div>
            </div>
            <div class="field-row">
              <div class="field-headline">
                <label class="field-label">{{ $t('hooks.promptLabel') }}</label>
                <button class="btn btn-secondary btn-sm" type="button" @click="applyDefaultAiPrompt(rule)">
                  {{ $t('hooks.useSafePrompt') }}
                </button>
              </div>
              <textarea
                v-model="rule.hook.prompt"
                class="form-input"
                rows="10"
                :placeholder="defaultAiPrompt()"
                spellcheck="false"
              ></textarea>
              <span class="field-hint">{{ $t('hooks.promptHelp') }}</span>
              <div class="hint-chip-row" v-pre>
                <code class="mini-chip">{{.ToolName}}</code>
                <code class="mini-chip">{{.FullName}}</code>
                <code class="mini-chip">{{.MCPName}}</code>
                <code class="mini-chip">{{.Arguments}}</code>
                <code class="mini-chip">{{.Result}}</code>
                <code class="mini-chip">{{.CallID}}</code>
              </div>
            </div>
          </template>

          <template v-if="rule.hook.type === 'http'">
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.webhookLabel') }}</label>
              <input
                v-model="rule.hook.webhook"
                class="form-input"
                list="hook-webhook-options"
                placeholder="audit"
                spellcheck="false"
              />
              <span class="field-hint">{{ $t('hooks.webhookHint') }}</span>
            </div>
          </template>
        </div>
      </div>

      <div v-if="rules.length > 0" style="margin-top:16px;display:flex;gap:10px">
        <button class="btn btn-primary" @click="save" :disabled="saving">
          {{ saving ? $t('hooks.saving') : $t('hooks.saveApply') }}
        </button>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchConfig, fetchToolHookSuggestions, saveConfig } from '../api.js'
import { DEFAULT_AI_HOOK_PROMPT } from '../utils.js'

const { t } = useI18n()

const rules = ref([])
const suggestions = ref([])
const recentLogs = ref(0)
const loadingSuggestions = ref(false)
const suggestionError = ref('')
const saving = ref(false)
const saveMsg = ref(null)
const webhookOptions = ref([])
const routeConfigMap = ref({})
const providerConfigMap = ref({})
const quickStartOpen = ref(true)
const suggestionsOpen = ref(true)

const routeModelMap = computed(() => buildRouteModelMap(routeConfigMap.value, providerConfigMap.value))

const quickStartOverview = computed(() => ({
  title: t('hooks.about'),
  desc: t('hooks.aboutDesc', {
    pattern: 'mcp_name__tool_name',
    wildcard: '*',
    example1: 'my_mcp__write_*',
    example2: '*',
  }),
  example: 'my_mcp__write_* / *',
}))

const quickStartCards = computed(() => ([
  {
    key: 'match',
    title: t('hooks.guideMatchTitle'),
    desc: t('hooks.guideMatchDesc'),
    example: 'filesystem__write_file / filesystem__* / *',
  },
  {
    key: 'exec',
    title: t('hooks.guideExecTitle'),
    desc: t('hooks.guideExecDesc'),
    example: '/usr/local/bin/guard-check --policy strict --format json',
  },
  {
    key: 'ai',
    title: t('hooks.guideAiTitle'),
    desc: t('hooks.guideAiDesc'),
    example: 'route=/openai, model=gpt-4.1-mini',
  },
  {
    key: 'http',
    title: t('hooks.guideHttpTitle'),
    desc: t('hooks.guideHttpDesc'),
    example: 'webhook=security-audit',
  },
]))

function emptyRule() {
  return {
    match: '',
    hook: {
      type: 'exec',
      when: 'pre',
      timeout: '5s',
      command: '',
      args: [],
      route: '',
      model: '',
      prompt: '',
      webhook: '',
    },
  }
}

function normalizeRule(rule) {
  return {
    match: rule.match || '',
    hook: {
      type: rule.hook?.type || 'exec',
      when: rule.hook?.when || 'pre',
      timeout: rule.hook?.timeout || '5s',
      command: rule.hook?.command || '',
      args: rule.hook?.args || [],
      route: rule.hook?.route || '',
      model: rule.hook?.model || '',
      prompt: rule.hook?.prompt || '',
      webhook: rule.hook?.webhook || '',
    },
  }
}

function addRule() {
  rules.value.push(emptyRule())
}

function removeRule(idx) {
  rules.value.splice(idx, 1)
}

function normalizeText(value) {
  return String(value || '').trim()
}

function sortTextValues(values) {
  return [...values].sort((a, b) => a.localeCompare(b))
}

function buildRouteModelMap(routes, providers) {
  const result = {}
  for (const [route, cfg] of Object.entries(routes || {})) {
    const models = new Set()
    for (const model of Object.keys(cfg?.system_prompts || {})) {
      if (model) models.add(model)
    }
    for (const providerName of cfg?.providers || []) {
      const provider = providers?.[providerName]
      if (!provider) continue
      for (const model of provider.models || []) {
        if (model) models.add(model)
      }
      for (const alias of Object.keys(provider.model_aliases || {})) {
        if (alias) models.add(alias)
      }
      for (const realModel of Object.values(provider.model_aliases || {})) {
        if (realModel) models.add(realModel)
      }
    }
    result[route] = sortTextValues(models)
  }
  return result
}

function routeOptionsForRule(rule) {
  const values = new Set(Object.keys(routeConfigMap.value || {}))
  const currentRoute = normalizeText(rule?.hook?.route)
  if (currentRoute) values.add(currentRoute)
  return sortTextValues(values)
}

function modelOptionsForRule(rule) {
  const route = normalizeText(rule?.hook?.route)
  const values = new Set(routeModelMap.value[route] || [])
  const currentModel = normalizeText(rule?.hook?.model)
  if (currentModel) values.add(currentModel)
  return sortTextValues(values)
}

function handleRuleRouteChange(rule) {
  const nextRoute = normalizeText(rule?.hook?.route)
  const currentModel = normalizeText(rule?.hook?.model)
  if (!currentModel) return
  const allowedModels = new Set(routeModelMap.value[nextRoute] || [])
  if (allowedModels.size > 0 && !allowedModels.has(currentModel)) {
    rule.hook.model = ''
  }
}

function hasExactRule(match) {
  const normalizedMatch = normalizeText(match)
  return rules.value.some(rule => normalizeText(rule.match) === normalizedMatch)
}

function formatArguments(value) {
  if (!value) return ''
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

function formatDateTime(value) {
  if (!value) return '-'
  const dt = new Date(value)
  if (Number.isNaN(dt.getTime())) return value
  return dt.toLocaleString()
}

function defaultAiPrompt() {
  return DEFAULT_AI_HOOK_PROMPT
}

function applyRuleTypeDefaults(rule) {
  if (rule.hook.type === 'ai' && !normalizeText(rule.hook.prompt)) {
    rule.hook.prompt = defaultAiPrompt()
  }
}

function applyDefaultAiPrompt(rule) {
  rule.hook.prompt = defaultAiPrompt()
}

function findRouteHint(suggestion, route = '', model = '') {
  const normalizedRoute = normalizeText(route)
  const normalizedModel = normalizeText(model)
  if (normalizedRoute) {
    const matched = (suggestion.routes || []).find(item =>
      normalizeText(item.route) === normalizedRoute &&
      (!normalizedModel || normalizeText(item.primary_model) === normalizedModel)
    )
    if (matched) return matched
  }
  return (suggestion.routes || [])[0] || null
}

function buildSuggestedRule(suggestion, type, routeHint = null) {
  const hint = type === 'ai'
    ? (routeHint || findRouteHint(suggestion, suggestion.primary_route, suggestion.primary_model))
    : null
  if (type === 'ai') {
    return {
      match: suggestion.match,
      hook: {
        type: 'ai',
        when: 'pre',
        timeout: '5s',
        command: '',
        args: [],
        route: hint?.route || '',
        model: hint?.primary_model || '',
        prompt: defaultAiPrompt(),
      },
    }
  }
  if (type === 'http') {
    return {
      match: suggestion.match,
      hook: {
        type: 'http',
        when: 'pre',
        timeout: '5s',
        command: '',
        args: [],
        route: '',
        model: '',
        prompt: '',
        webhook: '',
      },
    }
  }
  return {
    match: suggestion.match,
    hook: {
      type: 'exec',
      when: 'pre',
      timeout: '5s',
      command: '',
      args: [],
      route: '',
      model: '',
      prompt: '',
      webhook: '',
    },
  }
}

function hasSuggestedRule(type, suggestion, routeHint = null) {
  return findSuggestedRuleIndex(buildSuggestedRule(suggestion, type, routeHint)) >= 0
}

function suggestionActionLabel(type, suggestion, routeHint = null) {
  const fill = hasSuggestedRule(type, suggestion, routeHint)
  if (type === 'ai') return fill ? t('hooks.fillAiRule') : t('hooks.addAiRule')
  if (type === 'http') return fill ? t('hooks.fillHttpRule') : t('hooks.addHttpRule')
  return fill ? t('hooks.fillExecRule') : t('hooks.addExecRule')
}

function findSuggestedRuleIndex(nextRule) {
  const normalizedMatch = normalizeText(nextRule.match)
  const normalizedRoute = normalizeText(nextRule.hook.route)
  const normalizedModel = normalizeText(nextRule.hook.model)

  const exactIdx = rules.value.findIndex(rule => {
    if (normalizeText(rule.match) !== normalizedMatch) return false
    if (rule.hook.type !== nextRule.hook.type || rule.hook.when !== nextRule.hook.when) return false
    if (nextRule.hook.type !== 'ai') return true
    return normalizeText(rule.hook.route) === normalizedRoute &&
      normalizeText(rule.hook.model) === normalizedModel
  })
  if (exactIdx >= 0) return exactIdx

  if (nextRule.hook.type !== 'ai') return -1

  return rules.value.findIndex(rule => {
    if (normalizeText(rule.match) !== normalizedMatch) return false
    if (rule.hook.type !== nextRule.hook.type || rule.hook.when !== nextRule.hook.when) return false
    const route = normalizeText(rule.hook.route)
    const model = normalizeText(rule.hook.model)
    const routeCompatible = !route || route === normalizedRoute
    const modelCompatible = !model || model === normalizedModel
    return routeCompatible && modelCompatible
  })
}

function mergeSuggestedRule(target, nextRule) {
  if (!target.match) target.match = nextRule.match
  if (!target.hook.timeout) target.hook.timeout = nextRule.hook.timeout

  if (target.hook.type === 'ai') {
    if (!target.hook.route) target.hook.route = nextRule.hook.route
    if (!target.hook.model) target.hook.model = nextRule.hook.model
    if (!target.hook.prompt) target.hook.prompt = nextRule.hook.prompt
    return
  }

  if (target.hook.type === 'exec') {
    if (!target.hook.command) target.hook.command = nextRule.hook.command
    if ((!target.hook.args || target.hook.args.length === 0) && nextRule.hook.args?.length) {
      target.hook.args = [...nextRule.hook.args]
    }
    return
  }

  if (target.hook.type === 'http') {
    if (!target.hook.webhook) target.hook.webhook = nextRule.hook.webhook
  }
}

function upsertSuggestedRule(nextRule) {
  const idx = findSuggestedRuleIndex(nextRule)

  if (idx >= 0) {
    mergeSuggestedRule(rules.value[idx], nextRule)
    saveMsg.value = { type: 'msg-success', text: t('hooks.filledExistingRule', { n: idx + 1 }) }
    return
  }

  rules.value.unshift(nextRule)
  saveMsg.value = { type: 'msg-success', text: t('hooks.addedSuggestedRule') }
}

function addSuggestedAIRule(suggestion, routeHint = null) {
  const hint = routeHint || findRouteHint(suggestion, suggestion.primary_route, suggestion.primary_model)
  if (!hint?.route) {
    saveMsg.value = { type: 'msg-error', text: t('hooks.noRouteHint') }
    return
  }
  upsertSuggestedRule(buildSuggestedRule(suggestion, 'ai', hint))
}

function addSuggestedExecRule(suggestion) {
  upsertSuggestedRule(buildSuggestedRule(suggestion, 'exec'))
}

function addSuggestedHTTPRule(suggestion) {
  upsertSuggestedRule(buildSuggestedRule(suggestion, 'http'))
}

async function loadConfig() {
  const cfg = await fetchConfig()
  rules.value = (cfg.tool_hooks || []).map(normalizeRule)
  webhookOptions.value = Object.keys(cfg.webhook || {}).sort()
  routeConfigMap.value = cfg.route || {}
  providerConfigMap.value = cfg.provider || {}
}

async function loadSuggestions() {
  loadingSuggestions.value = true
  suggestionError.value = ''
  try {
    const resp = await fetchToolHookSuggestions()
    recentLogs.value = resp.recent_logs || 0
    suggestions.value = resp.suggestions || []
  } catch (e) {
    suggestionError.value = t('hooks.loadSuggestionsFailed', { error: e.message })
  } finally {
    loadingSuggestions.value = false
  }
}

async function load() {
  try {
    await loadConfig()
  } catch (e) {
    saveMsg.value = { type: 'msg-error', text: t('hooks.loadFailed', { error: e.message }) }
  }
  await loadSuggestions()
}

async function save() {
  saving.value = true
  saveMsg.value = null
  try {
    const cfg = await fetchConfig()
    cfg.tool_hooks = rules.value.map(rule => {
      const hook = {
        type: rule.hook.type,
        when: rule.hook.when,
      }
      if (rule.hook.timeout && rule.hook.timeout !== '5s') hook.timeout = rule.hook.timeout
      if (rule.hook.type === 'exec') {
        hook.command = rule.hook.command
        if (rule.hook.args && rule.hook.args.length > 0) hook.args = rule.hook.args
      } else if (rule.hook.type === 'ai') {
        hook.route = rule.hook.route
        hook.model = rule.hook.model
        hook.prompt = rule.hook.prompt
      } else if (rule.hook.type === 'http') {
        hook.webhook = rule.hook.webhook
      }
      return { match: rule.match, hook }
    })
    await saveConfig(cfg)
    saveMsg.value = { type: 'msg-success', text: t('hooks.savedMsg') }
  } catch (e) {
    saveMsg.value = { type: 'msg-error', text: t('hooks.saveFailed', { error: e.message }) }
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.desc,
.section-subtitle {
  font-size: 13px;
  color: var(--c-text-2);
  line-height: 1.6;
  margin: 4px 0 0;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 14px;
}

.section-header h3 {
  margin: 0;
}

.section-toggle {
  flex: 1;
  cursor: pointer;
}

.section-toggle-split {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.guide-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.quick-start-stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.guide-overview,
.guide-card {
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 14px;
  background: linear-gradient(180deg, #fff 0%, #f7fbff 100%);
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.04);
}

.guide-overview {
  padding: 18px;
  background: linear-gradient(135deg, #fffaf0 0%, #f3f9ff 100%);
  border-color: #d8e7f7;
}

.guide-overview-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.guide-overview-example {
  display: inline-flex;
  align-items: center;
  min-height: 30px;
  padding: 0 10px;
  border-radius: 999px;
  background: #fff;
  color: #184a7a;
  border: 1px solid #d8e7f7;
  font-size: 12px;
}

.guide-card-title {
  font-weight: 700;
  color: var(--c-text-1);
}

.guide-card-desc {
  margin: 8px 0 0;
  font-size: 13px;
  color: var(--c-text-2);
  line-height: 1.6;
}

.guide-example {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 12px;
  font-size: 12px;
}

.guide-label {
  color: var(--c-text-3);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-weight: 700;
}

.suggestion-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 12px;
}

.suggestion-card,
.rule-card {
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 14px;
  background: #fafcff;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.04);
}

.suggestion-head,
.rule-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 10px;
  margin-bottom: 12px;
}

.suggestion-name {
  font-weight: 700;
}

.suggestion-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 4px;
  font-size: 12px;
  color: var(--c-text-3);
}

.meta-chip-group,
.hint-chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.meta-chip,
.mini-chip {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  padding: 0 8px;
  border-radius: 999px;
  background: #edf5ff;
  color: #184a7a;
  font-size: 11px;
  font-weight: 600;
}

.suggestion-code {
  margin: 0;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: #0d1628;
  color: #dbe8ff;
  font-size: 12px;
  line-height: 1.5;
  overflow: auto;
}

.route-hints {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.route-hint {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: #fff;
}

.route-hint-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.route-hint-meta {
  font-size: 12px;
  color: var(--c-text-3);
}

.suggestion-actions {
  display: flex;
  gap: 8px;
  margin-top: 14px;
}

.status-badge {
  padding: 4px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  white-space: nowrap;
}

.status-configured {
  color: #0b6b34;
  background: #daf5e3;
}

.rule-summary {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.rule-title-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.rule-index {
  font-weight: 700;
  font-size: 12px;
  color: var(--c-text-3);
  font-family: var(--font-mono);
}

.rule-match {
  font-size: 13px;
}

.rule-fields {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.field-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.field-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--c-text-2);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.field-headline {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.field-hint {
  font-size: 11px;
  color: var(--c-text-3);
  line-height: 1.5;
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 10px;
}

@media (max-width: 768px) {
  .section-header,
  .field-headline,
  .guide-overview-head,
  .route-hint,
  .suggestion-actions {
    flex-direction: column;
    align-items: stretch;
  }

  .guide-grid {
    grid-template-columns: 1fr;
  }
}
</style>
