<template>
  <div class="provider-form">
    <section class="form-panel primary-panel">
      <div class="form-panel-head">
        <div>
          <h4>{{ $t("providerDetail.quickSetupSection") }}</h4>
          <p class="section-desc">{{ $t("providerDetail.quickSetupDesc") }}</p>
        </div>
        <div v-if="currentPreset" class="panel-badges">
          <span class="badge badge-ok">{{ currentPreset.title }}</span>
        </div>
      </div>

      <div class="form-grid">
        <label for="provider-type">{{ $t("providerDetail.providerType") }} <span class="req">*</span></label>
        <div class="field-stack">
          <select
            id="provider-type"
            :value="selectedPresetId"
            class="form-input"
            @change="$emit('access-type-change', $event.target.value)"
          >
            <option v-if="!selectedPresetId" value="" disabled>
              {{ $t("providerDetail.selectProviderTypePlaceholder") }}
            </option>
            <option v-for="option in providerPresets" :key="option.id" :value="option.id">
              {{ option?.title || "" }}
            </option>
          </select>
          <p class="hint">{{ currentPreset?.summary || "" }}</p>
          <p v-if="!currentPreset" class="hint">{{ $t("providerDetail.noPresetWarning") }}</p>
        </div>

        <label for="provider-name">{{ $t("providerDetail.name") }} <span class="req">*</span></label>
        <input
          v-if="isCreate"
          id="provider-name"
          :value="providerName"
          @input="$emit('update:providerName', $event.target.value.trim())"
          class="form-input"
          :placeholder="$t('providerDetail.namePlaceholder')"
        />
        <input v-else id="provider-name" :value="providerName" class="form-input" readonly />

        <template v-if="mode === 'direct'">
          <label for="provider-url">url <span class="req">*</span></label>
          <div class="field-stack">
            <input
              id="provider-url"
              v-model="localProviderConfig.url"
              class="form-input"
              :placeholder="providerUrlPlaceholder(localProviderConfig)"
            />
          </div>

          <label for="auth-source">{{ $t("providerDetail.authSource") }}</label>
          <div class="field-stack">
            <select
              id="auth-source"
              :value="selectedAuthSource"
              @change="$emit('update:selectedAuthSource', $event.target.value)"
              class="form-input"
            >
              <option v-for="option in directAuthOptions" :key="option.id" :value="option.id">
                {{ option.title }}
              </option>
            </select>
            <p v-if="authSourceHint" class="hint">{{ authSourceHint }}</p>

            <div class="auth-source-details">
              <template v-if="effectiveAuthMode === 'api_key'">
                <label class="auth-detail-label" for="api-key-input">api_key</label>
                <div class="secret-field">
                  <input
                    id="api-key-input"
                    :type="showAPIKey ? 'text' : 'password'"
                    :value="secretDisplay(localProviderConfig.api_key)"
                    @input="handleApiKeyInput"
                    class="form-input"
                    :placeholder="$t('providerDetail.apiKeyPlaceholder')"
                  />
                  <button
                    class="btn-icon"
                    @click="$emit('update:showAPIKey', !showAPIKey)"
                    type="button"
                    :aria-label="$t('providerDetail.toggleApiKeyVisibility')"
                  >
                    {{ showAPIKey ? "🙈" : "👁" }}
                  </button>
                  <span :class="['badge', isSecretConfigured(localProviderConfig.api_key) ? 'badge-ok' : 'badge-none']">
                    {{ isSecretConfigured(localProviderConfig.api_key) ? $t("common.configured") : $t("common.notSet") }}
                  </span>
                </div>
                <ConnectionProbe
                  :provider-name="providerName"
                  :url="localProviderConfig.url"
                  :api-key="localProviderConfig.api_key"
                  :headers="localProviderConfig.headers"
                  :proxy="localProviderConfig.proxy"
                  @suggest="handleProbeSuggest"
                />
              </template>

              <template v-else-if="effectiveAuthMode === 'command'">
                <label class="auth-detail-label" for="api-key-command">api_key_command</label>
                <div class="field-stack">
                  <input
                    id="api-key-command"
                    v-model="localProviderConfig.api_key_command"
                    class="form-input"
                    :placeholder="$t('providerDetail.apiKeyCommandPlaceholder')"
                  />
                  <p class="hint">{{ $t("providerDetail.apiKeyCommandHint") }}</p>
                </div>
                <div class="auth-command-grid">
                  <div class="field-stack">
                    <label class="auth-detail-label" for="api-key-command-timeout">{{ $t("providerDetail.apiKeyCommandTimeout") }}</label>
                    <input id="api-key-command-timeout" v-model="localProviderConfig.api_key_command_timeout" class="form-input" placeholder="5s" />
                  </div>
                  <div class="field-stack">
                    <label class="auth-detail-label" for="api-key-command-ttl">{{ $t("providerDetail.apiKeyCommandTTL") }}</label>
                    <input id="api-key-command-ttl" v-model="localProviderConfig.api_key_command_ttl" class="form-input" placeholder="5m" />
                  </div>
                </div>
              </template>
            </div>
          </div>

        </template>

        <template v-else-if="mode === 'cliproxy'">
          <label for="provider-url">{{ $t("providerDetail.cliproxyEndpoint") }}</label>
          <div class="field-stack">
            <input
              id="provider-url"
              v-model="localProviderConfig.url"
              class="form-input"
              :placeholder="providerUrlPlaceholder(localProviderConfig)"
            />
            <p class="hint">{{ $t("providerDetail.cliproxyEndpointHint") }}</p>
          </div>

          <label for="backend-provider">backend_provider <span class="req">*</span></label>
          <div class="field-stack">
            <input
              id="backend-provider"
              v-model="localProviderConfig.backend_provider"
              class="form-input"
              placeholder="codex"
            />
          </div>

          <div class="form-grid-full">
            <CLIProxyAuthManager
              :is-managed-cli-proxy-access="true"
              :config-doc="configDoc"
              :is-create="isCreate"
              :provider-name="providerName"
              verify-model=""
              @success="$emit('auth-success', $event)"
              @error="$emit('auth-error', $event)"
            />
          </div>
        </template>

        <template v-else-if="mode === 'copilot'">
          <label for="config-dir">config_dir <span class="req">*</span></label>
          <div class="field-stack">
            <input
              id="config-dir"
              v-model="localProviderConfig.config_dir"
              class="form-input"
              :placeholder="configDirPlaceholder"
            />
            <p class="hint">{{ $t("providerDetail.configDirAuthHint") }}</p>
          </div>
        </template>

        <label for="service-interface">{{ $t("providerDetail.availableInterfaces") }}</label>
        <div class="field-stack">
          <select
            id="service-interface"
            :value="selectedServiceTemplateId"
            class="form-input"
            @change="$emit('service-template-change', $event.target.value)"
          >
            <option v-if="!selectedServiceTemplateId" value="" disabled>
              {{ $t("providerDetail.selectInterfacePlaceholder") }}
            </option>
            <option v-for="template in visibleServiceProtocolTemplates" :key="template.id" :value="template.id">
              {{ serviceTemplateTitle(template) }}
            </option>
          </select>
          <p class="hint">{{ currentServiceTemplateSummary }}</p>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  normalizeServiceProtocols,
  providerBackend,
  providerFamily,
} from "../config-utils.js";
import {
  effectiveAuthSource as resolveEffectiveAuthSource,
  isSecretConfigured,
  providerUrlPlaceholder,
  secretDisplay,
} from "../utils/providerHelpers.ts";
import CLIProxyAuthManager from "./CLIProxyAuthManager.vue";
import ConnectionProbe from "./ConnectionProbe.vue";

const { t } = useI18n();

const props = defineProps({
  providerName: { type: String, required: true },
  providerConfig: { type: Object, required: true },
  selectedPresetId: { type: String, required: true },
  selectedServiceTemplateId: { type: String, required: true },
  selectedAuthSource: { type: String, required: true },
  showAPIKey: { type: Boolean, required: true },
  apiKeyTouched: { type: Boolean, required: true },
  isCreate: { type: Boolean, default: false },
  providerPresets: { type: Array, required: true },
  currentPreset: { type: Object, default: null },
  visibleServiceProtocolTemplates: { type: Array, required: true },
  configDoc: { type: Object, required: true },
});

const emit = defineEmits([
  "update:providerName",
  "update:providerConfig",
  "update:selectedAuthSource",
  "update:showAPIKey",
  "update:apiKeyTouched",
  "access-type-change",
  "service-template-change",
  "auth-success",
  "auth-error",
]);

// Read-only access to providerConfig. Template v-model on nested properties
// mutates the object directly; the parent detects changes via deep watch.
const localProviderConfig = computed(() => props.providerConfig);

const currentServiceTemplateSummary = computed(() => {
  if (!props.selectedServiceTemplateId) return t("providerDetail.interfaceTemplateNoMatchDesc");
  const current = props.visibleServiceProtocolTemplates.find((template) => template.id === props.selectedServiceTemplateId);
  return current?.summary || t("providerDetail.interfaceTemplateNoMatchDesc");
});

const mode = computed(() => {
  const family = providerFamily(props.providerConfig);
  const backend = providerBackend(props.providerConfig);
  if (family === "copilot") return "copilot";
  if (backend === "cliproxy") return "cliproxy";
  return "direct";
});

const directAuthOptions = computed(() => {
  return ["api_key", "command", "none"].map((id) => ({
    id,
    title: authSourceTitle(id),
  }));
});

const effectiveAuthMode = computed(() => {
  return resolveEffectiveAuthSource(props.providerConfig, props.selectedAuthSource);
});

const authSourceHint = computed(() => {
  switch (effectiveAuthMode.value) {
    case "command": return t("providerDetail.apiKeyCommandSecurityHint");
    case "none": return t("providerDetail.noAuthHint");
    default: return "";
  }
});

const configDirPlaceholder = computed(() => {
  if (props.currentPreset?.default_config_dir) return props.currentPreset.default_config_dir;
  if (providerFamily(props.providerConfig) === "copilot") return "~/.config/github-copilot";
  return "";
});

function serviceTemplateTitle(template) {
  if (!template?.id) return "";
  const key = `providerDetail.interfaceTemplate_${template.id}`;
  const translated = t(key);
  return translated === key ? template.title || template.id : translated;
}

function authSourceTitle(source) {
  switch (source) {
    case "api_key": return t("providerDetail.authSourceStatic");
    case "command": return t("providerDetail.authSourceCommand");
    case "config_dir": return t("providerDetail.authSourceConfigDir");
    case "none": return t("providerDetail.authSourceNone");
    default: return source || "-";
  }
}

function handleApiKeyInput(event) {
  emit("update:apiKeyTouched", true);
  const newConfig = { ...props.providerConfig, api_key: event.target.value };
  emit("update:providerConfig", newConfig);
}

function handleProbeSuggest(suggestion) {
  const newConfig = { ...props.providerConfig };
  const capabilities = suggestion.capabilities || [];
  const formats = suggestion.formats || [];

  newConfig.service_protocols = [...capabilities];

  // Dual-protocol provider: create multi-endpoint configuration
  if (formats.length > 1 && formats.includes("openai") && formats.includes("anthropic")) {
    const url = suggestion.resolvedURL || props.providerConfig.url;
    newConfig.endpoint = {
      default: {
        url,
        format: "openai",
        protocols: capabilities.filter((p) => p !== "anthropic"),
      },
      anthropic: {
        url,
        format: "anthropic",
        protocols: ["chat", "anthropic"],
      },
    };
    newConfig.family = "openai";
  } else {
    if (formats.length === 1) {
      newConfig.family = formats[0];
    } else if (formats.length > 1) {
      const currentFamily = providerFamily(newConfig);
      if (!formats.includes(currentFamily)) {
        newConfig.family = "openai";
      }
    }

    if (newConfig.family === "openai") {
      newConfig.anthropic_to_chat = capabilities.includes("anthropic");
    } else if (newConfig.family === "anthropic") {
      newConfig.anthropic_to_responses = capabilities.includes("responses");
    }
  }

  emit("update:providerConfig", newConfig);

  const suggested = normalizeServiceProtocols(capabilities);
  const matchedTemplate = props.visibleServiceProtocolTemplates.find((template) => {
    const templateProtocols = normalizeServiceProtocols(template.service_protocols);
    if (templateProtocols.length !== suggested.length) return false;
    return templateProtocols.every((p) => suggested.includes(p));
  });

  if (matchedTemplate) {
    emit("service-template-change", matchedTemplate.id);
  }
}
</script>

<style scoped>
.provider-form {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.form-panel {
  border-top: 1px solid var(--c-border);
  padding-top: 18px;
}

.form-panel:first-of-type {
  border-top: none;
  padding-top: 0;
}

.form-panel-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 12px;
}

.form-panel-head h4 {
  margin: 0;
  font-size: 15px;
}

.panel-badges {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.section-desc {
  margin-top: 6px;
  font-size: 13px;
  color: var(--c-text-3);
  max-width: 760px;
}

.form-grid {
  display: grid;
  grid-template-columns: 160px 1fr;
  gap: 10px 14px;
  align-items: start;
}

.form-grid > label {
  padding-top: 7px;
  font-size: 12px;
  color: var(--c-text-2);
  font-family: var(--font-mono);
}

.form-grid-full {
  grid-column: 1 / -1;
}

.field-stack {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.auth-source-details {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  padding-top: 4px;
}

.auth-detail-label {
  font-size: 12px;
  color: var(--c-text-2);
  font-family: var(--font-mono);
}

.auth-command-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.req {
  color: var(--c-danger);
}

.hint {
  color: var(--c-text-3);
  font-size: 11px;
  font-weight: normal;
}

.secret-field {
  display: flex;
  gap: 8px;
  align-items: center;
}

.secret-field .form-input {
  flex: 1;
}

.badge-muted {
  background: var(--c-bg-soft);
  color: var(--c-text-2);
}

@media (max-width: 768px) {
  .form-grid,
  .auth-command-grid {
    grid-template-columns: 1fr;
  }

  .form-grid > label {
    padding-top: 0;
  }

  .secret-field {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
