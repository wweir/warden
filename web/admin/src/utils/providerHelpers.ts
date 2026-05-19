import { normalizeServiceProtocols, providerBackend, providerFamily } from "../config-utils.js";

function providerEffectiveBackend(provider: any): string {
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

function authSourceIDs(provider: any): string[] {
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

// parseProviderModels parses raw model entries (strings or objects) and extracts deduplicated IDs
export function parseProviderModels(models: any[]): { parsed: any[]; ids: string[] } {
  const parsed = (models || []).map((model) => {
    if (typeof model === "string") {
      try {
        return JSON.parse(model);
      } catch {
        return { id: model };
      }
    }
    return model;
  });

  const ids: string[] = [];
  for (const model of parsed) {
    const id = typeof model?.id === "string" ? model.id.trim() : "";
    if (!id || ids.includes(id)) continue;
    ids.push(id);
  }

  return { parsed, ids };
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

// migrateAccessModeToLegacy converts backend endpoint config to form fields.
// Handles multi-endpoint `endpoints` structure by using the first endpoint as primary.
export function migrateAccessModeToLegacy(provider: any): any {
  const endpoints = provider?.endpoints || provider?.endpoint;
  const endpointNames = endpoints ? Object.keys(endpoints) : [];
  const configuredProtocols = firstNonEmptyArray(provider?.service_protocols, provider?.protocols);
  if (endpointNames.length === 0) {
    return {
      ...provider,
      family: provider?.family || provider?.format || "",
      service_protocols: configuredProtocols,
    };
  }

  const result = { ...provider };
  if (endpointNames.length === 1) {
    const ep = endpoints[endpointNames[0]];
    result.url = ep.url || provider.url || "";
    result.family = ep.format || endpointNames[0];
    result.service_protocols = firstNonEmptyArray(ep.protocols, configuredProtocols);
    result.responses_to_chat = !!ep.responses_to_chat;
    result.anthropic_to_chat = !!ep.anthropic_to_chat;
    result.anthropic_to_responses = !!ep.anthropic_to_responses;
    if (ep.models?.length) result.models = ep.models;
    if (ep.headers) result.headers = { ...ep.headers };
  } else {
    // Multi-endpoint: merge all protocols for display; preserve endpoint structure
    const allProtocols = new Set<string>();
    for (const name of endpointNames) {
      const ep = endpoints[name];
      if (ep?.protocols) {
        for (const p of ep.protocols) allProtocols.add(p);
      }
    }
    const firstEp = endpoints[endpointNames[0]];
    result.url = firstEp.url || provider.url || "";
    result.family = firstEp.format || endpointNames[0];
    result.service_protocols = Array.from(allProtocols);
    result.endpoint = endpoints;
  }
  return result;
}

function firstNonEmptyArray(...values: any[]): any[] {
  for (const value of values) {
    if (Array.isArray(value) && value.length > 0) return value;
  }
  return [];
}

// buildProviderSaveConfig converts frontend form fields to backend provider config fields.
// Maps family -> format, service_protocols -> protocols, and ensures required defaults.
export function buildProviderSaveConfig(provider: any): any {
  const result = { ...provider };
  const family = providerFamily(provider);

  // Remove internal-only fields
  delete result.family;
  delete result.service_protocols;
  delete result.protocol;

  result.format = family || "openai";

  if (family === "copilot") {
    return result;
  }

  // Preserve multi-endpoint configuration
  const endpoint = provider.endpoint || provider.endpoints;
  if (endpoint && Object.keys(endpoint).length > 1) {
    const protocols = normalizeServiceProtocols(provider.service_protocols);
    for (const name of Object.keys(endpoint)) {
      const ep = endpoint[name];
      if (ep.format === "openai") {
        ep.protocols = Array.from(new Set([...protocols.filter((p: string) => p !== "anthropic"), "chat"]));
      } else if (ep.format === "anthropic") {
        ep.protocols = ["chat", "anthropic"];
      }
    }
    result.endpoint = endpoint;
    delete result.url;
    delete result.format;
    delete result.protocols;
    delete result.anthropic_to_chat;
    delete result.anthropic_to_responses;
    return result;
  }

  const sp = new Set<string>();
  if (provider.service_protocols) {
    for (const p of provider.service_protocols) sp.add(p);
  }
  sp.add("chat");

  // Derive bridge flags from selected protocols
  if (family === "openai") {
    result.anthropic_to_chat = sp.has("anthropic");
  } else if (family === "anthropic") {
    sp.add("anthropic");
    result.anthropic_to_responses = sp.has("responses");
  }
  result.protocols = Array.from(sp);

  return result;
}
