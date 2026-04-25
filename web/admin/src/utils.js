export function fmtNum(n) {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(n);
}

function normalizeText(value) {
  return String(value || "")
    .trim()
    .toLowerCase();
}

function normalizeConfiguredServiceProtocols(serviceProtocols) {
  if (!Array.isArray(serviceProtocols) || serviceProtocols.length === 0)
    return [];

  const out = [];
  const seen = new Set();
  const add = (protocol) => {
    if (!protocol || seen.has(protocol)) return;
    seen.add(protocol);
    out.push(protocol);
  };

  for (const raw of serviceProtocols) {
    const protocol = normalizeText(raw);
    switch (protocol) {
      case "chat":
      case "responses_stateless":
      case "responses_stateful":
      case "anthropic":
      case "embeddings":
        add(protocol);
        if (protocol === "responses_stateful") add("responses_stateless");
        break;
    }
  }

  return out;
}

function routeProtocolsFromServiceProtocols(serviceProtocols) {
  const out = [];
  const seen = new Set();
  const add = (protocol) => {
    if (!protocol || seen.has(protocol)) return;
    seen.add(protocol);
    out.push(protocol);
  };

  for (const protocol of serviceProtocols || []) {
    switch (protocol) {
      case "chat":
        add("chat");
        break;
      case "responses_stateless":
        add("responses_stateless");
        break;
      case "responses_stateful":
        add("responses_stateless");
        add("responses_stateful");
        break;
      case "anthropic":
        add("anthropic");
        break;
    }
  }

  return out;
}

function defaultServiceProtocols(provider) {
  const family = normalizeText(provider?.family || provider?.protocol);
  switch (family) {
    case "anthropic":
      return ["chat", "anthropic"];
    case "openai": {
      const protocols = [
        "chat",
        "responses_stateless",
        "responses_stateful",
        "embeddings",
      ];
      if (provider?.anthropic_to_chat) protocols.push("anthropic");
      return protocols;
    }
    case "copilot":
      return ["chat"];
    default:
      return [];
  }
}

export function providerRouteProtocols(provider) {
  const configuredServiceProtocols = normalizeConfiguredServiceProtocols(
    provider?.service_protocols,
  );
  if (configuredServiceProtocols.length > 0) {
    return routeProtocolsFromServiceProtocols(configuredServiceProtocols);
  }
  return routeProtocolsFromServiceProtocols(defaultServiceProtocols(provider));
}

export const DEFAULT_AI_HOOK_PROMPT = `You are a security reviewer for tool calls. Review the tool call below and return ONLY compact JSON:
{"allow": true/false, "reason": "short reason"}

Default to allow=false when the risk is unclear or the context is insufficient.
Allow only when the action is clearly necessary for the task, narrowly scoped, and does not expose private or secret information.

Prioritize command-execution safety. Be strict when the tool or arguments contain shell commands or shell-like syntax.
Deny if the command may:
- destroy or overwrite data, change permissions, stop services, kill processes, install software, or make broad system changes
- read, print, copy, or transmit secrets or personal data, including environment variables, tokens, keys, shell history, SSH/AWS credentials, browser data, or sensitive files
- download and run remote code, open reverse shells, exfiltrate local data, or contact unexpected network endpoints
- hide intent, bypass review, chain multiple risky actions, or use pipes, redirects, subshells, base64, heredocs, or command substitution to evade inspection

Tool: {{.FullName}}
Call ID: {{.CallID}}
Arguments: {{.Arguments}}
Result: {{.Result}}

Return allow=false for any destructive, privacy-invasive, malicious, or ambiguous case. The reason must name the specific risk.`;

export function formatDuration(ms) {
  const durationMs = Number(ms);
  if (!Number.isFinite(durationMs) || durationMs <= 0) return "0ms";
  if (durationMs < 1_000) return `${Math.round(durationMs)}ms`;

  const totalSeconds = durationMs / 1_000;
  if (durationMs < 60_000) {
    const precision = totalSeconds < 10 ? 1 : 0;
    return `${Number(totalSeconds.toFixed(precision))}s`;
  }

  const totalMinutes = Math.floor(totalSeconds / 60);
  const seconds = Math.floor(totalSeconds % 60);
  if (durationMs < 3_600_000) {
    if (seconds === 0) return `${totalMinutes}m`;
    return `${totalMinutes}m ${seconds}s`;
  }

  const totalHours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (minutes === 0) return `${totalHours}h`;
  return `${totalHours}h ${minutes}m`;
}
