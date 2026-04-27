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

    <div
      v-if="configSource && !configSource.source_type?.file"
      class="msg warning"
    >
      {{
        $t("config.nonFileWarning", {
          path: configSource.config_path || "remote",
        })
      }}
    </div>

    <div v-if="configFileChanged" class="msg warning">
      {{ $t("config.externalChange") }}
      <button @click="load" class="btn btn-sm">
        {{ $t("common.reload") }}
      </button>
    </div>

    <div v-if="message" class="msg success">{{ message }}</div>
    <div v-if="error" class="msg error">{{ error }}</div>

    <div v-if="loading" class="msg">{{ $t("common.loading") }}</div>
    <div v-else class="detail-layout">
      <section class="info-section">
        <div class="section-top">
          <div>
            <h3>{{ $t("providerDetail.configEditor") }}</h3>
            <p class="section-desc">
              {{ $t("providerDetail.configEditorDesc") }}
            </p>
          </div>
          <div class="actions">
            <button
              @click="apply"
              class="btn btn-primary"
              :disabled="
                saving || (configSource && !configSource.source_type?.file)
              "
            >
              {{
                saving
                  ? waitingAlive
                    ? $t("config.waitingService", { n: waitingElapsed })
                    : $t("providerDetail.saving")
                  : $t("providerDetail.saveApply")
              }}
            </button>
            <button
              v-if="dirty && !saving"
              @click="discard"
              class="btn btn-secondary"
            >
              {{ $t("config.discardChanges") }}
            </button>
            <button
              v-if="!create && !saving"
              @click="deleteProvider"
              class="btn btn-danger"
            >
              {{ $t("providerDetail.deleteProvider") }}
            </button>
          </div>
        </div>

        <div class="provider-form">
          <section class="form-panel primary-panel">
            <div class="form-panel-head">
              <div>
                <h4>{{ $t("providerDetail.quickSetupSection") }}</h4>
                <p class="section-desc">
                  {{ $t("providerDetail.quickSetupDesc") }}
                </p>
              </div>
              <div v-if="currentPreset" class="panel-badges">
                <span class="badge badge-ok">{{ currentPreset.title }}</span>
              </div>
            </div>
            <div class="form-grid">
              <label
                >{{ $t("providerDetail.providerType") }}
                <span class="req">*</span></label
              >
              <div class="field-stack">
                <select
                  :value="selectedAccessTypeId"
                  class="form-input"
                  @change="handleAccessTypeChange($event.target.value)"
                >
                  <option
                    v-for="option in accessTypeOptions"
                    :key="option.id"
                    :value="option.id"
                  >
                    {{ accessTypeTitle(option) }}
                  </option>
                </select>
                <p class="hint">{{ currentAccessTypeSummary }}</p>
              </div>

              <div v-if="isCustomAccessType" class="form-grid-full custom-interface-editor">
                <div class="custom-interface-head">
                  <span class="interface-preview-title">
                    {{ $t("providerDetail.customAccessSection") }}
                  </span>
                  <span class="hint">
                    {{ $t("providerDetail.customAccessDesc") }}
                  </span>
                </div>
                <div class="form-grid compact-grid">
                  <label
                    >{{ $t("providerDetail.family") }}
                    <span class="req">*</span></label
                  >
                  <select v-model="providerConfig.family" class="form-input">
                    <option value="">
                      {{ $t("providerDetail.selectFamily") }}
                    </option>
                    <option value="openai">openai</option>
                    <option value="anthropic">anthropic</option>
                    <option value="copilot">copilot</option>
                  </select>

                  <template v-if="providerFamily(providerConfig) === 'openai'">
                    <label>backend</label>
                    <select v-model="providerConfig.backend" class="form-input">
                      <option value="">default</option>
                      <option value="cliproxy">cliproxy</option>
                    </select>

                    <template v-if="providerBackend(providerConfig) === 'cliproxy'">
                      <label>backend_provider <span class="req">*</span></label>
                      <input
                        v-model="providerConfig.backend_provider"
                        class="form-input"
                        placeholder="codex"
                      />
                    </template>
                  </template>
                </div>
              </div>

              <label
                >{{ $t("providerDetail.name") }}
                <span class="req">*</span></label
              >
              <input
                v-if="create"
                v-model.trim="providerName"
                class="form-input"
                :placeholder="$t('providerDetail.namePlaceholder')"
              />
              <input v-else :value="providerName" class="form-input" readonly />

              <template v-if="showsURLField">
                <label
                  >{{ providerUrlLabel }} <span class="req">*</span></label
                >
                <div class="field-stack">
                  <input
                    v-model="providerConfig.url"
                    class="form-input"
                    :placeholder="providerUrlPlaceholder(providerConfig)"
                  />
                  <p v-if="providerUrlHint" class="hint">
                    {{ providerUrlHint }}
                  </p>
                </div>
              </template>

              <template v-else>
                <label>{{ $t("providerDetail.connectionSection") }}</label>
                <div class="section-note">
                  {{ connectionNote }}
                </div>
              </template>

              <template v-if="authMode === 'api_key'">
                <label>api_key</label>
                <div class="secret-field">
                  <input
                    :type="showAPIKey ? 'text' : 'password'"
                    :value="secretDisplay(providerConfig.api_key)"
                    @input="
                      apiKeyTouched = true;
                      providerConfig.api_key = $event.target.value;
                    "
                    class="form-input"
                    :placeholder="$t('providerDetail.apiKeyPlaceholder')"
                  />
                  <button
                    class="btn-icon"
                    @click="showAPIKey = !showAPIKey"
                    type="button"
                    :aria-label="$t('providerDetail.toggleApiKeyVisibility')"
                  >
                    {{ showAPIKey ? "🙈" : "👁" }}
                  </button>
                  <span
                    :class="[
                      'badge',
                      isSecretConfigured(providerConfig.api_key)
                        ? 'badge-ok'
                        : 'badge-none',
                    ]"
                  >
                    {{
                      isSecretConfigured(providerConfig.api_key)
                        ? $t("common.configured")
                        : $t("common.notSet")
                    }}
                  </span>
                </div>
              </template>

              <template v-else-if="authMode === 'config_dir'">
                <label>config_dir</label>
                <input
                  v-model="providerConfig.config_dir"
                  class="form-input"
                  :placeholder="configDirPlaceholder"
                />
              </template>

              <template v-else>
                <label>{{ $t("providerDetail.authSection") }}</label>
                <div class="section-note">
                  {{ authNote }}
                </div>
              </template>

              <label>{{ $t("providerDetail.availableInterfaces") }}</label>
              <div class="field-stack">
                <select
                  :value="selectedServiceTemplateId"
                  class="form-input"
                  @change="handleServiceTemplateChange($event.target.value)"
                >
                  <option
                    v-for="template in capabilityTemplateOptions"
                    :key="template.id"
                    :value="template.id"
                  >
                    {{ serviceTemplateTitle(template) }}
                  </option>
                </select>
                <p class="hint">{{ currentServiceTemplateSummary }}</p>
              </div>

              <div class="form-grid-full interface-preview">
                <span class="interface-preview-title">
                  {{ $t("providerDetail.finalInterfaces") }}
                </span>
                <div class="protocol-chip-list">
                  <span
                    v-for="protocol in effectiveServiceProtocols"
                    :key="protocol"
                    class="badge badge-muted"
                  >
                    {{ serviceProtocolTitle(protocol) }}
                  </span>
                  <span
                    v-if="effectiveServiceProtocols.length === 0"
                    class="hint"
                  >
                    {{ $t("providerDetail.noEffectiveProtocols") }}
                  </span>
                </div>
                <p class="hint">
                  {{ $t("providerDetail.finalInterfacesHint") }}
                </p>
              </div>

              <div
                v-if="isCustomServiceTemplate"
                class="form-grid-full custom-interface-editor"
              >
                <div class="custom-interface-head">
                  <span class="interface-preview-title">
                    {{ $t("providerDetail.customInterfacesSection") }}
                  </span>
                  <span class="hint">
                    {{ $t("providerDetail.customInterfacesDesc") }}
                  </span>
                </div>
                <div class="form-grid compact-grid">
                  <label>{{ $t("providerDetail.rawServiceProtocols") }}</label>
                  <div class="service-protocols-editor">
                    <TagListEditor
                      v-model="providerConfig.service_protocols"
                      :suggestions="serviceProtocolSuggestions"
                      :placeholder="
                        $t('providerDetail.serviceProtocolsPlaceholder')
                      "
                    />
                    <p class="hint service-protocols-hint">
                      {{ $t("providerDetail.serviceProtocolsHint") }}
                    </p>
                  </div>

                  <template v-if="providerFamily(providerConfig) === 'openai'">
                    <label>responses_to_chat</label>
                    <div class="form-hint-row">
                      <input
                        type="checkbox"
                        v-model="providerConfig.responses_to_chat"
                        class="form-checkbox"
                      />
                      <span class="hint">{{
                        $t("config.responsesToChatHint")
                      }}</span>
                    </div>

                    <label>anthropic_to_chat</label>
                    <div class="form-hint-row">
                      <input
                        type="checkbox"
                        v-model="providerConfig.anthropic_to_chat"
                        class="form-checkbox"
                      />
                      <span class="hint">{{
                        $t("config.anthropicToChatHint")
                      }}</span>
                    </div>
                  </template>
                </div>
              </div>
            </div>
          </section>

          <section
            v-if="providerFamily(providerConfig)"
            class="form-panel optional-panel"
          >
            <div class="form-panel-head">
              <div>
                <h4>
                  {{ $t("providerDetail.staticModelsSection") }}
                  <span class="summary-count">
                    {{
                      $t("providerDetail.modelsConfiguredCount", {
                        n: providerConfig.models.length,
                      })
                    }}
                  </span>
                </h4>
              </div>
            </div>
            <p class="section-desc advanced-desc">
              {{ $t("providerDetail.staticModelsSectionDesc") }}
            </p>
            <div class="models-editor">
              <div class="models-toolbar">
                <div class="models-meta">
                  <span class="badge badge-none">
                    {{ $t("providerDetail.modelsOptional") }}
                  </span>
                  <span v-if="!create" class="hint">
                    {{
                      $t("providerDetail.modelsDiscoveredCount", {
                        n: discoveredModelIds.length,
                      })
                    }}
                  </span>
                </div>
                <button
                  v-if="missingDiscoveredModelIds.length > 0"
                  type="button"
                  class="btn btn-secondary btn-sm"
                  @click="appendDiscoveredModels"
                >
                  {{
                    $t("providerDetail.addDiscoveredModels", {
                      n: missingDiscoveredModelIds.length,
                    })
                  }}
                </button>
              </div>
              <p class="hint models-hint">
                {{
                  discoveredModelIds.length > 0
                    ? $t("providerDetail.modelsSuggestionHint", {
                        n: discoveredModelIds.length,
                      })
                    : $t("providerDetail.modelsNoSuggestionHint")
                }}
              </p>
              <TagListEditor
                v-model="providerConfig.models"
                :suggestions="discoveredModelIds"
                :placeholder="$t('providerDetail.modelsPlaceholder')"
              />
              <p class="hint models-hint">
                {{ $t("providerDetail.modelsBehaviorHint") }}
              </p>
            </div>
          </section>

          <section class="form-panel advanced-panel">
            <div class="form-panel-head">
              <div>
                <h4>{{ $t("providerDetail.advancedSection") }}</h4>
              </div>
            </div>
            <p class="section-desc advanced-desc">
              {{ $t("providerDetail.advancedSectionDesc") }}
            </p>
            <div class="form-grid">
              <template v-if="showsHeadersField">
                <label>headers</label>
                <KeyValueEditor
                  v-model="providerConfig.headers"
                  keyPlaceholder="Header name"
                  valuePlaceholder="Value"
                />
              </template>

              <label>proxy</label>
              <input
                v-model="providerConfig.proxy"
                class="form-input"
                :placeholder="$t('providerDetail.proxyPlaceholder')"
              />

              <label>{{ $t("providerDetail.timeout") }}</label>
              <input
                v-model="providerConfig.timeout"
                class="form-input"
                :placeholder="$t('providerDetail.defaultTimeout')"
              />
            </div>
          </section>
        </div>
      </section>

      <div v-if="!create && detail" class="runtime-stack">
        <section class="info-section">
          <div class="section-top compact-top">
            <div>
              <h3>{{ $t("providerDetail.runtimeTools") }}</h3>
              <p class="section-desc">
                {{ $t("providerDetail.runtimeToolsDesc") }}
              </p>
            </div>
            <div class="actions runtime-actions">
              <button
                @click="runHealthCheck"
                class="btn btn-primary"
                :disabled="checking"
              >
                {{
                  checking
                    ? $t("providerDetail.checking")
                    : $t("providerDetail.healthCheck")
                }}
              </button>
              <button
                v-if="detail.status && !detail.status.manual_suppressed"
                @click="suppressProvider"
                class="btn btn-danger"
              >
                {{ $t("providerDetail.suppressBtn") }}
              </button>
              <button
                v-else-if="detail.status"
                @click="unsuppressProvider"
                class="btn btn-secondary"
              >
                {{ $t("providerDetail.unsuppressBtn") }}
              </button>
            </div>
          </div>
          <span
            v-if="healthResult"
            class="health-result"
            :class="
              healthResult.status === 'ok' ? 'text-success' : 'text-error'
            "
          >
            {{
              healthResult.status === "ok"
                ? $t("providerDetail.healthOk", {
                    latency: healthResult.latency_ms,
                    count: healthResult.model_count,
                  })
                : $t("providerDetail.healthError", {
                    error: healthResult.error,
                  })
            }}
          </span>
        </section>

        <details class="info-section runtime-panel" open>
          <summary>{{ $t("providerDetail.runtimeOverview") }}</summary>
          <div class="runtime-tables">
            <table class="info-table">
              <tr>
                <td>{{ $t("providerDetail.name") }}</td>
                <td>{{ detail.name }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.providerType") }}</td>
                <td>{{ runtimeAccessTypeTitle }}</td>
              </tr>
              <tr v-if="!detailIsManagedCLIProxy">
                <td>{{ $t("providerDetail.url") }}</td>
                <td>
                  <code>{{ detail.url }}</code>
                </td>
              </tr>
              <tr v-if="!detailIsManagedCLIProxy">
                <td>{{ $t("providerDetail.family") }}</td>
                <td>{{ detail.family || detail.protocol }}</td>
              </tr>
              <tr v-if="!detailIsManagedCLIProxy && detail.backend">
                <td>backend</td>
                <td>{{ detail.backend }}</td>
              </tr>
              <tr v-if="!detailIsManagedCLIProxy && detail.backend_provider">
                <td>backend_provider</td>
                <td>{{ detail.backend_provider }}</td>
              </tr>
              <tr v-if="detailIsManagedCLIProxy">
                <td>{{ $t("providerDetail.connectionSection") }}</td>
                <td>{{ $t("providerDetail.cliproxyConnectionNote") }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.configuredProtocols") }}</td>
                <td>
                  {{ (detail.configured_protocols || []).join(", ") || "-" }}
                </td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.displayProtocols") }}</td>
                <td>
                  {{ (detail.display_protocols || []).join(", ") || "-" }}
                </td>
              </tr>
              <tr v-if="detailIsManagedCLIProxy">
                <td>{{ $t("providerDetail.authSection") }}</td>
                <td>{{ $t("providerDetail.cliproxyAuthNote") }}</td>
              </tr>
              <tr v-else>
                <td>{{ $t("providerDetail.apiKey") }}</td>
                <td>
                  {{
                    detail.has_api_key
                      ? $t("common.configured")
                      : $t("common.notSet")
                  }}
                </td>
              </tr>
            </table>

            <table v-if="detail.status" class="info-table">
              <tr>
                <td>{{ $t("providerDetail.suppressed") }}</td>
                <td>
                  {{
                    detail.status.suppressed
                      ? $t("providerDetail.yes")
                      : $t("providerDetail.no")
                  }}
                </td>
              </tr>
              <tr v-if="detail.status.suppressed">
                <td>{{ $t("providerDetail.suppressedUntil") }}</td>
                <td>{{ formatTime(detail.status.suppress_until) }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.consecutiveFailures") }}</td>
                <td>{{ detail.status.consecutive_failures }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.totalRequests") }}</td>
                <td>{{ detail.status.total_requests }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.success") }}</td>
                <td>{{ detail.status.success_count }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.failure") }}</td>
                <td>{{ detail.status.failure_count }}</td>
              </tr>
              <tr>
                <td>{{ $t("providerDetail.avgLatency") }}</td>
                <td>
                  {{
                    detail.status.total_requests > 0
                      ? detail.status.avg_latency_ms.toFixed(0) + "ms"
                      : "-"
                  }}
                </td>
              </tr>
            </table>
          </div>
        </details>

        <details class="info-section runtime-panel">
          <summary>
            {{
              $t("providerDetail.availableModels", { n: detail.models.length })
            }}
          </summary>
          <div v-if="detail.models.length === 0" class="empty">
            {{ $t("providerDetail.noModels") }}
          </div>
          <table v-else class="data-table">
            <thead>
              <tr>
                <th>{{ $t("providerDetail.modelId") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in parsedModels" :key="m.id">
                <td>
                  <code>{{ m.id }}</code>
                </td>
              </tr>
            </tbody>
          </table>
        </details>

        <details class="info-section runtime-panel">
          <summary>{{ $t("providerDetail.protocolDetection") }}</summary>
          <div class="runtime-actions">
            <button
              @click="runProtocolDetect"
              class="btn btn-secondary"
              :disabled="detectingProtocols"
            >
              {{
                detectingProtocols
                  ? $t("providerDetail.detecting")
                  : $t("providerDetail.detectDisplayProtocols")
              }}
            </button>
            <span v-if="detail.last_protocol_probe" class="hint">
              {{ detail.last_protocol_probe.status }} ·
              {{ formatTime(detail.last_protocol_probe.checked_at) }}
              <span v-if="detail.last_protocol_probe.error">
                · {{ detail.last_protocol_probe.error }}</span
              >
            </span>
          </div>

          <div class="probe-grid">
            <select v-model="selectedProbeModel" class="form-input">
              <option value="">{{ $t("providerDetail.selectModel") }}</option>
              <option
                v-for="model in probeableModels"
                :key="model"
                :value="model"
              >
                {{ model }}
              </option>
            </select>
            <select v-model="selectedProbeProtocol" class="form-input">
              <option value="chat">chat</option>
              <option value="responses_stateless">responses_stateless</option>
              <option value="responses_stateful">responses_stateful</option>
              <option value="anthropic">anthropic</option>
            </select>
            <button
              @click="runExactProtocolProbe"
              class="btn btn-primary"
              :disabled="exactProbing || !selectedProbeModel"
            >
              {{
                exactProbing
                  ? $t("providerDetail.probing")
                  : $t("providerDetail.probeModelProtocol")
              }}
            </button>
          </div>

          <div
            v-if="protocolProbeResult"
            class="hint"
            :class="
              protocolProbeResult.status === 'supported'
                ? 'text-success'
                : protocolProbeResult.status === 'unsupported'
                  ? 'text-error'
                  : 'text-warning'
            "
          >
            {{ protocolProbeResult.model }} ·
            {{ protocolProbeResult.protocol }} ·
            {{ protocolProbeResult.status }}
            <span v-if="protocolProbeResult.error">
              · {{ protocolProbeResult.error }}</span
            >
          </div>

          <table
            v-if="exactProbeResults.length > 0"
            class="data-table probe-results-table"
          >
            <thead>
              <tr>
                <th>{{ $t("providerDetail.probeColModel") }}</th>
                <th>{{ $t("providerDetail.probeColProtocol") }}</th>
                <th>{{ $t("providerDetail.probeColStatus") }}</th>
                <th>{{ $t("providerDetail.probeColCheckedAt") }}</th>
                <th>{{ $t("providerDetail.probeColError") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="probe in exactProbeResults"
                :key="`${probe.model}/${probe.protocol}`"
              >
                <td>
                  <code>{{ probe.model }}</code>
                </td>
                <td>
                  <code>{{ probe.protocol }}</code>
                </td>
                <td>{{ probe.status }}</td>
                <td>{{ formatTime(probe.checked_at) }}</td>
                <td>{{ probe.error || "-" }}</td>
              </tr>
            </tbody>
          </table>
        </details>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import {
  detectProviderProtocols,
  fetchConfig,
  fetchConfigSource,
  fetchProviderDetail,
  fetchProviderFormMeta,
  fetchStatus,
  healthCheck,
  probeProviderModelProtocol,
  restartGateway,
  saveConfig,
  setProviderSuppress,
  validateConfig,
} from "../api.js";
import KeyValueEditor from "../components/KeyValueEditor.vue";
import TagListEditor from "../components/TagListEditor.vue";

const { t } = useI18n();
const router = useRouter();

const REDACTED = "__REDACTED__";
const CUSTOM_ACCESS_TYPE = "__custom_access__";
const CUSTOM_SERVICE_TEMPLATE = "__custom__";

const props = defineProps({
  name: { type: String, default: "" },
  create: { type: Boolean, default: false },
});

const detail = ref(null);
const configDoc = ref({});
const configSource = ref(null);
const providerFormMeta = ref({ presets: [], service_protocol_templates: [] });
const providerName = ref("");
const providerConfig = ref(createEmptyProviderConfig());
const selectedPresetId = ref("");
const selectedServiceTemplateId = ref(CUSTOM_SERVICE_TEMPLATE);
const error = ref("");
const message = ref("");
const checking = ref(false);
const healthResult = ref(null);
const saving = ref(false);
const loading = ref(false);
const dirty = ref(false);
const suppressDirty = ref(false);
const showAPIKey = ref(false);
const apiKeyTouched = ref(false);
const configFileChanged = ref(false);
const waitingAlive = ref(false);
const waitingElapsed = ref(0);
const detectingProtocols = ref(false);
const selectedProbeModel = ref("");
const selectedProbeProtocol = ref("chat");
const protocolProbeResult = ref(null);
const exactProbing = ref(false);

watch(
  [providerName, providerConfig],
  () => {
    if (!suppressDirty.value) dirty.value = true;
  },
  { deep: true },
);

watch(
  () => [
    providerConfig.value.family,
    providerConfig.value.backend,
    providerConfig.value.backend_provider,
    providerConfig.value.url,
    providerConfig.value.config_dir,
    providerConfig.value.service_protocols,
    providerConfig.value.anthropic_to_chat,
  ],
  () => {
    if (selectedPresetId.value !== CUSTOM_ACCESS_TYPE) {
      selectedPresetId.value =
        inferPresetID(providerConfig.value) || CUSTOM_ACCESS_TYPE;
    }
    if (selectedServiceTemplateId.value !== CUSTOM_SERVICE_TEMPLATE) {
      syncSelectedServiceTemplate();
    }
  },
  { deep: true },
);

watch(
  () => [props.name, props.create],
  () => {
    load();
  },
  { immediate: true },
);

const pageTitle = computed(() =>
  props.create
    ? t("providerDetail.newProviderTitle")
    : providerName.value || props.name,
);

const providerPresets = computed(() => providerFormMeta.value?.presets || []);
const serviceProtocolTemplates = computed(
  () => providerFormMeta.value?.service_protocol_templates || [],
);
const accessTypeOptions = computed(() => [
  ...providerPresets.value,
  {
    id: CUSTOM_ACCESS_TYPE,
    title: t("providerDetail.customAccessType"),
    summary: t("providerDetail.customAccessTypeDesc"),
  },
]);

const currentPreset = computed(
  () =>
    providerPresets.value.find(
      (preset) => preset.id === selectedPresetId.value,
    ) || null,
);
const selectedAccessTypeId = computed(() =>
  currentPreset.value ? currentPreset.value.id : CUSTOM_ACCESS_TYPE,
);
const isCustomAccessType = computed(
  () => selectedAccessTypeId.value === CUSTOM_ACCESS_TYPE,
);
const isCLIProxyBackend = computed(
  () => providerBackend(providerConfig.value) === "cliproxy",
);
const isManagedCLIProxyAccess = computed(
  () => isCLIProxyBackend.value && !isCustomAccessType.value,
);
const currentAccessTypeSummary = computed(() => {
  const current = accessTypeOptions.value.find(
    (option) => option.id === selectedAccessTypeId.value,
  );
  return accessTypeSummary(current);
});

const visibleServiceProtocolTemplates = computed(() => {
  const family = providerFamily(providerConfig.value);
  const backend = providerBackend(providerConfig.value);
  return serviceProtocolTemplates.value.filter((template) => {
    if (
      Array.isArray(template.families) &&
      template.families.length > 0 &&
      !template.families.includes(family)
    ) {
      return false;
    }
    if (
      Array.isArray(template.backends) &&
      template.backends.length > 0 &&
      !template.backends.includes(backend)
    ) {
      return false;
    }
    return true;
  });
});

const capabilityTemplateOptions = computed(() => [
  ...visibleServiceProtocolTemplates.value,
  {
    id: CUSTOM_SERVICE_TEMPLATE,
    title: t("providerDetail.interfaceTemplateCustom"),
    summary: t("providerDetail.interfaceTemplateCustomDesc"),
  },
]);

const currentServiceTemplateSummary = computed(() => {
  const current = capabilityTemplateOptions.value.find(
    (template) => template.id === selectedServiceTemplateId.value,
  );
  return serviceTemplateSummary(current);
});

const isCustomServiceTemplate = computed(
  () => selectedServiceTemplateId.value === CUSTOM_SERVICE_TEMPLATE,
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

const missingDiscoveredModelIds = computed(() =>
  discoveredModelIds.value.filter(
    (id) => !(providerConfig.value.models || []).includes(id),
  ),
);

const probeableModels = computed(() =>
  discoveredModelIds.value.length > 0
    ? discoveredModelIds.value
    : [...(providerConfig.value.models || [])],
);

const exactProbeResults = computed(() => {
  const entries = detail.value?.model_protocol_probes || [];
  return [...entries].sort((a, b) => {
    const modelCmp = String(a.model || "").localeCompare(String(b.model || ""));
    if (modelCmp !== 0) return modelCmp;
    return String(a.protocol || "").localeCompare(String(b.protocol || ""));
  });
});

const detailIsManagedCLIProxy = computed(
  () => providerBackend(detail.value) === "cliproxy",
);

const runtimeAccessTypeTitle = computed(() => {
  if (!detail.value) return "-";
  const presetID = inferPresetID(detail.value);
  const preset = providerPresets.value.find((item) => item.id === presetID);
  return preset?.title || detail.value.family || detail.value.protocol || "-";
});

const effectiveServiceProtocols = computed(() => {
  const configured = normalizeServiceProtocols(
    providerConfig.value.service_protocols,
  );
  if (configured.length > 0) return configured;
  return defaultServiceProtocolsForProvider(providerConfig.value);
});

const showsURLField = computed(
  () =>
    !!providerFamily(providerConfig.value) &&
    !["copilot"].includes(providerFamily(providerConfig.value)) &&
    !isManagedCLIProxyAccess.value,
);

const showsHeadersField = computed(() => showsURLField.value);

const connectionNote = computed(() =>
  isManagedCLIProxyAccess.value
    ? t("providerDetail.cliproxyConnectionNote")
    : t("providerDetail.noUrlRequired"),
);

const authNote = computed(() =>
  isManagedCLIProxyAccess.value
    ? t("providerDetail.cliproxyAuthNote")
    : t("providerDetail.authManagedByBackend"),
);

const providerUrlLabel = computed(() =>
  isCLIProxyBackend.value ? t("providerDetail.cliproxyEndpoint") : "url",
);

const providerUrlHint = computed(() =>
  isCLIProxyBackend.value ? t("providerDetail.cliproxyEndpointHint") : "",
);

const authMode = computed(() => {
  if (providerBackend(providerConfig.value) === "cliproxy") {
    return "none";
  }
  switch (providerFamily(providerConfig.value)) {
    case "copilot":
      return "config_dir";
    case "":
      return "";
    default:
      return "api_key";
  }
});

const configDirPlaceholder = computed(() => {
  if (currentPreset.value?.default_config_dir)
    return currentPreset.value.default_config_dir;
  switch (providerFamily(providerConfig.value)) {
    case "copilot":
      return "~/.config/github-copilot";
    default:
      return "";
  }
});

const serviceProtocolSuggestions = computed(() => {
  if (providerBackend(providerConfig.value) === "cliproxy") {
    return ["chat", "responses_stateless", "responses_stateful"];
  }
  switch (providerFamily(providerConfig.value)) {
    case "openai": {
      const protocols = [
        "chat",
        "responses_stateless",
        "responses_stateful",
        "embeddings",
      ];
      if (providerConfig.value?.anthropic_to_chat) protocols.push("anthropic");
      return protocols;
    }
    case "anthropic":
      return ["chat", "anthropic"];
    case "copilot":
      return ["chat"];
    default:
      return [];
  }
});

function createEmptyProviderConfig() {
  return {
    url: "",
    family: "",
    backend: "",
    backend_provider: "",
    service_protocols: [],
    models: [],
    responses_to_chat: false,
    anthropic_to_chat: false,
    proxy: "",
    timeout: "",
    config_dir: "",
    headers: {},
    api_key: "",
  };
}

function normalizeText(value) {
  return String(value || "")
    .trim()
    .toLowerCase();
}

function cloneData(value) {
  return JSON.parse(JSON.stringify(value ?? {}));
}

function providerFamily(provider) {
  return normalizeText(provider?.family || provider?.protocol);
}

function providerBackend(provider) {
  return normalizeText(provider?.backend);
}

function normalizeServiceProtocols(protocols) {
  const out = [];
  const seen = new Set();
  for (const raw of protocols || []) {
    const protocol = normalizeText(raw);
    if (!protocol || seen.has(protocol)) continue;
    seen.add(protocol);
    out.push(protocol);
    if (protocol === "responses_stateful" && !seen.has("responses_stateless")) {
      seen.add("responses_stateless");
      out.push("responses_stateless");
    }
  }
  return out;
}

function defaultServiceProtocolsForProvider(provider) {
  if (providerBackend(provider) === "cliproxy") {
    return [];
  }
  switch (providerFamily(provider)) {
    case "openai": {
      const protocols = [
        "chat",
        "responses_stateless",
        "responses_stateful",
        "embeddings",
      ];
      if (provider?.anthropic_to_chat) protocols.push("anthropic");
      return protocols;
    }
    case "anthropic":
      return ["chat", "anthropic"];
    case "copilot":
      return ["chat"];
    default:
      return [];
  }
}

function serviceProtocolsEqual(left, right) {
  const a = normalizeServiceProtocols(left);
  const b = normalizeServiceProtocols(right);
  if (a.length !== b.length) return false;
  return a.every((protocol, index) => protocol === b[index]);
}

function inferPresetID(provider) {
  const family = providerFamily(provider);
  const backend = providerBackend(provider);
  const backendProvider = normalizeText(provider?.backend_provider);
  const url = String(provider?.url || "").trim();
  if (family === "openai" && backend === "cliproxy" && backendProvider) {
    const match = providerPresets.value.find(
      (preset) =>
        preset.family === family &&
        normalizeText(preset.backend) === backend &&
        normalizeText(preset.backend_provider) === backendProvider,
    );
    return match?.id || "";
  }
  if (family === "openai" && backend === "") {
    if (url === "http://127.0.0.1:11434/v1") return "ollama-chat";
    return "openai-compatible";
  }
  if (family === "anthropic" && url === "https://api.anthropic.com/v1")
    return "anthropic-official";
  if (family === "copilot") return "copilot-cli";
  return "";
}

function inferServiceTemplateID(provider) {
  if (provider.responses_to_chat) return CUSTOM_SERVICE_TEMPLATE;
  for (const template of visibleServiceProtocolTemplates.value) {
    if (
      !serviceProtocolsEqual(
        provider.service_protocols,
        template.service_protocols,
      )
    )
      continue;
    if (!!provider.anthropic_to_chat !== !!template.anthropic_to_chat) continue;
    return template.id;
  }
  return CUSTOM_SERVICE_TEMPLATE;
}

function syncSelectedServiceTemplate() {
  selectedServiceTemplateId.value = inferServiceTemplateID(
    providerConfig.value,
  );
}

function accessTypeTitle(option) {
  return option?.title || "";
}

function accessTypeSummary(option) {
  if (!option?.id) return "";
  if (option.id === CUSTOM_ACCESS_TYPE) {
    return option.summary || t("providerDetail.customAccessTypeDesc");
  }
  return option.summary || "";
}

function serviceTemplateTitle(template) {
  if (!template?.id) return "";
  if (template.id === CUSTOM_SERVICE_TEMPLATE) return template.title || "";
  const key = `providerDetail.interfaceTemplate_${template.id}`;
  const translated = t(key);
  return translated === key ? template.title || template.id : translated;
}

function serviceTemplateSummary(template) {
  if (!template?.id) return t("providerDetail.interfaceTemplateCustomDesc");
  if (template.id === CUSTOM_SERVICE_TEMPLATE) {
    return template.summary || t("providerDetail.interfaceTemplateCustomDesc");
  }
  const key = `providerDetail.interfaceTemplate_${template.id}_desc`;
  const translated = t(key);
  return translated === key ? template.summary || "" : translated;
}

function serviceProtocolTitle(protocol) {
  const key = `providerDetail.serviceProtocol_${protocol}`;
  const translated = t(key);
  return translated === key ? protocol : translated;
}

function applyServiceProtocolTemplateByID(templateID) {
  const template = serviceProtocolTemplates.value.find(
    (item) => item.id === templateID,
  );
  if (!template) return;
  providerConfig.value.service_protocols = [
    ...(template.service_protocols || []),
  ];
  providerConfig.value.anthropic_to_chat = !!template.anthropic_to_chat;
  selectedServiceTemplateId.value = templateID;
}

function applyPresetByID(presetID) {
  const preset = providerPresets.value.find((item) => item.id === presetID);
  if (!preset) return;

  const current = providerConfig.value || createEmptyProviderConfig();
  const previousPreset = currentPreset.value;
  const next = createEmptyProviderConfig();
  next.family = preset.family || "";
  next.backend = preset.backend || "";
  next.backend_provider = preset.backend_provider || "";
  next.url = presetFieldValue(
    current.url,
    previousPreset?.default_url,
    preset.default_url,
  );
  next.config_dir = presetFieldValue(
    current.config_dir,
    previousPreset?.default_config_dir,
    preset.default_config_dir,
  );
  next.models = [...(current.models || [])];
  next.headers = cloneData(current.headers || {});
  next.proxy = current.proxy || "";
  next.timeout = current.timeout || "";
  next.api_key = current.api_key || "";
  providerConfig.value = next;
  selectedPresetId.value = preset.id;
  showAPIKey.value = false;
  if (preset.service_protocol_template) {
    applyServiceProtocolTemplateByID(preset.service_protocol_template);
  } else {
    syncSelectedServiceTemplate();
  }
}

function applyAccessPresetByID(presetID) {
  const preset = providerPresets.value.find((item) => item.id === presetID);
  if (!preset) return;

  const previousPreset = currentPreset.value;
  providerConfig.value.family = preset.family || "";
  providerConfig.value.backend = preset.backend || "";
  providerConfig.value.backend_provider = preset.backend_provider || "";
  providerConfig.value.url = presetFieldValue(
    providerConfig.value.url,
    previousPreset?.default_url,
    preset.default_url,
  );
  providerConfig.value.config_dir = presetFieldValue(
    providerConfig.value.config_dir,
    previousPreset?.default_config_dir,
    preset.default_config_dir,
  );
  selectedPresetId.value = preset.id;
  if (preset.service_protocol_template) {
    applyServiceProtocolTemplateByID(preset.service_protocol_template);
  } else {
    syncSelectedServiceTemplate();
  }
}

function handleAccessTypeChange(presetID) {
  if (presetID === CUSTOM_ACCESS_TYPE) {
    selectedPresetId.value = CUSTOM_ACCESS_TYPE;
    return;
  }
  if (props.create) {
    applyPresetByID(presetID);
    return;
  }
  applyAccessPresetByID(presetID);
}

function presetFieldValue(currentValue, previousDefault, nextDefault) {
  const current = String(currentValue || "").trim();
  const prev = String(previousDefault || "").trim();
  const next = String(nextDefault || "").trim();
  if (!current) return next;
  if (prev && current === prev) return next;
  return currentValue || "";
}

function handleServiceTemplateChange(templateID) {
  selectedServiceTemplateId.value = templateID;
  if (templateID === CUSTOM_SERVICE_TEMPLATE) return;
  applyServiceProtocolTemplateByID(templateID);
}

function secretDisplay(value) {
  return value === REDACTED ? REDACTED : value || "";
}

function isSecretConfigured(value) {
  return !!value;
}

function providerUrlPlaceholder(provider) {
  if (providerBackend(provider) === "cliproxy") {
    return currentPreset.value?.default_url || "http://127.0.0.1:18741/v1";
  }
  switch (providerFamily(provider)) {
    case "":
      return "Select family first";
    case "anthropic":
      return "https://api.anthropic.com/v1";
    case "copilot":
      return "https://api.githubcopilot.com";
    default:
      return "https://api.openai.com/v1";
  }
}

function appendDiscoveredModels() {
  if (missingDiscoveredModelIds.value.length === 0) return;
  providerConfig.value.models = [
    ...(providerConfig.value.models || []),
    ...missingDiscoveredModelIds.value,
  ];
}

function formatTime(timeValue) {
  if (!timeValue) return "";
  return new Date(timeValue).toLocaleString();
}

function cleanConfig(obj) {
  if (obj === null || obj === undefined) return obj;
  if (Array.isArray(obj)) return obj;
  if (typeof obj !== "object") return obj;

  const out = {};
  for (const [key, value] of Object.entries(obj)) {
    if (value === null || value === undefined) continue;
    if (typeof value === "object" && !Array.isArray(value)) {
      const cleaned = {};
      for (const [innerKey, innerValue] of Object.entries(value)) {
        if (innerKey.startsWith("__new_")) continue;
        cleaned[innerKey] = cleanConfig(innerValue);
      }
      if (Object.keys(cleaned).length > 0) out[key] = cleaned;
    } else {
      out[key] = value;
    }
  }
  return out;
}

function pruneProviderReferences(nextConfig, targetProvider) {
  if (!nextConfig?.route || typeof nextConfig.route !== "object") return;

  for (const [prefix, route] of Object.entries(nextConfig.route)) {
    if (!route || typeof route !== "object") continue;

    const nextExactModels = {};
    for (const [modelName, modelConfig] of Object.entries(
      route.exact_models || {},
    )) {
      if (!modelConfig || typeof modelConfig !== "object") continue;
      const upstreams = (modelConfig.upstreams || []).filter(
        (upstream) => upstream?.provider !== targetProvider,
      );
      if (upstreams.length === 0) continue;
      nextExactModels[modelName] = { ...modelConfig, upstreams };
    }

    const nextWildcardModels = {};
    for (const [pattern, modelConfig] of Object.entries(
      route.wildcard_models || {},
    )) {
      if (!modelConfig || typeof modelConfig !== "object") continue;
      const providers = (modelConfig.providers || []).filter(
        (provider) => provider !== targetProvider,
      );
      if (providers.length === 0) continue;
      nextWildcardModels[pattern] = { ...modelConfig, providers };
    }

    if (
      Object.keys(nextExactModels).length === 0 &&
      Object.keys(nextWildcardModels).length === 0
    ) {
      delete nextConfig.route[prefix];
      continue;
    }

    route.exact_models = nextExactModels;
    route.wildcard_models = nextWildcardModels;
  }
}

async function load() {
  loading.value = true;
  suppressDirty.value = true;
  error.value = "";
  healthResult.value = null;
  configFileChanged.value = false;
  protocolProbeResult.value = null;
  try {
    const [cfg, source, formMeta] = await Promise.all([
      fetchConfig(),
      fetchConfigSource(),
      fetchProviderFormMeta(),
    ]);
    configDoc.value = cfg;
    configSource.value = source;
    providerFormMeta.value = formMeta;
    showAPIKey.value = false;
    apiKeyTouched.value = false;

    if (props.create) {
      providerName.value = "";
      detail.value = null;
      selectedProbeModel.value = "";
      selectedPresetId.value = "";
      providerConfig.value = createEmptyProviderConfig();
      const defaultPresetID = providerPresets.value[0]?.id || "";
      if (defaultPresetID) {
        applyPresetByID(defaultPresetID);
      }
    } else {
      providerName.value = props.name;
      const provider = cfg.provider?.[props.name];
      if (!provider) {
        throw new Error(
          t("providerDetail.providerConfigMissing", { name: props.name }),
        );
      }
      providerConfig.value = {
        ...createEmptyProviderConfig(),
        ...cloneData(provider),
        family: provider.family || provider.protocol || "",
        service_protocols: [...(provider.service_protocols || [])],
        models: [...(provider.models || [])],
        headers: cloneData(provider.headers || {}),
      };
      selectedPresetId.value = inferPresetID(providerConfig.value);
      syncSelectedServiceTemplate();
      detail.value = await fetchProviderDetail(props.name);
      if (!selectedProbeModel.value && discoveredModelIds.value.length > 0) {
        selectedProbeModel.value = discoveredModelIds.value[0];
      }
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

async function pollUntilAlive(timeoutMs = 60000, intervalMs = 1500) {
  const deadline = Date.now() + timeoutMs;
  waitingAlive.value = true;
  waitingElapsed.value = 0;
  const startMs = Date.now();
  const ticker = setInterval(() => {
    waitingElapsed.value = Math.floor((Date.now() - startMs) / 1000);
  }, 500);
  try {
    await new Promise((resolve) => setTimeout(resolve, 800));
    while (Date.now() < deadline) {
      try {
        await fetchStatus();
        return true;
      } catch {
        await new Promise((resolve) => setTimeout(resolve, intervalMs));
      }
    }
    return false;
  } finally {
    clearInterval(ticker);
    waitingAlive.value = false;
    waitingElapsed.value = 0;
  }
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
    const backend =
      family === "openai" ? providerBackend(providerConfig.value) : "";
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
    nextProviderConfig.backend =
      nextProviderConfig.family === "openai"
        ? providerBackend(nextProviderConfig)
        : "";
    nextProviderConfig.backend_provider = normalizeText(
      nextProviderConfig.backend_provider,
    );
    nextProviderConfig.service_protocols = normalizeServiceProtocols(
      nextProviderConfig.service_protocols,
    );
    if (!nextProviderConfig.backend) {
      delete nextProviderConfig.backend;
      delete nextProviderConfig.backend_provider;
    }
    if (nextProviderConfig.service_protocols.length === 0) {
      delete nextProviderConfig.service_protocols;
    }
    delete nextProviderConfig.protocol;
    if (!apiKeyTouched.value) {
      delete nextProviderConfig.api_key;
    }
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
      error.value = t("config.savedButRestartFailed", {
        error: restart.error || "unknown error",
      });
      return;
    }

    const alive = await pollUntilAlive();
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

async function deleteProvider() {
  if (
    !confirm(
      t("providerDetail.confirmDeleteProvider", { name: providerName.value }),
    )
  )
    return;

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
      error.value = t("config.savedButRestartFailed", {
        error: restart.error || "unknown error",
      });
      return;
    }

    const alive = await pollUntilAlive();
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

async function runHealthCheck() {
  checking.value = true;
  healthResult.value = null;
  try {
    healthResult.value = await healthCheck(props.name);
  } catch (e) {
    healthResult.value = { status: "error", error: e.message };
  } finally {
    checking.value = false;
  }
}

async function runProtocolDetect() {
  if (!props.name) return;
  detectingProtocols.value = true;
  error.value = "";
  try {
    await detectProviderProtocols(props.name);
    await load();
  } catch (e) {
    error.value = e.message;
  } finally {
    detectingProtocols.value = false;
  }
}

async function runExactProtocolProbe() {
  if (!props.name || !selectedProbeModel.value) return;
  exactProbing.value = true;
  error.value = "";
  protocolProbeResult.value = null;
  try {
    protocolProbeResult.value = await probeProviderModelProtocol(
      props.name,
      selectedProbeModel.value,
      selectedProbeProtocol.value,
    );
    await load();
  } catch (e) {
    error.value = e.message;
  } finally {
    exactProbing.value = false;
  }
}

async function suppressProvider() {
  try {
    await setProviderSuppress(props.name, true);
    await load();
  } catch (e) {
    error.value = e.message;
  }
}

async function unsuppressProvider() {
  try {
    await setProviderSuppress(props.name, false);
    await load();
  } catch (e) {
    error.value = e.message;
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

.runtime-actions {
  margin-top: -8px;
}

.health-result {
  font-size: 13px;
}

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

.field-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
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

.runtime-panel summary {
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
}

.summary-count {
  margin-left: 8px;
  color: var(--c-text-3);
  font-size: 12px;
  font-weight: normal;
}

.advanced-desc {
  margin-bottom: 12px;
}

.runtime-stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.compact-top {
  margin-bottom: 8px;
}

.runtime-panel {
  padding-top: 14px;
}

.runtime-panel summary {
  margin-bottom: 12px;
}

.runtime-tables {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 14px;
}

.probe-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 220px) auto;
  gap: 10px;
  align-items: center;
  margin-top: 12px;
}

.probe-results-table {
  margin-top: 12px;
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

.models-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.models-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.models-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.models-hint {
  display: block;
  margin: 0;
}

.form-checkbox {
  width: 16px;
  height: 16px;
  margin-top: 1px;
}

.badge-none {
  background: var(--c-border-light);
  color: var(--c-text-3);
}

.badge-muted {
  background: var(--c-bg-soft);
  color: var(--c-text-2);
}

@media (max-width: 768px) {
  .section-top,
  .form-panel-head {
    flex-direction: column;
  }

  .form-grid,
  .probe-grid {
    grid-template-columns: 1fr;
  }

  .form-grid > label {
    padding-top: 0;
  }

  .secret-field,
  .models-toolbar {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
