<template>
  <div>
    <h2>{{ $t('config.title') }}</h2>

    <div v-if="configSource && !configSource.source_type?.file" class="msg warning">
      {{ $t('config.nonFileWarning', { path: configSource.config_path || 'remote' }) }}
    </div>

    <div v-if="configFileChanged" class="msg warning">
      {{ $t('config.externalChange') }}
      <button @click="load" class="btn btn-sm">{{ $t('common.reload') }}</button>
    </div>

    <div v-if="message" :class="['msg', messageType]">{{ message }}</div>
    <div v-if="error" class="msg error">{{ error }}</div>

    <div class="actions">
      <button @click="apply" class="btn btn-primary" :disabled="applying || (configSource && !configSource.source_type?.file)">
        {{ applying ? (waitingAlive ? $t('config.waitingService', { n: waitingElapsed }) : $t('config.applying')) : $t('config.apply') }}
      </button>
      <button v-if="dirty && !applying" @click="discard" class="btn btn-secondary">{{ $t('config.discardChanges') }}</button>
    </div>

    <!-- General -->
    <section class="config-section">
      <h3>{{ $t('config.general') }}</h3>
      <div class="form-grid">
        <label>addr</label>
        <input v-model="config.addr" class="form-input" placeholder=":8080" />

        <label>admin_password</label>
        <div class="secret-field">
          <input
            :type="showAdminPw ? 'text' : 'password'"
            :value="adminPwDisplay"
            @input="onAdminPwInput($event.target.value)"
            class="form-input"
            placeholder="(not set)"
          />
          <button class="btn-icon" @click="showAdminPw = !showAdminPw" type="button">
            {{ showAdminPw ? '🙈' : '👁' }}
          </button>
          <span :class="['badge', adminPwConfigured ? 'badge-ok' : 'badge-none']">
            {{ adminPwConfigured ? 'Configured' : 'Not set' }}
          </span>
        </div>
      </div>

      <!-- log targets -->
      <div class="subsection-header">
        <span>log.targets</span>
        <button class="btn btn-sm" @click="addLogTarget">{{ $t('config.addTarget') }}</button>
      </div>
      <div v-for="(t, i) in (config.log?.targets || [])" :key="i" class="card">
        <div class="card-header" @click="toggleCard('log-target', i)">
          <strong>{{ t.type || 'file' }}</strong>
          <span v-if="t.type === 'file'" class="tag-proto">{{ t.dir || '(no dir)' }}</span>
          <span v-else class="tag-proto">{{ t.webhook || '(no webhook)' }}</span>
          <span class="chevron">{{ isCardOpen('log-target', i) ? '▼' : '▶' }}</span>
        </div>
        <div v-show="isCardOpen('log-target', i)" class="card-body">
          <div class="form-grid">
            <label>type <span class="req">*</span></label>
            <select v-model="t.type" class="form-input">
              <option value="file">file</option>
              <option value="http">http</option>
            </select>

            <template v-if="t.type === 'file'">
              <label>dir <span class="req">*</span></label>
              <input v-model="t.dir" class="form-input" placeholder="./logs" />
            </template>

            <template v-else>
              <label>webhook <span class="req">*</span></label>
              <select v-model="t.webhook" class="form-input">
                <option value="">(none)</option>
                <option v-for="w in webhookNames" :key="w" :value="w">{{ w }}</option>
              </select>
            </template>
          </div>
          <button class="btn btn-danger btn-sm" @click="removeLogTarget(i)">Delete</button>
        </div>
      </div>
    </section>

    <!-- Webhook -->
    <section class="config-section">
      <div class="section-header" @click="webhookOpen = !webhookOpen">
        <h3>{{ $t('config.webhook') }} <span class="count">({{ webhookCount }})</span></h3>
        <span class="chevron">{{ webhookOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="webhookOpen">
        <div class="add-row">
          <template v-if="addingSection === 'webhook'">
            <input ref="addInputRef" v-model="addingKey" class="form-input add-input"
              placeholder="Webhook name" @keyup.enter="confirmAdd" @keyup.esc="cancelAdd" />
            <button class="btn btn-sm" @click="confirmAdd">Confirm</button>
            <button class="btn btn-secondary btn-sm" @click="cancelAdd">Cancel</button>
          </template>
          <button v-else class="btn btn-sm" @click="startAdd('webhook')">+ Add</button>
        </div>
        <div v-for="(cfg, name) in config.webhook" :key="'webhook-'+name" class="card">
          <div class="card-header" @click="toggleCard('webhook', name)">
            <strong>{{ name }}</strong>
            <span class="tag-proto">{{ cfg.url || '(no url)' }}</span>
            <span class="chevron">{{ isCardOpen('webhook', name) ? '▼' : '▶' }}</span>
          </div>
          <div v-show="isCardOpen('webhook', name)" class="card-body">
            <div class="form-grid">
              <label>url <span class="req">*</span></label>
              <input v-model="cfg.url" class="form-input" placeholder="https://your-log-sink/api/ingest" />

              <label>method</label>
              <select v-model="cfg.method" class="form-input">
                <option value="">POST (default)</option>
                <option value="POST">POST</option>
                <option value="PUT">PUT</option>
                <option value="PATCH">PATCH</option>
              </select>

              <label>headers</label>
              <KeyValueEditor v-model="cfg.headers" keyPlaceholder="Header name" valuePlaceholder="Value" />

              <label>body_template</label>
              <textarea v-model="cfg.body_template" class="form-input form-textarea"
                placeholder="Go template; omit to send record as plain JSON&#10;Example: {&quot;id&quot;: &quot;{{ .Record.RequestID }}&quot;}" />

              <label>timeout</label>
              <input v-model="cfg.timeout" class="form-input" placeholder="5s" />

              <label>retry</label>
              <input v-model.number="cfg.retry" class="form-input" type="number" placeholder="2" />
            </div>
            <button class="btn btn-danger btn-sm" @click="deleteMapEntry('webhook', name)">Delete</button>
          </div>
        </div>
      </div>
    </section>
    <section class="config-section">
      <div class="section-header" @click="sshOpen = !sshOpen">
        <h3>{{ $t('config.ssh') }} <span class="count">({{ sshCount }})</span></h3>
        <span class="chevron">{{ sshOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="sshOpen">
        <div class="add-row">
          <template v-if="addingSection === 'ssh'">
            <input ref="addInputRef" v-model="addingKey" class="form-input add-input"
              placeholder="Entry name" @keyup.enter="confirmAdd" @keyup.esc="cancelAdd" />
            <button class="btn btn-sm" @click="confirmAdd">Confirm</button>
            <button class="btn btn-secondary btn-sm" @click="cancelAdd">Cancel</button>
          </template>
          <button v-else class="btn btn-sm" @click="startAdd('ssh')">+ Add</button>
        </div>
        <div v-for="(cfg, name) in config.ssh" :key="'ssh-'+name" class="card">
          <div class="card-header" @click="toggleCard('ssh', name)">
            <strong>{{ name }}</strong>
            <span class="chevron">{{ isCardOpen('ssh', name) ? '▼' : '▶' }}</span>
          </div>
          <div v-show="isCardOpen('ssh', name)" class="card-body">
            <div class="form-grid">
              <label>host <span class="req">*</span></label>
              <input v-model="cfg.host" class="form-input" />
              <label>port</label>
              <input v-model.number="cfg.port" class="form-input" type="number" placeholder="22" />
              <label>user</label>
              <input v-model="cfg.user" class="form-input" />
              <label>identity_file</label>
              <input v-model="cfg.identity_file" class="form-input" />
            </div>
            <button class="btn btn-danger btn-sm" @click="deleteMapEntry('ssh', name)">Delete</button>
          </div>
        </div>
      </div>
    </section>

    <!-- Providers -->
    <section class="config-section">
      <div class="section-header" @click="providersOpen = !providersOpen">
        <h3>{{ $t('config.providersSection') }} <span class="count">({{ providerCount }})</span></h3>
        <span class="chevron">{{ providersOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="providersOpen">
        <div class="add-row">
          <template v-if="addingSection === 'provider'">
            <input ref="addInputRef" v-model="addingKey" class="form-input add-input"
              placeholder="Provider name" @keyup.enter="confirmAdd" @keyup.esc="cancelAdd" />
            <button class="btn btn-sm" @click="confirmAdd">Confirm</button>
            <button class="btn btn-secondary btn-sm" @click="cancelAdd">Cancel</button>
          </template>
          <button v-else class="btn btn-sm" @click="startAdd('provider')">+ Add</button>
        </div>
        <div v-for="(cfg, name) in config.provider" :key="'prov-'+name" class="card">
          <div class="card-header" @click="toggleCard('provider', name)">
            <strong>{{ name }}</strong>
            <span class="tag-proto">{{ cfg.protocol || 'openai' }}</span>
            <span class="chevron">{{ isCardOpen('provider', name) ? '▼' : '▶' }}</span>
          </div>
          <div v-show="isCardOpen('provider', name)" class="card-body">
            <div class="form-grid">
              <!-- protocol always first -->
              <label>protocol <span class="req">*</span></label>
              <select v-model="cfg.protocol" class="form-input">
                <option value="openai">openai</option>
                <option value="anthropic">anthropic</option>
                <option value="ollama">ollama</option>
                <option value="qwen">qwen</option>
                <option value="copilot">copilot</option>
              </select>

              <!-- url + proxy: only for openai/anthropic/ollama -->
              <template v-if="!['qwen','copilot'].includes(cfg.protocol || 'openai')">
                <label>url <span class="req">*</span></label>
                <div class="url-field">
                  <input v-model="cfg.url" class="form-input"
                    :placeholder="providerUrlPlaceholder(cfg.protocol)" />
                  <input v-model="cfg.proxy" class="form-input url-proxy"
                    placeholder="proxy (socks5://...)" />
                </div>

                <label>api_key</label>
                <div class="secret-field">
                  <input
                    :type="isSecretVisible('prov-apikey-'+name) ? 'text' : 'password'"
                    :value="secretDisplay(cfg.api_key)"
                    @input="cfg.api_key = $event.target.value"
                    class="form-input"
                    placeholder="(not set)"
                  />
                  <button class="btn-icon" @click="toggleSecret('prov-apikey-'+name)" type="button">
                    {{ isSecretVisible('prov-apikey-'+name) ? '🙈' : '👁' }}
                  </button>
                  <span :class="['badge', isSecretConfigured(cfg.api_key) ? 'badge-ok' : 'badge-none']">
                    {{ isSecretConfigured(cfg.api_key) ? 'Configured' : 'Not set' }}
                  </span>
                </div>
              </template>

              <!-- config_dir + ssh: for qwen/copilot OAuth credentials -->
              <template v-if="['qwen','copilot'].includes(cfg.protocol)">
                <label>config_dir</label>
                <input v-model="cfg.config_dir" class="form-input"
                  :placeholder="cfg.protocol === 'qwen' ? '~/.qwen' : '~/.config/github-copilot'" />

                <label>ssh</label>
                <select v-model="cfg.ssh" class="form-input">
                  <option value="">(none)</option>
                  <option v-for="s in sshNames" :key="s" :value="s">{{ s }}</option>
                </select>

                <label>proxy</label>
                <input v-model="cfg.proxy" class="form-input" placeholder="socks5://127.0.0.1:1080" />
              </template>

              <label>timeout</label>
              <input v-model="cfg.timeout" class="form-input" placeholder="60s" />

              <!-- chat_to_responses: only for openai protocol -->
              <template v-if="cfg.protocol === 'openai'">
                <label>chat_to_responses</label>
                <div class="form-hint-row">
                  <input type="checkbox" v-model="cfg.chat_to_responses" class="form-checkbox" />
                  <span class="hint">{{ $t('config.chatToResponsesHint') }}</span>
                </div>
              </template>

              <!-- headers: for openai/anthropic/ollama only -->
              <template v-if="!['qwen','copilot'].includes(cfg.protocol || 'openai')">
                <label>headers</label>
                <KeyValueEditor v-model="cfg.headers" keyPlaceholder="Header name" valuePlaceholder="Value" />
              </template>

              <label>models</label>
              <TagListEditor v-model="cfg.models" placeholder="Model ID" />

              <template v-if="cfg.protocol !== 'copilot'">
                <label>model_aliases</label>
                <KeyValueEditor v-model="cfg.model_aliases" keyPlaceholder="Alias" valuePlaceholder="Real model" />
              </template>

              <!-- request_patch: advanced, for openai/anthropic/ollama -->
              <template v-if="!['qwen','copilot'].includes(cfg.protocol || 'openai')">
                <label>request_patch</label>
                <div>
                  <div v-for="(op, i) in (cfg.request_patch || [])" :key="i" class="patch-row">
                    <select v-model="op.op" class="form-input form-input-sm">
                      <option>add</option>
                      <option>remove</option>
                      <option>replace</option>
                      <option>move</option>
                      <option>copy</option>
                      <option>test</option>
                    </select>
                    <input v-model="op.path" class="form-input" placeholder="/path" />
                    <input v-if="op.op === 'move' || op.op === 'copy'" v-model="op.from" class="form-input" placeholder="/from" />
                    <input v-if="['add','replace','test'].includes(op.op)" v-model="op.value" class="form-input" placeholder="value" />
                    <button class="btn-icon btn-danger-icon" @click="removePatchOp(cfg, i)" type="button">✕</button>
                  </div>
                  <button class="btn btn-sm" @click="addPatchOp(cfg)" type="button">+ Add op</button>
                </div>
              </template>
            </div>
            <button class="btn btn-danger btn-sm" @click="deleteMapEntry('provider', name)">Delete</button>
          </div>
        </div>
      </div>
    </section>

    <!-- Routes -->
    <section class="config-section">
      <div class="section-header" @click="routesOpen = !routesOpen">
        <h3>{{ $t('config.routesSection') }} <span class="count">({{ routeCount }})</span></h3>
        <span class="chevron">{{ routesOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="routesOpen">
        <div class="add-row">
          <template v-if="addingSection === 'route'">
            <input ref="addInputRef" v-model="addingKey" class="form-input add-input"
              placeholder="Route prefix" @keyup.enter="confirmAdd" @keyup.esc="cancelAdd" />
            <button class="btn btn-sm" @click="confirmAdd">Confirm</button>
            <button class="btn btn-secondary btn-sm" @click="cancelAdd">Cancel</button>
          </template>
          <button v-else class="btn btn-sm" @click="startAdd('route')">+ Add</button>
        </div>
        <div v-for="(cfg, prefix) in config.route" :key="'route-'+prefix" class="card">
          <div class="card-header" @click="toggleCard('route', prefix)">
            <strong>{{ prefix }}</strong>
            <span class="chevron">{{ isCardOpen('route', prefix) ? '▼' : '▶' }}</span>
          </div>
          <div v-show="isCardOpen('route', prefix)" class="card-body">
            <div class="form-grid">
              <label>prefix</label>
              <input :value="prefix" class="form-input" readonly />

              <label>providers</label>
              <TagListEditor v-model="cfg.providers" :suggestions="providerNames" placeholder="Provider name" />

              <label>tools</label>
              <TagListEditor v-model="cfg.tools" :suggestions="mcpNames" placeholder="MCP name" />

              <label>system_prompts</label>
              <KeyValueEditor v-model="cfg.system_prompts" keyPlaceholder="Model name" valuePlaceholder="System prompt" />
            </div>
            <button class="btn btn-danger btn-sm" @click="deleteMapEntry('route', prefix)">Delete</button>
          </div>
        </div>
      </div>
    </section>

    <!-- Tool Hooks -->
    <section class="config-section">
      <div class="section-header" @click="toolHooksOpen = !toolHooksOpen">
        <h3>{{ $t('config.toolHooks') }} <span class="count">({{ toolHookCount }})</span></h3>
        <span class="chevron">{{ toolHooksOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="toolHooksOpen">
        <div class="subsection-header">
          <span>tool_hooks</span>
          <button class="btn btn-sm" @click="addToolHookRule">{{ $t('hooks.addRule') }}</button>
        </div>
        <div v-if="toolHookCount === 0" class="hint" style="margin-top:10px">
          {{ $t('hooks.noRules') }}
        </div>
        <div v-for="(rule, idx) in (config.tool_hooks || [])" :key="'tool-hook-'+idx" class="card">
          <div class="card-header" @click="toggleCard('tool-hook', idx)">
            <strong>{{ rule.match || '*' }}</strong>
            <span class="tag-proto">{{ rule.hook?.type || 'exec' }} · {{ rule.hook?.when || 'pre' }}</span>
            <span class="chevron">{{ isCardOpen('tool-hook', idx) ? '▼' : '▶' }}</span>
          </div>
          <div v-show="isCardOpen('tool-hook', idx)" class="card-body">
            <div class="form-grid">
              <label>{{ $t('hooks.matchPattern') }} <span class="req">*</span></label>
              <input v-model="rule.match" class="form-input" :placeholder="$t('hooks.matchPlaceholder')" spellcheck="false" />

              <label>{{ $t('hooks.type') }} <span class="req">*</span></label>
              <select v-model="rule.hook.type" class="form-input" @change="applyToolHookTypeDefaults(rule)">
                <option value="exec">exec</option>
                <option value="ai">ai</option>
                <option value="http">http</option>
              </select>

              <label>{{ $t('hooks.when') }} <span class="req">*</span></label>
              <select v-model="rule.hook.when" class="form-input">
                <option value="pre">{{ $t('hooks.preBlock') }}</option>
                <option value="post">{{ $t('hooks.postAudit') }}</option>
              </select>

              <label>{{ $t('hooks.timeout') }}</label>
              <input v-model="rule.hook.timeout" class="form-input" placeholder="5s" />

              <template v-if="rule.hook.type === 'exec'">
                <label>{{ $t('hooks.command') }} <span class="req">*</span></label>
                <input v-model="rule.hook.command" class="form-input" placeholder="/usr/bin/guard-check" spellcheck="false" />

                <label>{{ $t('hooks.args') }}</label>
                <div>
                  <input
                    :value="(rule.hook.args || []).join(' ')"
                    @input="rule.hook.args = $event.target.value.split(' ').filter(Boolean)"
                    class="form-input"
                    placeholder="--flag value"
                    spellcheck="false"
                  />
                  <div class="hint" style="margin-top:6px">{{ $t('hooks.argsHint') }}</div>
                </div>
              </template>

              <template v-if="rule.hook.type === 'ai'">
                <label>{{ $t('hooks.route') }} <span class="req">*</span></label>
                <div>
                  <input
                    v-model="rule.hook.route"
                    class="form-input"
                    list="config-hook-route-options"
                    placeholder="/openai"
                    spellcheck="false"
                  />
                  <div class="hint" style="margin-top:6px">{{ $t('hooks.routeHint') }}</div>
                </div>

                <label>{{ $t('hooks.model') }} <span class="req">*</span></label>
                <div>
                  <input v-model="rule.hook.model" class="form-input" placeholder="gpt-4o" spellcheck="false" />
                  <div class="hint" style="margin-top:6px">{{ $t('hooks.modelHint') }}</div>
                </div>

                <label>{{ $t('hooks.promptLabel') }} <span class="req">*</span></label>
                <div>
                  <div style="display:flex;justify-content:space-between;align-items:center;gap:8px;margin-bottom:6px">
                    <span class="hint">{{ $t('hooks.promptHelp') }}</span>
                    <button class="btn btn-secondary btn-sm" type="button" @click="applyDefaultToolHookPrompt(rule)">
                      {{ $t('hooks.useSafePrompt') }}
                    </button>
                  </div>
                  <textarea
                    v-model="rule.hook.prompt"
                    class="form-input form-textarea"
                    rows="8"
                    :placeholder="defaultAiPrompt()"
                    spellcheck="false"
                  />
                </div>
              </template>

              <template v-if="rule.hook.type === 'http'">
                <label>{{ $t('hooks.webhookLabel') }} <span class="req">*</span></label>
                <select v-model="rule.hook.webhook" class="form-input">
                  <option value="">(none)</option>
                  <option v-for="w in webhookNames" :key="'tool-hook-webhook-'+w" :value="w">{{ w }}</option>
                </select>
              </template>
            </div>
            <button class="btn btn-danger btn-sm" @click="removeToolHookRule(idx)">{{ $t('hooks.remove') }}</button>
          </div>
        </div>
      </div>
    </section>

    <!-- MCP -->
    <section class="config-section">
      <div class="section-header" @click="mcpOpen = !mcpOpen">
        <h3>{{ $t('config.mcp') }} <span class="count">({{ mcpCount }})</span></h3>
        <span class="chevron">{{ mcpOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="mcpOpen">
        <div class="add-row">
          <template v-if="addingSection === 'mcp'">
            <input ref="addInputRef" v-model="addingKey" class="form-input add-input"
              placeholder="MCP name" @keyup.enter="confirmAdd" @keyup.esc="cancelAdd" />
            <button class="btn btn-sm" @click="confirmAdd">Confirm</button>
            <button class="btn btn-secondary btn-sm" @click="cancelAdd">Cancel</button>
          </template>
          <button v-else class="btn btn-sm" @click="startAdd('mcp')">+ Add</button>
        </div>
        <div v-for="(cfg, name) in config.mcp" :key="'mcp-'+name" class="card">
          <div class="card-header" @click="toggleCard('mcp', name)">
            <strong>{{ name }}</strong>
            <span class="chevron">{{ isCardOpen('mcp', name) ? '▼' : '▶' }}</span>
          </div>
          <div v-show="isCardOpen('mcp', name)" class="card-body">
            <div class="form-grid">
              <label>command <span class="req">*</span></label>
              <input v-model="cfg.command" class="form-input" />

              <label>ssh</label>
              <select v-model="cfg.ssh" class="form-input">
                <option value="">(none)</option>
                <option v-for="s in sshNames" :key="s" :value="s">{{ s }}</option>
              </select>

              <label>args</label>
              <TagListEditor v-model="cfg.args" placeholder="Argument" />

              <label>env</label>
              <KeyValueEditor v-model="cfg.env" keyPlaceholder="Variable" valuePlaceholder="Value" />
            </div>
            <button class="btn btn-danger btn-sm" @click="deleteMapEntry('mcp', name)">Delete</button>
          </div>
        </div>
      </div>
    </section>

    <datalist id="config-hook-route-options">
      <option v-for="route in routeNames" :key="'config-hook-route-'+route" :value="route"></option>
    </datalist>
  </div>
</template>

<script setup>
import { ref, computed, reactive, watch, nextTick, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchConfig, fetchConfigSource, saveConfig, validateConfig, restartGateway, fetchStatus } from '../api.js'
import KeyValueEditor from '../components/KeyValueEditor.vue'
import TagListEditor from '../components/TagListEditor.vue'
import { DEFAULT_AI_HOOK_PROMPT } from '../utils.js'

const { t } = useI18n()

const REDACTED = '__REDACTED__'

const config = ref({})
const configSource = ref(null) // { source_type: { file: bool }, config_path: string, config_hash: string }
const message = ref('')
const messageType = ref('success')
const error = ref('')
const applying = ref(false)
const dirty = ref(false)
const loading = ref(false)
const configFileChanged = ref(false) // true if config file changed externally

// track config modifications via deep watch
watch(config, () => {
  if (!loading.value) dirty.value = true
}, { deep: true })

// section collapse state
const sshOpen = ref(false)
const webhookOpen = ref(false)
const providersOpen = ref(true)
const routesOpen = ref(true)
const toolHooksOpen = ref(false)
const mcpOpen = ref(false)
const showAdminPw = ref(false)

// per-card collapse state
const openCards = reactive({})

// secret visibility toggles
const visibleSecrets = reactive({})

// counts
const sshCount = computed(() => Object.keys(config.value.ssh || {}).length)
const webhookCount = computed(() => Object.keys(config.value.webhook || {}).length)
const providerCount = computed(() => Object.keys(config.value.provider || {}).length)
const routeCount = computed(() => Object.keys(config.value.route || {}).length)
const toolHookCount = computed(() => (config.value.tool_hooks || []).length)
const mcpCount = computed(() => Object.keys(config.value.mcp || {}).length)

// names for cross-references
const sshNames = computed(() => Object.keys(config.value.ssh || {}))
const webhookNames = computed(() => Object.keys(config.value.webhook || {}))
const providerNames = computed(() => Object.keys(config.value.provider || {}))
const routeNames = computed(() => Object.keys(config.value.route || {}))
const mcpNames = computed(() => Object.keys(config.value.mcp || {}))

// admin password handling
const adminPwEdited = ref(false)
const adminPwValue = ref('')

const adminPwConfigured = computed(() => {
  if (adminPwEdited.value) return adminPwValue.value !== ''
  const raw = config.value.admin_password
  return raw && raw !== '' && raw !== REDACTED ? true : raw === REDACTED
})

const adminPwDisplay = computed(() => {
  if (adminPwEdited.value) return adminPwValue.value
  return config.value.admin_password === REDACTED ? REDACTED : (config.value.admin_password || '')
})

function onAdminPwInput(val) {
  adminPwEdited.value = true
  adminPwValue.value = val
  config.value.admin_password = val
}

// log targets helpers
function addLogTarget() {
  if (!config.value.log) config.value.log = { targets: [] }
  if (!config.value.log.targets) config.value.log.targets = []
  const i = config.value.log.targets.length
  config.value.log.targets.push({ type: 'file', dir: '' })
  nextTick(() => { openCards['log-target/' + i] = true })
}
function removeLogTarget(i) {
  config.value.log.targets.splice(i, 1)
}

// card toggle
function toggleCard(section, key) {
  const id = section + '/' + key
  openCards[id] = !openCards[id]
}
function isCardOpen(section, key) {
  return !!openCards[section + '/' + key]
}

// secret helpers
function isSecretVisible(id) { return !!visibleSecrets[id] }
function toggleSecret(id) { visibleSecrets[id] = !visibleSecrets[id] }
function secretDisplay(val) { return val === REDACTED ? REDACTED : (val || '') }
function isSecretConfigured(val) { return val && val !== '' }

// provider url placeholder by protocol
function providerUrlPlaceholder(protocol) {
  switch (protocol) {
    case 'anthropic': return 'https://api.anthropic.com'
    case 'ollama': return 'http://localhost:11434'
    case 'qwen': return '(defaults to dashscope or portal.qwen.ai)'
    case 'copilot': return '(defaults to api.githubcopilot.com)'
    default: return 'https://api.openai.com/v1'
  }
}

// request_patch helpers
function addPatchOp(cfg) {
  if (!cfg.request_patch) cfg.request_patch = []
  cfg.request_patch.push({ op: 'add', path: '', value: '' })
}
function removePatchOp(cfg, i) {
  cfg.request_patch.splice(i, 1)
}

function emptyToolHookRule() {
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

function defaultAiPrompt() {
  return DEFAULT_AI_HOOK_PROMPT
}

function applyToolHookTypeDefaults(rule) {
  if (rule?.hook?.type === 'ai' && !String(rule.hook.prompt || '').trim()) {
    rule.hook.prompt = defaultAiPrompt()
  }
}

function applyDefaultToolHookPrompt(rule) {
  rule.hook.prompt = defaultAiPrompt()
}

function normalizeToolHookRule(rule) {
  return {
    match: rule?.match || '',
    hook: {
      type: rule?.hook?.type || 'exec',
      when: rule?.hook?.when || 'pre',
      timeout: rule?.hook?.timeout || '5s',
      command: rule?.hook?.command || '',
      args: rule?.hook?.args || [],
      route: rule?.hook?.route || '',
      model: rule?.hook?.model || '',
      prompt: rule?.hook?.prompt || '',
      webhook: rule?.hook?.webhook || '',
    },
  }
}

function addToolHookRule() {
  if (!config.value.tool_hooks) config.value.tool_hooks = []
  const idx = config.value.tool_hooks.length
  config.value.tool_hooks.push(emptyToolHookRule())
  nextTick(() => { openCards['tool-hook/' + idx] = true })
}

function removeToolHookRule(idx) {
  config.value.tool_hooks.splice(idx, 1)
}

// add/delete map entries
const addingSection = ref('')
const addingKey = ref('')
const addInputRef = ref(null)

function startAdd(section) {
  addingSection.value = section
  addingKey.value = ''
  // open the section and focus input after render
  if (section === 'ssh') sshOpen.value = true
  else if (section === 'webhook') webhookOpen.value = true
  else if (section === 'provider') providersOpen.value = true
  else if (section === 'route') routesOpen.value = true
  else if (section === 'mcp') mcpOpen.value = true
  nextTick(() => addInputRef.value?.focus())
}

function confirmAdd() {
  const section = addingSection.value
  const key = addingKey.value.trim()
  if (!key) return
  const map = config.value[section]
  if (map && key in map) {
    error.value = t('config.alreadyExists', { section, key })
    return
  }
  if (!config.value[section]) config.value[section] = {}
  const defaults = {
    ssh: { host: '' },
    webhook: { url: '', method: 'POST' },
    provider: { url: '', protocol: 'openai' },
    route: { providers: [], tools: [] },
    mcp: { command: '' },
  }
  config.value[section][key] = defaults[section] || {}
  openCards[section + '/' + key] = true
  addingSection.value = ''
  addingKey.value = ''
}

function cancelAdd() {
  addingSection.value = ''
  addingKey.value = ''
}

function deleteMapEntry(section, key) {
  if (!confirm(t('config.confirmDelete', { section, key }))) return
  delete config.value[section][key]
  // force reactivity
  config.value[section] = { ...config.value[section] }
}

// clean config before sending: remove empty/null maps, strip __new_ keys from KV editors
function cleanConfig(obj) {
  if (obj === null || obj === undefined) return obj
  if (Array.isArray(obj)) return obj
  if (typeof obj !== 'object') return obj

  const out = {}
  for (const [k, v] of Object.entries(obj)) {
    if (v === null || v === undefined) continue
    if (typeof v === 'object' && !Array.isArray(v)) {
      const cleaned = {}
      for (const [ik, iv] of Object.entries(v)) {
        if (ik.startsWith('__new_')) continue
        cleaned[ik] = cleanConfig(iv)
      }
      if (Object.keys(cleaned).length > 0) out[k] = cleaned
    } else {
      out[k] = v
    }
  }
  return out
}

// load config from server, reset dirty state
async function load() {
  loading.value = true
  try {
    const [cfg, source] = await Promise.all([fetchConfig(), fetchConfigSource()])
    config.value = {
      ...cfg,
      tool_hooks: (cfg.tool_hooks || []).map(normalizeToolHookRule),
    }
    configSource.value = source
    adminPwEdited.value = false
    adminPwValue.value = ''
    error.value = ''
    message.value = ''
    configFileChanged.value = false
    await nextTick() // let deep watcher run while loading=true
    dirty.value = false
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

// discard local edits, reload running config
function discard() {
  if (!confirm(t('config.confirmDiscard'))) return
  load()
}

// validate → save → restart → poll until alive → reload
const waitingAlive = ref(false)
const waitingElapsed = ref(0)

async function pollUntilAlive(timeoutMs = 60000, intervalMs = 1500) {
  const deadline = Date.now() + timeoutMs
  waitingAlive.value = true
  waitingElapsed.value = 0
  const startMs = Date.now()
  const ticker = setInterval(() => {
    waitingElapsed.value = Math.floor((Date.now() - startMs) / 1000)
  }, 500)
  try {
    // first wait a short period so the old process has time to shut down
    await new Promise(r => setTimeout(r, 800))
    while (Date.now() < deadline) {
      try {
        await fetchStatus()
        return true
      } catch {
        await new Promise(r => setTimeout(r, intervalMs))
      }
    }
    return false
  } finally {
    clearInterval(ticker)
    waitingAlive.value = false
    waitingElapsed.value = 0
  }
}

async function apply() {
  applying.value = true
  message.value = ''
  error.value = ''
  try {
    // check if config source is file-based
    if (!configSource.value?.source_type?.file) {
      error.value = t('config.savingDisabled')
      return
    }

    // step 1: validate
    const result = await validateConfig(cleanConfig(config.value))
    if (!result.valid) {
      error.value = t('config.validationFailed', { error: result.error })
      return
    }
    // step 2: save to file
    await saveConfig(cleanConfig(config.value))
    // step 3: restart gateway to apply
    const restart = await restartGateway()
    if (restart.status !== 'ok') {
      error.value = t('config.savedButRestartFailed', { error: restart.error || 'unknown error' })
      return
    }
    // step 4: poll until service is back up (max 60s)
    const alive = await pollUntilAlive()
    if (!alive) {
      error.value = t('config.serviceTimeout')
      return
    }
    // step 5: reload fresh state
    await load()
    message.value = t('config.applied')
    messageType.value = 'success'
  } catch (e) {
    if (e.message?.includes('config file changed externally')) {
      configFileChanged.value = true
      error.value = t('config.externalChangeError')
    } else {
      error.value = e.message
    }
  } finally {
    applying.value = false
  }
}

onMounted(load)
</script>

<style scoped>
h2 { margin-bottom: 16px; }
h3 { margin: 0; font-size: 14px; font-weight: 600; }

.actions {
  display: flex;
  gap: 10px;
  margin-bottom: 20px;
  align-items: center;
}

.config-section {
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  padding: 18px;
  margin-bottom: 16px;
  box-shadow: var(--shadow);
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  cursor: pointer;
  user-select: none;
}

.chevron { color: var(--c-text-3); font-size: 12px; margin-left: auto; }
.count { color: var(--c-text-3); font-weight: normal; font-size: 13px; }

.form-grid {
  display: grid;
  grid-template-columns: 140px 1fr;
  gap: 10px 14px;
  align-items: start;
  margin: 10px 0;
}

.form-grid > label {
  padding-top: 7px;
  font-size: 12px;
  color: var(--c-text-2);
  font-family: var(--font-mono);
}

.req { color: var(--c-danger); }
.hint { color: var(--c-text-3); font-size: 11px; font-weight: normal; }

.patch-row {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-bottom: 6px;
}
.patch-row .form-input { flex: 1; }
.form-input-sm { flex: 0 0 90px !important; }
.btn-danger-icon { color: var(--c-danger); }

.url-field {
  display: flex;
  gap: 6px;
}
.url-field .form-input { flex: 1; }
.url-proxy { flex: 0 0 210px !important; color: var(--c-text-3); }

.secret-field {
  display: flex;
  gap: 6px;
  align-items: center;
}
.secret-field .form-input { flex: 1; }

.badge-none { background: var(--c-border-light); color: var(--c-text-3); }

.card {
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  margin: 8px 0;
  overflow: hidden;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  background: #f8fafc;
  cursor: pointer;
  user-select: none;
  transition: background var(--transition);
}
.card-header:hover { background: var(--c-border-light); }

.card-body {
  padding: 14px;
  border-top: 1px solid var(--c-border);
}

.add-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.add-input { width: 220px; flex: 0 0 220px; }

.tag-proto {
  font-size: 11px;
  background: var(--c-primary-bg);
  color: var(--c-primary);
  padding: 1px 6px;
  border-radius: 3px;
  font-weight: 500;
}

.subsection-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 14px 0 4px;
  font-size: 12px;
  color: var(--c-text-2);
  font-family: var(--font-mono);
}

.form-textarea {
  width: 100%;
  min-height: 90px;
  resize: vertical;
  font-family: var(--font-mono);
  font-size: 12px;
  box-sizing: border-box;
}

.form-hint-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-top: 6px;
}

.form-checkbox {
  width: 16px;
  height: 16px;
  cursor: pointer;
}

@media (max-width: 768px) {
  .form-grid {
    grid-template-columns: 1fr;
    gap: 4px 0;
  }

  .form-grid > label {
    padding-top: 4px;
    padding-bottom: 0;
  }

  .config-section {
    padding: 14px;
  }

  .secret-field {
    flex-wrap: wrap;
  }

  .actions {
    flex-wrap: wrap;
  }
}
</style>
