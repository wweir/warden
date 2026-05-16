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
        <label>{{ $t("providerDetail.providerType") }} <span class="req">*</span></label>
        <div class="field-stack">
          <select
            :value="selectedPresetId"
            class="form-input"
            @change="$emit('access-type-change', $event.target.value)"
          >
            <option v-for="option in accessTypeOptions" :key="option.id" :value="option.id">
              {{ accessTypeTitle(option) }}
            </option>
          </select>
          <p class="hint">{{ currentAccessTypeSummary }}</p>
        </div>

        <div v-if="isCustomAccessType" class="form-grid-full custom-interface-editor">
          <div class="custom-interface-head">
            <span class="interface-preview-title">{{ $t("providerDetail.customAccessSection") }}</span>
            <span class="hint">{{ $t("providerDetail.customAccessDesc") }}</span>
          </div>
          <div class="form-grid compact-grid">
            <label>{{ $t("providerDetail.family") }} <span class="req">*</span></label>
            <select v-model="localProviderConfig.family" class="form-input">
              <option value="">{{ $t("providerDetail.selectFamily") }}</option>
              <option value="openai">openai</option>
              <option value="anthropic">anthropic</option>
              <option value="copilot">copilot</option>
            </select>

            <template v-if="providerFamily(localProviderConfig) === 'openai'">
              <label>backend</label>
              <select v-model="localProviderConfig.backend" class="form-input">
                <option value="">default</option>
                <option value="cliproxy">cliproxy</option>
              </select>

              <template v-if="providerBackend(localProviderConfig) === 'cliproxy'">
                <label>backend_provider <span class="req">*</span></label>
                <input v-model="localProviderConfig.backend_provider" class="form-input" placeholder="codex" />
              </template>
            </template>
          </div>
        </div>

        <label>{{ $t("providerDetail.name") }} <span class="req">*</span></label>
        <input
          v-if="isCreate"
          :value="providerName"
          @input="$emit('update:providerName', $event.target.value.trim())"
          class="form-input"
          :placeholder="$t('providerDetail.namePlaceholder')"
        />
        <input v-else :value="providerName" class="form-input" readonly />

        <template v-if="showsURLField">
          <label>{{ providerUrlLabel }} <span class="req">*</span></label>
          <div class="field-stack">
            <input
              v-model="localProviderConfig.url"
              class="form-input"
              :placeholder="providerUrlPlaceholder(localProviderConfig)"
            />
            <p v-if="providerUrlHint" class="hint">{{ providerUrlHint }}</p>
          </div>
        </template>

        <template v-else>
          <label>{{ $t("providerDetail.connectionSection") }}</label>
          <div class="section-note">{{ connectionNote }}</div>
        </template>

        <template v-if="authSourceOptions.length > 0">
          <label>{{ $t("providerDetail.authSource") }}</label>
          <div class="field-stack">
            <select
              :value="selectedAuthSource"
              @change="$emit('update:selectedAuthSource', $event.target.value)"
              class="form-input"
            >
              <option v-for="option in authSourceOptions" :key="option.id" :value="option.id">
                {{ option.title }}
              </option>
            </select>
            <p v-if="authSourceHint" class="hint">{{ authSourceHint }}</p>
            <div class="auth-source-details">
              <template v-if="authMode === 'api_key'">
                <label class="auth-detail-label">api_key</label>
                <div class="secret-field">
                  <input
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
                    {{
                      isSecretConfigured(localProviderConfig.api_key)
                        ? $t("common.configured")
                        : $t("common.notSet")
                    }}
                  </span>
                </div>
              </template>

              <template v-else-if="authMode === 'command'">
                <label class="auth-detail-label">api_key_command</label>
                <div class="field-stack">
                  <input
                    v-model="localProviderConfig.api_key_command"
                    class="form-input"
                    :placeholder="$t('providerDetail.apiKeyCommandPlaceholder')"
                  />
                  <p class="hint">{{ $t("providerDetail.apiKeyCommandHint") }}</p>
                </div>
                <div class="auth-command-grid">
                  <div class="field-stack">
                    <label class="auth-detail-label">{{ $t("providerDetail.apiKeyCommandTimeout") }}</label>
                    <input v-model="localProviderConfig.api_key_command_timeout" class="form-input" placeholder="5s" />
                  </div>
                  <div class="field-stack">
                    <label class="auth-detail-label">{{ $t("providerDetail.apiKeyCommandTTL") }}</label>
                    <input v-model="localProviderConfig.api_key_command_ttl" class="form-input" placeholder="5m" />
                  </div>
                </div>
              </template>

              <template v-else-if="authMode === 'config_dir'">
                <label class="auth-detail-label">config_dir</label>
                <input v-model="localProviderConfig.config_dir" class="form-input" :placeholder="configDirPlaceholder" />
              </template>

              <template v-else>
                <div class="section-note">{{ authNote }}</div>
                <CLIProxyAuthManager
                  v-if="isManagedCLIProxyAccess"
                  :is-managed-cli-proxy-access="isManagedCLIProxyAccess"
                  :config-doc="configDoc"
                  :is-create="isCreate"
                  :provider-name="providerName"
                  :verify-model="verifyModel"
                  @success="handleAuthSuccess"
                  @error="handleAuthError"
                />
              </template>
            </div>
          </div>
        </template>

        <label>{{ $t("providerDetail.availableInterfaces") }}</label>
        <div class="field-stack">
          <select
            :value="selectedServiceTemplateId"
            class="form-input"
            @change="$emit('service-template-change', $event.target.value)"
          >
            <option v-for="template in capabilityTemplateOptions" :key="template.id" :value="template.id">
              {{ serviceTemplateTitle(template) }}
            </option>
          </select>
          <p class="hint">{{ currentServiceTemplateSummary }}</p>
        </div>

        <div class="form-grid-full interface-preview">
          <span class="interface-preview-title">{{ $t("providerDetail.finalInterfaces") }}</span>
          <div class="protocol-chip-list">
            <span v-for="protocol in effectiveServiceProtocols" :key="protocol" class="badge badge-muted">
              {{ serviceProtocolTitle(protocol) }}
            </span>
            <span v-if="effectiveServiceProtocols.length === 0" class="hint">
              {{ $t("providerDetail.noEffectiveProtocols") }}
            </span>
          </div>
          <p class="hint">{{ $t("providerDetail.finalInterfacesHint") }}</p>
        </div>

        <div v-if="isCustomServiceTemplate" class="form-grid-full custom-interface-editor">
          <div class="custom-interface-head">
            <span class="interface-preview-title">{{ $t("providerDetail.customInterfacesSection") }}</span>
            <span class="hint">{{ $t("providerDetail.customInterfacesDesc") }}</span>
          </div>
          <div class="form-grid compact-grid">
            <label>{{ $t("providerDetail.rawServiceProtocols") }}</label>
            <div class="service-protocols-editor">
              <TagListEditor
                v-model="localProviderConfig.service_protocols"
                :suggestions="serviceProtocolSuggestions"
                :placeholder="$t('providerDetail.serviceProtocolsPlaceholder')"
              />
              <p class="hint service-protocols-hint">{{ $t("providerDetail.serviceProtocolsHint") }}</p>
            </div>

            <template v-if="providerFamily(localProviderConfig) === 'openai'">
              <label>responses_to_chat</label>
              <div class="form-hint-row">
                <input type="checkbox" v-model="localProviderConfig.responses_to_chat" class="form-checkbox" />
                <span class="hint">{{ $t("config.responsesToChatHint") }}</span>
              </div>

              <label>anthropic_to_chat</label>
              <div class="form-hint-row">
                <input type="checkbox" v-model="localProviderConfig.anthropic_to_chat" class="form-checkbox" />
                <span class="hint">{{ $t("config.anthropicToChatHint") }}</span>
              </div>
            </template>

            <template v-if="providerFamily(localProviderConfig) === 'anthropic'">
              <label>anthropic_to_responses</label>
              <div class="form-hint-row">
                <input type="checkbox" v-model="localProviderConfig.anthropic_to_responses" class="form-checkbox" />
                <span class="hint">{{ $t("config.anthropicToResponsesHint") }}</span>
              </div>
            </template>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  defaultServiceProtocolsForProvider,
  normalizeServiceProtocols,
  providerBackend,
  providerFamily,
} from "../config-utils.js";
import { inferAuthSource, isSecretConfigured, providerUrlPlaceholder, secretDisplay } from "../utils/providerHelpers.ts";
import CLIProxyAuthManager from "./CLIProxyAuthManager.vue";
import TagListEditor from "./TagListEditor.vue";

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
  serviceProtocolTemplates: { type: Array, required: true },
  currentPreset: { type: Object, default: null },
  isCustomAccessType: { type: Boolean, required: true },
  isManagedCLIProxyAccess: { type: Boolean, required: true },
  isCustomServiceTemplate: { type: Boolean, required: true },
  visibleServiceProtocolTemplates: { type: Array, required: true },
  configDoc: { type: Object, required: true },
});

const emit = defineEmits([
  "update:providerName",
  "update:providerConfig",
  "update:selectedPresetId",
  "update:selectedServiceTemplateId",
  "update:selectedAuthSource",
  "update:showAPIKey",
  "update:apiKeyTouched",
  "access-type-change",
  "service-template-change",
  "auth-success",
  "auth-error",
]);

const CUSTOM_ACCESS_TYPE = "__custom_access__";
const CUSTOM_SERVICE_TEMPLATE = "__custom__";

const localProviderConfig = computed({
  get: () => props.providerConfig,
  set: (val) => emit("update:providerConfig", val),
});

const accessTypeOptions = computed(() => [
  ...props.providerPresets,
  {
    id: CUSTOM_ACCESS_TYPE,
    title: t("providerDetail.customAccessType"),
    summary: t("providerDetail.customAccessTypeDesc"),
  },
]);

const currentAccessTypeSummary = computed(() => {
  const current = accessTypeOptions.value.find((option) => option.id === props.selectedPresetId);
  return current?.summary || "";
});

const capabilityTemplateOptions = computed(() => [
  ...props.visibleServiceProtocolTemplates,
  {
    id: CUSTOM_SERVICE_TEMPLATE,
    title: t("providerDetail.interfaceTemplateCustom"),
    summary: t("providerDetail.interfaceTemplateCustomDesc"),
  },
]);

const currentServiceTemplateSummary = computed(() => {
  const current = capabilityTemplateOptions.value.find((template) => template.id === props.selectedServiceTemplateId);
  return current?.summary || t("providerDetail.interfaceTemplateCustomDesc");
});

const effectiveServiceProtocols = computed(() => {
  const configured = normalizeServiceProtocols(props.providerConfig.service_protocols);
  if (configured.length > 0) return configured;
  return defaultServiceProtocolsForProvider(props.providerConfig);
});

const showsURLField = computed(
  () =>
    !!providerFamily(props.providerConfig) &&
    !["copilot"].includes(providerFamily(props.providerConfig)) &&
    !props.isManagedCLIProxyAccess
);

const isCLIProxyBackend = computed(() => providerBackend(props.providerConfig) === "cliproxy");

const connectionNote = computed(() =>
  props.isManagedCLIProxyAccess ? t("providerDetail.cliproxyConnectionNote") : t("providerDetail.noUrlRequired")
);

const authNote = computed(() =>
  props.isManagedCLIProxyAccess ? t("providerDetail.cliproxyAuthNote") : t("providerDetail.authManagedByBackend")
);

const providerUrlLabel = computed(() =>
  isCLIProxyBackend.value ? t("providerDetail.cliproxyEndpoint") : "url"
);

const providerUrlHint = computed(() =>
  isCLIProxyBackend.value ? t("providerDetail.cliproxyEndpointHint") : ""
);

const authSourceOptions = computed(() => {
  const family = providerFamily(props.providerConfig);
  if (!family) return [];
  if (providerBackend(props.providerConfig) === "cliproxy") {
    return [{ id: "none", title: t("providerDetail.authSourceCLIProxyAuthDir") }];
  }
  if (family === "copilot") {
    return [
      { id: "config_dir", title: authSourceTitle("config_dir") },
      { id: "api_key", title: authSourceTitle("api_key") },
      { id: "command", title: authSourceTitle("command") },
      { id: "none", title: authSourceTitle("none") },
    ];
  }
  return [
    { id: "api_key", title: authSourceTitle("api_key") },
    { id: "command", title: authSourceTitle("command") },
    { id: "none", title: authSourceTitle("none") },
  ];
});

const authMode = computed(() => {
  const available = authSourceOptions.value.map((option) => option.id);
  if (available.includes(props.selectedAuthSource)) {
    return props.selectedAuthSource;
  }
  const inferred = inferAuthSource(props.providerConfig);
  if (available.includes(inferred)) {
    return inferred;
  }
  return available[0] || "";
});

const authSourceHint = computed(() => {
  if (props.isManagedCLIProxyAccess) {
    return t("providerDetail.cliproxyAuthNote");
  }
  switch (authMode.value) {
    case "command": return t("providerDetail.apiKeyCommandSecurityHint");
    case "config_dir": return t("providerDetail.configDirAuthHint");
    case "none": return t("providerDetail.noAuthHint");
    default: return "";
  }
});

const configDirPlaceholder = computed(() => {
  if (props.currentPreset?.default_config_dir) return props.currentPreset.default_config_dir;
  switch (providerFamily(props.providerConfig)) {
    case "copilot": return "~/.config/github-copilot";
    default: return "";
  }
});

const serviceProtocolSuggestions = computed(() => {
  if (providerBackend(props.providerConfig) === "cliproxy") {
    return ["chat", "responses"];
  }
  switch (providerFamily(props.providerConfig)) {
    case "openai": {
      const protocols = ["chat", "responses", "embeddings"];
      if (props.providerConfig?.anthropic_to_chat) protocols.push("anthropic");
      return protocols;
    }
    case "anthropic": {
      const protocols = ["chat", "anthropic"];
      if (props.providerConfig?.anthropic_to_responses) protocols.push("responses");
      return protocols;
    }
    case "copilot": return ["chat"];
    default: return [];
  }
});

const verifyModel = computed(() => "");

function accessTypeTitle(option) {
  return option?.title || "";
}

function serviceTemplateTitle(template) {
  if (!template?.id) return "";
  if (template.id === CUSTOM_SERVICE_TEMPLATE) return template.title || "";
  const key = `providerDetail.interfaceTemplate_${template.id}`;
  const translated = t(key);
  return translated === key ? template.title || template.id : translated;
}

function serviceProtocolTitle(protocol) {
  const key = `providerDetail.serviceProtocol_${protocol}`;
  const translated = t(key);
  return translated === key ? protocol : translated;
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

function handleAuthSuccess(filename) {
  emit("auth-success", filename);
}

function handleAuthError(error) {
  emit("auth-error", error);
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

.compact-grid {
  grid-template-columns: 150px 1fr;
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

.protocol-chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.interface-preview {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  background: var(--c-bg-soft);
  border: 1px solid var(--c-border);
  border-radius: 8px;
}

.interface-preview-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text-2);
}

.custom-interface-editor {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--c-border);
  border-radius: 8px;
}

.custom-interface-head {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.section-note {
  font-size: 13px;
  color: var(--c-text-3);
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

.form-hint-row {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 34px;
}

.service-protocols-editor {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.service-protocols-hint {
  margin: 0;
}

.form-checkbox {
  width: 16px;
  height: 16px;
  margin-top: 1px;
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
