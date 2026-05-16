import { providerBackend, providerFamily } from "../config-utils.js";

export function providerEffectiveBackend(provider: any): string {
  return providerFamily(provider) === "openai" ? providerBackend(provider) : "";
}

export function providerUrlPlaceholder(provider: any): string {
  if (providerEffectiveBackend(provider) === "cliproxy") {
    return "http://127.0.0.1:18741/v1";
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

export function secretDisplay(value: string): string {
  const REDACTED = "__REDACTED__";
  return value === REDACTED ? REDACTED : value || "";
}

export function isSecretConfigured(value: string): boolean {
  return !!value;
}

export function inferAuthSource(provider: any): string {
  if (!providerFamily(provider)) return "";
  if (providerEffectiveBackend(provider) === "cliproxy") return "none";
  if (String(provider?.api_key_command || "").trim()) return "command";
  if (provider?.api_key) return "api_key";
  if (providerFamily(provider) === "copilot") return "config_dir";
  return "api_key";
}

export function authSourceIDs(provider: any): string[] {
  const family = providerFamily(provider);
  if (!family) return [];
  if (providerEffectiveBackend(provider) === "cliproxy") return ["none"];
  if (family === "copilot") return ["config_dir", "api_key", "command", "none"];
  return ["api_key", "command", "none"];
}

export function effectiveAuthSource(provider: any, selected: string): string {
  const available = authSourceIDs(provider);
  if (available.includes(selected)) return selected;
  const inferred = inferAuthSource(provider);
  if (available.includes(inferred)) return inferred;
  return available[0] || "";
}

export function inferPresetID(provider: any, presets: any[]): string {
  const family = providerFamily(provider);
  const backend = providerEffectiveBackend(provider);
  const backendProvider = String(provider?.backend_provider || "").toLowerCase().trim();
  const url = String(provider?.url || "").trim();
  const hasPreset = (id: string) => presets.some((preset) => preset.id === id);
  const presetID = (id: string) => hasPreset(id) ? id : "";

  if (family === "openai" && backend === "cliproxy" && backendProvider) {
    const match = presets.find(
      (preset) =>
        preset.family === family &&
        String(preset.backend || "").toLowerCase().trim() === backend &&
        String(preset.backend_provider || "").toLowerCase().trim() === backendProvider
    );
    return match?.id || "";
  }
  if (family === "openai" && backend === "") {
    if (url === "http://127.0.0.1:11434/v1") return presetID("ollama-chat");
    return presetID("openai-compatible");
  }
  if (family === "anthropic") return presetID("anthropic-official");
  if (family === "copilot") return presetID("copilot-cli");
  return "";
}

export function createEmptyProviderConfig() {
  return {
    url: "",
    family: "",
    backend: "",
    backend_provider: "",
    service_protocols: [],
    models: [],
    responses_to_chat: false,
    anthropic_to_chat: false,
    anthropic_to_responses: false,
    proxy: "",
    timeout: "",
    config_dir: "",
    headers: {},
    api_key: "",
    api_key_command: "",
    api_key_command_timeout: "",
    api_key_command_ttl: "",
  };
}
