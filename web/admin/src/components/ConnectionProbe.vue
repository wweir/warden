<template>
  <div class="connection-probe">
    <div class="probe-actions">
      <button
        type="button"
        class="btn btn-sm btn-secondary"
        @click="runProbe"
        :disabled="probing || !canProbe"
        :aria-busy="probing ? 'true' : 'false'"
      >
        {{ probing ? $t("providerDetail.accessProbing") : $t("providerDetail.detectAccessModes") }}
      </button>
    </div>

    <div v-if="hasResult" class="probe-results">
      <div v-if="capabilities.length > 0" class="probe-capabilities-box">
        <div class="probe-formats-label">{{ $t("providerDetail.probeDetectedFormats") }}</div>
        <div class="probe-capability-tags">
          <span
            v-for="cap in capabilities"
            :key="cap"
            class="probe-capability-badge"
          >
            {{ capabilityLabel(cap) }}
          </span>
        </div>
        <button
          type="button"
          class="btn btn-sm btn-primary"
          @click="applyAll"
        >
          {{ $t("providerDetail.applyProbeSuggestion") }}
        </button>
      </div>

      <div v-else class="probe-result-row">
        <span class="probe-error" role="status">
          {{ $t("providerDetail.probeFailed") }}{{ probeError ? `: ${probeError}` : "" }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from "vue";
import { probeProviderAccess } from "../api.js";

const props = defineProps({
  providerName: { type: String, default: "" },
  url: { type: String, required: true },
  apiKey: { type: String, default: "" },
  headers: { type: Object, default: () => ({}) },
  proxy: { type: String, default: "" },
});

const emit = defineEmits(["suggest"]);

const probing = ref(false);
const probeResult = ref(null);

const canProbe = computed(() => {
  return props.url && props.url.trim().length > 0;
});

const hasResult = computed(() => probeResult.value !== null);

const capabilities = computed(() => {
  if (!probeResult.value) return [];
  return probeResult.value.capabilities || [];
});

const probeError = computed(() => {
  const items = probeResult.value?.formats || [];
  const errors = items.map((item) => item.error).filter(Boolean);
  return errors[0] || "";
});

async function runProbe() {
  if (!canProbe.value) return;

  probing.value = true;
  probeResult.value = null;
  try {
    const result = await probeProviderAccess(props.providerName, props.url, props.apiKey, props.headers, props.proxy);
    probeResult.value = result;
  } catch (e) {
    probeResult.value = {
      capabilities: [],
      formats: [
        { mode: "openai", available: false, error: e.message },
        { mode: "anthropic", available: false, error: e.message },
      ],
    };
  } finally {
    probing.value = false;
  }
}

function applyAll() {
  if (!probeResult.value) return;
  const formats = (probeResult.value.formats || [])
    .filter((item) => item.available)
    .map((item) => item.mode);
  const resolvedURL = probeResult.value.formats?.find((f) => f.available)?.resolved_url || "";
  emit("suggest", {
    capabilities: capabilities.value,
    formats,
    resolvedURL,
  });
}

function capabilityLabel(cap) {
  const labels = {
    chat: "Chat",
    responses: "Responses",
    embeddings: "Embeddings",
    anthropic: "Anthropic",
  };
  return labels[cap] || cap;
}
</script>

<style scoped>
.connection-probe {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.probe-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.probe-results {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.probe-capabilities-box {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.probe-formats-label {
  font-size: 12px;
  color: var(--c-text-3);
}

.probe-capability-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.probe-capability-badge {
  font-size: 12px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--c-bg-2);
  color: var(--c-text-1);
}

.probe-result-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.probe-error {
  font-size: 11px;
  color: var(--c-danger);
}

@media (max-width: 768px) {
  .probe-result-row {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
