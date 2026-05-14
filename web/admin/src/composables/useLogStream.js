import { ref, nextTick, onMounted, onUnmounted, watch } from "vue";
import { createLogStream } from "../api.js";

const MAX_LOGS = 500;

function textParts(value) {
	if (value == null) return "";
	if (typeof value === "string") return value;
	if (Array.isArray(value)) {
		return value.map((part) => {
			if (typeof part === "string") return part;
			if (!part || typeof part !== "object") return "";
			if (["text", "input_text", "output_text"].includes(part.type) && typeof part.text === "string") return part.text;
			if (typeof part.input_text === "string") return part.input_text;
			if (typeof part.output_text === "string") return part.output_text;
			if (part.type === "tool_result") return String(part.tool_use_id || "") + textParts(part.content);
			return "";
		}).filter(Boolean).join("");
	}
	if (typeof value === "object") {
		if (typeof value.text === "string") return value.text;
		if (typeof value.input_text === "string") return value.input_text;
		if (typeof value.output_text === "string") return value.output_text;
	}
	return "";
}

function conversationTextFromRequest(request) {
	if (!request || typeof request !== "object") return "";
	const parts = [];
	if (request.system) parts.push("system:" + textParts(request.system));
	if (Array.isArray(request.messages)) {
		for (const msg of request.messages) {
			if (!msg || typeof msg !== "object") continue;
			if (msg.role === "system") parts.push("system:" + textParts(msg.content));
			if (msg.role === "user") parts.push("turn:" + textParts(msg.content));
			if (msg.role === "assistant") {
				const toolCalls = Array.isArray(msg.tool_calls)
					? msg.tool_calls.map((tc) => (tc.function?.name || "") + (tc.function?.arguments || "")).join("")
					: "";
				parts.push("turn:" + toolCalls + textParts(msg.content));
			}
			if (msg.role === "tool" || msg.role === "function") {
				parts.push("turn:" + String(msg.tool_call_id || "") + textParts(msg.content));
			}
		}
	} else if (typeof request.input === "string") {
		parts.push("turn:" + request.input);
	} else if (Array.isArray(request.input)) {
		for (const item of request.input) {
			if (typeof item === "string") {
				parts.push("turn:" + item);
				continue;
			}
			if (!item || typeof item !== "object") continue;
			if (item.type === "message") {
				if (item.role === "system") parts.push("system:" + textParts(item.content));
				if (item.role === "user" || item.role === "assistant") parts.push("turn:" + textParts(item.content));
			}
			if (item.type === "function_call" || item.type === "custom_tool_call") {
				parts.push("turn:" + (item.name || item.tool_name || "") + String(item.arguments ?? item.input ?? ""));
			}
			if (item.type === "function_call_output" || item.type === "custom_tool_call_output") {
				parts.push("turn:" + String(item.call_id || "") + String(item.output ?? item.content ?? item.result ?? ""));
			}
		}
	}
	return parts.filter(Boolean).join("\x1f");
}

function continuesLog(current, previous) {
	if (!current?.request_id || !previous?.request_id || current.request_id === previous.request_id) return false;
	if ((current.route || "(unknown)") !== (previous.route || "(unknown)")) return false;
	const currentText = conversationTextFromRequest(current.request);
	const previousText = conversationTextFromRequest(previous.request);
	return Boolean(currentText && previousText && currentText.length > previousText.length && currentText.includes(previousText));
}

export function useLogStream() {
	const logs = ref([]);
	const paused = ref(false);
	const error = ref("");
	const autoScrollEnabled = ref(true);

	const requestIndexMap = new Map();
	let pendingLogs = [];
	let flushFrame = 0;
	let autoScrollFrame = 0;
	let stopStream = null;
	let lastUserScroll = 0;

	function onUserScroll() {
		lastUserScroll = Date.now();
	}

	function attachScrollListener() {
	window.addEventListener("scroll", onUserScroll, { passive: true });
}

function detachScrollListener() {
	window.removeEventListener("scroll", onUserScroll, { passive: true });
}

attachScrollListener();

	function rebuildRequestIndex() {
		requestIndexMap.clear();
		for (let i = 0; i < logs.value.length; i++) {
			requestIndexMap.set(logs.value[i].request_id, i);
		}
	}

	function findContinuationIndex(log) {
		for (let i = 0; i < logs.value.length; i++) {
			if (continuesLog(log, logs.value[i])) return i;
		}
		return -1;
	}

	function upsertLog(log) {
		const existingIdx = requestIndexMap.get(log.request_id);

		if (existingIdx >= 0) {
			// Same request_id: replace (pending -> final within one request).
			logs.value[existingIdx] = log;
			return;
		}

		const continuationIdx = findContinuationIndex(log);
		if (continuationIdx >= 0) {
			const oldRequestId = logs.value[continuationIdx].request_id;
			requestIndexMap.delete(oldRequestId);
			logs.value[continuationIdx] = log;
			requestIndexMap.set(log.request_id, continuationIdx);
			return;
		}

		// New record.
		const idx = logs.value.length;
		logs.value.push(log);
		requestIndexMap.set(log.request_id, idx);

		if (logs.value.length > MAX_LOGS) {
			logs.value = logs.value.slice(-MAX_LOGS);
			rebuildRequestIndex();
		}
	}

	function isNearBottom() {
		if (Date.now() - lastUserScroll < 3000) return false;
		return document.documentElement.scrollHeight - (window.scrollY + window.innerHeight) < 80;
	}

	function scheduleAutoScroll() {
		if (autoScrollFrame) return;
		autoScrollFrame = requestAnimationFrame(() => {
			autoScrollFrame = 0;
			window.scrollTo({ top: document.documentElement.scrollHeight, behavior: "auto" });
		});
	}

	function flushPendingLogs() {
		flushFrame = 0;
		if (paused.value || pendingLogs.length === 0) return;
		const shouldStickToBottom = isNearBottom();
		const batch = pendingLogs;
		pendingLogs = [];
		for (const log of batch) {
			upsertLog(log);
		}
		if (shouldStickToBottom && autoScrollEnabled.value) {
			nextTick(() => scheduleAutoScroll());
		}
	}

	function enqueueLog(log) {
		pendingLogs.push(log);
		if (flushFrame) return;
		flushFrame = requestAnimationFrame(() => flushPendingLogs());
	}

	function togglePause() {
		paused.value = !paused.value;
	}

	function clearLogs() {
		pendingLogs = [];
		logs.value = [];
		requestIndexMap.clear();
	}

	watch(paused, (value) => {
		if (!value && pendingLogs.length > 0) {
			flushPendingLogs();
		}
	});

	function startStream() {
		const stream = createLogStream();
		stopStream = stream.start(
			(data) => {
				if (paused.value) {
					pendingLogs.push(data);
					return;
				}
				enqueueLog(data);
			},
			(err) => {
				error.value = err.message;
			},
		);
	}

	function stopStreamFn() {
		if (stopStream) stopStream();
		if (flushFrame) cancelAnimationFrame(flushFrame);
		if (autoScrollFrame) cancelAnimationFrame(autoScrollFrame);
		detachScrollListener();
	}

	onMounted(startStream);
	onUnmounted(stopStreamFn);

	function setAutoScroll(enabled) {
		autoScrollEnabled.value = enabled;
	}

	return {
		logs,
		paused,
		error,
		togglePause,
		clearLogs,
		setAutoScroll,
	};
}
