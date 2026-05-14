<template>
	<div>
		<div class="header-row">
			<div class="page-heading">
				<h2 class="page-title">{{ $t('logs.title') }}</h2>
				<p class="page-subtitle">{{ $t('logs.pageSubtitle') }}</p>
			</div>
			<div class="header-actions">
				<button @click="togglePause" class="btn btn-secondary btn-sm">
					{{ paused ? $t('logs.resume') : $t('logs.pause') }}
				</button>
				<button @click="handleClear" class="btn btn-secondary btn-sm">{{ $t('logs.clear') }}</button>
			</div>
		</div>

		<div v-if="error" class="msg msg-error">{{ error }}</div>

		<div
			class="logs-workspace"
			:class="{
				'logs-workspace-no-tree': !sessionTree.length,
			}"
		>
			<SessionTreePanel
				:tree="sessionTree"
				:activeRoute="activeRoute"
				:activeSession="activeSession"
				:logCount="logs.length"
				@select-all="selectAll"
				@select-route="selectRoute"
				@select-session="selectSession"
			/>

			<section class="logs-content">
				<div class="logs-stats">
					<div class="stats-main">
						<span class="stats-route">{{ scopeLabel }}</span>
						<span v-if="scopeWindowLabel" class="stats-time">{{ scopeWindowLabel }}</span>
					</div>
					<span class="stats-count">{{ filteredLogs.length }} {{ $t('logs.reqs') }}</span>
				</div>

				<div v-if="logs.length" class="filters-bar">
					<input
						v-model="filters.prompt"
						class="filter-input"
						:class="{ active: filters.prompt }"
						:placeholder="$t('logs.filterPrompt')"
						:aria-label="$t('logs.filterPrompt')"
					/>
					<input
						v-model="filters.model"
						class="filter-input"
						:class="{ active: filters.model }"
						:placeholder="$t('logs.filterModel')"
						:aria-label="$t('logs.filterModel')"
					/>
					<input
						v-model="filters.provider"
						class="filter-input"
						:class="{ active: filters.provider }"
						:placeholder="$t('logs.filterProvider')"
						:aria-label="$t('logs.filterProvider')"
					/>
					<input
						v-model="filters.status"
						class="filter-input"
						:class="{ active: filters.status }"
						:placeholder="$t('logs.filterStatus')"
						:aria-label="$t('logs.filterStatus')"
					/>
				</div>

				<div v-if="selectedDetailLog" class="detail-inline panel">
					<div class="detail-inline-header">
						<span class="detail-inline-label">{{ $t('logs.requestDetail') }}</span>
						<button class="btn btn-secondary btn-sm" @click="closeDetail">
							{{ $t('logs.collapse') }}
						</button>
					</div>
					<LogDetailPanel
						:log="selectedDetailLog"
						:lastUserPreview="lastUserPreview"
					/>
				</div>

				<div class="table-wrap panel">
					<template v-if="logs.length">
						<table class="data-table desktop-log-table">
							<thead>
								<tr>
									<th class="th-actions"></th>
									<th>{{ $t('logs.time') }}</th>
									<th>{{ $t('logs.prompt') }}</th>
									<th>{{ $t('logs.model') }}</th>
									<th>{{ $t('logs.provider') }}</th>
									<th>{{ $t('logs.duration') }}</th>
									<th>{{ $t('logs.status') }}</th>
								</tr>
							</thead>
							<tbody>
								<tr
									v-for="log in filteredLogs"
									:key="log.request_id"
									class="log-row"
									:class="{ 'log-row-active': isDetailOpen(log) }"
								>
									<td class="cell-actions">
										<button
											class="btn btn-secondary btn-sm"
											type="button"
											:aria-label="$t('logs.viewLogDetails')"
											@click="toggleDetail(log)"
										>
											{{ isDetailOpen(log) ? $t('logs.collapse') : $t('logs.view') }}
										</button>
									</td>
									<td class="cell-time">{{ formatTime(log.timestamp) }}</td>
									<td class="cell-prompt">
										<span v-if="showRouteChip" class="row-route-chip">{{ routeName(log.route) }}</span>
										<span class="cell-prompt-text">{{ lastUserPreview(log) }}</span>
									</td>
									<td>{{ log.model || '\u2014' }}</td>
									<td>{{ log.provider || '\u2014' }}</td>
									<td class="cell-num">{{ formatDuration(log.duration_ms) }}</td>
									<td>
										<span class="status-pill" :class="statusClass(log)">{{ statusText(log) }}</span>
									</td>
								</tr>
								<tr v-if="!filteredLogs.length">
									<td colspan="7" class="empty-hint">{{ emptyMessage }}</td>
								</tr>
							</tbody>
						</table>

						<div class="mobile-log-list">
							<article
								v-for="log in filteredLogs"
								:key="log.request_id + '-m'"
								class="mobile-log-card"
								:class="{ 'mobile-log-card-active': isDetailOpen(log) }"
							>
								<div class="mobile-log-top">
									<div class="mobile-log-time">{{ formatTime(log.timestamp) }}</div>
									<button
										class="btn btn-secondary btn-sm"
										type="button"
										@click="toggleDetail(log)"
									>
										{{ isDetailOpen(log) ? $t('logs.collapse') : $t('logs.view') }}
									</button>
								</div>
								<div class="mobile-log-prompt">
									<span v-if="showRouteChip" class="row-route-chip">{{ routeName(log.route) }}</span>
									{{ lastUserPreview(log) || $t('logs.noPrompt') }}
								</div>
								<dl class="mobile-log-meta">
									<div>
										<dt>{{ $t('logs.model') }}</dt>
										<dd>{{ log.model || '\u2014' }}</dd>
									</div>
									<div>
										<dt>{{ $t('logs.provider') }}</dt>
										<dd>{{ log.provider || '\u2014' }}</dd>
									</div>
									<div>
										<dt>{{ $t('logs.duration') }}</dt>
										<dd>{{ formatDuration(log.duration_ms) }}</dd>
									</div>
									<div>
										<dt>{{ $t('logs.status') }}</dt>
										<dd>
											<span class="status-pill" :class="statusClass(log)">{{ statusText(log) }}</span>
										</dd>
									</div>
								</dl>
							</article>
							<div v-if="!filteredLogs.length" class="empty-hint">{{ emptyMessage }}</div>
						</div>
					</template>
					<div v-else class="empty-hint">{{ emptyMessage }}</div>
				</div>
			</section>
		</div>

	</div>
</template>

<script setup>
import { ref, computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { formatDuration } from "../utils.js";
import { useLogStream } from "../composables/useLogStream.js";
import { lastUserPreview, failoverCount, isRecoveredByFailover, hasRejectedVerdict, getTimestampMs } from "../log-utils.js";
import SessionTreePanel from "../components/SessionTreePanel.vue";
import LogDetailPanel from "../components/LogDetailPanel.vue";

const { t, locale } = useI18n();

// --- Log stream ---
const { logs, paused, error, togglePause, clearLogs, setAutoScroll } = useLogStream();

// --- UI state ---
const filters = ref({ prompt: "", model: "", provider: "", status: "" });
const activeRoute = ref("");
const activeSession = ref("");
const activeDetailRequestID = ref("");
const selectedDetailLog = ref(null);

watch(activeDetailRequestID, (val) => {
	setAutoScroll(!val);
});

// --- Session tree ---
const sessionTree = computed(() => {
	const map = new Map();
	for (const log of logs.value) {
		const route = log.route || "(unknown)";
		const sessionID = log.request_id || log.fingerprint || "";
		if (!map.has(route)) map.set(route, new Map());
		map.get(route).set(sessionID, log);
	}
	return Array.from(map.entries()).map(([route, sessionMap]) => ({
		route,
		sessions: Array.from(sessionMap.entries()).map(([sessionID, log]) => ({
			fingerprint: sessionID,
			log,
			preview: lastUserPreview(log) || sessionID.slice(0, 8) || t("logs.unknown"),
		})),
	}));
});

// --- Filtering ---
function colMatch(query, ...fields) {
	if (!query) return true;
	const q = query.toLowerCase();
	return fields.some((f) => (f || "").toLowerCase().includes(q));
}

const filteredLogs = computed(() => {
	const f = filters.value;
	const route = activeRoute.value;
	const sessionFp = activeSession.value;
	let result = logs.value;
	if (route) {
		result = result.filter((log) => (log.route || "(unknown)") === route);
	}
	if (sessionFp) {
		result = result.filter((log) => (log.request_id || log.fingerprint || "") === sessionFp);
	}
	if (!f.prompt && !f.model && !f.provider && !f.status) return result;
	return result.filter((log) => {
		const prompt = lastUserPreview(log);
		const status = log.error || t("common.ok");
		return (
			colMatch(f.prompt, prompt) &&
			colMatch(f.model, log.model) &&
			colMatch(f.provider, log.provider) &&
			colMatch(f.status, status)
		);
	});
});

const liveDetailLog = computed(() => {
	if (!activeDetailRequestID.value) return null;
	return logs.value.find((log) => log.request_id === activeDetailRequestID.value) || null;
});

watch(liveDetailLog, (log) => {
	if (log) {
		selectedDetailLog.value = log;
	}
});

const showRouteChip = computed(() => {
	if (activeRoute.value) return false;
	const routes = new Set();
	for (const log of filteredLogs.value) {
		routes.add(log.route || "(unknown)");
		if (routes.size > 1) return true;
	}
	return false;
});

// --- Stats bar ---
const scopeLabel = computed(() => {
	if (activeSession.value) {
		const scopedLog = filteredLogs.value[0];
		const preview = scopedLog ? lastUserPreview(scopedLog) : "";
		return preview || activeSession.value.slice(0, 12) || t("logs.session");
	}
	if (activeRoute.value) return activeRoute.value;
	return t("logs.allRoutes");
});

const timeFormatter = computed(() =>
	new Intl.DateTimeFormat(locale.value, {
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
	}),
);

function formatTime(t) {
	if (!t) return "";
	const date = new Date(t);
	if (Number.isNaN(date.getTime())) return "";
	return timeFormatter.value.format(date);
}

const scopeWindowLabel = computed(() => {
	const items = filteredLogs.value;
	if (!items.length) return "";
	let min = Infinity;
	let max = -Infinity;
	for (const log of items) {
		const ts = getTimestampMs(log);
		if (ts < min) min = ts;
		if (ts > max) max = ts;
	}
	const start = formatTime(min === Infinity ? 0 : min);
	const end = formatTime(max === -Infinity ? 0 : max);
	if (start === end) return start;
	return `${start} \u2013 ${end}`;
});

const emptyMessage = computed(() => {
	const hasFilters = Object.values(filters.value).some((v) => v.trim());
	return hasFilters ? t("logs.noMatchingLogs") : t("logs.noLogsYet");
});

// --- Actions ---
function routeName(route) {
	if (typeof route === "string" && route.trim()) return route;
	return "(unknown)";
}

function handleClear() {
	if (!confirm(t("logs.confirmClear"))) return;
	clearLogs();
	activeRoute.value = "";
	activeSession.value = "";
	closeDetail();
}

function selectAll() {
	activeRoute.value = "";
	activeSession.value = "";
	closeDetail();
}

function selectRoute(routeKey) {
	activeRoute.value = routeKey;
	activeSession.value = "";
	closeDetail();
}

function selectSession({ route, fingerprint }) {
	activeRoute.value = route || "";
	activeSession.value = fingerprint || "";
	closeDetail();
}

function isDetailOpen(log) {
	return Boolean(log?.request_id && activeDetailRequestID.value === log.request_id);
}

function toggleDetail(log) {
	if (!log?.request_id) return;
	if (isDetailOpen(log)) {
		closeDetail();
		return;
	}
	activeDetailRequestID.value = log.request_id;
	selectedDetailLog.value = log;
}

function closeDetail() {
	activeDetailRequestID.value = "";
	selectedDetailLog.value = null;
}

function statusClass(log) {
	if (log?.pending) return "status-streaming";
	if (log?.error) return "status-error";
	if (hasRejectedVerdict(log) || isRecoveredByFailover(log)) return "status-warn";
	return "status-ok";
}

function statusText(log) {
	if (log.pending) return t("logs.streaming");
	if (log.error) return log.error;
	const parts = [];
	if (hasRejectedVerdict(log)) parts.push(t("logs.toolRejected"));
	const n = failoverCount(log);
	if (n > 0) parts.push(t("logs.failoverRecovered", { n }));
	const steps = log.steps?.length;
	if (steps) parts.push(t("logs.steps", { n: steps }));
	if (!parts.length) return t("common.ok");
	return t("common.ok") + " \u00B7 " + parts.join(" \u00B7 ");
}


</script>

<style scoped>
/* Header */
.header-row {
	display: flex;
	align-items: flex-end;
	justify-content: space-between;
	gap: 16px;
	margin-bottom: 20px;
	flex-wrap: wrap;
}

.page-heading {
	display: flex;
	flex-direction: column;
	gap: 4px;
	max-width: 760px;
}

.page-title {
	margin: 0;
}

.page-subtitle {
	font-size: 13px;
	line-height: 1.5;
	color: var(--c-text-2);
	max-width: 58ch;
}

.header-actions {
	display: flex;
	align-items: center;
	gap: 10px;
	flex-wrap: wrap;
	margin-left: auto;
}

/* Workspace */
.logs-workspace {
	display: grid;
	grid-template-columns: minmax(260px, 320px) minmax(0, 1fr);
	gap: 20px;
	align-items: start;
}

.logs-workspace-no-tree {
	grid-template-columns: minmax(0, 1fr);
}

.logs-content {
	display: flex;
	flex-direction: column;
	gap: 12px;
	min-width: 0;
}

/* Stats bar */
.logs-stats {
	display: flex;
	align-items: center;
	gap: 12px;
	padding: 10px 16px;
	background: var(--c-surface);
	border: 1px solid var(--c-border);
	border-radius: var(--radius);
	font-size: 13px;
}

.stats-main {
	display: flex;
	align-items: center;
	gap: 12px;
	flex-wrap: wrap;
	margin-right: auto;
	min-width: 0;
}

.stats-route {
	font-weight: 600;
	font-size: 15px;
	color: var(--c-text);
}

.stats-time {
	color: var(--c-text-3);
	font-size: 12px;
}

.stats-count {
	padding: 2px 10px;
	border-radius: 999px;
	background: var(--c-border-light);
	color: var(--c-text-2);
	font-size: 12px;
	font-weight: 600;
	white-space: nowrap;
	flex-shrink: 0;
}

/* Inline detail panel */
.detail-inline {
	padding: 12px;
	max-height: 55vh;
	overflow-y: auto;
}

.detail-inline-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 12px;
	margin-bottom: 10px;
	position: sticky;
	top: 0;
	background: var(--c-surface);
	z-index: 2;
	padding: 4px 0;
}

.detail-inline-label {
	font-size: 13px;
	font-weight: 600;
	color: var(--c-text-2);
}

.log-row-active {
	background: var(--c-primary-bg);
}

.mobile-log-card-active {
	border-color: var(--c-primary);
	box-shadow: 0 0 0 1px var(--c-primary);
}

/* Filters */
.filters-bar {
	display: flex;
	gap: 8px;
	flex-wrap: wrap;
}

.filter-input {
	flex: 1 1 140px;
	min-width: 120px;
	max-width: 220px;
	padding: 6px 10px;
	border: 1px solid var(--c-border);
	border-radius: var(--radius-sm);
	background: var(--c-surface);
	color: var(--c-text);
	font-size: 12px;
	font-weight: 400;
	outline: none;
	transition: border-color 0.15s, box-shadow 0.15s, background-color 0.15s;
}

.filter-input:focus {
	border-color: var(--c-primary);
	box-shadow: 0 0 0 3px var(--c-primary-bg);
}

.filter-input.active {
	border-color: var(--c-primary);
	background: var(--c-primary-bg);
}

/* Table */
.table-wrap {
	overflow: hidden;
	border-radius: var(--radius);
	background: var(--c-surface);
}

.data-table {
	table-layout: fixed;
}

.data-table th {
	position: sticky;
	top: 0;
	z-index: 1;
	vertical-align: middle;
	background: var(--c-surface-soft);
	border-bottom: 1px solid var(--c-border);
	font-size: 11px;
	font-weight: 600;
	letter-spacing: 0.05em;
	text-transform: uppercase;
	color: var(--c-text-2);
	overflow: hidden;
	text-overflow: ellipsis;
}

.data-table th:nth-child(1) { width: 56px; }
.data-table th:nth-child(2) { width: 116px; }
.data-table th:nth-child(3) { width: auto; }
.data-table th:nth-child(4) { width: 120px; }
.data-table th:nth-child(5) { width: 96px; }
.data-table th:nth-child(6) { width: 86px; }
.data-table th:nth-child(7) { width: 150px; }

.th-actions {
	width: 56px;
	min-width: 56px;
}

/* Rows */
.log-row {
	transition: background-color 0.12s;
}

.log-row:hover {
	background: var(--c-surface-tint);
}

.cell-actions {
	width: 56px;
	white-space: nowrap;
}

.cell-time {
	white-space: nowrap;
	font-size: 12px;
	color: var(--c-text-2);
}

.cell-prompt {
	max-width: 320px;
	font-size: 12px;
	color: var(--c-text-2);
}

.cell-prompt-text {
	display: block;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.cell-num {
	white-space: nowrap;
	font-variant-numeric: tabular-nums;
}

/* Route chip */
.row-route-chip {
	display: inline-block;
	max-width: 100%;
	margin-bottom: 3px;
	margin-right: 6px;
	padding: 1px 7px;
	border-radius: 999px;
	background: var(--c-primary-bg);
	color: var(--c-primary);
	font-size: 10px;
	font-weight: 600;
	line-height: 1.4;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	vertical-align: middle;
}

/* Status pills */
.status-pill {
	display: inline-flex;
	align-items: center;
	padding: 3px 10px;
	border-radius: 999px;
	font-size: 11px;
	font-weight: 600;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	max-width: 100%;
}

.status-ok {
	background: var(--c-success-soft);
	color: var(--c-success-text);
}

.status-streaming {
	background: var(--c-primary-bg);
	color: var(--c-primary);
}

.status-warn {
	background: var(--c-warning-bg);
	color: var(--c-warning-text);
}

.status-error {
	background: var(--c-danger-bg);
	color: var(--c-danger-text);
}

/* Empty */
.empty-hint {
	padding: 28px;
	text-align: center;
	color: var(--c-text-3);
}

/* Desktop / Mobile toggle */
.desktop-log-table {
	display: table;
}

.mobile-log-list {
	display: none;
}

/* Mobile cards */
.mobile-log-card {
	padding: 14px;
	border: 1px solid var(--c-border);
}

.mobile-log-top {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 12px;
	margin-bottom: 10px;
}

.mobile-log-time {
	font-size: 12px;
	color: var(--c-text-3);
}

.mobile-log-prompt {
	font-size: 14px;
	font-weight: 600;
	line-height: 1.5;
	margin-bottom: 12px;
	overflow-wrap: anywhere;
}

.mobile-log-meta {
	display: grid;
	grid-template-columns: repeat(2, minmax(0, 1fr));
	gap: 10px;
	margin: 0;
}

.mobile-log-meta dt {
	font-size: 11px;
	color: var(--c-text-3);
	text-transform: uppercase;
	letter-spacing: 0.05em;
	margin-bottom: 4px;
}

.mobile-log-meta dd {
	margin: 0;
	font-size: 13px;
	overflow-wrap: anywhere;
}

/* Responsive */
@media (max-width: 768px) {
	.logs-workspace {
		grid-template-columns: 1fr;
	}

	.desktop-log-table {
		display: none;
	}

	.mobile-log-list {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.logs-stats {
		flex-wrap: wrap;
	}

	.filters-bar {
		gap: 6px;
	}

	.filter-input {
		flex: 1 1 calc(50% - 3px);
		max-width: none;
	}
}
</style>
