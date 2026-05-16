<template>
  <section class="form-panel optional-panel">
    <div class="form-panel-head">
      <div>
        <h4>
          {{ $t("providerDetail.staticModelsSection") }}
          <span class="summary-count">
            {{ $t("providerDetail.modelsConfiguredCount", { n: modelValue.length }) }}
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
          <span class="badge badge-none">{{ $t("providerDetail.modelsOptional") }}</span>
          <span v-if="!isCreate" class="hint">
            {{ $t("providerDetail.modelsDiscoveredCount", { n: discoveredModelIds.length }) }}
          </span>
        </div>
        <button
          v-if="missingDiscoveredModelIds.length > 0"
          type="button"
          class="btn btn-secondary btn-sm"
          @click="appendDiscoveredModels"
        >
          {{ $t("providerDetail.addDiscoveredModels", { n: missingDiscoveredModelIds.length }) }}
        </button>
      </div>
      <p class="hint models-hint">
        {{
          discoveredModelIds.length > 0
            ? $t("providerDetail.modelsSuggestionHint", { n: discoveredModelIds.length })
            : $t("providerDetail.modelsNoSuggestionHint")
        }}
      </p>
      <TagListEditor
        :model-value="modelValue"
        @update:model-value="$emit('update:modelValue', $event)"
        :suggestions="discoveredModelIds"
        :placeholder="$t('providerDetail.modelsPlaceholder')"
      />
      <p class="hint models-hint">{{ $t("providerDetail.modelsBehaviorHint") }}</p>
    </div>
  </section>
</template>

<script setup>
import { computed } from "vue";
import TagListEditor from "./TagListEditor.vue";

const props = defineProps({
  modelValue: { type: Array, required: true },
  discoveredModelIds: { type: Array, default: () => [] },
  isCreate: { type: Boolean, default: false },
});

const emit = defineEmits(["update:modelValue"]);

const missingDiscoveredModelIds = computed(() =>
  props.discoveredModelIds.filter((id) => !props.modelValue.includes(id))
);

function appendDiscoveredModels() {
  if (missingDiscoveredModelIds.value.length === 0) return;
  emit("update:modelValue", [...props.modelValue, ...missingDiscoveredModelIds.value]);
}
</script>

<style scoped>
.form-panel {
  border-top: 1px solid var(--c-border);
  padding-top: 18px;
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

.summary-count {
  margin-left: 8px;
  color: var(--c-text-3);
  font-size: 12px;
  font-weight: normal;
}

.section-desc {
  margin-top: 6px;
  font-size: 13px;
  color: var(--c-text-3);
  max-width: 760px;
}

.advanced-desc {
  margin-bottom: 12px;
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

.hint {
  color: var(--c-text-3);
  font-size: 11px;
  font-weight: normal;
}

.badge-none {
  background: var(--c-border-light);
  color: var(--c-text-3);
}

@media (max-width: 768px) {
  .models-toolbar {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
