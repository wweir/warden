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
            <option v-if="!selectedPresetId" value="" disabled>
              {{ $t("providerDetail.selectProviderTypePlaceholder") }}
            </option>
            <option v-for="option in accessTypeOptions" :key="option.id" :value="option.id">
              {{ accessTypeTitle(option) }}
            </option>
          </select>
          <p class="hint">{{ currentAccessTypeSummary }}</p>
          <p v-if="!currentPreset" class="hint">{{ $t("providerDetail.noPresetWarning") }}</p>
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
            <option v-if="!selectedServiceTemplateId" value="" disabled>
              {{ $t("providerDetail.selectInterfacePlaceholder") }}
            </option>
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
      </div>
    </section>
  </div>
</template>

<script setup>
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import {
  normalizeServiceProtocols,
  providerFamily,
} from "../config-utils.js";
import {
  authSourceIDs,
  effectiveAuthSource as resolveEffectiveAuthSource,
  isSecretConfigured,
  providerEffectiveBackend,
  providerUrlPlaceholder,
  secretDisplay,
} from "../utils/providerHelpers.ts";
import CLIProxyAuthManager from "./CLIProxyAuthManager.vue";

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
  isManagedCLIProxyAccess: { type: Boolean, required: true },
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


const localProviderConfig = computed({
  get: () => props.providerConfig,
  set: (val) => emit("update:providerConfig", val),
});

const accessTypeOptions = computed(() => props.providerPresets);

const currentAccessTypeSummary = computed(() => {
  const current = accessTypeOptions.value.find((option) => option.id === props.selectedPresetId);
  return current?.summary || "";
});

const capabilityTemplateOptions = computed(() => props.visibleServiceProtocolTemplates);

const currentServiceTemplateSummary = computed(() => {
  if (!props.selectedServiceTemplateId) return t("providerDetail.interfaceTemplateNoMatchDesc");
  const current = capabilityTemplateOptions.value.find((template) => template.id === props.selectedServiceTemplateId);
  return current?.summary || t("providerDetail.interfaceTemplateNoMatchDesc");
});

const effectiveServiceProtocols = computed(() => {
  return normalizeServiceProtocols(props.providerConfig.service_protocols);
});

const showsURLField = computed(
  () =>
    !!providerFamily(props.providerConfig) &&
    !["copilot"].includes(providerFamily(props.providerConfig)) &&
    !props.isManagedCLIProxyAccess
);

const isCLIProxyBackend = computed(() => providerEffectiveBackend(props.providerConfig) === "cliproxy");

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
  return authSourceIDs(props.providerConfig).map((id) => ({
    id,
    title: id === "none" && isCLIProxyBackend.value ? t("providerDetail.authSourceCLIProxyAuthDir") : authSourceTitle(id),
  }));
});

const authMode = computed(() => {
  return resolveEffectiveAuthSource(props.providerConfig, props.selectedAuthSource);
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

const verifyModel = computed(() => "");

function accessTypeTitle(option) {
  return option?.title || "";
}

function serviceTemplateTitle(template) {
  if (!template?.id) return "";
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
