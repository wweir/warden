<template>
  <div class="runtime-stack">
    <section class="info-section">
      <div class="section-top compact-top">
        <div>
          <h3>{{ $t("providerDetail.runtimeTools") }}</h3>
          <p class="section-desc">{{ $t("providerDetail.runtimeToolsDesc") }}</p>
        </div>
        <div class="actions runtime-actions">
          <button @click="runHealthCheck" class="btn btn-primary" :disabled="checking">
            {{ checking ? $t("providerDetail.checking") : $t("providerDetail.healthCheck") }}
          </button>
          <button
            v-if="detail.status && !detail.status.manual_suppressed"
            @click="handleSuppress"
            class="btn btn-danger"
          >
            {{ $t("providerDetail.suppressBtn") }}
          </button>
          <button v-else-if="detail.status" @click="handleUnsuppress" class="btn btn-secondary">
            {{ $t("providerDetail.unsuppressBtn") }}
          </button>
        </div>
      </div>
      <span
        v-if="healthResult"
        class="health-result"
        :class="healthResult.status === 'ok' ? 'text-success' : 'text-error'"
      >
        {{
          healthResult.status === "ok"
            ? $t("providerDetail.healthOk", { latency: healthResult.latency_ms, count: healthResult.model_count })
            : $t("providerDetail.healthError", { error: healthResult.error })
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
            <td><code>{{ detail.url }}</code></td>
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
            <td>{{ $t("providerDetail.serviceInterfaces") }}</td>
            <td>{{ (detail.service_protocols || []).join(", ") || "-" }}</td>
          </tr>
          <tr>
            <td>{{ $t("providerDetail.displayProtocols") }}</td>
            <td>{{ (detail.display_protocols || []).join(", ") || "-" }}</td>
          </tr>
          <tr v-if="detailIsManagedCLIProxy">
            <td>{{ $t("providerDetail.authSourceRuntime") }}</td>
            <td>{{ $t("providerDetail.authSourceCLIProxyAuthDir") }}</td>
          </tr>
          <tr v-else>
            <td>{{ $t("providerDetail.authSourceRuntime") }}</td>
            <td>{{ runtimeAuthSourceTitle }}</td>
          </tr>
        </table>

        <table v-if="detail.status" class="info-table">
          <tr>
            <td>{{ $t("providerDetail.suppressed") }}</td>
            <td>{{ detail.status.suppressed ? $t("providerDetail.yes") : $t("providerDetail.no") }}</td>
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
              {{ detail.status.total_requests > 0 ? detail.status.avg_latency_ms.toFixed(0) + "ms" : "-" }}
            </td>
          </tr>
        </table>
      </div>
    </details>

    <details class="info-section runtime-panel">
      <summary>{{ $t("providerDetail.availableModels", { n: parsedModels.length }) }}</summary>
      <div v-if="parsedModels.length === 0" class="empty">{{ $t("providerDetail.noModels") }}</div>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>{{ $t("providerDetail.modelId") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="m in parsedModels" :key="m.id">
            <td><code>{{ m.id }}</code></td>
          </tr>
        </tbody>
      </table>
    </details>

    <details class="info-section runtime-panel">
      <summary>{{ $t("providerDetail.protocolDetection") }}</summary>
      <div class="runtime-actions">
        <button @click="handleDetectProtocols" class="btn btn-secondary" :disabled="detectingProtocols">
          {{ detectingProtocols ? $t("providerDetail.detecting") : $t("providerDetail.detectDisplayProtocols") }}
        </button>
        <span v-if="detail.last_protocol_probe" class="hint">
          {{ detail.last_protocol_probe.status }} · {{ formatTime(detail.last_protocol_probe.checked_at) }}
          <span v-if="detail.last_protocol_probe.error"> · {{ detail.last_protocol_probe.error }}</span>
        </span>
      </div>

      <div class="probe-grid">
        <select v-model="selectedProbeModel" class="form-input">
          <option value="">{{ $t("providerDetail.selectModel") }}</option>
          <option v-for="model in probeableModels" :key="model" :value="model">{{ model }}</option>
        </select>
        <select v-model="selectedProbeProtocol" class="form-input">
          <option value="chat">chat</option>
          <option value="responses">responses</option>
          <option value="anthropic">anthropic</option>
        </select>
        <button @click="handleProbe" class="btn btn-primary" :disabled="exactProbing || !selectedProbeModel">
          {{ exactProbing ? $t("providerDetail.probing") : $t("providerDetail.probeModelProtocol") }}
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
        {{ protocolProbeResult.model }} · {{ protocolProbeResult.protocol }} · {{ protocolProbeResult.status }}
        <span v-if="protocolProbeResult.error"> · {{ protocolProbeResult.error }}</span>
      </div>

      <table v-if="exactProbeResults.length > 0" class="data-table probe-results-table">
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
          <tr v-for="probe in exactProbeResults" :key="`${probe.model}/${probe.protocol}`">
            <td><code>{{ probe.model }}</code></td>
            <td><code>{{ probe.protocol }}</code></td>
            <td>{{ probe.status }}</td>
            <td>{{ formatTime(probe.checked_at) }}</td>
            <td>{{ probe.error || "-" }}</td>
          </tr>
        </tbody>
      </table>
    </details>
  </div>
</template>

<script setup>
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useProviderRuntime } from "../composables/useProviderRuntime.ts";
import { formatTime } from "../utils/providerFormatters.ts";
import { inferAuthSource, inferPresetID } from "../utils/providerHelpers.ts";
import { providerBackend } from "../config-utils.js";

const { t } = useI18n();

const props = defineProps({
  detail: { type: Object, required: true },
  providerPresets: { type: Array, required: true },
  discoveredModelIds: { type: Array, required: true },
  providerConfig: { type: Object, required: true },
});

const emit = defineEmits(["reload", "error"]);

const {
  checking,
  healthResult,
  detectingProtocols,
  selectedProbeModel,
  selectedProbeProtocol,
  protocolProbeResult,
  exactProbing,
  runHealthCheck,
  runProtocolDetect,
  runExactProtocolProbe,
  suppressProvider,
  unsuppressProvider,
} = useProviderRuntime(props.detail.name);

const parsedModels = computed(() => {
  if (!props.detail) return [];
  return props.detail.models.map((model) => {
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

const probeableModels = computed(() =>
  props.discoveredModelIds.length > 0 ? props.discoveredModelIds : [...(props.providerConfig.models || [])]
);

const exactProbeResults = computed(() => {
  const entries = props.detail?.model_protocol_probes || [];
  return [...entries].sort((a, b) => {
    const modelCmp = String(a.model || "").localeCompare(String(b.model || ""));
    if (modelCmp !== 0) return modelCmp;
    return String(a.protocol || "").localeCompare(String(b.protocol || ""));
  });
});

const detailIsManagedCLIProxy = computed(() => providerBackend(props.detail) === "cliproxy");

const runtimeAccessTypeTitle = computed(() => {
  if (!props.detail) return "-";
  const presetID = inferPresetID(props.detail, props.providerPresets);
  const preset = props.providerPresets.find((item) => item.id === presetID);
  return preset?.title || props.detail.family || props.detail.protocol || "-";
});

const runtimeAuthSourceTitle = computed(() => {
  const source = props.detail?.auth_source || inferAuthSource(props.detail);
  switch (source) {
    case "api_key": return t("providerDetail.authSourceStatic");
    case "command": return t("providerDetail.authSourceCommand");
    case "config_dir": return t("providerDetail.authSourceConfigDir");
    case "none": return t("providerDetail.authSourceNone");
    default: return source || "-";
  }
});

async function handleDetectProtocols() {
  try {
    await runProtocolDetect();
    emit("reload");
  } catch (e) {
    emit("error", e.message);
  }
}

async function handleProbe() {
  try {
    await runExactProtocolProbe();
    emit("reload");
  } catch (e) {
    emit("error", e.message);
  }
}

async function handleSuppress() {
  try {
    await suppressProvider();
    emit("reload");
  } catch (e) {
    emit("error", e.message);
  }
}

async function handleUnsuppress() {
  try {
    await unsuppressProvider();
    emit("reload");
  } catch (e) {
    emit("error", e.message);
  }
}
</script>

<style scoped>
.runtime-stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.section-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 14px;
}

.compact-top {
  margin-bottom: 8px;
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

.runtime-panel {
  padding-top: 14px;
}

.runtime-panel summary {
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
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

.hint {
  color: var(--c-text-3);
  font-size: 11px;
  font-weight: normal;
}

@media (max-width: 768px) {
  .section-top {
    flex-direction: column;
  }

  .probe-grid {
    grid-template-columns: 1fr;
  }
}
</style>
