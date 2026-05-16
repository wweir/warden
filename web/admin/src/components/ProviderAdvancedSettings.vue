<template>
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
          :model-value="headers"
          @update:model-value="$emit('update:headers', $event)"
          keyPlaceholder="Header name"
          valuePlaceholder="Value"
        />
      </template>

      <label>proxy</label>
      <input
        :value="proxy"
        @input="$emit('update:proxy', $event.target.value)"
        class="form-input"
        :placeholder="$t('providerDetail.proxyPlaceholder')"
      />

      <label>{{ $t("providerDetail.timeout") }}</label>
      <input
        :value="timeout"
        @input="$emit('update:timeout', $event.target.value)"
        class="form-input"
        :placeholder="$t('providerDetail.defaultTimeout')"
      />
    </div>
  </section>
</template>

<script setup>
import KeyValueEditor from "./KeyValueEditor.vue";

defineProps({
  headers: { type: Object, required: true },
  proxy: { type: String, default: "" },
  timeout: { type: String, default: "" },
  showsHeadersField: { type: Boolean, default: true },
});

defineEmits(["update:headers", "update:proxy", "update:timeout"]);
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

.section-desc {
  margin-top: 6px;
  font-size: 13px;
  color: var(--c-text-3);
  max-width: 760px;
}

.advanced-desc {
  margin-bottom: 12px;
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

@media (max-width: 768px) {
  .form-grid {
    grid-template-columns: 1fr;
  }

  .form-grid > label {
    padding-top: 0;
  }
}
</style>
