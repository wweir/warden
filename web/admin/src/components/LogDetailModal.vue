<template>
	<div v-if="log" class="modal-overlay" @click.self="close">
		<div
			ref="modalRef"
			class="modal"
			role="dialog"
			aria-modal="true"
			:aria-labelledby="detailTitleId"
			@keydown="handleModalKeydown"
		>
			<div class="modal-header">
				<h3 :id="detailTitleId">{{ $t('logs.requestDetail') }}</h3>
				<div class="modal-header-actions">
					<div class="view-toggle">
						<button class="btn btn-sm" :class="detailView === 'timeline' ? 'btn-primary' : 'btn-secondary'" @click="detailView = 'timeline'">{{ $t('logs.timeline') }}</button>
						<button class="btn btn-sm" :class="detailView === 'json' ? 'btn-primary' : 'btn-secondary'" @click="detailView = 'json'">{{ $t('logs.json') }}</button>
					</div>
					<button class="btn btn-secondary btn-sm" @click="copyJSON">{{ copied ? '\u2713' : $t('common.copy') }}</button>
					<button ref="closeButtonRef" class="btn btn-secondary btn-sm" @click="close">{{ $t('common.close') }}</button>
				</div>
			</div>

			<div class="modal-body">
				<!-- === JSON View === -->
				<div v-if="detailView === 'json'">
					<pre class="code-block code-block-json">{{ selectedJSON }}</pre>
				</div>

				<!-- === Timeline View === -->
				<div v-else class="detail-layout">
					<section class="detail-summary panel">
						<div class="detail-summary-head">
							<div>
								<div class="section-eyebrow">{{ $t('logs.requestSummary') }}</div>
								<h4 class="detail-summary-title">{{ detailTitle }}</h4>
							</div>
							<span class="detail-status-pill" :class="responseClass(log)">
								{{ log.error || responseStatusText(log) }}
							</span>
						</div>

						<div class="detail-meta-grid">
							<div class="detail-meta-item">
								<span>{{ $t('logs.requestId') }}</span>
								<code>{{ log.request_id }}</code>
							</div>
							<div class="detail-meta-item">
								<span>{{ $t('logs.route') }}</span>
								<code>{{ log.route }}</code>
							</div>
							<div class="detail-meta-item">
								<span>{{ $t('logs.model') }}</span>
								<strong>{{ log.model }}</strong>
							</div>
							<div class="detail-meta-item">
								<span>{{ $t('logs.provider') }}</span>
								<strong>{{ log.provider }}</strong>
							</div>
							<div class="detail-meta-item">
								<span>{{ $t('logs.duration') }}</span>
								<strong>{{ formatDuration(log.duration_ms) }}</strong>
							</div>
							<div v-if="log.fingerprint" class="detail-meta-item detail-meta-item-wide">
								<span>{{ $t('logs.session') }}</span>
								<code class="fp-str">{{ log.fingerprint }}</code>
							</div>
						</div>
					</section>

					<section v-if="log.tool_verdicts?.length" class="detail-section panel">
						<div class="detail-section-head">
							<div>
								<div class="section-eyebrow">{{ $t('logs.toolVerdicts') }}</div>
							</div>
						</div>
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

					<section class="detail-section panel">
						<div class="detail-section-head">
							<div>
								<div class="section-eyebrow">{{ $t('logs.conversation') }}</div>
								<h4 class="detail-section-title">{{ $t('logs.timeline') }}</h4>
							</div>
						</div>

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

									<div
										v-if="node.preview"
										class="chain-preview"
										:class="{ 'chain-preview-oneline': node.dotType === 'system' }"
									>{{ node.preview }}</div>

									<!-- tool call + result pair -->
									<div v-if="node.type === 'tool-pair'" class="tool-pair-block">
										<div class="tool-chip">
											<span class="tool-arrow">{{ $t('logs.toolCall') }}</span>
											<code>{{ node.toolName }}</code>
										</div>
										<details class="tool-pair-details" :open="node.defaultOpen || undefined">
											<summary>{{ $t('logs.arguments') }}</summary>
											<pre class="code-block">{{ formatJSON(node.toolArgs) }}</pre>
										</details>
										<div class="tool-chip" v-if="node.toolResult !== undefined">
											<span class="tool-arrow" :class="{ 'text-error': node.toolError }">{{ node.toolError ? $t('logs.toolFail') : $t('logs.toolResult') }}</span>
										</div>
										<details v-if="node.toolResult !== undefined" class="tool-pair-details" :open="node.defaultOpen || undefined">
											<summary>{{ $t('logs.output') }}</summary>
											<pre class="code-block code-block-raw">{{ renderEscapes(node.toolResult) }}</pre>
										</details>
									</div>

									<!-- tool_calls from assistant (unpaired) -->
									<div v-if="node.toolCalls?.length" class="chain-tools">
										<div v-for="(tc, j) in node.toolCalls" :key="j" class="tool-chip">
											<span class="tool-arrow">{{ $t('logs.toolCall') }}</span>
											<code>{{ tc.function?.name || tc.name }}</code>
											<details :open="node.defaultOpen || undefined">
												<summary>{{ $t('logs.arguments') }}</summary>
												<pre class="code-block">{{ formatJSON(tc.function?.arguments || tc.arguments) }}</pre>
											</details>
										</div>
									</div>

									<!-- expandable raw content -->
									<details v-if="node.raw" :open="node.defaultOpen || undefined">
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
									<details open>
										<summary>{{ $t('logs.body') }}</summary>
										<pre class="code-block code-block-raw">{{ renderEscapes(formatJSON(log.request)) }}</pre>
									</details>
								</div>
							</div>
						</div>
					</section>

					<section class="detail-section panel">
						<div class="detail-section-head">
							<div>
								<div class="section-eyebrow">{{ $t('logs.result') }}</div>
								<h4 class="detail-section-title">{{ $t('logs.response') }}</h4>
							</div>
						</div>

						<div class="response-block" :class="responseClass(log)">
							<span class="response-status">{{ log.error || responseStatusText(log) }}</span>
							<div v-if="log.response" class="response-pane">
								<div class="pane-label">{{ $t('logs.response') }}</div>
								<!-- tool calls from response (all protocols) -->
								<div v-if="responseToolCalls.length" class="chain-tools" style="margin-bottom:8px">
									<div v-for="(tc, j) in responseToolCalls" :key="j" class="tool-chip">
										<span class="tool-arrow">{{ $t('logs.toolCall') }}</span>
										<code>{{ tc.name }}</code>
										<details>
											<summary>{{ $t('logs.arguments') }}</summary>
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
									<pre class="code-block code-block-raw">{{ renderEscapes(formatJSON(log.response)) }}</pre>
								</details>
							</div>
						</div>
					</section>
				</div>
			</div>
		</div>
	</div>
</template>

<script setup>
import { ref, watch, nextTick, onUnmounted } from "vue";
import { useI18n } from "vue-i18n";
import { useTimeline } from "../composables/useTimeline.js";
import { formatDuration } from "../utils.js";

const props = defineProps({
	log: { default: null },
	lastUserPreview: { type: Function, required: true },
});

const emit = defineEmits(["close"]);
const { t } = useI18n();

const modalRef = ref(null);
const closeButtonRef = ref(null);
const detailView = ref("timeline");
const copied = ref(false);
let copyTimer = null;
const detailTitleId = "log-detail-title";
let lastFocusedElement = null;

const selected = ref(props.log);
watch(() => props.log, (val) => { selected.value = val; });

const {
	timelineNodes,
	responseToolCalls,
	responseHasText,
	assembledText,
	selectedJSON,
	formatJSON,
	renderEscapes,
	latestContentPreview,
} = useTimeline(selected);

const detailTitle = computed(() => {
	if (!selected.value) return "";
	const preview = latestContentPreview(selected.value);
	if (preview) return preview;
	return props.lastUserPreview(selected.value) || selected.value.request_id || t("logs.requestDetail");
});

function responseClass(log) {
	if (log?.error) return "response-error";
	if (Array.isArray(log?.tool_verdicts) && log.tool_verdicts.some((v) => v.rejected)) return "response-warn";
	if (!log?.pending && !log?.error && Array.isArray(log?.failovers) && log.failovers.length > 0) return "response-warn";
	return "response-ok";
}

function responseStatusText(log) {
	if (Array.isArray(log?.tool_verdicts) && log.tool_verdicts.some((v) => v.rejected)) {
		return t("logs.toolRejected");
	}
	if (!log?.pending && !log?.error && Array.isArray(log?.failovers) && log.failovers.length > 0) {
		return t("logs.failoverRecovered", { n: log.failovers.length });
	}
	return t("common.ok");
}

function copyTextFallback(text) {
	const textarea = document.createElement("textarea");
	textarea.value = text;
	textarea.setAttribute("readonly", "");
	textarea.style.position = "fixed";
	textarea.style.opacity = "0";
	textarea.style.pointerEvents = "none";
	document.body.appendChild(textarea);
	textarea.select();
	textarea.setSelectionRange(0, textarea.value.length);
	document.execCommand("copy");
	document.body.removeChild(textarea);
}

async function copyJSON() {
	if (!selected.value) return;
	try {
		if (navigator.clipboard?.writeText) {
			await navigator.clipboard.writeText(selectedJSON.value);
		} else {
			copyTextFallback(selectedJSON.value);
		}
		copied.value = true;
		clearTimeout(copyTimer);
		copyTimer = setTimeout(() => { copied.value = false; }, 2000);
	} catch {
		copyTextFallback(selectedJSON.value);
		copied.value = true;
		clearTimeout(copyTimer);
		copyTimer = setTimeout(() => { copied.value = false; }, 2000);
	}
}

function close() {
	emit("close");
}

function getFocusableElements() {
	if (!modalRef.value) return [];
	return [...modalRef.value.querySelectorAll(
		'button, summary, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
	)].filter((el) => !el.hasAttribute("disabled") && el.getAttribute("aria-hidden") !== "true");
}

function handleModalKeydown(event) {
	if (event.key === "Escape") {
		event.preventDefault();
		close();
		return;
	}
	if (event.key !== "Tab") return;
	const focusable = getFocusableElements();
	if (focusable.length === 0) return;
	const first = focusable[0];
	const last = focusable[focusable.length - 1];
	if (event.shiftKey && document.activeElement === first) {
		event.preventDefault();
		last.focus();
	} else if (!event.shiftKey && document.activeElement === last) {
		event.preventDefault();
		first.focus();
	}
}

function showDetail(log, trigger = null) {
	lastFocusedElement = trigger instanceof HTMLElement ? trigger : document.activeElement;
	selected.value = log;
	detailView.value = log.error ? "json" : "timeline";
}

watch(() => props.log, async (value, oldValue) => {
	if (value && !oldValue) {
		lastFocusedElement = document.activeElement;
		detailView.value = value.error ? "json" : "timeline";
	}
	if (value) {
		await nextTick();
		closeButtonRef.value?.focus();
		return;
	}
	await nextTick();
	if (lastFocusedElement instanceof HTMLElement && document.contains(lastFocusedElement)) {
		lastFocusedElement.focus();
	}
	lastFocusedElement = null;
});

onUnmounted(() => {
	clearTimeout(copyTimer);
});
</script>

<script>
import { computed } from "vue";
</script>

<style scoped>
/* Modal */
.modal-overlay {
	position: fixed;
	top: 0;
	left: 0;
	right: 0;
	bottom: 0;
	background: var(--c-overlay);
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
	color: var(--c-text-inverse);
}
.modal-body {
	padding: 20px;
	overflow-y: auto;
}

.detail-layout {
	display: flex;
	flex-direction: column;
	gap: 14px;
}

.detail-summary,
.detail-section {
	padding: 16px;
	box-shadow: none;
}

.detail-summary {
	background: linear-gradient(180deg, color-mix(in srgb, var(--c-surface-tint) 70%, var(--c-surface)) 0%, var(--c-surface) 100%);
}

.detail-summary-head,
.detail-section-head {
	display: flex;
	align-items: flex-start;
	justify-content: space-between;
	gap: 12px;
}

.section-eyebrow {
	font-size: 11px;
	font-weight: 700;
	letter-spacing: 0.08em;
	text-transform: uppercase;
	color: var(--c-text-3);
	margin-bottom: 4px;
}

.detail-summary-title,
.detail-section-title {
	margin: 0;
	font-size: 18px;
	line-height: 1.25;
}

.detail-status-pill {
	display: inline-flex;
	align-items: center;
	padding: 6px 10px;
	border-radius: 999px;
	font-size: 12px;
	font-weight: 600;
	white-space: nowrap;
}

.detail-status-pill.response-ok {
	background: var(--c-success-soft);
	color: var(--c-success-text);
}

.detail-status-pill.response-warn {
	background: var(--c-warning-bg);
	color: #92400e;
}

.detail-status-pill.response-error {
	background: var(--c-danger-bg);
	color: #991b1b;
}

.detail-meta-grid {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: 12px;
	margin-top: 16px;
}

.detail-meta-item {
	display: flex;
	flex-direction: column;
	gap: 6px;
	padding: 12px;
	border: 1px solid var(--c-border-light);
	border-radius: var(--radius-sm);
	background: color-mix(in srgb, var(--c-surface) 84%, var(--c-surface-tint));
	min-width: 0;
}

.detail-meta-item span {
	font-size: 11px;
	font-weight: 700;
	letter-spacing: 0.06em;
	text-transform: uppercase;
	color: var(--c-text-3);
}

.detail-meta-item strong,
.detail-meta-item code {
	font-size: 13px;
	line-height: 1.5;
}

.detail-meta-item-wide {
	grid-column: 1 / -1;
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
.dot-request { background: var(--c-primary); }
.dot-system { background: var(--c-text-3); }
.dot-user { background: var(--c-primary); }
.dot-assistant { background: var(--c-success); }
.dot-tool { background: var(--c-warning); }
.dot-step { background: var(--c-text-3); }
.dot-ok { background: var(--c-success); }
.dot-error { background: var(--c-danger); }

.chain-content {
	min-height: 24px;
}
.chain-label {
	font-weight: 600;
	font-size: 13px;
	margin-bottom: 4px;
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

.response-block {
	padding: 10px 14px;
	border-radius: var(--radius);
	font-size: 13px;
}
.response-ok {
	background: var(--c-success-soft);
	border: 1px solid color-mix(in srgb, var(--c-success) 28%, white);
}
.response-error {
	background: var(--c-danger-bg);
	border: 1px solid color-mix(in srgb, var(--c-danger) 28%, white);
}
.response-warn {
	background: var(--c-warning-bg);
	border: 1px solid color-mix(in srgb, var(--c-warning) 28%, white);
}
.response-status {
	font-weight: 600;
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
.tool-chip details { margin: 0; }
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
.tool-pair-block .tool-chip { margin-bottom: 2px; }
.tool-pair-details { margin: 2px 0 6px 0; }
.tool-pair-details summary {
	font-size: 12px;
	font-weight: 400;
	color: var(--c-text-3);
	padding: 2px 0;
}

.fp-str {
	font-size: 11px;
	color: var(--c-text-3);
}

.verdict-list {
	display: flex;
	flex-direction: column;
	gap: 6px;
}
.verdict-item {
	display: flex;
	align-items: center;
	gap: 8px;
	padding: 6px 10px;
	border-radius: var(--radius-sm);
	background: var(--c-bg);
	border: 1px solid var(--c-border-light);
	font-size: 13px;
}
.verdict-rejected {
	background: var(--c-danger-bg);
	border-color: color-mix(in srgb, var(--c-danger) 28%, var(--c-border));
}
.verdict-badge {
	display: inline-flex;
	padding: 1px 8px;
	border-radius: 999px;
	font-size: 11px;
	font-weight: 600;
	white-space: nowrap;
}
.verdict-badge-ok {
	background: var(--c-success-soft);
	color: var(--c-success-text);
}
.verdict-badge-rejected {
	background: var(--c-danger-bg);
	color: #991b1b;
}
.verdict-reason {
	font-size: 12px;
	color: var(--c-text-2);
}

@media (max-width: 768px) {
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

	.modal-header {
		align-items: flex-start;
		flex-direction: column;
		gap: 12px;
	}

	.modal-header-actions {
		width: 100%;
		flex-wrap: wrap;
	}

	.detail-summary-head,
	.detail-section-head {
		flex-direction: column;
	}

	.detail-meta-grid {
		grid-template-columns: 1fr;
	}

	.view-toggle {
		width: 100%;
	}

	.view-toggle .btn {
		flex: 1 1 0;
	}

	.response-dual {
		flex-direction: column;
	}
}
</style>
