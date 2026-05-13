<template>
	<div>
		<div class="detail-toolbar">
			<div class="view-toggle">
				<button class="btn btn-sm" :class="detailView === 'timeline' ? 'btn-primary' : 'btn-secondary'" @click="detailView = 'timeline'">{{ $t('logs.timeline') }}</button>
				<button class="btn btn-sm" :class="detailView === 'json' ? 'btn-primary' : 'btn-secondary'" @click="detailView = 'json'">{{ $t('logs.json') }}</button>
			</div>
			<button class="btn btn-secondary btn-sm" @click="copyJSON">{{ copied ? '\u2713' : $t('common.copy') }}</button>
		</div>

		<!-- === JSON View === -->
		<div v-if="detailView === 'json'">
			<pre class="code-block code-block-json">{{ selectedJSON }}</pre>
		</div>

		<!-- === Timeline View === -->
		<div v-else class="detail-layout">
			<!-- Compact header -->
			<div class="detail-compact-header">
				<div class="detail-compact-main">
					<span class="detail-status-pill" :class="responseClass(log)">
						{{ log.error || responseStatusText(log) }}
					</span>
					<span v-if="log.model" class="detail-meta-text">{{ log.model }}</span>
					<span v-if="log.provider" class="detail-meta-text">{{ log.provider }}</span>
					<span v-if="log.duration_ms != null" class="detail-meta-text">{{ formatDuration(log.duration_ms) }}</span>
					<span v-if="log.ttft_ms != null" class="detail-meta-text detail-meta-muted">TTFT {{ formatDuration(log.ttft_ms) }}</span>
				</div>
				<div class="detail-compact-sub">
					<span v-if="log.route" class="detail-meta-muted">{{ log.route }}</span>
					<span v-if="log.fingerprint" class="detail-meta-muted">{{ log.fingerprint.slice(0, 16) }}</span>
					<span class="detail-meta-muted">{{ log.request_id }}</span>
				</div>
			</div>

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

			<!-- Streaming output (top, for pending logs) -->
			<section v-if="log.pending && assembledText" class="detail-section panel streaming-pane">
				<div class="streaming-header">
					<span class="section-eyebrow">{{ $t('logs.response') }}</span>
					<span class="streaming-indicator">
						<span class="streaming-dot"></span>
						{{ $t('logs.streaming') }}
					</span>
				</div>
				<pre class="streaming-text">{{ assembledText }}</pre>
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
							<div class="chain-label">{{ node.label }}</div>

							<div
								v-if="nodePreviewText(node)"
								class="chain-preview"
								:class="{ 'chain-preview-oneline': node.dotType === 'system' || node.dotType === 'assistant' }"
							>{{ nodePreviewText(node) }}</div>

							<!-- Response text for last assistant node (when not streaming) -->
							<details v-if="node.dotType === 'assistant' && isLastAssistantNode(i) && !log.pending && assembledText" class="response-disclosure">
								<summary class="response-summary">
									<span class="response-summary-kind">{{ $t('logs.response') }}</span>
									<span class="response-summary-preview">{{ responsePreview }}</span>
								</summary>
								<pre class="code-block code-block-assembled">{{ assembledText }}</pre>
							</details>

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

							<!-- expandable raw content -->
							<details v-if="node.raw" class="chain-raw">
								<summary>{{ $t('logs.raw') }}</summary>
								<pre class="code-block code-block-raw">{{ renderEscapes(typeof node.raw === 'string' ? node.raw : formatJSON(node.raw)) }}</pre>
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

				<!-- Final response (when no timeline nodes or for non-chat formats) -->
				<div v-if="!log.pending && assembledText && !hasAssistantNode" class="chain-response-standalone">
					<pre class="code-block code-block-assembled">{{ assembledText }}</pre>
				</div>
			</section>
		</div>
	</div>
</template>

<script setup>
import { ref, computed } from "vue";
import { useI18n } from "vue-i18n";
import { useTimeline } from "../composables/useTimeline.js";
import { formatDuration } from "../utils.js";

const props = defineProps({
	log: { type: Object, required: true },
	lastUserPreview: { type: Function, required: true },
});

const { t } = useI18n();
const detailView = ref("timeline");
const copied = ref(false);
let copyTimer = null;

const selected = computed(() => props.log);

const {
	timelineNodes,
	assembledText,
	selectedJSON,
	formatJSON,
	renderEscapes,
} = useTimeline(selected);

const hasAssistantNode = computed(() =>
	timelineNodes.value.some((n) => n.dotType === "assistant"),
);

const responsePreview = computed(() => singleLine(assembledText.value, 180));

function isLastAssistantNode(index) {
	for (let i = timelineNodes.value.length - 1; i >= 0; i--) {
		if (timelineNodes.value[i].dotType === "assistant") return i === index;
	}
	return false;
}

function responseClass(log) {
	if (log?.pending) return "response-streaming";
	if (log?.error) return "response-error";
	if (Array.isArray(log?.tool_verdicts) && log.tool_verdicts.some((v) => v.rejected)) return "response-warn";
	if (!log?.error && Array.isArray(log?.failovers) && log.failovers.length > 0) return "response-warn";
	return "response-ok";
}

function responseStatusText(log) {
	if (log?.pending) return t("logs.streaming");
	if (Array.isArray(log?.tool_verdicts) && log.tool_verdicts.some((v) => v.rejected)) {
		return t("logs.toolRejected");
	}
	if (!log?.error && Array.isArray(log?.failovers) && log.failovers.length > 0) {
		return t("logs.failoverRecovered", { n: log.failovers.length });
	}
	return t("common.ok");
}

function nodePreviewText(node) {
	if (node?.dotType === "assistant") return assistantSummary(node);
	return node?.preview || "";
}

function assistantSummary(node) {
	const parts = [];
	const chars = String(node?.preview || "").length;
	if (chars > 0) parts.push(t("logs.contentChars", { n: chars }));
	if (node?.toolCalls?.length) parts.push(t("logs.tools", { n: node.toolCalls.length }));
	return parts.join(" · ");
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
	try {
		await navigator.clipboard.writeText(text);
	} catch {
		// clipboard API not available or permission denied
		return;
	}
	copied.value = true;
	clearTimeout(copyTimer);
	copyTimer = setTimeout(() => { copied.value = false; }, 2000);
}
</script>

<style scoped>
.detail-toolbar {
	display: flex;
	align-items: center;
	gap: 8px;
	margin-bottom: 10px;
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
	color: var(--c-text-inverse);
}

/* Compact header */
.detail-compact-header {
	display: flex;
	flex-direction: column;
	gap: 4px;
	padding: 10px 12px;
	background: var(--c-surface);
	border: 1px solid var(--c-border);
	border-radius: var(--radius);
	margin-bottom: 10px;
}

.detail-compact-main {
	display: flex;
	align-items: center;
	gap: 8px;
	flex-wrap: wrap;
}

.detail-compact-sub {
	display: flex;
	align-items: center;
	gap: 10px;
	flex-wrap: wrap;
}

.detail-status-pill {
	display: inline-flex;
	align-items: center;
	padding: 3px 8px;
	border-radius: 999px;
	font-size: 11px;
	font-weight: 600;
	white-space: nowrap;
}

.detail-status-pill.response-ok {
	background: var(--c-success-soft);
	color: var(--c-success-text);
}

.detail-status-pill.response-warn {
	background: var(--c-warning-bg);
	color: var(--c-warning-text);
}

.detail-status-pill.response-error {
	background: var(--c-danger-bg);
	color: var(--c-danger-text);
}

.detail-meta-text {
	font-size: 12px;
	font-weight: 500;
	color: var(--c-text);
}

.detail-meta-muted {
	font-size: 11px;
	color: var(--c-text-3);
	font-family: var(--font-mono);
}

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
.streaming-pane {
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
	border-left: 2px solid var(--c-border-light);
}
.chain-node-last {
	border-left-color: transparent;
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
.chain-label {
	font-weight: 600;
	font-size: 12px;
	margin-bottom: 2px;
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

.response-disclosure {
	margin: 3px 0 0 0;
	min-width: 0;
}

.response-summary {
	display: flex;
	align-items: baseline;
	gap: 6px;
	min-width: 0;
	padding: 2px 0;
	font-size: 12px;
	font-weight: 400;
	color: var(--c-text-2);
	white-space: nowrap;
}

.response-summary::marker {
	color: var(--c-text-3);
	font-size: 10px;
}

.response-summary-kind {
	flex: 0 0 auto;
	color: var(--c-text-3);
	font-family: var(--font-mono);
}

.response-summary-preview {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	color: var(--c-text-2);
}

.response-disclosure[open] .response-summary {
	margin-bottom: 4px;
}

.chain-response-standalone {
	margin-top: 8px;
}

.chain-raw {
	margin-top: 2px;
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

@media (max-width: 768px) {
	.detail-compact-main {
		gap: 6px;
	}
}
</style>
