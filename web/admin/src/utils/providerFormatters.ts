import { useI18n } from "vue-i18n";

export function formatTime(timeValue: string | null | undefined): string {
  if (!timeValue) return "";
  return new Date(timeValue).toLocaleString();
}

export function formatBytes(size: number): string {
  if (!Number.isFinite(size) || size < 0) return "-";
  if (size < 1024) return `${size} B`;
  const units = ["KB", "MB", "GB"];
  let value = size / 1024;
  let unit = units[0];
  for (let i = 1; i < units.length && value >= 1024; i += 1) {
    value /= 1024;
    unit = units[i];
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${unit}`;
}

export function useCliproxyAuthFormatters() {
  const { t } = useI18n();

  const validationLabel = (status: string) => {
    switch (String(status || "").toLowerCase()) {
      case "valid": return t("providerDetail.cliproxyAuthValidationValid");
      case "warning": return t("providerDetail.cliproxyAuthValidationWarning");
      case "invalid": return t("providerDetail.cliproxyAuthValidationInvalid");
      default: return t("providerDetail.cliproxyAuthValidationUnknown");
    }
  };

  const validationClass = (status: string) => {
    switch (String(status || "").toLowerCase()) {
      case "valid": return "badge-ok";
      case "warning": return "badge-warn";
      case "invalid": return "badge-error";
      default: return "badge-muted";
    }
  };

  const onlineLabel = (status: string) => {
    switch (String(status || "").toLowerCase()) {
      case "ok": return t("providerDetail.cliproxyAuthOnlineOk");
      case "error": return t("providerDetail.cliproxyAuthOnlineError");
      default: return t("providerDetail.cliproxyAuthValidationUnknown");
    }
  };

  const onlineClass = (status: string) => {
    switch (String(status || "").toLowerCase()) {
      case "ok": return "badge-ok";
      case "error": return "badge-error";
      default: return "badge-muted";
    }
  };

  const usageLabel = (status: string) => {
    switch (String(status || "").toLowerCase()) {
      case "ok": return t("providerDetail.cliproxyAuthUsageOk");
      case "warning": return t("providerDetail.cliproxyAuthUsageWarning");
      case "disabled": return t("providerDetail.cliproxyAuthUsageDisabled");
      case "error": return t("providerDetail.cliproxyAuthUsageError");
      default: return t("providerDetail.cliproxyAuthUsageUnknown");
    }
  };

  const usageClass = (status: string) => {
    switch (String(status || "").toLowerCase()) {
      case "ok": return "badge-ok";
      case "warning": return "badge-warn";
      case "disabled":
      case "unknown": return "badge-muted";
      case "error": return "badge-error";
      default: return "badge-muted";
    }
  };

  const usageMetrics = (usage: any) => {
    if (!usage) return [];
    if (usage.error) return [{ name: "error", value: usage.error }];
    if (usage.summary?.length) return usage.summary;
    const fallback = usage.note || usage.status_message || "-";
    return [{ name: "status", value: fallback }];
  };

  const usageMetricName = (name: string) => {
    const labels: Record<string, string> = {
      plan: t("providerDetail.cliproxyAuthUsageMetricPlan"),
      "5h": t("providerDetail.cliproxyAuthUsageMetric5h"),
      "5h_reset": t("providerDetail.cliproxyAuthUsageMetric5hReset"),
      weekly: t("providerDetail.cliproxyAuthUsageMetricWeekly"),
      weekly_reset: t("providerDetail.cliproxyAuthUsageMetricWeeklyReset"),
      auth: t("providerDetail.cliproxyAuthUsageMetricAuth"),
      quota: t("providerDetail.cliproxyAuthUsageMetricQuota"),
      recover_at: t("providerDetail.cliproxyAuthUsageMetricRecoverAt"),
      cooldown: t("providerDetail.cliproxyAuthUsageMetricCooldown"),
      model: t("providerDetail.cliproxyAuthUsageMetricModel"),
      reset_at: t("providerDetail.cliproxyAuthUsageMetricResetAt"),
      reset_after: t("providerDetail.cliproxyAuthUsageMetricResetAfter"),
      remaining: t("providerDetail.cliproxyAuthUsageMetricRemaining"),
      credits: t("providerDetail.cliproxyAuthUsageMetricCredits"),
      credit_balance: t("providerDetail.cliproxyAuthUsageMetricCredits"),
      credits_balance: t("providerDetail.cliproxyAuthUsageMetricCredits"),
    };
    return labels[name] || name;
  };

  const usageDetails = (usage: any) => {
    if (!usage) return "";
    const detail: any = {};
    if (usage.summary?.length) detail.summary = usage.summary;
    if (usage.data && Object.keys(usage.data).length > 0) detail.data = usage.data;
    if (usage.checked_at) detail.checked_at = usage.checked_at;
    if (usage.cached) detail.cached = true;
    if (usage.note) detail.note = usage.note;
    if (usage.error) detail.error = usage.error;
    return JSON.stringify(detail, null, 2);
  };

  return {
    validationLabel,
    validationClass,
    onlineLabel,
    onlineClass,
    usageLabel,
    usageClass,
    usageMetrics,
    usageMetricName,
    usageDetails,
  };
}
