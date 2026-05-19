<template>
  <div class="custom-interface-editor cliproxy-auth-panel">
    <div class="custom-interface-head">
      <strong>{{ $t("providerDetail.cliproxyAuthImportSection") }}</strong>
      <p class="hint">{{ $t("providerDetail.cliproxyAuthImportDesc") }}</p>
      <p class="hint">
        {{ $t("providerDetail.cliproxyAuthDir") }}:
        <code>{{ cliproxyAuthDirLabel }}</code>
      </p>
    </div>
    <div class="cliproxy-auth-import">
      <label>{{ $t("providerDetail.cliproxyAuthPasteLabel") }}</label>
      <textarea
        v-model.trim="cliproxyAuthContent"
        class="form-input cliproxy-auth-textarea"
        :placeholder="$t('providerDetail.cliproxyAuthPastePlaceholder')"
      ></textarea>
      <div class="cliproxy-auth-file-row">
        <input
          ref="cliproxyAuthFileInput"
          type="file"
          class="form-input"
          accept=".json,application/json"
          @change="handleCliproxyAuthFileSelect"
        />
        <input
          v-model.trim="cliproxyAuthFilename"
          class="form-input"
          :placeholder="$t('providerDetail.cliproxyAuthFilenamePlaceholder')"
        />
      </div>
    </div>
    <div class="actions">
      <button
        class="btn btn-primary btn-sm"
        type="button"
        :disabled="cliproxyAuthImportDisabled"
        @click="handleImport"
      >
        {{ $t("providerDetail.cliproxyAuthUpload") }}
      </button>
      <button class="btn btn-secondary btn-sm" type="button" @click="refreshCliproxyAuthFiles">
        {{ $t("providerDetail.cliproxyAuthRefresh") }}
      </button>
      <button class="btn btn-secondary btn-sm" type="button" @click="clearCliproxyAuthDraft">
        {{ $t("providerDetail.cliproxyAuthClear") }}
      </button>
    </div>
    <div class="auth-file-list">
      <div class="field-summary">
        <strong>{{ $t("providerDetail.cliproxyAuthFilesTitle") }}</strong>
      </div>
      <div v-if="cliproxyAuthFilesSummary.length > 0" class="auth-files-cards">
        <div v-for="file in cliproxyAuthFilesSummary" :key="file.filename" class="auth-file-card">
          <div class="auth-file-card-main">
            <code class="auth-file-name">{{ file.filename }}</code>
            <div class="auth-file-meta">
              <span>{{ file.provider || "-" }}</span>
              <span>{{ file.label || "-" }}</span>
              <span>{{ file.sizeLabel }}</span>
              <span>{{ formatTime(file.modified) }}</span>
            </div>
          </div>
          <div class="auth-file-status">
            <div>
              <span class="badge" :class="file.validationClass">{{ file.validationLabel }}</span>
              <span v-if="file.validation_message" class="validation-message">
                {{ file.validation_message }}
              </span>
            </div>
            <div>
              <span v-if="file.onlineResult" class="badge" :class="file.onlineClass">
                {{ file.onlineLabel }}
              </span>
              <span v-if="file.onlineResult?.error" class="validation-message">
                {{ file.onlineResult.error }}
              </span>
              <span v-else-if="file.onlineResult" class="validation-message">
                {{ file.onlineResult.model }} / {{ file.onlineResult.latency_ms }}ms
              </span>
            </div>
            <div>
              <span v-if="cliproxyAuthUsageLoading[file.filename]" class="badge badge-muted">
                {{ $t("providerDetail.cliproxyAuthUsageLoading") }}
              </span>
              <template v-else-if="file.usageResult">
                <span class="badge" :class="file.usageClass">{{ file.usageLabel }}</span>
                <span
                  class="validation-message usage-message"
                  tabindex="0"
                  :aria-label="formatters.usageDetails(file.usageResult)"
                >
                  <span class="usage-metrics">
                    <span
                      v-for="item in formatters.usageMetrics(file.usageResult)"
                      :key="`${file.filename}-${item.name}`"
                      class="usage-metric"
                    >
                      <span class="usage-metric-name">{{ formatters.usageMetricName(item.name) }}</span>
                      <span class="usage-metric-value">{{ item.value }}</span>
                    </span>
                    <span v-if="file.usageResult.cached" class="usage-metric usage-metric-muted">
                      {{ $t("providerDetail.cliproxyAuthUsageCached") }}
                    </span>
                  </span>
                  <pre class="usage-tooltip">{{ formatters.usageDetails(file.usageResult) }}</pre>
                </span>
              </template>
            </div>
          </div>
          <div class="auth-file-actions">
            <button
              class="btn btn-secondary btn-sm auth-file-action"
              type="button"
              :disabled="cliproxyAuthOnlineDisabled(file, isCreate, providerName)"
              @click="handleVerify(file)"
            >
              {{
                cliproxyAuthVerifying[file.filename]
                  ? $t("providerDetail.cliproxyAuthOnlineChecking")
                  : $t("providerDetail.cliproxyAuthOnlineVerify")
              }}
            </button>
            <button
              class="btn btn-danger btn-sm auth-file-action"
              type="button"
              :disabled="cliproxyAuthDeleting[file.filename]"
              @click="handleDelete(file)"
            >
              {{
                cliproxyAuthDeleting[file.filename]
                  ? $t("providerDetail.cliproxyAuthDeleting")
                  : $t("common.delete")
              }}
            </button>
          </div>
        </div>
      </div>
      <div v-else class="section-note">
        {{ $t("providerDetail.cliproxyAuthFilesEmpty") }}
      </div>
    </div>
  </div>
</template>

<script setup>
import { toRef } from "vue";
import { useCliproxyAuth } from "../composables/useCliproxyAuth.ts";
import { formatTime } from "../utils/providerFormatters.ts";

const props = defineProps({
  isManagedCLIProxyAccess: { type: Boolean, required: true },
  configDoc: { type: Object, required: true },
  isCreate: { type: Boolean, default: false },
  providerName: { type: String, default: "" },
  verifyModel: { type: String, default: "" },
});

const emit = defineEmits(["success", "error"]);

const {
  cliproxyAuthContent,
  cliproxyAuthFilename,
  cliproxyAuthFileInput,
  cliproxyAuthVerifying,
  cliproxyAuthDeleting,
  cliproxyAuthUsageLoading,
  cliproxyAuthDirLabel,
  cliproxyAuthFilesSummary,
  cliproxyAuthImportDisabled,
  refreshCliproxyAuthFiles,
  clearCliproxyAuthDraft,
  handleCliproxyAuthFileSelect,
  importCliproxyAuth,
  verifyCliproxyAuth,
  deleteCliproxyAuth,
  cliproxyAuthOnlineDisabled,
  formatters,
} = useCliproxyAuth(toRef(props, "isManagedCLIProxyAccess"), toRef(props, "configDoc"));

async function handleImport() {
  try {
    const result = await importCliproxyAuth();
    emit("success", result.file?.filename || "-");
  } catch (e) {
    emit("error", e.message);
  }
}

async function handleVerify(file) {
  try {
    await verifyCliproxyAuth(file, props.providerName, props.verifyModel);
  } catch (e) {
    emit("error", e.message);
  }
}

async function handleDelete(file) {
  try {
    await deleteCliproxyAuth(file);
    emit("success", file.filename);
  } catch (e) {
    emit("error", e.message);
  }
}
</script>

<style scoped>
.cliproxy-auth-panel {
  grid-column: 1 / -1;
  margin-top: 6px;
  min-width: 0;
}

.cliproxy-auth-import {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.cliproxy-auth-file-row {
  display: grid;
  grid-template-columns: minmax(180px, 0.7fr) minmax(180px, 1fr);
  gap: 10px;
}

.cliproxy-auth-textarea {
  min-height: 120px;
  resize: vertical;
  white-space: pre;
  font-family: var(--font-mono);
}

.auth-file-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}

.auth-files-cards {
  display: grid;
  gap: 8px;
  min-width: 0;
}

.auth-file-card {
  display: grid;
  grid-template-columns: minmax(0, 1.3fr) minmax(180px, 0.9fr) minmax(120px, auto);
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--c-border);
  border-radius: 8px;
  background: var(--c-surface);
}

.auth-file-card-main,
.auth-file-status {
  min-width: 0;
}

.auth-file-name {
  display: block;
  overflow-wrap: anywhere;
}

.auth-file-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 10px;
  margin-top: 6px;
  color: var(--c-text-3);
  font-size: 12px;
}

.auth-file-status {
  display: grid;
  gap: 6px;
}

.auth-file-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  min-width: 0;
}

.auth-file-action {
  white-space: nowrap;
}

.validation-message {
  display: block;
  margin-top: 4px;
  color: var(--c-text-3);
  line-height: 1.35;
}

.usage-message {
  position: relative;
  width: fit-content;
  max-width: 100%;
  cursor: help;
}

.usage-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  min-width: 0;
}

.usage-metric {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  padding: 2px 6px;
  border: 1px solid var(--c-border);
  border-radius: 6px;
  background: var(--c-bg-soft);
  color: var(--c-text-2);
  font-size: 12px;
  line-height: 1.35;
}

.usage-metric-name {
  color: var(--c-text-3);
}

.usage-metric-value {
  overflow-wrap: anywhere;
  color: var(--c-text);
}

.usage-metric-muted {
  color: var(--c-text-3);
}

.usage-tooltip {
  position: absolute;
  z-index: 20;
  left: 0;
  top: calc(100% + 6px);
  display: none;
  width: min(520px, 70vw);
  max-height: 320px;
  overflow: auto;
  margin: 0;
  padding: 10px;
  border: 1px solid var(--c-border);
  border-radius: 8px;
  background: var(--c-bg);
  color: var(--c-text);
  box-shadow: var(--shadow-md);
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.45;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.usage-message:hover .usage-tooltip,
.usage-message:focus .usage-tooltip,
.usage-message:focus-within .usage-tooltip {
  display: block;
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

.actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

@media (max-width: 768px) {
  .cliproxy-auth-file-row,
  .auth-file-card {
    grid-template-columns: 1fr;
  }
}
</style>
