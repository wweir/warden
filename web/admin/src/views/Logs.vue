<template>
	<div>
		<div class="header-row">
			<h2 class="page-title">{{ $t('logs.title') }}</h2>
			<button @click="togglePause" class="btn btn-secondary btn-sm">
				{{ paused ? $t('logs.resume') : $t('logs.pause') }}
			</button>
			<button @click="clear" class="btn btn-secondary btn-sm">{{ $t('logs.clear') }}</button>
		</div>

		<div v-if="error" class="msg msg-error">{{ error }}</div>

		<div class="tabs" v-if="routeKeys.length">
			<button
				class="tab"
				:class="{ active: activeTab === '' }"
				@click="activeTab = ''"
			>
				{{ $t('logs.all') }}
				<span class="badge">{{ chainedLogs.length }}</span>
			</button>
			<button
				v-for="key in routeKeys"
				:key="key"
				class="tab"
				:class="{ active: activeTab === key }"
				@click="activeTab = key"
			>
				{{ key }}
				<span class="badge">{{ chainsByRoute[key] || 0 }}</span>
			</button>
		</div>

		<div class="sessions" v-if="sessionChips.length">
			<span class="sessions-label">{{ $t('logs.sessions') }}</span>
			<button
				v-for="chip in sessionChips"
				:key="chip.id"
				class="tab session-chip"
				:class="{ active: activeSession === chip.id }"
				@click="activeSession = activeSession === chip.id ? null : chip.id"
			>
				{{ sessionName(chip) }}
				<span class="badge">{{ chip.logs.length }} {{ $t('logs.reqs') }}</span>
			</button>
		</div>

		<div class="table-wrap panel" ref="tableWrap">
			<table v-if="logs.length" class="data-table">
				<thead>
					<tr>
						<th class="th-toggle"></th>
						<th>
							<div class="th-col">
								<span>{{ $t('logs.time') }}</span>
							</div>
						</th>
						<th>
							<div class="th-col">
								<span>{{ $t('logs.prompt') }}</span>
								<input
									v-model="filters.prompt"
									class="col-filter"
									:class="{ active: filters.prompt }"
									:placeholder="$t('common.filter')"
									@click.stop
								/>
							</div>
						</th>
						<th>
							<div class="th-col">
								<span>{{ $t('logs.model') }}</span>
								<input
									v-model="filters.model"
									class="col-filter"
									:class="{ active: filters.model }"
									:placeholder="$t('common.filter')"
									@click.stop
								/>
							</div>
						</th>
						<th>
							<div class="th-col">
								<span>{{ $t('logs.provider') }}</span>
								<input
									v-model="filters.provider"
									class="col-filter"
									:class="{ active: filters.provider }"
									:placeholder="$t('common.filter')"
									@click.stop
								/>
							</div>
						</th>
						<th>
							<div class="th-col">
								<span>{{ $t('logs.duration') }}</span>
							</div>
						</th>
						<th>
							<div class="th-col">
								<span>{{ $t('logs.status') }}</span>
								<input
									v-model="filters.status"
									class="col-filter"
									:class="{ active: filters.status }"
									:placeholder="$t('common.filter')"
									@click.stop
								/>
							</div>
						</th>
					</tr>
				</thead>
				<tbody>
					<template v-for="chain in filteredChains" :key="chain.id">
						<!-- single request: flat row -->
						<tr
							v-if="chain.displayLogs.length === 1"
							:class="{ 'row-error': chain.displayLogs[0].error, 'row-clickable': true }"
							@click="showDetail(chain.displayLogs[0])"
						>
							<td></td>
							<td>{{ formatTime(chain.displayLogs[0].timestamp) }}</td>
							<td class="cell-prompt">{{ lastUserPreview(chain.displayLogs[0]) }}</td>
							<td>{{ chain.displayLogs[0].model }}</td>
							<td>{{ chain.displayLogs[0].provider }}</td>
							<td>{{ chain.displayLogs[0].duration_ms }}ms</td>
							<td>{{ statusText(chain.displayLogs[0]) }}</td>
						</tr>
						<!-- multi-request chain -->
						<template v-else>
							<tr
								class="row-chain-head row-clickable"
								:class="{ 'row-error': chain.displayLogs.some((l) => l.error) }"
								@click="toggleChain(chain.id)"
							>
								<td class="cell-toggle">
									<span class="toggle-icon">{{
										expandedChains.has(chain.id) ? "▼" : "▶"
									}}</span>
								</td>
								<td>{{ formatTime(chain.displayLogs[0].timestamp) }}</td>
								<td class="cell-prompt">{{ lastUserPreview(chain.displayLogs[0]) }}</td>
								<td>{{ chain.displayLogs[0].model }}</td>
								<td>-</td>
								<td>{{ chainTotalDuration(chain) }}ms</td>
								<td>
									<span class="badge badge-chain"
										>{{ chain.displayLogs.length }} {{ $t('logs.reqs') }}</span
									>
									{{ chainStatus(chain) !== "OK" ? " · " + chainStatus(chain) : "" }}
								</td>
							</tr>
							<template v-if="expandedChains.has(chain.id)">
								<tr
									v-for="(log, idx) in chain.displayLogs"
									:key="log.request_id"
									class="row-chain-child row-clickable"
									:class="{ 'row-error': log.error }"
									@click.stop="showDetail(log)"
								>
									<td class="cell-chain-indent">
										<span
											class="chain-line"
											:class="{
												'chain-line-last': idx === chain.displayLogs.length - 1,
											}"
										></span>
									</td>
									<td>{{ formatTime(log.timestamp) }}</td>
									<td class="cell-prompt">{{ lastUserPreview(log) }}</td>
									<td>{{ log.model }}</td>
									<td>{{ log.provider }}</td>
									<td>{{ log.duration_ms }}ms</td>
									<td>{{ statusText(log) }}</td>
								</tr>
							</template>
						</template>
					</template>
				<tr v-if="!filteredChains.length">
					<td colspan="7" class="empty-hint">{{ hasFilters ? $t('logs.noMatchingLogs') : $t('logs.noLogsYet') }}</td>
				</tr>
			</tbody>
			</table>
		</div>

		<!-- Detail Modal -->
		<div v-if="selected" class="modal-overlay" @click.self="selected = null">
			<div class="modal">
				<div class="modal-header">
					<h3>{{ $t('logs.requestDetail') }}</h3>
					<div class="modal-header-actions">
						<div class="view-toggle">
							<button class="btn btn-sm" :class="detailView === 'timeline' ? 'btn-primary' : 'btn-secondary'" @click="detailView = 'timeline'">{{ $t('logs.timeline') }}</button>
							<button class="btn btn-sm" :class="detailView === 'json' ? 'btn-primary' : 'btn-secondary'" @click="detailView = 'json'">{{ $t('logs.json') }}</button>
						</div>
						<button class="btn btn-secondary btn-sm" @click="selected = null">{{ $t('common.close') }}</button>
					</div>
				</div>

				<div class="modal-body">
					<!-- === JSON View === -->
					<div v-if="detailView === 'json'">
						<pre class="code-block code-block-json">{{ formatJSON(selected) }}</pre>
					</div>

					<!-- === Timeline View === -->
					<div v-else>
						<table class="meta-table">
							<tr><td>{{ $t('logs.requestId') }}</td><td><code>{{ selected.request_id }}</code></td></tr>
							<tr><td>{{ $t('logs.route') }}</td><td><code>{{ selected.route }}</code></td></tr>
							<tr><td>{{ $t('logs.model') }}</td><td>{{ selected.model }}</td></tr>
							<tr><td>{{ $t('logs.provider') }}</td><td>{{ selected.provider }}</td></tr>
							<tr><td>{{ $t('logs.duration') }}</td><td>{{ selected.duration_ms }}ms</td></tr>
							<tr v-if="selected.fingerprint"><td>{{ $t('logs.session') }}</td><td><code class="fp-str">{{ selected.fingerprint }}</code></td></tr>
						</table>

						<!-- Timeline nodes -->
						<div class="chain" v-if="timelineNodes.length">
							<div
								v-for="(node, i) in timelineNodes"
								:key="i"
								class="chain-node"
								:class="{ 'chain-node-last': i === timelineNodes.length - 1 }"
							>
								<div class="chain-dot" :class="'dot-' + node.dotType"></div>
								<div class="chain-content">
									<div class="chain-label">{{ node.label }}</div>

									<!-- text preview (system gets single-line truncated style) -->
									<div
										v-if="node.preview"
										class="chain-preview"
										:class="{ 'chain-preview-oneline': node.dotType === 'system' }"
									>{{ node.preview }}</div>

									<!-- tool call + result pair -->
									<div v-if="node.type === 'tool-pair'" class="tool-pair-block">
										<div class="tool-chip">
											<span class="tool-arrow">call</span>
											<code>{{ node.toolName }}</code>
										</div>
										<details class="tool-pair-details" :open="node.defaultOpen || undefined">
											<summary>arguments</summary>
											<pre class="code-block">{{ formatJSON(node.toolArgs) }}</pre>
										</details>
										<div class="tool-chip" v-if="node.toolResult !== undefined">
											<span class="tool-arrow" :class="{ 'text-error': node.toolError }">{{ node.toolError ? 'fail' : 'result' }}</span>
										</div>
										<details v-if="node.toolResult !== undefined" class="tool-pair-details" :open="node.defaultOpen || undefined">
											<summary>output</summary>
											<pre class="code-block code-block-raw">{{ renderEscapes(node.toolResult) }}</pre>
										</details>
									</div>

									<!-- tool_calls from assistant (unpaired) -->
									<div v-if="node.toolCalls?.length" class="chain-tools">
										<div v-for="(tc, j) in node.toolCalls" :key="j" class="tool-chip">
											<span class="tool-arrow">call</span>
											<code>{{ tc.function?.name || tc.name }}</code>
											<details :open="node.defaultOpen || undefined">
												<summary>args</summary>
												<pre class="code-block">{{ formatJSON(tc.function?.arguments || tc.arguments) }}</pre>
											</details>
										</div>
									</div>

									<!-- expandable raw content -->
									<details v-if="node.raw" :open="node.defaultOpen || undefined">
										<summary>raw</summary>
										<pre class="code-block code-block-raw">{{ renderEscapes(typeof node.raw === 'string' ? node.raw : formatJSON(node.raw)) }}</pre>
									</details>
								</div>
							</div>
						</div>

						<!-- Fallback: no messages parsed -->
						<div v-else class="chain">
							<div class="chain-node">
								<div class="chain-dot dot-user"></div>
								<div class="chain-content">
									<div class="chain-label">{{ $t('logs.request') }}</div>
									<details open>
										<summary>body</summary>
										<pre class="code-block code-block-raw">{{ renderEscapes(formatJSON(selected.request)) }}</pre>
									</details>
								</div>
							</div>
						</div>

						<!-- Response -->
						<div class="response-block" :class="selected.error ? 'response-error' : 'response-ok'">
							<span class="response-status">{{ selected.error || "OK" }}</span>
							<div v-if="selected.response" class="response-pane">
								<div class="pane-label">{{ $t('logs.response') }}</div>
								<!-- tool_use blocks from Anthropic response -->
								<div v-if="responseToolCalls.length" class="chain-tools" style="margin-bottom:8px">
									<div v-for="(tc, j) in responseToolCalls" :key="j" class="tool-chip">
										<span class="tool-arrow">call</span>
										<code>{{ tc.name }}</code>
										<details>
											<summary>args</summary>
											<pre class="code-block">{{ formatJSON(tc.input) }}</pre>
										</details>
									</div>
								</div>
								<!-- assembled text content -->
								<details v-if="responseHasText" :open="true">
									<summary>{{ $t('logs.content') }}</summary>
									<pre class="code-block code-block-assembled">{{ assembledText }}</pre>
								</details>
								<!-- raw JSON fallback -->
								<details v-else :open="true">
									<summary>{{ $t('logs.content') }}</summary>
									<pre class="code-block code-block-raw">{{ renderEscapes(formatJSON(selected.response)) }}</pre>
								</details>
							</div>
						</div>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from "vue";
import { createLogStream } from "../api.js";

const logs = ref([]);
const paused = ref(false);
const error = ref("");
const tableWrap = ref(null);
const selected = ref(null);
const detailView = ref("timeline"); // "timeline" | "json"
const filters = ref({ prompt: "", model: "", provider: "", status: "" });
let stopStream = null;
const MAX_LOGS = 500;

const activeTab = ref("");
const activeSession = ref(null);

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
	// Anthropic format: content[] with text/tool_use blocks
	if (resp.content && Array.isArray(resp.content)) {
		const textParts = resp.content.filter((b) => b.type === "text").map((b) => b.text);
		if (textParts.length) return textParts.join("");
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
			// Anthropic streaming: content_block_delta with text_delta
			if (chunk.type === "content_block_delta" && chunk.delta?.type === "text_delta" && chunk.delta?.text) {
				parts.push(chunk.delta.text);
			}
		} catch {
			// ignore parse errors
		}
	}
	return parts.join("");
}

function togglePause() {
	paused.value = !paused.value;
}

function clear() {
	logs.value = [];
}

function showDetail(log) {
	selected.value = log;
	detailView.value = log.error ? "json" : "timeline";
}

const assembledText = computed(() => {
	if (!selected.value) return "";
	return extractAssembledText(selected.value);
});

// check if response has structured content worth rendering (not just raw JSON)
const responseHasText = computed(() => {
	if (!selected.value?.response) return false;
	return assembledText.value !== formatJSON(selected.value.response);
});

// extract tool_use blocks from Anthropic response for timeline display
const responseToolCalls = computed(() => {
	if (!selected.value?.response) return [];
	let resp = selected.value.response;
	if (typeof resp === "string") {
		try { resp = JSON.parse(resp); } catch { return []; }
	}
	if (!resp.content || !Array.isArray(resp.content)) return [];
	return resp.content.filter((b) => b.type === "tool_use");
});

// normalize a raw message (any protocol) into a unified node structure
function normalizeMsg(msg) {
	// system messages get a short single-line preview
	const preview = msg.role === "system"
		? truncate((typeof msg.content === "string" ? msg.content : "").replace(/\s+/g, " "), 60)
		: extractPreview(msg);
	return {
		role: msg.role,
		raw: msg,
		toolCalls: msg.tool_calls || null,
		toolCallId: msg.tool_call_id || "",
		preview,
	};
}

// extract messages from Anthropic format request into unified nodes
// Anthropic: { system, messages: [{role, content: string|block[]}] }
// content blocks: {type:"text"|"tool_use"|"tool_result", ...}
function parseAnthropicMessages(req) {
	const nodes = [];
	// system field → synthetic system node (short preview, content in raw details)
	if (req.system) {
		nodes.push({
			role: "system",
			raw: { role: "system", content: req.system },
			toolCalls: null,
			toolCallId: "",
			preview: truncate(req.system.replace(/\s+/g, " "), 60),
		});
	}
	if (!Array.isArray(req.messages)) return nodes;

	for (const msg of req.messages) {
		const content = msg.content;
		// simple string content
		if (typeof content === "string" || !Array.isArray(content)) {
			nodes.push(normalizeMsg(msg));
			continue;
		}
		// content is an array of blocks
		// check if it contains tool_use or tool_result blocks
		const toolUseBlocks = content.filter((b) => b.type === "tool_use");
		const toolResultBlocks = content.filter((b) => b.type === "tool_result");
		const textBlocks = content.filter((b) => b.type === "text");

		if (toolUseBlocks.length > 0) {
			// assistant message with tool_use blocks → convert to tool_calls format
			const textPreview = textBlocks.map((b) => b.text).join(" ");
			const syntheticToolCalls = toolUseBlocks.map((b) => ({
				id: b.id,
				type: "function",
				function: {
					name: b.name,
					arguments: typeof b.input === "string" ? b.input : JSON.stringify(b.input),
				},
			}));
			nodes.push({
				role: "assistant",
				raw: msg,
				toolCalls: syntheticToolCalls,
				toolCallId: "",
				preview: textPreview ? truncate(textPreview, 120) : "",
			});
		} else if (toolResultBlocks.length > 0) {
			// user message with tool_result blocks → emit as tool messages
			for (const b of toolResultBlocks) {
				const resultContent = Array.isArray(b.content)
					? b.content.filter((c) => c.type === "text").map((c) => c.text).join("")
					: (b.content || "");
				nodes.push({
					role: "tool",
					raw: { role: "tool", tool_call_id: b.tool_use_id, content: resultContent },
					toolCalls: null,
					toolCallId: b.tool_use_id || "",
					preview: truncate(resultContent, 120),
				});
			}
		} else {
			nodes.push(normalizeMsg(msg));
		}
	}
	return nodes;
}

// extract messages from Responses API format request into unified nodes
// Responses API: { input: string | array }
function parseResponsesMessages(req) {
	const nodes = [];
	const input = req.input;
	if (!input) return nodes;
	if (typeof input === "string") {
		nodes.push({
			role: "user",
			raw: { role: "user", content: input },
			toolCalls: null,
			toolCallId: "",
			preview: truncate(input, 120),
		});
		return nodes;
	}
	if (!Array.isArray(input)) return nodes;

	for (const item of input) {
		if (typeof item === "string") {
			nodes.push({
				role: "user",
				raw: { role: "user", content: item },
				toolCalls: null,
				toolCallId: "",
				preview: truncate(item, 120),
			});
			continue;
		}
		const role = item.role || item.type || "user";
		if (item.type === "function_call") {
			// tool call from assistant
			nodes.push({
				role: "assistant",
				raw: item,
				toolCalls: [{
					id: item.call_id,
					type: "function",
					function: { name: item.name, arguments: typeof item.arguments === "string" ? item.arguments : JSON.stringify(item.arguments) },
				}],
				toolCallId: "",
				preview: "",
			});
		} else if (item.type === "function_call_output") {
			nodes.push({
				role: "tool",
				raw: item,
				toolCalls: null,
				toolCallId: item.call_id || "",
				preview: truncate(typeof item.output === "string" ? item.output : JSON.stringify(item.output), 120),
			});
		} else if (item.type === "message" && Array.isArray(item.content)) {
			// nested message object
			const textPreview = item.content.filter((c) => c.type === "text" || c.type === "output_text").map((c) => c.text).join(" ");
			nodes.push({
				role: item.role || "user",
				raw: item,
				toolCalls: null,
				toolCallId: "",
				preview: truncate(textPreview, 120),
			});
		} else {
			nodes.push({
				role,
				raw: item,
				toolCalls: null,
				toolCallId: "",
				preview: extractPreview(item),
			});
		}
	}
	return nodes;
}

// detect request protocol format
function detectRequestFormat(req) {
	if (!req) return "unknown";
	// Anthropic: has "system" string field OR messages with content blocks containing tool_use/tool_result
	if (typeof req.system === "string") return "anthropic";
	if (Array.isArray(req.messages)) {
		for (const m of req.messages) {
			if (Array.isArray(m.content) && m.content.some((b) => b.type === "tool_use" || b.type === "tool_result")) {
				return "anthropic";
			}
		}
		return "openai-chat";
	}
	if (req.input !== undefined) return "responses";
	return "unknown";
}

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

	const fmt = detectRequestFormat(req);
	if (fmt === "anthropic") return parseAnthropicMessages(req);
	if (fmt === "responses") return parseResponsesMessages(req);

	// openai-chat: standard messages array
	const msgs = req.messages;
	if (!Array.isArray(msgs)) return [];
	return msgs.map(normalizeMsg);
});

function extractPreview(msg) {
	const c = msg.content;
	if (!c) return "";
	if (typeof c === "string") return truncate(c, 120);
	if (Array.isArray(c)) {
		// support both OpenAI content parts (type:"text") and Anthropic blocks (type:"text"|"tool_use"|"tool_result")
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

// render escaped characters (\n, \t, etc.) in strings
function renderEscapes(s) {
	if (typeof s !== "string") return String(s);
	return s.replace(/\\n/g, "\n").replace(/\\t/g, "\t").replace(/\\r/g, "\r");
}

// build timeline nodes: pair tool calls with tool results, mark last request/response as defaultOpen
const timelineNodes = computed(() => {
	if (!selected.value) return [];
	const chain = messageChain.value;
	if (!chain.length) return [];

	// build a map of tool_call_id -> tool message for pairing
	const toolResultMap = new Map();
	for (const msg of chain) {
		if (msg.role === "tool" && msg.toolCallId) {
			toolResultMap.set(msg.toolCallId, msg);
		}
	}

	const nodes = [];
	const pairedToolIds = new Set();

	// find the last real user message (skip tool/system roles)
	let lastUserIdx = -1;
	for (let i = chain.length - 1; i >= 0; i--) {
		if (chain[i].role === "user") { lastUserIdx = i; break; }
	}

	for (let i = 0; i < chain.length; i++) {
		const msg = chain[i];

		// skip tool messages that are already paired
		if (msg.role === "tool" && pairedToolIds.has(msg.toolCallId)) continue;

		// only open the last user message; assistant/tool nodes stay collapsed
		const isLastSection = i === lastUserIdx;

		if (msg.role === "assistant" && msg.toolCalls?.length) {
			// assistant with tool_calls: emit text node if has preview, then emit paired tool nodes
			if (msg.preview) {
				nodes.push({
					type: "message", dotType: "assistant", label: "assistant",
					preview: msg.preview, raw: msg.raw, defaultOpen: isLastSection,
				});
			}
			for (const tc of msg.toolCalls) {
				const callId = tc.id || tc.tool_call_id;
				const result = callId ? toolResultMap.get(callId) : null;
				if (result) pairedToolIds.add(callId);
				nodes.push({
					type: "tool-pair", dotType: "tool", label: (tc.function?.name || tc.name || "tool"),
					toolName: tc.function?.name || tc.name,
					toolArgs: tc.function?.arguments || tc.arguments,
					toolResult: result ? (result.raw?.content ?? result.preview ?? "") : undefined,
					toolError: result?.raw?.is_error || false,
					defaultOpen: isLastSection,
				});
			}
			// if assistant also has unpaired tool_calls info in raw, skip re-rendering
			continue;
		}

		// regular message node
		nodes.push({
			type: "message", dotType: msg.role, label: msg.role,
			preview: msg.preview, raw: msg.raw, defaultOpen: isLastSection,
		});
	}

	// append gateway steps
	for (const step of selected.value.steps || []) {
		const stepNode = {
			type: "step", dotType: "step", label: "gateway step " + step.iteration,
		};
		nodes.push(stepNode);
		if (step.tool_calls?.length) {
			for (const tc of step.tool_calls) {
				const tr = step.tool_results?.find((r) => r.tool_call_id === tc.id);
				nodes.push({
					type: "tool-pair", dotType: "tool", label: tc.name || "tool",
					toolName: tc.name, toolArgs: tc.arguments,
					toolResult: tr ? tr.output : undefined,
					toolError: tr?.is_error || false,
				});
			}
		}
	}

	return nodes;
});

// --- conversation chain grouping ---

const CHAIN_TIME_GAP_MS = 10 * 60 * 1000; // 10 minutes fallback for logs without fingerprint

// djb2 string hash (kept for fallback path)
function hashStr(s) {
	let h = 5381;
	for (let i = 0; i < s.length; i++) {
		h = ((h << 5) + h + s.charCodeAt(i)) | 0;
	}
	return h.toString(36);
}

// parse fingerprint string "{sys_hash}{fsm}" into { sysHash, fsm }
// sysHash = 6-hex-char hash of system prompt
// fsm = variable-length hashes: first 6, then 5, 4, 3, minimum 2 chars each
// Returns array of hash strings for FSM prefix matching
function parseFingerprint(fp) {
	if (!fp || typeof fp !== "string" || fp.length < 6) return null;
	const sysHash = fp.slice(0, 6);
	const fsmStr = fp.slice(6);
	if (!fsmStr) return { sysHash, fsm: [] };

	// Parse fsm with decreasing lengths: 6, 5, 4, 3, 2, 2, 2...
	const fsm = [];
	let pos = 0;
	let width = 6;
	while (pos < fsmStr.length) {
		if (fsm.length > 0) {
			width = Math.max(2, 6 - fsm.length);
		}
		const end = pos + width;
		if (end > fsmStr.length) break;
		fsm.push(fsmStr.slice(pos, end));
		pos = end;
	}
	return { sysHash, fsm };
}

// check whether fsm_a is a strict prefix of fsm_b (fsm_b extends fsm_a by ≥1 turn)
function isFSMPrefix(fsm_a, fsm_b) {
	if (fsm_a.length === 0 || fsm_b.length <= fsm_a.length) return false;
	for (let i = 0; i < fsm_a.length; i++) {
		if (fsm_a[i] !== fsm_b[i]) return false;
	}
	return true;
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
		// OpenAI chat or Anthropic format: both use messages array
		// For Anthropic, filter out tool_result-only user messages (they're not real user turns)
		const users = req.messages.filter((m) => {
			if (m.role !== "user") return false;
			// skip pure tool_result user messages (Anthropic format)
			if (Array.isArray(m.content) && m.content.length > 0 &&
				m.content.every((b) => b.type === "tool_result")) return false;
			return true;
		});
		if (users.length) lastMsg = users[users.length - 1];
		// fallback: if no user messages found, use system field for Anthropic
		if (!lastMsg && typeof req.system === "string") return truncate(req.system, 40);
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

// group all logs into conversation chains.
// If fingerprint is present, use FSM prefix matching for session continuity.
// Falls back to legacy user-message hash + time-gap heuristic for logs without fingerprint.
const chainedLogs = computed(() => {
	const items = logs.value;
	if (!items.length) return [];

	const sorted = [...items].sort((a, b) => new Date(a.timestamp) - new Date(b.timestamp));

	// pre-compute legacy user message hashes for fallback path
	const hashesMap = new Map();
	for (const log of sorted) {
		hashesMap.set(log, extractUserHashes(log));
	}

	// chains: [{ id, logs, lastFP, lastUserHash }]
	// lastFP: fingerprint of the last log in the chain (for FSM prefix matching)
	// lastUserHash: last user message hash (legacy fallback)
	const chains = [];

	for (const log of sorted) {
		const parsed = parseFingerprint(log.fingerprint);
		const ts = new Date(log.timestamp).getTime();
		let matched = false;

		if (parsed && parsed.fsm.length > 0) {
			// fingerprint path: match by (model + sys_hash, FSM prefix)
			for (let i = chains.length - 1; i >= 0; i--) {
				const chain = chains[i];
				const lastParsed = chain.lastParsed;
				if (!lastParsed) continue;
				if (chain.lastModel !== log.model) continue;
				if (lastParsed.sysHash !== parsed.sysHash) continue;
				if (isFSMPrefix(lastParsed.fsm, parsed.fsm)) {
					chain.logs.push(log);
					chain.lastParsed = parsed;
					matched = true;
					break;
				}
			}
		}

		if (!matched) {
			// fallback: legacy hash + time-gap heuristic
			const hashes = hashesMap.get(log);
			for (let i = chains.length - 1; i >= 0; i--) {
				const chain = chains[i];
				if (chain.lastParsed) continue; // skip fingerprint chains
				const lastTs = new Date(chain.logs[chain.logs.length - 1].timestamp).getTime();
				if (ts - lastTs >= CHAIN_TIME_GAP_MS) continue;
				if (chain.lastUserHash && hashes.includes(chain.lastUserHash)) {
					chain.logs.push(log);
					if (hashes.length > 0) chain.lastUserHash = hashes[hashes.length - 1];
					matched = true;
					break;
				}
			}
		}

		if (!matched) {
			const hashes = hashesMap.get(log);
			chains.push({
				id: (log.request_id || "") + "_" + chains.length,
				logs: [log],
				lastModel: log.model,
				lastParsed: parsed && parsed.fsm.length > 0 ? parsed : null,
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

// single-log status text: includes step count if any
function statusText(log) {
	if (log.error) return log.error;
	const steps = log.steps?.length;
	return steps ? "OK · " + steps + " steps" : "OK";
}

// substring match (case-insensitive)
function colMatch(query, ...fields) {
	if (!query) return true;
	const q = query.toLowerCase();
	return fields.some((f) => (f || "").toLowerCase().includes(q));
}

const hasFilters = computed(() =>
	Object.values(filters.value).some((v) => v.trim()),
);

// count chains per route tab
const chainsByRoute = computed(() => {
	const map = {};
	for (const chain of chainedLogs.value) {
		const key = chain.logs[0]?.route || "(unknown)";
		map[key] = (map[key] || 0) + 1;
	}
	return map;
});

// session chips: multi-log chains, filtered by active route tab
const sessionChips = computed(() => {
	const tab = activeTab.value;
	return chainedLogs.value.filter((chain) => {
		if (chain.logs.length < 2) return false;
		if (tab && (chain.logs[0]?.route || "(unknown)") !== tab) return false;
		return true;
	});
});

function sessionName(chain) {
	const preview = lastUserPreview(chain.logs[0]);
	return preview || formatTime(chain.logs[0].timestamp);
}

// clear session when route tab changes
watch(activeTab, () => { activeSession.value = null; });

// auto-expand selected session
watch(activeSession, (id) => {
	if (id) expandedChains.value = new Set([...expandedChains.value, id]);
});

// filter chainedLogs by active tab + session + per-column filters; each chain gets a displayLogs subset
const filteredChains = computed(() => {
	const f = filters.value;
	const tab = activeTab.value;

	// session selected: show only that session's logs
	if (activeSession.value) {
		const chain = chainedLogs.value.find((c) => c.id === activeSession.value);
		if (!chain) return [];
		if (!hasFilters.value) return [{ ...chain, displayLogs: chain.logs }];
		const matchingLogs = chain.logs.filter((log) => {
			const prompt = lastUserPreview(log);
			const status = log.error || "OK";
			return (
				colMatch(f.prompt, prompt) &&
				colMatch(f.model, log.model) &&
				colMatch(f.provider, log.provider) &&
				colMatch(f.status, status)
			);
		});
		return matchingLogs.length ? [{ ...chain, displayLogs: matchingLogs }] : [];
	}

	// tab filter first
	const tabChains = chainedLogs.value.filter((chain) => {
		if (tab && (chain.logs[0]?.route || "(unknown)") !== tab) return false;
		return true;
	});

	if (!hasFilters.value) {
		return tabChains.map((chain) => ({ ...chain, displayLogs: chain.logs }));
	}
	const result = [];
	for (const chain of tabChains) {
		const matchingLogs = chain.logs.filter((log) => {
			const prompt = lastUserPreview(log);
			const status = log.error || "OK";
			return (
				colMatch(f.prompt, prompt) &&
				colMatch(f.model, log.model) &&
				colMatch(f.provider, log.provider) &&
				colMatch(f.status, status)
			);
		});
		if (matchingLogs.length > 0) {
			result.push({ ...chain, displayLogs: matchingLogs });
		}
	}
	return result;
});

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
	vertical-align: top;
}

/* Column header layout: label on top, filter input below */
.th-col {
	display: flex;
	flex-direction: column;
	gap: 4px;
}
.th-col > span {
	white-space: nowrap;
}
.col-filter {
	width: 100%;
	min-width: 60px;
	padding: 2px 6px;
	border: 1px solid var(--c-border-light);
	border-radius: 3px;
	background: var(--c-bg);
	color: var(--c-text);
	font-size: 11px;
	font-weight: 400;
	outline: none;
	box-sizing: border-box;
	transition: border-color 0.15s;
}
.col-filter:focus {
	border-color: var(--c-primary);
}
.col-filter.active {
	border-color: var(--c-primary);
	background: var(--c-primary-bg);
}

/* Session chips */
.sessions {
	display: flex;
	align-items: center;
	gap: 6px;
	margin-bottom: 12px;
	flex-wrap: wrap;
	padding: 8px 12px;
	background: var(--c-border-light);
	border-radius: var(--radius);
	border-left: 3px solid var(--c-primary);
}
.sessions-label {
	font-size: 11px;
	font-weight: 600;
	color: var(--c-text-3);
	text-transform: uppercase;
	letter-spacing: 0.05em;
	white-space: nowrap;
	margin-right: 2px;
}
.session-chip {
	max-width: 260px;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
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
.modal-header-actions {
	display: flex;
	align-items: center;
	gap: 8px;
}
.view-toggle {
	display: flex;
	gap: 2px;
	border: 1px solid var(--c-border-light);
	border-radius: var(--radius);
	overflow: hidden;
}
.view-toggle .btn {
	border-radius: 0;
	border: none;
}
.btn-primary {
	background: var(--c-primary);
	color: #fff;
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
}
.chain-preview-oneline {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
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
.code-block-json {
	white-space: pre-wrap;
	word-break: break-word;
	max-height: calc(85vh - 120px);
	overflow-y: auto;
}
.code-block-raw {
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
.tool-pair-block {
	margin: 4px 0 8px 0;
	padding: 8px 10px;
	background: var(--c-bg);
	border: 1px solid var(--c-border-light);
	border-radius: var(--radius);
}
.tool-pair-block .tool-chip {
	margin-bottom: 2px;
}
.tool-pair-details {
	margin: 2px 0 6px 0;
}
.tool-pair-details summary {
	font-size: 12px;
	font-weight: 400;
	color: var(--c-text-3);
	padding: 2px 0;
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

.fp-str {
	font-size: 11px;
	color: var(--c-text-3);
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
