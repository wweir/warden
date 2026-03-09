export function fmtNum(n) {
	if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
	if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
	return String(n);
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
