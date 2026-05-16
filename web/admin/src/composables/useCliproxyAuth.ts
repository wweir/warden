import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  createCLIProxyAuthFile,
  deleteCLIProxyAuthFile,
  fetchCLIProxyAuthFileUsage,
  fetchCLIProxyAuthFiles,
  verifyCLIProxyAuthFile,
} from "../api.js";
import { formatBytes, useCliproxyAuthFormatters } from "../utils/providerFormatters.js";

export function useCliproxyAuth(isManagedCLIProxyAccess: any, configDoc: any) {
  const { t } = useI18n();
  const formatters = useCliproxyAuthFormatters();

  const cliproxyAuthFiles = ref<any[]>([]);
  const cliproxyAuthLoading = ref(false);
  const cliproxyAuthContent = ref("");
  const cliproxyAuthFilename = ref("");
  const cliproxyAuthFileInput = ref<HTMLInputElement | null>(null);
  const cliproxyAuthVerifying = ref<Record<string, boolean>>({});
  const cliproxyAuthDeleting = ref<Record<string, boolean>>({});
  const cliproxyAuthOnlineResults = ref<Record<string, any>>({});
  const cliproxyAuthUsageLoading = ref<Record<string, boolean>>();
  const cliproxyAuthUsageResults = ref<Record<string, any>>({});
  const cliproxyAuthUsageRequestID = ref(0);

  const cliproxyAuthDirLabel = computed(() => configDoc.value?.cliproxy?.auth_dir || "/etc/warden");

  const cliproxyAuthFilesSummary = computed(() =>
    cliproxyAuthFiles.value.map((file) => ({
      ...file,
      sizeLabel: formatBytes(file.size),
      validationLabel: formatters.validationLabel(file.validation_status),
      validationClass: formatters.validationClass(file.validation_status),
      onlineResult: cliproxyAuthOnlineResults.value[file.filename] || null,
      onlineLabel: formatters.onlineLabel(cliproxyAuthOnlineResults.value[file.filename]?.status),
      onlineClass: formatters.onlineClass(cliproxyAuthOnlineResults.value[file.filename]?.status),
      usageResult: cliproxyAuthUsageResults.value[file.filename] || null,
      usageLabel: formatters.usageLabel(cliproxyAuthUsageResults.value[file.filename]?.status),
      usageClass: formatters.usageClass(cliproxyAuthUsageResults.value[file.filename]?.status),
    }))
  );

  const cliproxyAuthImportDisabled = computed(
    () => !isManagedCLIProxyAccess.value || cliproxyAuthLoading.value || !String(cliproxyAuthContent.value || "").trim()
  );

  watch(isManagedCLIProxyAccess, async (enabled) => {
    if (enabled) {
      await refreshCliproxyAuthFiles();
    } else {
      invalidateCliproxyAuthUsageRequests();
      cliproxyAuthFiles.value = [];
      cliproxyAuthUsageResults.value = {};
      cliproxyAuthUsageLoading.value = {};
      clearCliproxyAuthDraft();
    }
  }, { immediate: true });

  async function refreshCliproxyAuthFiles() {
    if (!isManagedCLIProxyAccess.value) return;
    cliproxyAuthLoading.value = true;
    try {
      const result = await fetchCLIProxyAuthFiles();
      cliproxyAuthFiles.value = result.files || [];
      void refreshCliproxyAuthUsageForFiles(cliproxyAuthFiles.value);
    } finally {
      cliproxyAuthLoading.value = false;
    }
  }

  async function refreshCliproxyAuthUsageForFiles(files: any[]) {
    const requestID = ++cliproxyAuthUsageRequestID.value;
    if (!Array.isArray(files) || files.length === 0) {
      cliproxyAuthUsageResults.value = {};
      cliproxyAuthUsageLoading.value = {};
      return;
    }
    const currentNames = new Set(files.map((file) => file.filename).filter(Boolean));
    const nextResults: Record<string, any> = {};
    for (const file of files) {
      if (cliproxyAuthUsageResults.value[file.filename]) {
        nextResults[file.filename] = cliproxyAuthUsageResults.value[file.filename];
      }
    }
    cliproxyAuthUsageResults.value = nextResults;
    await Promise.all(
      files.map(async (file) => {
        if (!file?.filename || file.validation_status === "invalid") return;
        cliproxyAuthUsageLoading.value = { ...cliproxyAuthUsageLoading.value, [file.filename]: true };
        try {
          const result = await fetchCLIProxyAuthFileUsage(file.filename);
          if (!isCurrentCliproxyAuthUsageRequest(requestID, file.filename, currentNames)) return;
          cliproxyAuthUsageResults.value = { ...cliproxyAuthUsageResults.value, [file.filename]: result };
        } catch (e: any) {
          if (!isCurrentCliproxyAuthUsageRequest(requestID, file.filename, currentNames)) return;
          cliproxyAuthUsageResults.value = {
            ...cliproxyAuthUsageResults.value,
            [file.filename]: { filename: file.filename, provider: file.provider, status: "error", error: e.message },
          };
        } finally {
          if (!isCurrentCliproxyAuthUsageRequest(requestID, file.filename, currentNames)) return;
          cliproxyAuthUsageLoading.value = { ...cliproxyAuthUsageLoading.value, [file.filename]: false };
        }
      })
    );
  }

  function invalidateCliproxyAuthUsageRequests() {
    cliproxyAuthUsageRequestID.value += 1;
  }

  function isCurrentCliproxyAuthUsageRequest(requestID: number, filename: string, expectedNames: Set<string>) {
    if (requestID !== cliproxyAuthUsageRequestID.value) return false;
    if (!expectedNames.has(filename)) return false;
    return cliproxyAuthFiles.value.some((file) => file?.filename === filename);
  }

  function clearCliproxyAuthDraft() {
    cliproxyAuthContent.value = "";
    cliproxyAuthFilename.value = "";
    if (cliproxyAuthFileInput.value) {
      cliproxyAuthFileInput.value.value = "";
    }
  }

  async function handleCliproxyAuthFileSelect(event: Event) {
    const file = (event?.target as HTMLInputElement)?.files?.[0];
    if (!file) return;
    cliproxyAuthContent.value = await file.text();
    if (!cliproxyAuthFilename.value) {
      cliproxyAuthFilename.value = file.name;
    }
  }

  async function importCliproxyAuth() {
    const content = String(cliproxyAuthContent.value || "").trim();
    if (!content) {
      throw new Error(t("providerDetail.cliproxyAuthUploadFailed", { error: "content is required" }));
    }
    const result = await createCLIProxyAuthFile(content, String(cliproxyAuthFilename.value || "").trim());
    clearCliproxyAuthDraft();
    await refreshCliproxyAuthFiles();
    return result;
  }

  async function verifyCliproxyAuth(file: any, providerName: string, model: string) {
    if (!file?.filename) return;
    cliproxyAuthVerifying.value = { ...cliproxyAuthVerifying.value, [file.filename]: true };
    try {
      const result = await verifyCLIProxyAuthFile(providerName, file.filename, model);
      cliproxyAuthOnlineResults.value = { ...cliproxyAuthOnlineResults.value, [file.filename]: result };
    } catch (e: any) {
      cliproxyAuthOnlineResults.value = {
        ...cliproxyAuthOnlineResults.value,
        [file.filename]: { status: "error", error: e.message },
      };
    } finally {
      cliproxyAuthVerifying.value = { ...cliproxyAuthVerifying.value, [file.filename]: false };
    }
  }

  async function deleteCliproxyAuth(file: any) {
    if (!file?.filename) return;
    if (!window.confirm(t("providerDetail.cliproxyAuthDeleteConfirm", { filename: file.filename }))) return;
    cliproxyAuthDeleting.value = { ...cliproxyAuthDeleting.value, [file.filename]: true };
    try {
      await deleteCLIProxyAuthFile(file.filename);
      const nextResults = { ...cliproxyAuthOnlineResults.value };
      delete nextResults[file.filename];
      cliproxyAuthOnlineResults.value = nextResults;
      const nextUsageResults = { ...cliproxyAuthUsageResults.value };
      delete nextUsageResults[file.filename];
      cliproxyAuthUsageResults.value = nextUsageResults;
      await refreshCliproxyAuthFiles();
    } finally {
      cliproxyAuthDeleting.value = { ...cliproxyAuthDeleting.value, [file.filename]: false };
    }
  }

  function cliproxyAuthOnlineDisabled(file: any, isCreate: boolean, providerName: string) {
    return (
      isCreate ||
      !providerName ||
      !isManagedCLIProxyAccess.value ||
      cliproxyAuthVerifying.value[file.filename] ||
      file.validation_status === "invalid"
    );
  }

  return {
    cliproxyAuthFiles,
    cliproxyAuthLoading,
    cliproxyAuthContent,
    cliproxyAuthFilename,
    cliproxyAuthFileInput,
    cliproxyAuthVerifying,
    cliproxyAuthDeleting,
    cliproxyAuthOnlineResults,
    cliproxyAuthUsageLoading,
    cliproxyAuthUsageResults,
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
  };
}
