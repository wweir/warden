<template>
  <div>
    <div class="breadcrumb">
      <router-link to="/">{{ $t("dashboard.title") }}</router-link>
      <span class="sep">/</span>
      <router-link to="/providers">{{ $t("providers.title") }}</router-link>
      <span class="sep">/</span>
      <span class="current">{{ pageTitle }}</span>
    </div>

    <h2 class="page-title">{{ pageTitle }}</h2>

    <div v-if="configSource && !configSource.source_type?.file" class="msg warning">
      {{ $t("config.nonFileWarning", { path: configSource.config_path || "remote" }) }}
    </div>

    <div v-if="configFileChanged" class="msg warning">
      {{ $t("config.externalChange") }}
      <button @click="load" class="btn btn-sm">{{ $t("common.reload") }}</button>
    </div>

    <div v-if="message" class="msg success">{{ message }}</div>
    <div v-if="error" class="msg error">{{ error }}</div>

    <div v-if="loading" class="msg">{{ $t("common.loading") }}</div>
    <div v-else class="detail-layout">
      <section class="info-section">
        <div class="section-top">
          <div>
            <h3>{{ $t("providerDetail.configEditor") }}</h3>
            <p class="section-desc">{{ $t("providerDetail.configEditorDesc") }}</p>
          </div>
          <div class="actions">
            <button
              @click="apply"
              class="btn btn-primary"
              :disabled="saving || (configSource && !configSource.source_type?.file)"
            >
              {{
                saving
                  ? waitingAlive
                    ? $t("config.waitingService", { n: waitingElapsed })
                    : $t("providerDetail.saving")
                  : $t("providerDetail.saveApply")
              }}
            </button>
            <button v-if="dirty && !saving" @click="discard" class="btn btn-secondary">
              {{ $t("config.discardChanges") }}
            </button>
            <button v-if="!create && !saving" @click="deleteProvider" class="btn btn-danger">
              {{ $t("providerDetail.deleteProvider") }}
            </button>
          </div>
        </div>

        <ProviderBasicForm
          v-model:provider-name="providerName"
          v-model:provider-config="providerConfig"
          v-model:selected-preset-id="selectedPresetId"
          v-model:selected-service-template-id="selectedServiceTemplateId"
          v-model:selected-auth-source="selectedAuthSource"
          v-model:show-api-key="showAPIKey"
          v-model:api-key-touched="apiKeyTouched"
          v-model:adapter-advanced-open="adapterAdvancedOpen"
          :is-create="create"
          :provider-presets="providerPresets"
          :service-protocol-templates="serviceProtocolTemplates"
          :current-preset="currentPreset"
          :is-managed-cli-proxy-access="isManagedCLIProxyAccess"
          :show-adapter-advanced="showAdapterAdvanced"
          :is-custom-service-template="isCustomServiceTemplate"
          :visible-service-protocol-templates="visibleServiceProtocolTemplates"
          :config-doc="configDoc"
          @access-type-change="handleAccessTypeChange"
          @service-template-change="handleServiceTemplateChange"
          @auth-success="handleAuthSuccess"
          @auth-error="handleError"
        />

        <ProviderModelsEditor
          v-if="providerFamily(providerConfig)"
          v-model="providerConfig.models"
          :discovered-model-ids="discoveredModelIds"
          :is-create="create"
        />

        <ProviderAdvancedSettings
          v-model:headers="providerConfig.headers"
          v-model:proxy="providerConfig.proxy"
          v-model:timeout="providerConfig.timeout"
          :shows-headers-field="showsHeadersField"
        />
      </section>

      <ProviderRuntimeTools
        v-if="!create && detail"
        :detail="detail"
        :provider-presets="providerPresets"
        :discovered-model-ids="discoveredModelIds"
        :provider-config="providerConfig"
        @reload="load"
        @error="handleError"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import {
  fetchConfig,
  fetchConfigSource,
  fetchProviderDetail,
  fetchProviderFormMeta,
  fetchStatus,
  restartGateway,
  saveConfig,
  validateConfig,
} from "../api.js";
import {
  cleanConfig,
  cloneData,
  normalizeLowerText,
  normalizeServiceProtocols,
  providerBackend,
  providerFamily,
} from "../config-utils.js";
import { bindPollState, pollUntilAlive } from "../runtime-utils.js";
import { useProviderForm } from "../composables/useProviderForm.ts";
import ProviderBasicForm from "../components/ProviderBasicForm.vue";
import ProviderModelsEditor from "../components/ProviderModelsEditor.vue";
import ProviderAdvancedSettings from "../components/ProviderAdvancedSettings.vue";
import ProviderRuntimeTools from "../components/ProviderRuntimeTools.vue";

const { t } = useI18n();
const router = useRouter();

const props = defineProps({
  name: { type: String, default: "" },
  create: { type: Boolean, default: false },
});

const detail = ref(null);
const configDoc = ref({});
const configSource = ref(null);
const providerFormMeta = ref({ presets: [], service_protocol_templates: [] });
const error = ref("");
const message = ref("");
const saving = ref(false);
const loading = ref(false);
const configFileChanged = ref(false);
const waitingAlive = ref(false);
const waitingElapsed = ref(0);

const {
  providerName,
  providerConfig,
  selectedPresetId,
  selectedServiceTemplateId,
  selectedAuthSource,
  dirty,
  suppressDirty,
  showAPIKey,
  apiKeyTouched,
  adapterAdvancedOpen,
  providerPresets,
  serviceProtocolTemplates,
  currentPreset,
  isManagedCLIProxyAccess,
  showAdapterAdvanced,
  effectiveAuthSource,
  isCustomServiceTemplate,
  visibleServiceProtocolTemplates,
  handleAccessTypeChange: formHandleAccessTypeChange,
  handleServiceTemplateChange: formHandleServiceTemplateChange,
  applyProviderAuthSource,
  loadProviderConfig,
  resetForm,
  applyPresetByID,
} = useProviderForm(providerFormMeta);

watch(
  () => [props.name, props.create],
  () => load(),
  { immediate: true }
);

const pageTitle = computed(() =>
  props.create ? t("providerDetail.newProviderTitle") : providerName.value || props.name
);

const parsedModels = computed(() => {
  if (!detail.value) return [];
  return detail.value.models.map((model) => {
    if (typeof model === "string") {
      try {
        return JSON.parse(model);
      } catch {
        return { id: model };
      }
    }
    return model;
  });
});

const discoveredModelIds = computed(() => {
  const ids = [];
  for (const model of parsedModels.value) {
    const id = typeof model?.id === "string" ? model.id.trim() : "";
    if (!id || ids.includes(id)) continue;
    ids.push(id);
  }
  return ids;
});

const showsHeadersField = computed(
  () =>
    !!providerFamily(providerConfig.value) &&
    !["copilot"].includes(providerFamily(providerConfig.value)) &&
    !isManagedCLIProxyAccess.value
);

function handleAccessTypeChange(presetID) {
  formHandleAccessTypeChange(presetID, props.create);
}

function handleServiceTemplateChange(templateID) {
  formHandleServiceTemplateChange(templateID);
}

function handleError(msg) {
  error.value = msg;
}

function handleAuthSuccess(filename) {
  message.value = t("providerDetail.cliproxyAuthUploadSuccess", { filename });
}

async function load() {
  loading.value = true;
  suppressDirty.value = true;
  error.value = "";
  configFileChanged.value = false;
  try {
    const [cfg, source, formMeta] = await Promise.all([
      fetchConfig(),
      fetchConfigSource(),
      fetchProviderFormMeta(),
    ]);
    configDoc.value = cfg;
    configSource.value = source;
    providerFormMeta.value = formMeta;

    if (props.create) {
      resetForm();
      const defaultPresetID = providerPresets.value[0]?.id || "";
      if (defaultPresetID) applyPresetByID(defaultPresetID);
    } else {
      providerName.value = props.name;
      const provider = cfg.provider?.[props.name];
      if (!provider) {
        throw new Error(t("providerDetail.providerConfigMissing", { name: props.name }));
      }
      loadProviderConfig(provider);
      detail.value = await fetchProviderDetail(props.name);
    }

    dirty.value = false;
  } catch (e) {
    error.value = e.message;
  } finally {
    suppressDirty.value = false;
    loading.value = false;
  }
}

function discard() {
  if (!confirm(t("config.confirmDiscard"))) return;
  load();
}

async function apply() {
  saving.value = true;
  message.value = "";
  error.value = "";
  try {
    if (!configSource.value?.source_type?.file) {
      error.value = t("config.savingDisabled");
      return;
    }

    const name = providerName.value.trim();
    if (!name) {
      error.value = t("providerDetail.nameRequired");
      return;
    }
    if (!providerFamily(providerConfig.value)) {
      error.value = t("providerDetail.familyRequired");
      return;
    }
    const family = providerFamily(providerConfig.value);
    if (!["copilot"].includes(family) && !providerConfig.value.url?.trim()) {
      error.value = t("providerDetail.urlRequired");
      return;
    }
    const selectedAuthMode = effectiveAuthSource.value;
    if (selectedAuthMode === "command" && !String(providerConfig.value.api_key_command || "").trim()) {
      error.value = t("providerDetail.apiKeyCommandRequired");
      return;
    }
    const backend = family === "openai" ? providerBackend(providerConfig.value) : "";
    if (backend === "cliproxy") {
      if (!String(providerConfig.value.backend_provider || "").trim()) {
        error.value = t("providerDetail.backendProviderRequired");
        return;
      }
      if ((providerConfig.value.service_protocols || []).length === 0) {
        error.value = t("providerDetail.backendServiceProtocolsRequired");
        return;
      }
    }

    const nextConfig = cloneData(configDoc.value);
    nextConfig.provider = nextConfig.provider || {};

    if (props.create && nextConfig.provider[name]) {
      error.value = t("providerDetail.providerExists", { name });
      return;
    }

    const nextProviderConfig = cloneData(providerConfig.value);
    nextProviderConfig.family = providerFamily(nextProviderConfig);
    nextProviderConfig.backend = nextProviderConfig.family === "openai" ? providerBackend(nextProviderConfig) : "";
    nextProviderConfig.backend_provider = normalizeLowerText(nextProviderConfig.backend_provider);
    nextProviderConfig.service_protocols = normalizeServiceProtocols(nextProviderConfig.service_protocols);
    if (!nextProviderConfig.backend) {
      delete nextProviderConfig.backend;
      delete nextProviderConfig.backend_provider;
    }
    if (nextProviderConfig.service_protocols.length === 0) {
      delete nextProviderConfig.service_protocols;
    }
    delete nextProviderConfig.protocol;
    applyProviderAuthSource(nextProviderConfig, selectedAuthMode);
    nextConfig.provider[name] = nextProviderConfig;

    const cleaned = cleanConfig(nextConfig);
    const result = await validateConfig(cleaned);
    if (!result.valid) {
      error.value = t("config.validationFailed", { error: result.error });
      return;
    }

    await saveConfig(cleaned);
    const restart = await restartGateway();
    if (restart.status !== "ok") {
      error.value = t("config.savedButRestartFailed", { error: restart.error || "unknown error" });
      return;
    }

    const alive = await pollUntilAlive(fetchStatus, bindPollState(waitingAlive, waitingElapsed));
    if (!alive) {
      error.value = t("config.serviceTimeout");
      return;
    }

    if (props.create) {
      await router.replace(`/providers/${encodeURIComponent(name)}`);
    } else {
      await load();
    }

    message.value = t("providerDetail.savedMsg", { name });
  } catch (e) {
    if (e.message?.includes("config file changed externally")) {
      configFileChanged.value = true;
      error.value = t("config.externalChangeError");
    } else {
      error.value = e.message;
    }
  } finally {
    saving.value = false;
  }
}

function pruneProviderReferences(nextConfig, targetProvider) {
  if (!nextConfig?.route || typeof nextConfig.route !== "object") return;

  for (const [prefix, route] of Object.entries(nextConfig.route)) {
    if (!route || typeof route !== "object") continue;

    const nextExactModels = {};
    for (const [modelName, modelConfig] of Object.entries(route.exact_models || {})) {
      if (!modelConfig || typeof modelConfig !== "object") continue;
      const upstreams = (modelConfig.upstreams || []).filter((upstream) => upstream?.provider !== targetProvider);
      if (upstreams.length === 0) continue;
      nextExactModels[modelName] = { ...modelConfig, upstreams };
    }

    const nextWildcardModels = {};
    for (const [pattern, modelConfig] of Object.entries(route.wildcard_models || {})) {
      if (!modelConfig || typeof modelConfig !== "object") continue;
      const providers = (modelConfig.providers || []).filter((provider) => provider !== targetProvider);
      if (providers.length === 0) continue;
      nextWildcardModels[pattern] = { ...modelConfig, providers };
    }

    if (Object.keys(nextExactModels).length === 0 && Object.keys(nextWildcardModels).length === 0) {
      delete nextConfig.route[prefix];
      continue;
    }

    route.exact_models = nextExactModels;
    route.wildcard_models = nextWildcardModels;
  }
}

async function deleteProvider() {
  if (!confirm(t("providerDetail.confirmDeleteProvider", { name: providerName.value }))) return;

  saving.value = true;
  message.value = "";
  error.value = "";
  try {
    if (!configSource.value?.source_type?.file) {
      error.value = t("config.savingDisabled");
      return;
    }

    const nextConfig = cloneData(configDoc.value);
    delete nextConfig.provider?.[providerName.value];
    pruneProviderReferences(nextConfig, providerName.value);

    const cleaned = cleanConfig(nextConfig);
    const result = await validateConfig(cleaned);
    if (!result.valid) {
      error.value = t("config.validationFailed", { error: result.error });
      return;
    }

    await saveConfig(cleaned);
    const restart = await restartGateway();
    if (restart.status !== "ok") {
      error.value = t("config.savedButRestartFailed", { error: restart.error || "unknown error" });
      return;
    }

    const alive = await pollUntilAlive(fetchStatus, bindPollState(waitingAlive, waitingElapsed));
    if (!alive) {
      error.value = t("config.serviceTimeout");
      return;
    }

    await router.push("/providers");
  } catch (e) {
    if (e.message?.includes("config file changed externally")) {
      configFileChanged.value = true;
      error.value = t("config.externalChangeError");
    } else {
      error.value = e.message;
    }
  } finally {
    saving.value = false;
  }
}
</script>

<style scoped>
.section-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 14px;
}

.section-desc {
  margin-top: 6px;
  font-size: 13px;
  color: var(--c-text-3);
  max-width: 760px;
}

.actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

@media (max-width: 768px) {
  .section-top {
    flex-direction: column;
  }
}
</style>
