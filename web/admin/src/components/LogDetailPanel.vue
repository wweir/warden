<template>
	<div>
		<!-- === JSON View === -->
		<div v-if="view === 'json'">
			<pre class="code-block code-block-json">{{ selectedJSON }}</pre>
		</div>

		<!-- === Timeline View === -->
		<div v-else class="detail-layout">
			<!-- Tool verdicts -->
			<section v-if="log.tool_verdicts?.length" class="detail-section panel">
				<div class="verdict-list">
					<div v-for="(v, i) in log.tool_verdicts" :key="i" class="verdict-item" :class="{ 'verdict-rejected': v.rejected }">
						<code>{{ v.tool_name }}</code>
						<span class="verdict-badge" :class="v.rejected ? 'verdict-badge-rejected' : 'verdict-badge-ok'">
							{{ v.rejected ? (v.mode === 'block' ? $t('logs.verdictBlocked') : $t('logs.verdictFlagged')) : $t('common.ok') }}
						</span>
						<span v-if="v.reason" class="verdict-reason">{{ v.reason }}</span>
					</div>
				</div>
			</section>

			<!-- Unified timeline -->
			<section class="detail-section">
				<div v-if="timelineNodes.length" class="chain">
					<div
						v-for="(node, i) in timelineNodes"
						:key="i"
						class="chain-node"
						:class="{ 'chain-node-last': i === timelineNodes.length - 1 }"
					>
						<div class="chain-dot" :class="'dot-' + node.dotType"></div>
						<div class="chain-content">
							<div class="chain-label-row">
								<div class="chain-label">{{ node.label }}</div>
							</div>

							<div
								v-if="nodePreviewText(node)"
								class="chain-preview"
								:class="{ 'chain-preview-oneline': isRolePreviewNode(node) }"
							>{{ nodePreviewText(node) }}</div>

							<!-- tool call + result pair -->
							<div v-if="node.type === 'tool-pair'" class="tool-pair-block">
								<details class="tool-disclosure">
									<summary class="tool-summary">
										<span class="tool-summary-kind">{{ $t('logs.toolCall') }}</span>
										<code>{{ node.toolName }}</code>
										<span class="tool-summary-preview">{{ payloadPreview(node.toolArgs) }}</span>
									</summary>
									<pre class="code-block">{{ formatJSON(node.toolArgs) }}</pre>
								</details>
								<details v-if="node.toolResult !== undefined" class="tool-disclosure">
									<summary class="tool-summary">
										<span class="tool-summary-kind" :class="{ 'text-error': node.toolError }">{{ node.toolError ? $t('logs.toolFail') : $t('logs.toolResult') }}</span>
										<span class="tool-summary-preview">{{ payloadPreview(node.toolResult) }}</span>
									</summary>
									<pre class="code-block code-block-raw">{{ renderEscapes(node.toolResult) }}</pre>
								</details>
							</div>

							<!-- tool_calls from assistant (unpaired) -->
							<div v-if="node.toolCalls?.length" class="chain-tools">
								<div v-for="(tc, j) in node.toolCalls" :key="j">
									<details class="tool-disclosure">
										<summary class="tool-summary">
											<span class="tool-summary-kind">{{ $t('logs.toolCall') }}</span>
											<code>{{ tc.function?.name || tc.name }}</code>
											<span class="tool-summary-preview">{{ payloadPreview(tc.function?.arguments || tc.arguments) }}</span>
										</summary>
										<pre class="code-block">{{ formatJSON(tc.function?.arguments || tc.arguments) }}</pre>
									</details>
								</div>
							</div>

							<details v-if="node.raw" class="raw-disclosure">
								<summary class="raw-summary">{{ $t('logs.raw') }}</summary>
								<div class="chain-raw">
									<div class="json-viewer">
										<div
											v-for="row in jsonRows(node.raw, `raw-${i}`)"
											:key="row.id"
											class="json-row"
											:style="{ '--json-depth': row.depth }"
										>
											<div class="json-field">
												<span v-if="row.fieldKey" class="json-key">&quot;{{ row.fieldKey }}&quot;</span>
												<span v-if="row.fieldKey" class="json-token json-token-punctuation">:</span>
												<span
													class="json-value"
													:class="`json-token-${row.kind}`"
												>{{ row.displayValue }}</span>
											</div>
										</div>
									</div>
								</div>
							</details>
						</div>
					</div>
				</div>

				<div v-else class="chain">
					<div class="chain-node">
						<div class="chain-dot dot-user"></div>
						<div class="chain-content">
							<div class="chain-label">{{ $t('logs.request') }}</div>
							<details>
								<summary>{{ $t('logs.body') }}</summary>
								<pre class="code-block code-block-raw">{{ renderEscapes(formatJSON(log.request)) }}</pre>
							</details>
						</div>
					</div>
				</div>

			</section>

			<!-- Response output stays after the fixed request timeline so updates only append at the tail. -->
			<section v-if="responseHasText" class="detail-section panel response-output-pane">
				<div class="streaming-header">
					<span class="section-eyebrow">{{ $t('logs.response') }}</span>
					<span v-if="log.pending" class="streaming-indicator">
						<span class="streaming-dot"></span>
						{{ $t('logs.streaming') }}
					</span>
				</div>
				<pre class="streaming-text">{{ assembledText }}</pre>
			</section>
		</div>
	</div>
</template>

<script setup>
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { useTimeline } from "../composables/useTimeline.js";
import { copyToClipboard } from "../utils.js";

const props = defineProps({
	log: { type: Object, required: true },
	lastUserPreview: { type: Function, required: true },
	view: { type: String, default: "timeline" },
});

const { t } = useI18n();

const selected = computed(() => props.log);

const {
	timelineNodes,
	assembledText,
	responseHasText,
	selectedJSON,
	formatJSON,
	renderEscapes,
} = useTimeline(selected);
function nodePreviewText(node) {
	const preview = node?.type === "tool-pair" ? node.assistantPreview : node?.preview;
	const text = singleLine(preview, 180);
	const parts = [text ? `${previewPrefix(node)}: ${text}` : ""].filter(Boolean);
	if (node?.toolCalls?.length) parts.push(t("logs.tools", { n: node.toolCalls.length }));
	return parts.join(" · ");
}

function previewPrefix(node) {
	if (node?.type === "tool-pair") return t("logs.assistant");
	return node?.label || t("logs.unknown");
}

function isRolePreviewNode(node) {
	return ["system", "user", "assistant", "tool"].includes(node?.dotType);
}

function normalizeRawValue(value) {
	if (typeof value !== "string") return value;
	const rendered = renderEscapes(value);
	const trimmed = rendered.trim();
	if (!trimmed) return rendered;
	if (!(trimmed.startsWith("{") || trimmed.startsWith("["))) return rendered;
	try {
		return JSON.parse(trimmed);
	} catch {
		return rendered;
	}
}

function jsonRows(value, pathPrefix) {
	const normalized = normalizeRawValue(value);
	const rows = [];
	appendJSONRows(rows, "", normalized, pathPrefix, "", 0);
	return rows;
}

function appendJSONRows(rows, key, value, pathPrefix, keyPath, depth) {
	const normalized = normalizeRawValue(value);
	if (normalized == null || typeof normalized !== "object") {
		rows.push(jsonPrimitiveRow(key, normalized, pathPrefix, keyPath, depth));
		return;
	}
	const isArray = Array.isArray(normalized);
	rows.push(jsonContainerRow(key, pathPrefix, keyPath, depth, isArray ? "[" : "{"));
	for (const [childKey, child] of Object.entries(normalized)) {
		const childPath = keyPath ? `${keyPath}.${childKey}` : childKey;
		appendJSONRows(rows, childKey, child, pathPrefix, childPath, depth + 1);
	}
	rows.push(jsonContainerRow("", pathPrefix, keyPath, depth, isArray ? "]" : "}"));
}

function jsonContainerRow(key, pathPrefix, keyPath, depth, token) {
	return jsonRow(key, pathPrefix, keyPath, depth, "punctuation", token);
}

function jsonPrimitiveRow(key, value, pathPrefix, keyPath, depth) {
	return jsonRow(key, pathPrefix, keyPath, depth, jsonValueKind(value), jsonDisplayValue(value));
}

function jsonRow(key, pathPrefix, keyPath, depth, kind, displayValue) {
	const rowPath = `${pathPrefix}-${keyPath || "root"}-${depth}`.replace(/[^a-zA-Z0-9_-]/g, "_");
	return {
		id: rowPath,
		fieldKey: key,
		depth,
		kind,
		displayValue,
	};
}

function jsonValueKind(value) {
	if (typeof value === "string") return "string";
	if (typeof value === "number") return "number";
	if (typeof value === "boolean") return "boolean";
	if (value == null) return "null";
	return "punctuation";
}

function jsonDisplayValue(value) {
	if (typeof value === "string") return JSON.stringify(value);
	if (typeof value === "number" || typeof value === "boolean") return String(value);
	if (value == null) return "null";
	return Array.isArray(value) ? "[]" : "{}";
}

function payloadPreview(value) {
	const text = value == null ? "" : renderEscapes(formatJSON(value));
	return singleLine(text, 160);
}

function singleLine(value, maxLen) {
	const text = String(value || "").replace(/\s+/g, " ").trim();
	if (!text) return "";
	return text.length > maxLen ? text.slice(0, maxLen) + "..." : text;
}

async function copyJSON() {
	const text = selectedJSON.value;
	return await copyToClipboard(text);
}

defineExpose({ copyJSON });
</script>

<style scoped>
/* Layout */
.detail-layout {
	display: flex;
	flex-direction: column;
	gap: 8px;
}

.detail-section {
	padding: 0;
	box-shadow: none;
}

.section-eyebrow {
	font-size: 10px;
	font-weight: 700;
	letter-spacing: 0.08em;
	text-transform: uppercase;
	color: var(--c-text-3);
}

/* Streaming pane */
.response-output-pane {
	padding: 10px 12px;
	background: var(--c-surface);
	border: 1px solid var(--c-border);
	border-radius: var(--radius);
}

.streaming-header {
	display: flex;
	align-items: center;
	gap: 10px;
	margin-bottom: 6px;
}

.streaming-indicator {
	display: inline-flex;
	align-items: center;
	gap: 6px;
	font-size: 12px;
	color: var(--c-primary);
	font-weight: 600;
}

.streaming-dot {
	width: 7px;
	height: 7px;
	border-radius: 50%;
	background: var(--c-primary);
	animation: pulse-dot 1.2s ease-in-out infinite;
	will-change: opacity, transform;
}

@keyframes pulse-dot {
	0%, 100% { opacity: 1; transform: scale(1); }
	50% { opacity: 0.4; transform: scale(0.8); }
}

@media (prefers-reduced-motion: reduce) {
	.streaming-dot {
		animation: none;
		opacity: 1;
	}
}

.streaming-text {
	margin: 0;
	white-space: pre-wrap;
	word-break: break-word;
	font-size: 13px;
	line-height: 1.55;
	color: var(--c-text);
	font-family: var(--font-mono);
	max-height: 200px;
	overflow-y: auto;
}

/* Details / summary */
details {
	margin-bottom: 6px;
}
summary {
	cursor: pointer;
	font-weight: 600;
	font-size: 12px;
	padding: 3px 0;
	user-select: none;
	color: var(--c-text-2);
}
summary:hover {
	color: var(--c-primary);
}

/* Dependency chain */
.chain {
	position: relative;
	padding-left: 12px;
}
.chain-node {
	position: relative;
	padding: 0 0 10px 12px;
}
.chain-node::before {
	content: "";
	position: absolute;
	left: 0;
	top: 2px;
	bottom: 0;
	width: 2px;
	background: var(--c-border-light);
}
.chain-node-last::before {
	display: none;
}
.chain-dot {
	position: absolute;
	left: -5px;
	top: 2px;
	width: 8px;
	height: 8px;
	border-radius: 50%;
	border: 2px solid var(--c-surface);
	box-sizing: border-box;
}
.dot-request { background: var(--c-primary); }
.dot-system { background: var(--c-text-3); }
.dot-user { background: var(--c-primary); }
.dot-assistant { background: var(--c-success); }
.dot-tool { background: var(--c-warning); }
.dot-step { background: var(--c-text-3); }
.dot-ok { background: var(--c-success); }
.dot-error { background: var(--c-danger); }

.chain-content {
	min-height: 16px;
}
.chain-label-row {
	display: flex;
	align-items: baseline;
	gap: 8px;
	min-width: 0;
	margin-bottom: 2px;
}

.chain-label {
	font-weight: 600;
	font-size: 12px;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.raw-disclosure {
	margin: 3px 0 0 0;
	min-width: 0;
}
.raw-summary {
	display: flex;
	align-items: baseline;
	gap: 6px;
	min-width: 0;
	padding: 2px 0;
	font-size: 12px;
	font-weight: 400;
	color: var(--c-text-2);
	white-space: nowrap;
	cursor: pointer;
}
.raw-summary:hover {
	color: var(--c-primary);
}
.raw-summary::marker {
	color: var(--c-text-3);
	font-size: 10px;
}
.raw-disclosure[open] .raw-summary {
	margin-bottom: 4px;
}
.chain-preview {
	font-size: 12px;
	color: var(--c-text-2);
	margin-bottom: 2px;
	white-space: pre-wrap;
}
.chain-preview-oneline {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.chain-raw {
	margin-top: 4px;
}

.json-viewer {
	max-height: 240px;
	overflow: auto;
	padding: 8px 0;
	background: var(--c-bg);
	border: 1px solid var(--c-border);
	border-radius: var(--radius-sm);
	font-family: var(--font-mono);
	font-size: 12px;
	line-height: 1.55;
}

.json-row {
	display: flex;
	align-items: baseline;
	padding: 1px 10px 1px calc(10px + var(--json-depth) * 16px);
	min-width: max-content;
}

.json-row:hover {
	background: var(--c-primary-bg);
}

.json-field {
	display: flex;
	align-items: baseline;
	gap: 4px;
	min-width: 0;
	white-space: pre;
}

.json-key {
	color: var(--c-primary);
	font-weight: 600;
}

.json-value {
	min-width: 0;
}

.json-token-string,
.json-value.json-token-string {
	color: var(--c-success-text);
}

.json-token-number,
.json-value.json-token-number {
	color: var(--c-warning-text);
}

.json-token-boolean,
.json-value.json-token-boolean {
	color: var(--c-primary);
	font-weight: 600;
}

.json-token-null,
.json-value.json-token-null {
	color: var(--c-text-3);
	font-style: italic;
}

.json-token-punctuation,
.json-value.json-token-punctuation {
	color: var(--c-text-2);
}

.response-block {
	padding: 8px 10px;
	border-radius: var(--radius);
	font-size: 12px;
}
.response-ok {
	background: var(--c-success-soft);
	border: 1px solid var(--c-border);
}
.response-error {
	background: var(--c-danger-bg);
	border: 1px solid var(--c-border);
}
.response-warn {
	background: var(--c-warning-bg);
	border: 1px solid var(--c-border);
}
.response-streaming {
	background: var(--c-primary-bg);
	border: 1px solid var(--c-border);
}
.response-status {
	font-weight: 600;
}

.code-block-assembled {
	white-space: pre-wrap;
	word-break: break-word;
	max-height: 200px;
	overflow-y: auto;
}
.code-block-json {
	white-space: pre-wrap;
	word-break: break-word;
	max-height: calc(85vh - 160px);
	overflow-y: auto;
}
.code-block-raw {
	white-space: pre-wrap;
	word-break: break-word;
	max-height: 200px;
	overflow-y: auto;
}
.chain-tools {
	display: flex;
	flex-direction: column;
	gap: 2px;
	margin: 2px 0;
}
.tool-pair-block {
	display: flex;
	flex-direction: column;
	gap: 2px;
	margin: 2px 0 3px 0;
	padding: 4px 6px;
	background: var(--c-bg);
	border: 1px solid var(--c-border-light);
	border-radius: var(--radius-sm);
}
.tool-disclosure {
	margin: 0;
	min-width: 0;
}
.tool-summary {
	display: flex;
	align-items: baseline;
	gap: 6px;
	min-width: 0;
	padding: 1px 0;
	font-size: 12px;
	font-weight: 400;
	color: var(--c-text-2);
	white-space: nowrap;
}
.tool-summary::marker {
	color: var(--c-text-3);
	font-size: 10px;
}
.tool-summary-kind {
	color: var(--c-text-3);
	font-family: var(--font-mono);
	flex: 0 0 auto;
}
.tool-summary code {
	flex: 0 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
}
.tool-summary-preview {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	color: var(--c-text-3);
}
.tool-disclosure[open] .tool-summary {
	margin-bottom: 3px;
}

.fp-str {
	font-size: 11px;
	color: var(--c-text-3);
}

.verdict-list {
	display: flex;
	flex-direction: column;
	gap: 4px;
}
.verdict-item {
	display: flex;
	align-items: center;
	gap: 6px;
	padding: 4px 8px;
	border-radius: var(--radius-sm);
	background: var(--c-bg);
	border: 1px solid var(--c-border-light);
	font-size: 12px;
}
.verdict-rejected {
	background: var(--c-danger-bg);
	border-color: color-mix(in srgb, var(--c-danger) 28%, var(--c-border));
}
.verdict-badge {
	display: inline-flex;
	padding: 1px 6px;
	border-radius: 999px;
	font-size: 10px;
	font-weight: 600;
	white-space: nowrap;
}
.verdict-badge-ok {
	background: var(--c-success-soft);
	color: var(--c-success-text);
}
.verdict-badge-rejected {
	background: var(--c-danger-bg);
	color: var(--c-danger-text);
}
.verdict-reason {
	font-size: 11px;
	color: var(--c-text-2);
}

</style>
