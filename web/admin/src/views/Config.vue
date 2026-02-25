<template>
  <div>
    <h2>Configuration</h2>

    <div v-if="message" :class="['msg', messageType]">{{ message }}</div>
    <div v-if="error" class="msg error">{{ error }}</div>

    <div class="actions">
      <button @click="apply" class="btn btn-primary" :disabled="applying">
        {{ applying ? 'Applying...' : 'Apply' }}
      </button>
      <button v-if="dirty" @click="discard" class="btn btn-secondary">Discard Changes</button>
    </div>

    <!-- General -->
    <section class="config-section">
      <h3>General</h3>
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

        <label>log.file_dir</label>
        <input :value="logFileDir" @input="setLogFileDir($event.target.value)" class="form-input" placeholder="/var/log/warden" />
      </div>
    </section>

    <!-- SSH -->
    <section class="config-section">
      <div class="section-header" @click="sshOpen = !sshOpen">
        <h3>SSH <span class="count">({{ sshCount }})</span></h3>
        <span class="chevron">{{ sshOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="sshOpen">
        <button class="btn btn-small" @click="addMapEntry('ssh')">+ Add</button>
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
            <button class="btn btn-danger btn-small" @click="deleteMapEntry('ssh', name)">Delete</button>
          </div>
        </div>
      </div>
    </section>

    <!-- Providers -->
    <section class="config-section">
      <div class="section-header" @click="providersOpen = !providersOpen">
        <h3>Providers <span class="count">({{ providerCount }})</span></h3>
        <span class="chevron">{{ providersOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="providersOpen">
        <button class="btn btn-small" @click="addMapEntry('provider')">+ Add</button>
        <div v-for="(cfg, name) in config.provider" :key="'prov-'+name" class="card">
          <div class="card-header" @click="toggleCard('provider', name)">
            <strong>{{ name }}</strong>
            <span class="tag-proto">{{ cfg.protocol || 'openai' }}</span>
            <span class="chevron">{{ isCardOpen('provider', name) ? '▼' : '▶' }}</span>
          </div>
          <div v-show="isCardOpen('provider', name)" class="card-body">
            <div class="form-grid">
              <label>url <span class="req">*</span></label>
              <input v-model="cfg.url" class="form-input" />

              <label>protocol</label>
              <select v-model="cfg.protocol" class="form-input">
                <option value="openai">openai</option>
                <option value="anthropic">anthropic</option>
                <option value="ollama">ollama</option>
                <option value="qwen">qwen</option>
                <option value="copilot">copilot</option>
              </select>

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

              <label>timeout</label>
              <input v-model="cfg.timeout" class="form-input" placeholder="60s" />

              <label>proxy</label>
              <input v-model="cfg.proxy" class="form-input" placeholder="socks5://127.0.0.1:1080" />

              <label>config_dir</label>
              <input v-model="cfg.config_dir" class="form-input" />

              <label>ssh</label>
              <select v-model="cfg.ssh" class="form-input">
                <option value="">(none)</option>
                <option v-for="s in sshNames" :key="s" :value="s">{{ s }}</option>
              </select>

              <label>models</label>
              <TagListEditor v-model="cfg.models" placeholder="Model ID" />

              <label>model_aliases</label>
              <KeyValueEditor v-model="cfg.model_aliases" keyPlaceholder="Alias" valuePlaceholder="Real model" />

              <label>headers</label>
              <KeyValueEditor v-model="cfg.headers" keyPlaceholder="Header name" valuePlaceholder="Value" />
            </div>
            <button class="btn btn-danger btn-small" @click="deleteMapEntry('provider', name)">Delete</button>
          </div>
        </div>
      </div>
    </section>

    <!-- Routes -->
    <section class="config-section">
      <div class="section-header" @click="routesOpen = !routesOpen">
        <h3>Routes <span class="count">({{ routeCount }})</span></h3>
        <span class="chevron">{{ routesOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="routesOpen">
        <button class="btn btn-small" @click="addMapEntry('route')">+ Add</button>
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
            <button class="btn btn-danger btn-small" @click="deleteMapEntry('route', prefix)">Delete</button>
          </div>
        </div>
      </div>
    </section>

    <!-- MCP -->
    <section class="config-section">
      <div class="section-header" @click="mcpOpen = !mcpOpen">
        <h3>MCP <span class="count">({{ mcpCount }})</span></h3>
        <span class="chevron">{{ mcpOpen ? '▼' : '▶' }}</span>
      </div>
      <div v-show="mcpOpen">
        <button class="btn btn-small" @click="addMapEntry('mcp')">+ Add</button>
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
            <button class="btn btn-danger btn-small" @click="deleteMapEntry('mcp', name)">Delete</button>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, computed, reactive, watch, nextTick, onMounted } from 'vue'
import { fetchConfig, saveConfig, validateConfig, restartGateway } from '../api.js'
import KeyValueEditor from '../components/KeyValueEditor.vue'
import TagListEditor from '../components/TagListEditor.vue'

const REDACTED = '__REDACTED__'

const config = ref({})
const message = ref('')
const messageType = ref('success')
const error = ref('')
const applying = ref(false)
const dirty = ref(false)
const loading = ref(false)

// track config modifications via deep watch
watch(config, () => {
  if (!loading.value) dirty.value = true
}, { deep: true })

// section collapse state
const sshOpen = ref(false)
const providersOpen = ref(true)
const routesOpen = ref(true)
const mcpOpen = ref(false)
const showAdminPw = ref(false)

// per-card collapse state
const openCards = reactive({})

// secret visibility toggles
const visibleSecrets = reactive({})

// counts
const sshCount = computed(() => Object.keys(config.value.ssh || {}).length)
const providerCount = computed(() => Object.keys(config.value.provider || {}).length)
const routeCount = computed(() => Object.keys(config.value.route || {}).length)
const mcpCount = computed(() => Object.keys(config.value.mcp || {}).length)

// names for cross-references
const sshNames = computed(() => Object.keys(config.value.ssh || {}))
const providerNames = computed(() => Object.keys(config.value.provider || {}))
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

// log config helper
const logFileDir = computed(() => config.value.log?.file_dir || '')
function setLogFileDir(val) {
  if (!config.value.log) config.value.log = {}
  config.value.log.file_dir = val
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

// add/delete map entries
function addMapEntry(section) {
  const key = prompt(`Enter name for new ${section} entry:`)
  if (!key) return
  const map = config.value[section]
  if (map && key in map) {
    error.value = `${section} "${key}" already exists`
    return
  }
  if (!config.value[section]) config.value[section] = {}
  const defaults = {
    ssh: { host: '' },
    provider: { url: '', protocol: 'openai' },
    route: { providers: [], tools: [] },
    mcp: { command: '' },
  }
  config.value[section][key] = defaults[section] || {}
  openCards[section + '/' + key] = true
}

function deleteMapEntry(section, key) {
  if (!confirm(`Delete ${section} "${key}"?`)) return
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
    const cfg = await fetchConfig()
    config.value = cfg
    adminPwEdited.value = false
    adminPwValue.value = ''
    error.value = ''
    message.value = ''
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
  if (!confirm('Discard all unsaved changes?')) return
  load()
}

// validate → save → restart → reload
async function apply() {
  applying.value = true
  message.value = ''
  error.value = ''
  try {
    // step 1: validate
    const result = await validateConfig(cleanConfig(config.value))
    if (!result.valid) {
      error.value = 'Validation failed: ' + result.error
      return
    }
    // step 2: save to file
    await saveConfig(cleanConfig(config.value))
    // step 3: restart gateway to apply
    const restart = await restartGateway()
    if (restart.status !== 'ok') {
      error.value = 'Saved but restart failed: ' + (restart.error || 'unknown error')
      return
    }
    // step 4: reload fresh state
    await load()
    message.value = 'Configuration applied'
    messageType.value = 'success'
  } catch (e) {
    error.value = e.message
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

.tag-proto {
  font-size: 11px;
  background: var(--c-primary-bg);
  color: var(--c-primary);
  padding: 1px 6px;
  border-radius: 3px;
  font-weight: 500;
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
