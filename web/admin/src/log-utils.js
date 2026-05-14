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

	const content = extractTextParts(message.content);
	if (content) return content;

	return extractTextParts(message.input_text) ||
		extractTextParts(message.output_text) ||
		extractTextParts(message.text);
}

function extractTextParts(value) {
	if (value == null) return "";
	if (typeof value === "string") return value;
	if (Array.isArray(value)) {
		return value
			.map((part) => {
				if (typeof part === "string") return part;
				if (!part || typeof part !== "object") return "";
				if (["text", "input_text", "output_text"].includes(part.type) && typeof part.text === "string") return part.text;
				if (typeof part.input_text === "string") return part.input_text;
				if (typeof part.output_text === "string") return part.output_text;
				return "";
			})
			.filter(Boolean)
			.join(" ");
	}
	if (typeof value === "object") {
		if (typeof value.text === "string") return value.text;
		if (typeof value.input_text === "string") return value.input_text;
		if (typeof value.output_text === "string") return value.output_text;
	}
	return "";
}

function extractPreview(msg) {
	const text = extractUserMessageText(msg);
	if (text) return truncate(text.replace(/\s+/g, " "), 120);
	if (Array.isArray(msg?.content)) {
		const types = [...new Set(msg.content.map((p) => p?.type).filter(Boolean))];
		if (types.length) return "[" + types.join(", ") + "]";
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
			const users = req.input.filter((m) => {
				if (typeof m === "string") return true;
				if (!m || typeof m !== "object") return false;
				if (m.role === "user") return true;
				return m.type === "message" && !m.role;
			});
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
