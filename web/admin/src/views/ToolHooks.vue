<template>
  <div>
    <div class="page-title">{{ $t('hooks.title') }}</div>

    <div v-if="saveMsg" :class="['msg', saveMsg.type]">{{ saveMsg.text }}</div>

    <section class="info-section" style="margin-bottom:20px">
      <h3>{{ $t('hooks.about') }}</h3>
      <p class="desc">
        {{ $t('hooks.aboutDesc', {
          pattern: 'mcp_name__tool_name',
          wildcard: '*',
          example1: 'my_mcp__write_*',
          example2: '*'
        }) }}
      </p>
    </section>

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
          <span class="rule-index">#{{ idx + 1 }}</span>
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
              <select v-model="rule.hook.type" class="form-input">
                <option value="exec">exec</option>
                <option value="ai">ai</option>
              </select>
            </div>
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.when') }}</label>
              <select v-model="rule.hook.when" class="form-input">
                <option value="pre">{{ $t('hooks.preBlock') }}</option>
                <option value="post">{{ $t('hooks.postAudit') }}</option>
              </select>
            </div>
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.timeout') }}</label>
              <input v-model="rule.hook.timeout" class="form-input" placeholder="5s" />
            </div>
          </div>

          <!-- exec fields -->
          <template v-if="rule.hook.type === 'exec'">
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.command') }}</label>
              <input v-model="rule.hook.command" class="form-input" placeholder="/usr/bin/guard-check" spellcheck="false" />
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

          <!-- ai fields -->
          <template v-if="rule.hook.type === 'ai'">
            <div class="field-grid">
              <div class="field-row">
                <label class="field-label">{{ $t('hooks.route') }}</label>
                <input v-model="rule.hook.route" class="form-input" placeholder="/openai" spellcheck="false" />
              </div>
              <div class="field-row">
                <label class="field-label">{{ $t('hooks.model') }}</label>
                <input v-model="rule.hook.model" class="form-input" placeholder="gpt-4o" spellcheck="false" />
              </div>
            </div>
            <div class="field-row">
              <label class="field-label">{{ $t('hooks.promptLabel') }}</label>
              <textarea
                v-model="rule.hook.prompt"
                class="form-input"
                rows="4"
                placeholder="Tool {{.ToolName}} called with {{.Arguments}}. Should this be allowed? Reply with JSON: {&quot;allow&quot;: true/false, &quot;reason&quot;: &quot;...&quot;}"
                spellcheck="false"
              ></textarea>
              <span class="field-hint" v-pre>Supports: {{.ToolName}}, {{.Arguments}}, {{.Result}}, {{.CallID}}</span>
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
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchConfig, saveConfig } from '../api.js'

const { t } = useI18n()

const rules = ref([])
const saving = ref(false)
const saveMsg = ref(null)

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
    },
  }
}

function addRule() {
  rules.value.push(emptyRule())
}

function removeRule(idx) {
  rules.value.splice(idx, 1)
}

async function load() {
  try {
    const cfg = await fetchConfig()
    rules.value = (cfg.tool_hooks || []).map(r => ({
      match: r.match || '',
      hook: {
        type: r.hook?.type || 'exec',
        when: r.hook?.when || 'pre',
        timeout: r.hook?.timeout || '5s',
        command: r.hook?.command || '',
        args: r.hook?.args || [],
        route: r.hook?.route || '',
        model: r.hook?.model || '',
        prompt: r.hook?.prompt || '',
      },
    }))
  } catch (e) {
    saveMsg.value = { type: 'msg-error', text: t('hooks.loadFailed', { error: e.message }) }
  }
}

async function save() {
  saving.value = true
  saveMsg.value = null
  try {
    const cfg = await fetchConfig()
    // build clean hook list, strip empty fields per type
    cfg.tool_hooks = rules.value.map(r => {
      const hook = {
        type: r.hook.type,
        when: r.hook.when,
      }
      if (r.hook.timeout && r.hook.timeout !== '5s') hook.timeout = r.hook.timeout
      if (r.hook.type === 'exec') {
        hook.command = r.hook.command
        if (r.hook.args && r.hook.args.length > 0) hook.args = r.hook.args
      } else if (r.hook.type === 'ai') {
        hook.route = r.hook.route
        hook.model = r.hook.model
        hook.prompt = r.hook.prompt
      }
      return { match: r.match, hook }
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
.desc {
  font-size: 13px;
  color: var(--c-text-2);
  line-height: 1.6;
}
.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 14px;
}
.section-header h3 { margin: 0; }

.rule-card {
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 14px;
  margin-bottom: 12px;
  background: #fafcff;
}
.rule-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.rule-index {
  font-weight: 700;
  font-size: 12px;
  color: var(--c-text-3);
  font-family: var(--font-mono);
}
.rule-fields { display: flex; flex-direction: column; gap: 10px; }
.field-row { display: flex; flex-direction: column; gap: 4px; }
.field-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--c-text-2);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.field-hint {
  font-size: 11px;
  color: var(--c-text-3);
}
.field-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 10px;
}
</style>
