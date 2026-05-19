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

const ROUTE_PROTOCOLS = new Set(["chat", "responses", "anthropic"]);

function defaultServiceProtocols(provider) {
  const family = normalizeText(provider?.family || provider?.format);
  switch (family) {
    case "anthropic": {
      const protocols = ["chat", "anthropic"];
      if (provider?.anthropic_to_responses) protocols.push("responses");
      return protocols;
    }
    case "openai": {
      const protocols = ["chat", "responses", "embeddings"];
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
  // Multi-endpoint: merge all endpoint protocols
  const endpoints = provider?.endpoints || provider?.endpoint;
  if (endpoints && Object.keys(endpoints).length > 0) {
    const all = new Set();
    for (const ep of Object.values(endpoints)) {
      if (ep?.protocols) {
        for (const p of ep.protocols) all.add(normalizeText(p));
      } else {
        const fmt = normalizeText(ep?.format);
        if (fmt === "openai") {
          all.add("chat");
          all.add("responses");
          all.add("embeddings");
        } else if (fmt === "anthropic") {
          all.add("chat");
          all.add("anthropic");
        } else if (fmt === "copilot") {
          all.add("chat");
        }
      }
    }
    const out = [];
    for (const p of all) {
      if (ROUTE_PROTOCOLS.has(p) && !out.includes(p)) out.push(p);
    }
    return out;
  }

  const rawProtocols = provider?.service_protocols || provider?.protocols;
  const configured = Array.isArray(rawProtocols) && rawProtocols.length > 0
    ? rawProtocols
    : defaultServiceProtocols(provider);
  const out = [];
  const seen = new Set();
  for (const raw of configured) {
    const protocol = normalizeText(raw);
    if (!ROUTE_PROTOCOLS.has(protocol) || seen.has(protocol)) continue;
    seen.add(protocol);
    out.push(protocol);
  }
  return out;
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
