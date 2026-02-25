<template>
	<div>
		<div class="header-row">
			<h2 class="page-title">Request Logs</h2>
			<button @click="togglePause" class="btn btn-secondary btn-sm">
				{{ paused ? "Resume" : "Pause" }}
			</button>
			<button @click="clear" class="btn btn-secondary btn-sm">Clear</button>
		</div>

		<div v-if="error" class="msg msg-error">{{ error }}</div>

		<div class="tabs" v-if="routeKeys.length">
			<button
				v-for="key in routeKeys"
				:key="key"
				class="tab"
				:class="{ active: activeTab === key }"
				@click="activeTab = key"
			>
				{{ key }}
				<span class="badge">{{ groupedLogs[key].length }}</span>
			</button>
		</div>

		<div class="table-wrap panel" ref="tableWrap">
			<table v-if="chainedLogs.length" class="data-table">
				<thead>
					<tr>
						<th class="th-toggle"></th>
						<th>Time</th>
						<th>Prompt</th>
						<th>Endpoint</th>
						<th>Model</th>
						<th>Provider</th>
						<th>Duration</th>
						<th>Tools</th>
						<th>Status</th>
					</tr>
				</thead>
				<tbody>
					<template v-for="chain in chainedLogs" :key="chain.id">
						<!-- single request: flat row -->
						<tr
							v-if="chain.logs.length === 1"
							:class="{ 'row-error': chain.logs[0].error, 'row-clickable': true }"
							@click="showDetail(chain.logs[0])"
						>
							<td></td>
							<td>{{ formatTime(chain.logs[0].timestamp) }}</td>
							<td class="cell-prompt">{{ lastUserPreview(chain.logs[0]) }}</td>
							<td>{{ chain.logs[0].endpoint }}</td>
							<td>{{ chain.logs[0].model }}</td>
							<td>{{ chain.logs[0].provider }}</td>
							<td>{{ chain.logs[0].duration_ms }}ms</td>
							<td>{{ chain.logs[0].steps?.length || "-" }}</td>
							<td>{{ chain.logs[0].error || "OK" }}</td>
						</tr>
						<!-- multi-request chain -->
						<template v-else>
							<tr
								class="row-chain-head row-clickable"
								:class="{ 'row-error': chain.logs.some((l) => l.error) }"
								@click="toggleChain(chain.id)"
							>
								<td class="cell-toggle">
									<span class="toggle-icon">{{
										expandedChains.has(chain.id) ? "▼" : "▶"
									}}</span>
								</td>
								<td>{{ formatTime(chain.logs[0].timestamp) }}</td>
								<td class="cell-prompt">{{ lastUserPreview(chain.logs[0]) }}</td>
								<td>{{ chain.logs[0].endpoint }}</td>
								<td>{{ chain.logs[0].model }}</td>
								<td>-</td>
								<td>{{ chainTotalDuration(chain) }}ms</td>
								<td>
									<span class="badge badge-chain"
										>{{ chain.logs.length }} reqs</span
									>
								</td>
								<td>{{ chainStatus(chain) }}</td>
							</tr>
							<template v-if="expandedChains.has(chain.id)">
								<tr
									v-for="(log, idx) in chain.logs"
									:key="log.request_id"
									class="row-chain-child row-clickable"
									:class="{ 'row-error': log.error }"
									@click.stop="showDetail(log)"
								>
									<td class="cell-chain-indent">
										<span
											class="chain-line"
											:class="{
												'chain-line-last': idx === chain.logs.length - 1,
											}"
										></span>
									</td>
									<td>{{ formatTime(log.timestamp) }}</td>
									<td class="cell-prompt">{{ lastUserPreview(log) }}</td>
									<td>{{ log.endpoint }}</td>
									<td>{{ log.model }}</td>
									<td>{{ log.provider }}</td>
									<td>{{ log.duration_ms }}ms</td>
									<td>{{ log.steps?.length || "-" }}</td>
									<td>{{ log.error || "OK" }}</td>
								</tr>
							</template>
						</template>
					</template>
				</tbody>
			</table>
			<div v-else class="empty-hint">No logs yet.</div>
		</div>

		<!-- Detail Modal -->
		<div v-if="selected" class="modal-overlay" @click.self="selected = null">
			<div class="modal">
				<div class="modal-header">
					<h3>Request Detail</h3>
					<button class="btn btn-secondary btn-sm" @click="selected = null">Close</button>
				</div>

				<div class="modal-body">
					<table class="meta-table">
						<tr>
							<td>Request ID</td>
							<td>
								<code>{{ selected.request_id }}</code>
							</td>
						</tr>
						<tr>
							<td>Route</td>
							<td>
								<code>{{ selected.route }}</code>
							</td>
						</tr>
						<tr>
							<td>Model</td>
							<td>{{ selected.model }}</td>
						</tr>
						<tr>
							<td>Provider</td>
							<td>{{ selected.provider }}</td>
						</tr>
						<tr>
							<td>Duration</td>
							<td>{{ selected.duration_ms }}ms</td>
						</tr>
					</table>

					<!-- Message Chain -->
					<div class="chain" v-if="messageChain.length">
						<div
							v-for="(msg, i) in messageChain"
							:key="i"
							class="chain-node"
							:class="{
								'chain-node-last':
									i === messageChain.length - 1 && !selected.steps?.length,
							}"
						>
							<div class="chain-dot" :class="'dot-' + msg.role"></div>
							<div class="chain-content">
								<div class="chain-label">{{ msg.role }}</div>
								<!-- text content preview -->
								<div v-if="msg.preview" class="chain-preview">
									{{ msg.preview }}
								</div>
								<!-- tool_calls from assistant -->
								<div v-if="msg.toolCalls?.length" class="chain-tools">
									<div
										v-for="(tc, j) in msg.toolCalls"
										:key="j"
										class="tool-chip"
									>
										<span class="tool-arrow">call</span>
										<code>{{ tc.function?.name || tc.name }}</code>
										<details>
											<summary>args</summary>
											<pre class="code-block">{{
												formatJSON(tc.function?.arguments || tc.arguments)
											}}</pre>
										</details>
									</div>
								</div>
								<!-- tool result name -->
								<div v-if="msg.role === 'tool'" class="chain-meta">
									tool_call_id: {{ msg.toolCallId }}
								</div>
								<!-- full content expandable -->
								<details v-if="msg.raw">
									<summary>raw</summary>
									<pre class="code-block">{{ formatJSON(msg.raw) }}</pre>
								</details>
							</div>
						</div>

						<!-- Gateway Steps -->
						<div
							v-for="(step, i) in selected.steps || []"
							:key="'s' + i"
							class="chain-node"
							:class="{ 'chain-node-last': i === selected.steps.length - 1 }"
						>
							<div class="chain-dot dot-step"></div>
							<div class="chain-content">
								<div class="chain-label">gateway step {{ step.iteration }}</div>
								<div v-if="step.tool_calls?.length" class="chain-tools">
									<div
										v-for="(tc, j) in step.tool_calls"
										:key="'c' + j"
										class="tool-chip"
									>
										<span class="tool-arrow">call</span>
										<code>{{ tc.name }}</code>
										<details>
											<summary>args</summary>
											<pre class="code-block">{{
												formatJSON(tc.arguments)
											}}</pre>
										</details>
									</div>
								</div>
								<div v-if="step.tool_results?.length" class="chain-tools">
									<div
										v-for="(tr, j) in step.tool_results"
										:key="'r' + j"
										class="tool-chip"
									>
										<span
											class="tool-arrow"
											:class="{ 'text-error': tr.is_error }"
											>{{ tr.is_error ? "fail" : "result" }}</span
										>
										<details>
											<summary>output</summary>
											<pre class="code-block">{{ tr.output }}</pre>
										</details>
									</div>
								</div>
							</div>
						</div>
					</div>

					<!-- Fallback: no messages parsed -->
					<div v-else class="chain">
						<div class="chain-node">
							<div class="chain-dot dot-user"></div>
							<div class="chain-content">
								<div class="chain-label">request</div>
								<details>
									<summary>body</summary>
									<pre class="code-block">{{ formatJSON(selected.request) }}</pre>
								</details>
							</div>
						</div>
					</div>

					<!-- Response -->
					<div
						class="response-block"
						:class="selected.error ? 'response-error' : 'response-ok'"
					>
						<span class="response-status">{{ selected.error || "OK" }}</span>

						<!-- streaming request: assembled text on the left, raw response on the right -->
						<div v-if="selected.stream && selected.response" class="response-dual">
							<div class="response-pane">
								<div class="pane-label">Assembled Content</div>
								<pre class="code-block code-block-assembled">{{
									assembledText
								}}</pre>
							</div>
							<div class="response-pane">
								<details>
									<summary>response body</summary>
									<pre class="code-block code-block-sse">{{
										formatResponseBody(selected)
									}}</pre>
								</details>
							</div>
						</div>

						<!-- non-streaming: single view -->
						<details v-else-if="selected.response">
							<summary>response body</summary>
							<pre class="code-block">{{ formatJSON(selected.response) }}</pre>
						</details>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from "vue";
import { createLogStream } from "../api.js";

const logs = ref([]);
const paused = ref(false);
const error = ref("");
const tableWrap = ref(null);
const selected = ref(null);
let stopStream = null;
const MAX_LOGS = 500;

const activeTab = ref("");

const groupedLogs = computed(() => {
	const map = {};
	for (const log of logs.value) {
		const key = log.route || "(unknown)";
		if (!map[key]) map[key] = [];
		map[key].push(log);
	}
	return map;
});

const routeKeys = computed(() => Object.keys(groupedLogs.value));

const activeLogs = computed(() => {
	if (activeTab.value && groupedLogs.value[activeTab.value]) {
		return groupedLogs.value[activeTab.value];
	}
	return logs.value;
});

function formatTime(t) {
	if (!t) return "";
	return new Date(t).toLocaleTimeString();
}

function formatJSON(data) {
	if (!data) return "";
	try {
		const obj = typeof data === "string" ? JSON.parse(data) : data;
		return JSON.stringify(obj, null, 2);
	} catch {
		return String(data);
	}
}

// extract assembled text content from a streaming response
function extractAssembledText(log) {
	if (!log) return "";
	// prefer the assembled response (backend sets this for streaming requests)
	let resp = log.response;
	if (!resp) return "";
	if (typeof resp === "string") {
		try {
			resp = JSON.parse(resp);
		} catch {
			// might be raw SSE text, try to parse SSE events directly
			return extractTextFromSSE(resp);
		}
	}
	// Chat Completions format: choices[0].message.content
	if (resp.choices && Array.isArray(resp.choices) && resp.choices.length > 0) {
		const msg = resp.choices[0].message || resp.choices[0].delta;
		if (msg) {
			const content = msg.content;
			if (typeof content === "string") return content;
			if (Array.isArray(content)) {
				return content
					.filter((p) => p.type === "text")
					.map((p) => p.text)
					.join("");
			}
		}
	}
	// Responses API format: output[].content[].text or output[].text
	if (resp.output && Array.isArray(resp.output)) {
		const parts = [];
		for (const item of resp.output) {
			if (typeof item === "string") {
				parts.push(item);
				continue;
			}
			if (item.type === "message" && Array.isArray(item.content)) {
				for (const c of item.content) {
					if (c.type === "output_text" && c.text) parts.push(c.text);
					else if (c.type === "text" && c.text) parts.push(c.text);
				}
			}
		}
		if (parts.length) return parts.join("\n");
	}
	return formatJSON(resp);
}

// parse SSE text and extract delta content
function extractTextFromSSE(text) {
	const lines = text.split("\n");
	const parts = [];
	for (const line of lines) {
		if (!line.startsWith("data: ")) continue;
		const data = line.slice(6);
		if (data === "[DONE]") continue;
		try {
			const chunk = JSON.parse(data);
			// Chat Completions streaming chunk
			if (chunk.choices?.[0]?.delta?.content) {
				parts.push(chunk.choices[0].delta.content);
			}
			// Responses API: response.completed event
			if (chunk.response?.output) {
				for (const item of chunk.response.output) {
					if (item.type === "message" && Array.isArray(item.content)) {
						for (const c of item.content) {
							if ((c.type === "output_text" || c.type === "text") && c.text) {
								parts.push(c.text);
							}
						}
					}
				}
			}
		} catch {
			// ignore parse errors
		}
	}
	return parts.join("");
}

// format the raw response body for display (prefer raw_response if available)
function formatResponseBody(log) {
	const raw = log.raw_response || log.response;
	if (!raw) return "";
	// try to unwrap JSON-encoded string (ensureJSON wraps non-JSON as string)
	if (typeof raw === "string") {
		try {
			const parsed = JSON.parse(raw);
			if (typeof parsed === "string") return parsed;
			return JSON.stringify(parsed, null, 2);
		} catch {
			return raw;
		}
	}
	return JSON.stringify(raw, null, 2);
}

function togglePause() {
	paused.value = !paused.value;
}

function clear() {
	logs.value = [];
}

function showDetail(log) {
	selected.value = log;
}

const assembledText = computed(() => {
	if (!selected.value || !selected.value.stream) return "";
	return extractAssembledText(selected.value);
});

const messageChain = computed(() => {
	if (!selected.value) return [];
	let req = selected.value.request;
	if (!req) return [];
	if (typeof req === "string") {
		try {
			req = JSON.parse(req);
		} catch {
			return [];
		}
	}
	const msgs = req.messages;
	if (!Array.isArray(msgs)) return [];

	return msgs.map((msg) => {
		const node = {
			role: msg.role,
			raw: msg,
			toolCalls: msg.tool_calls || null,
			toolCallId: msg.tool_call_id || "",
			preview: extractPreview(msg),
		};
		return node;
	});
});

function extractPreview(msg) {
	const c = msg.content;
	if (!c) return "";
	if (typeof c === "string") return truncate(c, 120);
	if (Array.isArray(c)) {
		const text = c
			.filter((p) => p.type === "text")
			.map((p) => p.text)
			.join(" ");
		if (text) return truncate(text, 120);
		const types = [...new Set(c.map((p) => p.type))];
		return "[" + types.join(", ") + "]";
	}
	return "";
}

function truncate(s, n) {
	return s.length > n ? s.slice(0, n) + "..." : s;
}

// --- conversation chain grouping ---

const CHAIN_TIME_GAP_MS = 10 * 60 * 1000; // 10 minutes

// djb2 string hash
function hashStr(s) {
	let h = 5381;
	for (let i = 0; i < s.length; i++) {
		h = ((h << 5) + h + s.charCodeAt(i)) | 0;
	}
	return h.toString(36);
}

function parseRequest(log) {
	let req = log.request;
	if (!req) return null;
	if (typeof req === "string") {
		try {
			req = JSON.parse(req);
		} catch {
			return null;
		}
	}
	return req;
}

// extract a short preview of the last user message in a request
function lastUserPreview(log) {
	const req = parseRequest(log);
	if (!req) return "";

	let lastMsg = null;
	if (Array.isArray(req.messages)) {
		const users = req.messages.filter((m) => m.role === "user");
		if (users.length) lastMsg = users[users.length - 1];
	} else if (req.input != null) {
		if (typeof req.input === "string") return truncate(req.input, 40);
		if (Array.isArray(req.input)) {
			const users = req.input.filter((m) => m.role === "user" || typeof m === "string");
			if (users.length) lastMsg = users[users.length - 1];
		}
	}

	if (!lastMsg) return "";
	if (typeof lastMsg === "string") return truncate(lastMsg, 40);
	return truncate(extractPreview(lastMsg), 40);
}

function hashContent(content) {
	return hashStr(typeof content === "string" ? content : JSON.stringify(content));
}

// extract hashes of all user messages in a request (ordered)
function extractUserHashes(log) {
	const req = parseRequest(log);
	if (!req) return [];

	// Chat Completions: request.messages
	if (Array.isArray(req.messages)) {
		return req.messages
			.filter((m) => m.role === "user" && m.content != null)
			.map((m) => hashContent(m.content));
	}

	// Responses API: request.input
	if (req.input != null) {
		if (typeof req.input === "string") return [hashStr(req.input)];
		if (Array.isArray(req.input)) {
			return req.input
				.filter((m) => m.role === "user" || typeof m === "string")
				.map((m) => hashContent(typeof m === "string" ? m : m.content || m));
		}
	}

	return [];
}

const expandedChains = ref(new Set());

function toggleChain(chainId) {
	if (expandedChains.value.has(chainId)) {
		expandedChains.value.delete(chainId);
	} else {
		expandedChains.value.add(chainId);
	}
	// trigger reactivity
	expandedChains.value = new Set(expandedChains.value);
}

// group activeLogs into conversation chains
// two requests belong to the same chain if:
//   1. time gap < threshold
//   2. the later request's user messages contain the earlier request's last user message
const chainedLogs = computed(() => {
	const items = activeLogs.value;
	if (!items.length) return [];

	const sorted = [...items].sort((a, b) => new Date(a.timestamp) - new Date(b.timestamp));

	// pre-compute user message hashes for each log
	const hashesMap = new Map();
	for (const log of sorted) {
		hashesMap.set(log, extractUserHashes(log));
	}

	const chains = []; // [{ id, logs, lastUserHash }]

	for (const log of sorted) {
		const ts = new Date(log.timestamp).getTime();
		const hashes = hashesMap.get(log);

		// try to find an existing chain whose last user message appears in this request
		let matched = false;
		for (let i = chains.length - 1; i >= 0; i--) {
			const chain = chains[i];
			const lastTs = new Date(chain.logs[chain.logs.length - 1].timestamp).getTime();
			if (ts - lastTs >= CHAIN_TIME_GAP_MS) continue;
			if (chain.lastUserHash && hashes.includes(chain.lastUserHash)) {
				chain.logs.push(log);
				// update chain's last user hash to this request's last user message
				if (hashes.length > 0) {
					chain.lastUserHash = hashes[hashes.length - 1];
				}
				matched = true;
				break;
			}
		}

		if (!matched) {
			chains.push({
				id: (log.request_id || "") + "_" + chains.length,
				logs: [log],
				lastUserHash: hashes.length > 0 ? hashes[hashes.length - 1] : null,
			});
		}
	}

	return chains;
});

function chainTotalDuration(chain) {
	return chain.logs.reduce((sum, l) => sum + (l.duration_ms || 0), 0);
}

function chainStatus(chain) {
	const errors = chain.logs.filter((l) => l.error);
	if (errors.length === 0) return "OK";
	if (errors.length === chain.logs.length) return "FAIL";
	return errors.length + "/" + chain.logs.length + " failed";
}

function scrollToBottom() {
	nextTick(() => {
		if (tableWrap.value) {
			tableWrap.value.scrollTop = tableWrap.value.scrollHeight;
		}
	});
}

onMounted(() => {
	const stream = createLogStream();
	stopStream = stream.start(
		(data) => {
			if (paused.value) return;
			logs.value.push(data);
			if (logs.value.length > MAX_LOGS) {
				logs.value = logs.value.slice(-MAX_LOGS);
			}
			if (!activeTab.value) {
				activeTab.value = data.route || "(unknown)";
			}
			scrollToBottom();
		},
		(err) => {
			error.value = err.message;
		},
	);
});

onUnmounted(() => {
	if (stopStream) stopStream();
});
</script>

<style scoped>
/* Header layout */
.header-row {
	display: flex;
	align-items: center;
	gap: 12px;
	margin-bottom: 16px;
}

/* Table container */
.table-wrap {
	max-height: 70vh;
	overflow-y: auto;
	border-radius: var(--radius);
}

.data-table th {
	position: sticky;
	top: 0;
	z-index: 1;
}

/* Route tabs */
.tabs {
	display: flex;
	gap: 4px;
	margin-bottom: 12px;
	flex-wrap: wrap;
}
.tab {
	display: inline-flex;
	align-items: center;
	gap: 6px;
	padding: 6px 14px;
	border: 1px solid var(--c-border-light);
	border-radius: var(--radius);
	background: var(--c-surface);
	cursor: pointer;
	font-size: 13px;
	font-weight: 500;
	color: var(--c-text-2);
	transition: all 0.15s;
}
.tab:hover {
	border-color: var(--c-primary);
	color: var(--c-primary);
}
.tab.active {
	background: var(--c-primary);
	border-color: var(--c-primary);
	color: #fff;
}
.tab.active .badge {
	background: rgba(255, 255, 255, 0.25);
}
.badge {
	font-size: 11px;
	font-weight: 600;
	padding: 1px 7px;
	border-radius: 10px;
	background: var(--c-border-light);
}
.empty-hint {
	padding: 24px;
	text-align: center;
	color: var(--c-text-3);
}

/* Row states */
.row-error {
	background: var(--c-danger-bg);
}
.row-clickable {
	cursor: pointer;
}
.row-clickable:hover {
	background: var(--c-primary-bg);
}
.row-error.row-clickable:hover {
	background: #fecaca;
}

/* Modal */
.modal-overlay {
	position: fixed;
	top: 0;
	left: 0;
	right: 0;
	bottom: 0;
	background: rgba(0, 0, 0, 0.4);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 100;
}
.modal {
	background: var(--c-surface);
	border-radius: var(--radius);
	width: 90%;
	max-width: 900px;
	max-height: 85vh;
	display: flex;
	flex-direction: column;
	box-shadow: var(--shadow-md);
}
.modal-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	padding: 16px 20px;
	border-bottom: 1px solid var(--c-border-light);
}
.modal-header h3 {
	margin: 0;
}
.modal-body {
	padding: 20px;
	overflow-y: auto;
}

/* Meta table */
.meta-table {
	width: 100%;
	margin-bottom: 16px;
	border-collapse: collapse;
}
.meta-table td {
	padding: 4px 12px 4px 0;
	font-size: 13px;
	border-bottom: 1px solid var(--c-border-light);
}
.meta-table td:first-child {
	font-weight: 600;
	white-space: nowrap;
	width: 100px;
	color: var(--c-text-3);
}

/* Details / summary */
details {
	margin-bottom: 12px;
}
summary {
	cursor: pointer;
	font-weight: 600;
	font-size: 13px;
	padding: 6px 0;
	user-select: none;
}
summary:hover {
	color: var(--c-primary);
}
/* Dependency chain */
.chain {
	position: relative;
	padding-left: 20px;
}
.chain-node {
	position: relative;
	padding: 0 0 20px 20px;
	border-left: 2px solid var(--c-border-light);
}
.chain-node-last {
	border-left-color: transparent;
}
.chain-dot {
	position: absolute;
	left: -7px;
	top: 2px;
	width: 12px;
	height: 12px;
	border-radius: 50%;
	border: 2px solid var(--c-surface);
	box-sizing: border-box;
}
.dot-request {
	background: var(--c-primary);
}
.dot-system {
	background: #94a3b8;
}
.dot-user {
	background: var(--c-primary);
}
.dot-assistant {
	background: #22c55e;
}
.dot-tool {
	background: #f59e0b;
}
.dot-step {
	background: var(--c-text-3);
}
.dot-ok {
	background: #22c55e;
}
.dot-error {
	background: #ef4444;
}

.chain-content {
	min-height: 24px;
}
.chain-label {
	font-weight: 600;
	font-size: 13px;
	margin-bottom: 4px;
}
.chain-meta {
	font-size: 12px;
	color: var(--c-text-3);
	margin-bottom: 6px;
}
.chain-preview {
	font-size: 12px;
	color: var(--c-text-2);
	margin-bottom: 4px;
	white-space: pre-wrap;
	word-break: break-word;
}
.text-ok {
	color: #22c55e;
}
.response-block {
	margin-top: 12px;
	padding: 10px 14px;
	border-radius: var(--radius);
	font-size: 13px;
}
.response-ok {
	background: #f0fdf4;
	border: 1px solid #bbf7d0;
}
.response-error {
	background: #fef2f2;
	border: 1px solid #fecaca;
}
.response-status {
	font-weight: 600;
}
.response-dual {
	display: flex;
	gap: 12px;
	margin-top: 8px;
}
.response-pane {
	flex: 1;
	min-width: 0;
}
.pane-label {
	font-weight: 600;
	font-size: 13px;
	margin-bottom: 6px;
}
.code-block-assembled {
	white-space: pre-wrap;
	word-break: break-word;
	max-height: 500px;
	overflow-y: auto;
}
.code-block-sse {
	white-space: pre-wrap;
	word-break: break-all;
	max-height: 400px;
	overflow-y: auto;
}
.chain-tools {
	display: flex;
	flex-direction: column;
	gap: 4px;
	margin: 4px 0;
}
.tool-chip {
	display: flex;
	align-items: baseline;
	gap: 6px;
	font-size: 13px;
}
.tool-chip details {
	margin: 0;
}
.tool-chip summary {
	font-weight: 400;
	font-size: 12px;
	color: var(--c-text-3);
	padding: 0;
	display: inline;
}
.tool-arrow {
	color: var(--c-text-3);
	font-family: monospace;
	flex-shrink: 0;
}

/* Chain grouping */
.th-toggle {
	width: 28px;
	min-width: 28px;
	max-width: 28px;
}
.cell-toggle {
	text-align: center;
	width: 28px;
}
.toggle-icon {
	font-size: 10px;
	color: var(--c-text-3);
	user-select: none;
}
.row-chain-head {
	border-left: 3px solid var(--c-primary);
	font-weight: 600;
}
.row-chain-head td:first-child {
	padding-left: 5px;
}
.badge-chain {
	background: var(--c-primary);
	color: #fff;
	font-size: 11px;
	font-weight: 600;
	padding: 1px 8px;
	border-radius: 10px;
}
.row-chain-child {
	background: var(--c-bg);
	border-left: 3px solid var(--c-border-light);
}
.row-chain-child:hover {
	background: var(--c-primary-bg);
}
.row-chain-child.row-error {
	background: var(--c-danger-bg);
	border-left-color: var(--c-danger);
}
.cell-chain-indent {
	position: relative;
	width: 28px;
	padding: 0 !important;
}
.chain-line {
	position: absolute;
	left: 13px;
	top: 0;
	bottom: 0;
	width: 2px;
	background: var(--c-border-light);
}
.chain-line::after {
	content: "";
	position: absolute;
	left: 0;
	top: 50%;
	width: 8px;
	height: 2px;
	background: var(--c-border-light);
}
.chain-line-last {
	bottom: 50%;
}
.cell-prompt {
	max-width: 200px;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	font-size: 12px;
	color: var(--c-text-2);
}

@media (max-width: 768px) {
	.header-row {
		flex-wrap: wrap;
		gap: 8px;
	}

	.table-wrap {
		overflow-x: auto;
		-webkit-overflow-scrolling: touch;
	}

	.modal {
		width: 100%;
		max-width: 100%;
		height: 100%;
		max-height: 100%;
		border-radius: 0;
	}

	.modal-body {
		padding: 14px;
	}

	.meta-table td:first-child {
		width: 80px;
	}

	.response-dual {
		flex-direction: column;
	}
}
</style>
