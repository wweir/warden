import { computed, ref, watch } from "vue";
import { cloneData, normalizeLowerText, providerBackend, providerFamily, serviceProtocolsEqual } from "../config-utils.js";
import { createEmptyProviderConfig, inferAuthSource, inferPresetID } from "../utils/providerHelpers.js";

const REDACTED = "__REDACTED__";
const CLEAR_API_KEY_MARKER = "__clear_api_key__";
const CUSTOM_ACCESS_TYPE = "__custom_access__";
const CUSTOM_SERVICE_TEMPLATE = "__custom__";

export function useProviderForm(providerFormMeta: any) {
  const providerName = ref("");
  const providerConfig = ref(createEmptyProviderConfig());
  const selectedPresetId = ref("");
  const selectedServiceTemplateId = ref(CUSTOM_SERVICE_TEMPLATE);
  const selectedAuthSource = ref("");
  const dirty = ref(false);
  const suppressDirty = ref(false);
  const showAPIKey = ref(false);
  const apiKeyTouched = ref(false);

  const providerPresets = computed(() => providerFormMeta.value?.presets || []);
  const serviceProtocolTemplates = computed(() => providerFormMeta.value?.service_protocol_templates || []);

  const currentPreset = computed(() =>
    providerPresets.value.find((preset: any) => preset.id === selectedPresetId.value) || null
  );

  const selectedAccessTypeId = computed(() =>
    currentPreset.value ? currentPreset.value.id : CUSTOM_ACCESS_TYPE
  );

  const isCustomAccessType = computed(() => selectedAccessTypeId.value === CUSTOM_ACCESS_TYPE);

  const isCLIProxyBackend = computed(() => providerBackend(providerConfig.value) === "cliproxy");

  const isManagedCLIProxyAccess = computed(() => isCLIProxyBackend.value && !isCustomAccessType.value);

  const isCustomServiceTemplate = computed(() => selectedServiceTemplateId.value === CUSTOM_SERVICE_TEMPLATE);

  const visibleServiceProtocolTemplates = computed(() => {
    const family = providerFamily(providerConfig.value);
    const backend = providerBackend(providerConfig.value);
    return serviceProtocolTemplates.value.filter((template: any) => {
      if (Array.isArray(template.families) && template.families.length > 0 && !template.families.includes(family)) {
        return false;
      }
      if (Array.isArray(template.backends) && template.backends.length > 0 && !template.backends.includes(backend)) {
        return false;
      }
      return true;
    });
  });

  watch([providerName, providerConfig], () => {
    if (!suppressDirty.value) dirty.value = true;
  }, { deep: true });

  watch(
    () => [
      providerConfig.value.family,
      providerConfig.value.backend,
      providerConfig.value.backend_provider,
      providerConfig.value.url,
      providerConfig.value.config_dir,
      providerConfig.value.api_key_command,
      providerConfig.value.service_protocols,
      providerConfig.value.anthropic_to_chat,
      providerConfig.value.anthropic_to_responses,
    ],
    () => {
      if (selectedPresetId.value !== CUSTOM_ACCESS_TYPE) {
        selectedPresetId.value = inferPresetID(providerConfig.value, providerPresets.value) || CUSTOM_ACCESS_TYPE;
      }
      if (selectedServiceTemplateId.value !== CUSTOM_SERVICE_TEMPLATE) {
        syncSelectedServiceTemplate();
      }
    },
    { deep: true }
  );

  function inferServiceTemplateID(provider: any) {
    if (provider.responses_to_chat) return CUSTOM_SERVICE_TEMPLATE;
    for (const template of visibleServiceProtocolTemplates.value) {
      if (!serviceProtocolsEqual(provider.service_protocols, template.service_protocols)) continue;
      if (!!provider.anthropic_to_chat !== !!template.anthropic_to_chat) continue;
      if (!!provider.anthropic_to_responses !== !!template.anthropic_to_responses) continue;
      return template.id;
    }
    return CUSTOM_SERVICE_TEMPLATE;
  }

  function syncSelectedServiceTemplate() {
    selectedServiceTemplateId.value = inferServiceTemplateID(providerConfig.value);
  }

  function applyServiceProtocolTemplateByID(templateID: string) {
    const template = serviceProtocolTemplates.value.find((item: any) => item.id === templateID);
    if (!template) return;
    providerConfig.value.service_protocols = [...(template.service_protocols || [])];
    providerConfig.value.responses_to_chat = false;
    providerConfig.value.anthropic_to_chat = !!template.anthropic_to_chat;
    providerConfig.value.anthropic_to_responses = !!template.anthropic_to_responses;
    selectedServiceTemplateId.value = templateID;
  }

  function presetFieldValue(currentValue: any, previousDefault: any, nextDefault: any) {
    const current = String(currentValue || "").trim();
    const prev = String(previousDefault || "").trim();
    const next = String(nextDefault || "").trim();
    if (!current) return next;
    if (prev && current === prev) return next;
    return currentValue || "";
  }

  function applyPresetByID(presetID: string) {
    const preset = providerPresets.value.find((item: any) => item.id === presetID);
    if (!preset) return;

    const current = providerConfig.value || createEmptyProviderConfig();
    const previousPreset = currentPreset.value;
    const next = createEmptyProviderConfig();
    next.family = preset.family || "";
    next.backend = preset.backend || "";
    next.backend_provider = preset.backend_provider || "";
    next.url = presetFieldValue(current.url, previousPreset?.default_url, preset.default_url);
    next.config_dir = presetFieldValue(current.config_dir, previousPreset?.default_config_dir, preset.default_config_dir);
    next.models = [...(current.models || [])];
    next.headers = cloneData(current.headers || {});
    next.proxy = current.proxy || "";
    next.timeout = current.timeout || "";
    next.api_key = current.api_key || "";
    next.api_key_command = current.api_key_command || "";
    next.api_key_command_timeout = current.api_key_command_timeout || "";
    next.api_key_command_ttl = current.api_key_command_ttl || "";
    providerConfig.value = next;
    selectedPresetId.value = preset.id;
    selectedAuthSource.value = inferAuthSource(next);
    showAPIKey.value = false;
    if (preset.service_protocol_template) {
      applyServiceProtocolTemplateByID(preset.service_protocol_template);
    } else {
      syncSelectedServiceTemplate();
    }
  }

  function applyAccessPresetByID(presetID: string) {
    const preset = providerPresets.value.find((item: any) => item.id === presetID);
    if (!preset) return;

    const previousPreset = currentPreset.value;
    providerConfig.value.family = preset.family || "";
    providerConfig.value.backend = preset.backend || "";
    providerConfig.value.backend_provider = preset.backend_provider || "";
    providerConfig.value.url = presetFieldValue(providerConfig.value.url, previousPreset?.default_url, preset.default_url);
    providerConfig.value.config_dir = presetFieldValue(providerConfig.value.config_dir, previousPreset?.default_config_dir, preset.default_config_dir);
    selectedPresetId.value = preset.id;
    selectedAuthSource.value = inferAuthSource(providerConfig.value);
    if (preset.service_protocol_template) {
      applyServiceProtocolTemplateByID(preset.service_protocol_template);
    } else {
      providerConfig.value.service_protocols = [];
      providerConfig.value.anthropic_to_chat = false;
      providerConfig.value.anthropic_to_responses = false;
      providerConfig.value.responses_to_chat = false;
      syncSelectedServiceTemplate();
    }
  }

  function handleAccessTypeChange(presetID: string, isCreate: boolean) {
    if (presetID === CUSTOM_ACCESS_TYPE) {
      selectedPresetId.value = CUSTOM_ACCESS_TYPE;
      return;
    }
    if (isCreate) {
      applyPresetByID(presetID);
    } else {
      applyAccessPresetByID(presetID);
    }
  }

  function handleServiceTemplateChange(templateID: string) {
    selectedServiceTemplateId.value = templateID;
    if (templateID === CUSTOM_SERVICE_TEMPLATE) return;
    applyServiceProtocolTemplateByID(templateID);
  }

  function applyProviderAuthSource(provider: any, source: string) {
    delete provider.api_key_command;
    delete provider.api_key_command_timeout;
    delete provider.api_key_command_ttl;
    delete provider.config_dir;

    switch (source) {
      case "api_key":
        if (!apiKeyTouched.value) {
          if (provider.api_key === REDACTED) {
            provider.api_key = REDACTED;
            return;
          }
          delete provider.api_key;
          return;
        }
        provider.api_key = String(provider.api_key || "");
        return;
      case "command":
        delete provider.api_key;
        provider.api_key_command = String(providerConfig.value.api_key_command || "").trim();
        if (String(providerConfig.value.api_key_command_timeout || "").trim()) {
          provider.api_key_command_timeout = String(providerConfig.value.api_key_command_timeout).trim();
        }
        if (String(providerConfig.value.api_key_command_ttl || "").trim()) {
          provider.api_key_command_ttl = String(providerConfig.value.api_key_command_ttl).trim();
        }
        return;
      case "config_dir":
        delete provider.api_key;
        provider.config_dir = providerConfig.value.config_dir || "";
        return;
      case "none":
        delete provider.api_key;
        provider[CLEAR_API_KEY_MARKER] = true;
        return;
      default:
        delete provider.api_key;
    }
  }

  function loadProviderConfig(provider: any) {
    providerConfig.value = {
      ...createEmptyProviderConfig(),
      ...cloneData(provider),
      family: provider.family || provider.protocol || "",
      service_protocols: [...(provider.service_protocols || [])],
      models: [...(provider.models || [])],
      headers: cloneData(provider.headers || {}),
    };
    selectedPresetId.value = inferPresetID(providerConfig.value, providerPresets.value);
    selectedAuthSource.value = inferAuthSource(providerConfig.value);
    syncSelectedServiceTemplate();
  }

  function resetForm() {
    providerName.value = "";
    providerConfig.value = createEmptyProviderConfig();
    selectedPresetId.value = "";
    selectedServiceTemplateId.value = CUSTOM_SERVICE_TEMPLATE;
    selectedAuthSource.value = "";
    dirty.value = false;
    showAPIKey.value = false;
    apiKeyTouched.value = false;
  }

  return {
    providerName,
    providerConfig,
    selectedPresetId,
    selectedServiceTemplateId,
    selectedAuthSource,
    dirty,
    suppressDirty,
    showAPIKey,
    apiKeyTouched,
    providerPresets,
    serviceProtocolTemplates,
    currentPreset,
    selectedAccessTypeId,
    isCustomAccessType,
    isCLIProxyBackend,
    isManagedCLIProxyAccess,
    isCustomServiceTemplate,
    visibleServiceProtocolTemplates,
    handleAccessTypeChange,
    handleServiceTemplateChange,
    applyProviderAuthSource,
    loadProviderConfig,
    resetForm,
    applyPresetByID,
    syncSelectedServiceTemplate,
    CUSTOM_ACCESS_TYPE,
    CUSTOM_SERVICE_TEMPLATE,
  };
}
