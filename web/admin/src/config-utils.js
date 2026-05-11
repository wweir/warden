export function normalizeText(value) {
  return String(value || "").trim();
}

export function normalizeLowerText(value) {
  return normalizeText(value).toLowerCase();
}

export function cloneData(value) {
  return JSON.parse(JSON.stringify(value ?? {}));
}

export function deepClone(value) {
  return JSON.parse(JSON.stringify(value));
}

export function cleanConfig(obj) {
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

export function providerFamily(provider) {
  return normalizeLowerText(provider?.family || provider?.protocol);
}

export function providerBackend(provider) {
  return normalizeLowerText(provider?.backend);
}

export function normalizeServiceProtocols(protocols) {
  const out = [];
  const seen = new Set();
  for (const raw of protocols || []) {
    const protocol = normalizeLowerText(raw);
    if (!protocol || seen.has(protocol)) continue;
    seen.add(protocol);
    out.push(protocol);
  }
  return out;
}

export function defaultServiceProtocolsForProvider(provider) {
  if (providerBackend(provider) === "cliproxy") {
    return [];
  }
  switch (providerFamily(provider)) {
    case "openai": {
      const protocols = ["chat", "responses", "embeddings"];
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

export function serviceProtocolsEqual(left, right) {
  const a = normalizeServiceProtocols(left);
  const b = normalizeServiceProtocols(right);
  if (a.length !== b.length) return false;
  return a.every((protocol, index) => protocol === b[index]);
}
