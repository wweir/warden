// Pure utilities for log record inspection and formatting.
// No Vue reactivity — safe to call from composables, components, or tests.

const previewCache = new WeakMap();
const parsedRequestCache = new WeakMap();

function truncate(s, n) {
	return s.length > n ? s.slice(0, n) + "..." : s;
}

function parseRequest(log) {
	if (!log || typeof log !== "object") return null;
	if (parsedRequestCache.has(log)) return parsedRequestCache.get(log);

	let req = log.request;
	if (!req) {
		parsedRequestCache.set(log, null);
		return null;
	}
	if (typeof req === "string") {
		try {
			req = JSON.parse(req);
		} catch {
			parsedRequestCache.set(log, null);
			return null;
		}
	}
	parsedRequestCache.set(log, req);
	return req;
}

function extractUserMessageText(message) {
	if (message == null) return "";
	if (typeof message === "string") return message;

	const content = message.content;
	if (typeof content === "string") return content;
	if (Array.isArray(content)) {
		const text = content
			.filter((part) => ["text", "input_text", "output_text"].includes(part?.type) && typeof part.text === "string")
			.map((part) => part.text)
			.join(" ");
		if (text) return text;
	}

	if (Array.isArray(message.input_text)) {
		const text = message.input_text.filter((p) => typeof p === "string").join(" ");
		if (text) return text;
	}
	if (typeof message.input_text === "string") return message.input_text;
	if (typeof message.text === "string") return message.text;
	return "";
}

function extractPreview(msg) {
	const c = msg.content;
	if (!c) return "";
	if (typeof c === "string") return truncate(c, 120);
	if (Array.isArray(c)) {
		const text = c
			.filter((part) => ["text", "input_text", "output_text"].includes(part?.type) && typeof part.text === "string")
			.map((part) => part.text)
			.join(" ");
		if (text) return truncate(text, 120);
		const types = [...new Set(c.map((p) => p.type))];
		return "[" + types.join(", ") + "]";
	}
	return "";
}

export function lastUserPreview(log) {
	if (!log || typeof log !== "object") return "";
	if (previewCache.has(log)) return previewCache.get(log);

	const req = parseRequest(log);
	if (!req) {
		previewCache.set(log, "");
		return "";
	}

	if (req.type === "provider_event") {
		const eventName = typeof req.event === "string" ? req.event : "event";
		const providerName = typeof req.provider === "string" ? req.provider : "";
		const preview = truncate([eventName, providerName].filter(Boolean).join(" · "), 40);
		previewCache.set(log, preview);
		return preview;
	}

	let lastMsg = null;
	if (Array.isArray(req.messages)) {
		const users = req.messages.filter((m) => {
			if (m.role !== "user") return false;
			if (Array.isArray(m.content) && m.content.length > 0 && m.content.every((b) => b.type === "tool_result")) return false;
			return true;
		});
		if (users.length) lastMsg = users[users.length - 1];
		if (!lastMsg && typeof req.system === "string") {
			const preview = truncate(req.system, 40);
			previewCache.set(log, preview);
			return preview;
		}
	} else if (req.input != null) {
		if (typeof req.input === "string") {
			const preview = truncate(req.input, 40);
			previewCache.set(log, preview);
			return preview;
		}
		if (Array.isArray(req.input)) {
			const users = req.input.filter((m) => m.role === "user" || typeof m === "string");
			if (users.length) lastMsg = users[users.length - 1];
		}
	}

	let preview = "";
	if (!lastMsg) {
		preview = "";
	} else if (typeof lastMsg === "string") {
		preview = truncate(lastMsg, 40);
	} else {
		preview = truncate(extractUserMessageText(lastMsg) || extractPreview(lastMsg), 40);
	}
	previewCache.set(log, preview);
	return preview;
}

export function failoverCount(log) {
	return Array.isArray(log?.failovers) ? log.failovers.length : 0;
}

export function isRecoveredByFailover(log) {
	return !log?.pending && !log?.error && failoverCount(log) > 0;
}

export function hasRejectedVerdict(log) {
	return Array.isArray(log?.tool_verdicts) && log.tool_verdicts.some((v) => v.rejected);
}

export function getTimestampMs(log) {
	if (!log || typeof log !== "object") return 0;
	const ts = new Date(log.timestamp).getTime();
	return Number.isFinite(ts) ? ts : 0;
}
