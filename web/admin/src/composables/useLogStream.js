import { ref, nextTick, onMounted, onUnmounted, watch } from "vue";
import { createLogStream } from "../api.js";

const MAX_LOGS = 500;

export function useLogStream() {
	const logs = ref([]);
	const paused = ref(false);
	const error = ref("");

	const requestIndexMap = new Map();
	let pendingLogs = [];
	let flushFrame = 0;
	let autoScrollFrame = 0;
	let stopStream = null;

	function rebuildRequestIndex() {
		requestIndexMap.clear();
		for (let i = 0; i < logs.value.length; i++) {
			requestIndexMap.set(logs.value[i].request_id, i);
		}
	}

	function upsertLog(log) {
		const idx = requestIndexMap.get(log.request_id);
		if (idx >= 0) {
			logs.value[idx] = log;
		} else {
			requestIndexMap.set(log.request_id, logs.value.length);
			logs.value.push(log);
		}
		if (logs.value.length > MAX_LOGS) {
			logs.value = logs.value.slice(-MAX_LOGS);
			rebuildRequestIndex();
		}
	}

	function isNearBottom() {
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
		if (shouldStickToBottom) {
			nextTick(() => {
				scheduleAutoScroll();
			});
		}
	}

	function enqueueLog(log) {
		pendingLogs.push(log);
		if (flushFrame) return;
		flushFrame = requestAnimationFrame(() => {
			flushPendingLogs();
		});
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
	}

	onMounted(startStream);
	onUnmounted(stopStreamFn);

	return {
		logs,
		paused,
		error,
		togglePause,
		clearLogs,
	};
}
