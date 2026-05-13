import { ref, nextTick, onMounted, onUnmounted, watch } from "vue";
import { createLogStream } from "../api.js";

const MAX_LOGS = 500;

function getSessionKey(log) {
	if (!log.fingerprint || typeof log.fingerprint !== "string" || log.fingerprint.length < 6) {
		return null;
	}
	const sysHash = log.fingerprint.slice(0, 6);
	return (log.route || "(unknown)") + "\0" + sysHash;
}

export function useLogStream() {
	const logs = ref([]);
	const paused = ref(false);
	const error = ref("");
	const autoScrollEnabled = ref(true);

	const requestIndexMap = new Map();
	const sessionIndexMap = new Map();
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

	function rebuildSessionIndex() {
		sessionIndexMap.clear();
		for (let i = 0; i < logs.value.length; i++) {
			const key = getSessionKey(logs.value[i]);
			if (key) sessionIndexMap.set(key, i);
		}
	}

	function upsertLog(log) {
		const existingIdx = requestIndexMap.get(log.request_id);

		if (existingIdx >= 0) {
			// Same request_id: replace (pending -> final within one request).
			logs.value[existingIdx] = log;
			return;
		}

		const sessionKey = getSessionKey(log);
		if (sessionKey) {
			const sessionIdx = sessionIndexMap.get(sessionKey);
			if (sessionIdx >= 0) {
				// Same session: overwrite the older request from the same conversation.
				const oldRequestId = logs.value[sessionIdx].request_id;
				requestIndexMap.delete(oldRequestId);
				logs.value[sessionIdx] = log;
				requestIndexMap.set(log.request_id, sessionIdx);
				sessionIndexMap.set(sessionKey, sessionIdx);
				return;
			}
		}

		// New record.
		const idx = logs.value.length;
		logs.value.push(log);
		requestIndexMap.set(log.request_id, idx);
		if (sessionKey) sessionIndexMap.set(sessionKey, idx);

		if (logs.value.length > MAX_LOGS) {
			logs.value = logs.value.slice(-MAX_LOGS);
			rebuildRequestIndex();
			rebuildSessionIndex();
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
		sessionIndexMap.clear();
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
