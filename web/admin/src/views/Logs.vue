<template>
	<div>
		<div class="header-row">
			<div class="page-heading">
				<div class="section-eyebrow">{{ $t('logs.currentScope') }}</div>
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
				'logs-workspace-tree-collapsed': sessionTreeCollapsed,
				'logs-workspace-no-tree': !routeTree.length,
			}"
		>
			<SessionTreePanel
				:routeTree="routeTree"
				:chainedLogs="chainedLogs"
				:activeTab="activeTab"
				:activeSession="activeSession"
				:sessionTreeCollapsed="sessionTreeCollapsed"
				:collapsedRouteGroups="collapsedRouteGroups"
				:logCount="logs.length"
				:sessionTitlePreview="sessionTitlePreview"
				@select-all="selectAllLogs"
				@select-route="selectRoute"
				@select-session="selectSession"
				@toggle-collapse="toggleSessionTree"
				@toggle-route-group="toggleRouteGroup"
			/>

			<section class="logs-content" :class="scopeStateClass">
				<div class="logs-scope panel">
					<div class="logs-scope-main">
						<div class="section-eyebrow">{{ scopeEyebrow }}</div>
						<h3 class="logs-scope-title">{{ scopeTitle }}</h3>
						<p class="logs-scope-description">{{ scopeDescription }}</p>
					</div>
					<div class="logs-scope-side">
						<div class="logs-scope-pills">
							<span class="scope-pill">
								<span class="scope-pill-label">{{ $t('logs.route') }}</span>
								{{ scopeRouteLabel }}
							</span>
							<span class="scope-pill">
								<span class="scope-pill-label">{{ $t('logs.sessions') }}</span>
								{{ visibleSessionCount }}
							</span>
							<span v-if="scopeWindowLabel" class="scope-pill">
								<span class="scope-pill-label">{{ $t('logs.time') }}</span>
								{{ scopeWindowLabel }}
							</span>
							<span class="scope-pill scope-pill-strong">{{ filteredLogCount }} {{ $t('logs.reqs') }}</span>
						</div>
					</div>
				</div>

				<div class="table-wrap panel">
			<table v-if="logs.length" class="data-table desktop-log-table">
				<thead>
					<tr>
						<th class="th-toggle">
							<div class="th-col">
								<span>{{ $t('logs.actions') }}</span>
							</div>
						</th>
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
									:aria-label="$t('logs.filterPrompt')"
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
									:aria-label="$t('logs.filterModel')"
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
									:aria-label="$t('logs.filterProvider')"
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
									:aria-label="$t('logs.filterStatus')"
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
							:class="rowClass(chain.displayLogs[0])"
						>
							<td class="cell-actions">
								<button
									class="btn btn-secondary btn-sm action-btn"
									type="button"
									:aria-label="$t('logs.viewLogDetails')"
									@click="showDetail(chain.displayLogs[0], $event.currentTarget)"
								>
									{{ $t('logs.view') }}
								</button>
							</td>
							<td>{{ formatTime(chain.displayLogs[0].timestamp) }}</td>
							<td class="cell-prompt">
								<span v-if="showRowRouteMeta" class="row-route-chip">{{ routeName(chain.displayLogs[0]?.route) }}</span>
								<span class="cell-prompt-text">{{ lastUserPreview(chain.displayLogs[0]) }}</span>
							</td>
							<td>{{ chain.displayLogs[0].model }}</td>
							<td>{{ chain.displayLogs[0].provider }}</td>
							<td>{{ formatDuration(chain.displayLogs[0].duration_ms) }}</td>
							<td>{{ statusText(chain.displayLogs[0]) }}</td>
						</tr>
						<!-- multi-request chain -->
						<template v-else>
							<tr
								class="row-chain-head"
								:class="chainRowClass(chain)"
							>
								<td class="cell-toggle">
									<button
										class="btn btn-secondary btn-sm toggle-btn"
										type="button"
										:aria-expanded="expandedChains.has(chain.id)"
										:aria-label="expandedChains.has(chain.id) ? $t('logs.collapseSession') : $t('logs.expandSession')"
										@click="toggleChain(chain.id)"
									>
										<span class="toggle-icon">{{
											expandedChains.has(chain.id) ? "\u25BC" : "\u25B6"
										}}</span>
										<span class="sr-only">
											{{ expandedChains.has(chain.id) ? $t('logs.collapseSession') : $t('logs.expandSession') }}
										</span>
									</button>
								</td>
								<td>{{ formatTime(chain.displayLogs[0].timestamp) }}</td>
								<td class="cell-prompt">
									<span v-if="showRowRouteMeta" class="row-route-chip">{{ routeName(chain.displayLogs[0]?.route) }}</span>
									<span class="cell-prompt-text">{{ lastUserPreview(chain.displayLogs[0]) }}</span>
								</td>
								<td>{{ chain.displayLogs[0].model }}</td>
								<td>-</td>
								<td>{{ formatDuration(chainTotalDuration(chain)) }}</td>
								<td>
									<span class="badge badge-chain"
										>{{ chain.displayLogs.length }} {{ $t('logs.reqs') }}</span
									>
									{{ !chainStatus(chain).isOk ? " \u00B7 " + chainStatus(chain).text : "" }}
								</td>
							</tr>
							<template v-if="expandedChains.has(chain.id)">
								<tr
									v-for="(log, idx) in chain.displayLogs"
									:key="log.request_id"
									class="row-chain-child"
									:class="childRowClass(log)"
								>
									<td class="cell-chain-indent">
										<span
											class="chain-line"
											:class="{
												'chain-line-last': idx === chain.displayLogs.length - 1,
											}"
										></span>
										<button
											class="btn btn-secondary btn-sm action-btn chain-detail-btn"
											type="button"
											:aria-label="$t('logs.viewLogDetails')"
											@click="showDetail(log, $event.currentTarget)"
										>
											{{ $t('logs.view') }}
										</button>
									</td>
									<td>{{ formatTime(log.timestamp) }}</td>
									<td class="cell-prompt">
										<span v-if="showRowRouteMeta" class="row-route-chip">{{ routeName(log.route) }}</span>
										<span class="cell-prompt-text">{{ lastUserPreview(log) }}</span>
									</td>
									<td>{{ log.model }}</td>
									<td>{{ log.provider }}</td>
									<td>{{ formatDuration(log.duration_ms) }}</td>
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

			<div v-if="logs.length" class="mobile-log-list">
				<template v-for="chain in filteredChains" :key="chain.id + '-mobile'">
					<article class="mobile-log-card panel" :class="mobileCardClass(chain)">
						<div class="mobile-log-top">
							<div class="mobile-log-time">{{ formatTime(chain.displayLogs[0].timestamp) }}</div>
							<div class="mobile-log-actions">
								<button
									v-if="chain.displayLogs.length > 1"
									class="btn btn-secondary btn-sm"
									type="button"
									:aria-expanded="expandedChains.has(chain.id)"
									@click="toggleChain(chain.id)"
								>
									{{ expandedChains.has(chain.id) ? $t('logs.collapse') : $t('logs.expand') }}
								</button>
								<button
									v-else
									class="btn btn-secondary btn-sm"
									type="button"
									@click="showDetail(chain.displayLogs[0], $event.currentTarget)"
								>
									{{ $t('logs.view') }}
								</button>
							</div>
						</div>
						<div class="mobile-log-prompt">{{ lastUserPreview(chain.displayLogs[0]) || $t('logs.noPrompt') }}</div>
						<dl class="mobile-log-meta">
							<div>
								<dt>{{ $t('logs.model') }}</dt>
								<dd>{{ chain.displayLogs[0].model || "\u2014" }}</dd>
							</div>
							<div>
								<dt>{{ $t('logs.provider') }}</dt>
								<dd>{{ chain.displayLogs[0].provider || "\u2014" }}</dd>
							</div>
							<div>
								<dt>{{ $t('logs.duration') }}</dt>
								<dd>{{ formatDuration(chain.displayLogs.length > 1 ? chainTotalDuration(chain) : chain.displayLogs[0].duration_ms) }}</dd>
							</div>
							<div>
								<dt>{{ $t('logs.status') }}</dt>
								<dd>
									<span v-if="chain.displayLogs.length > 1" class="badge badge-chain">
										{{ chain.displayLogs.length }} {{ $t('logs.reqs') }}
									</span>
									<span v-if="chain.displayLogs.length > 1">{{ !chainStatus(chain).isOk ? chainStatus(chain).text : $t('common.ok') }}</span>
									<span v-else>{{ statusText(chain.displayLogs[0]) }}</span>
								</dd>
							</div>
						</dl>
						<div v-if="chain.displayLogs.length > 1 && expandedChains.has(chain.id)" class="mobile-log-children">
							<button
								v-for="log in chain.displayLogs"
								:key="log.request_id + '-child-mobile'"
								class="mobile-log-child"
								type="button"
								@click="showDetail(log, $event.currentTarget)"
							>
								<span class="mobile-log-child-time">{{ formatTime(log.timestamp) }}</span>
								<span class="mobile-log-child-main">{{ lastUserPreview(log) || $t('logs.noPrompt') }}</span>
								<span class="mobile-log-child-status">{{ statusText(log) }}</span>
							</button>
						</div>
					</article>
				</template>
				<div v-if="!filteredChains.length" class="empty-hint">
					{{ hasFilters ? $t('logs.noMatchingLogs') : $t('logs.noLogsYet') }}
				</div>
			</div>
			<div v-else class="empty-hint">
				{{ hasFilters ? $t('logs.noMatchingLogs') : $t('logs.noLogsYet') }}
			</div>
				</div>
			</section>
		</div>

		<!-- Detail Modal -->
		<LogDetailModal
			:log="selected"
			:lastUserPreview="lastUserPreview"
			@close="closeDetail"
		/>
	</div>
</template>

<script setup>
import { ref, computed, watch } from "vue";
import { useI18n } from "vue-i18n";
import { formatDuration } from "../utils.js";
import { useLogStream } from "../composables/useLogStream.js";
import { useSessionChaining } from "../composables/useSessionChaining.js";
import SessionTreePanel from "../components/SessionTreePanel.vue";
import LogDetailModal from "../components/LogDetailModal.vue";

const { t, locale } = useI18n();

// --- Log stream ---
const { logs, paused, error, togglePause, clearLogs } = useLogStream();

// --- Session chaining ---
const {
	chainedLogs,
	lastUserPreview,
	sessionTitlePreview,
	chainTotalDuration,
	failoverCount,
	isRecoveredByFailover,
	chainStatus,
	getTimestampMs,
} = useSessionChaining(logs);

// --- UI state ---
const selected = ref(null);
const filters = ref({ prompt: "", model: "", provider: "", status: "" });
const activeTab = ref("");
const activeSession = ref(null);
const sessionTreeCollapsed = ref(false);
const collapsedRouteGroups = ref(new Set());
const expandedChains = ref(new Set());

// --- Derived state ---
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

const routeTree = computed(() => routeKeys.value.map((key) => ({
	key,
	chains: chainedLogs.value.filter((chain) => (chain.logs[0]?.route || "(unknown)") === key),
})));

const hasFilters = computed(() =>
	Object.values(filters.value).some((v) => v.trim()),
);

const activeSessionChain = computed(() => {
	if (!activeSession.value) return null;
	return chainedLogs.value.find((item) => item.id === activeSession.value) || null;
});

const visibleSessionCount = computed(() => filteredChains.value.length);

const visibleRouteCount = computed(() => {
	const seen = new Set();
	for (const chain of filteredChains.value) {
		seen.add(routeName(chain.logs[0]?.route));
	}
	return seen.size;
});

const showRowRouteMeta = computed(() =>
	!activeTab.value && !activeSession.value && visibleRouteCount.value > 1,
);

const filteredLogCount = computed(() =>
	filteredChains.value.reduce((sum, chain) => sum + chain.displayLogs.length, 0),
);

// --- Scope bar ---
const scopeEyebrow = computed(() => {
	if (activeSession.value) return t("logs.selectedSession");
	if (activeTab.value) return t("logs.selectedRoute");
	return t("logs.currentScope");
});

const scopeRouteLabel = computed(() => {
	if (activeSessionChain.value) return routeName(activeSessionChain.value.logs[0]?.route);
	if (activeTab.value) return routeName(activeTab.value);
	return t("logs.allRoutes");
});

const scopeWindowLabel = computed(() => {
	const rangeLogs = filteredChains.value.flatMap((chain) => chain.displayLogs);
	if (!rangeLogs.length) return "";
	const sorted = [...rangeLogs].sort((a, b) => getTimestampMs(a) - getTimestampMs(b));
	const start = formatTime(sorted[0].timestamp);
	const end = formatTime(sorted[sorted.length - 1].timestamp);
	if (!start) return end;
	if (!end || start === end) return start;
	return `${start} - ${end}`;
});

const scopeDescription = computed(() => {
	if (activeSessionChain.value) {
		return t("logs.scopeHintSession", { route: scopeRouteLabel.value });
	}
	if (activeTab.value) {
		return t("logs.scopeHintRoute", { route: scopeRouteLabel.value });
	}
	return t("logs.scopeHintAll");
});

const scopeStateClass = computed(() => ({
	"logs-content-scoped": Boolean(activeTab.value || activeSession.value),
	"logs-content-session": Boolean(activeSession.value),
}));

const scopeTitle = computed(() => {
	if (activeSessionChain.value) return sessionName(activeSessionChain.value);
	if (activeTab.value) return activeTab.value;
	return t("logs.allRequests");
});

// --- Filtering ---
function colMatch(query, ...fields) {
	if (!query) return true;
	const q = query.toLowerCase();
	return fields.some((f) => (f || "").toLowerCase().includes(q));
}

const filteredChains = computed(() => {
	const f = filters.value;
	const tab = activeTab.value;

	if (activeSession.value) {
		const chain = chainedLogs.value.find((c) => c.id === activeSession.value);
		if (!chain) return [];
		if (!hasFilters.value) return [{ ...chain, displayLogs: chain.logs }];
		const matchingLogs = chain.logs.filter((log) => {
			const prompt = lastUserPreview(log);
			const status = log.error || t("common.ok");
			return (
				colMatch(f.prompt, prompt) &&
				colMatch(f.model, log.model) &&
				colMatch(f.provider, log.provider) &&
				colMatch(f.status, status)
			);
		});
		return matchingLogs.length ? [{ ...chain, displayLogs: matchingLogs }] : [];
	}

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
			const status = log.error || t("common.ok");
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

// --- Actions ---
function formatTime(t) {
	if (!t) return "";
	const date = new Date(t);
	if (Number.isNaN(date.getTime())) return "";
	return new Intl.DateTimeFormat(locale.value, {
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		second: "2-digit",
	}).format(date);
}

function routeName(route) {
	if (typeof route === "string" && route.trim()) return route;
	return t("logs.unknown");
}

function sessionName(chain) {
	const preview = sessionTitlePreview(chain);
	return preview || formatTime(chain.logs[0].timestamp);
}

function handleClear() {
	clearLogs();
	selected.value = null;
	activeSession.value = null;
	expandedChains.value = new Set();
	collapsedRouteGroups.value = new Set();
}

function showDetail(log, trigger = null) {
	selected.value = log;
}

function closeDetail() {
	selected.value = null;
}

function selectAllLogs() {
	activeTab.value = "";
	activeSession.value = null;
}

function toggleSessionTree() {
	sessionTreeCollapsed.value = !sessionTreeCollapsed.value;
}

function toggleRouteGroup(routeKey) {
	if (collapsedRouteGroups.value.has(routeKey)) {
		collapsedRouteGroups.value.delete(routeKey);
	} else {
		collapsedRouteGroups.value.add(routeKey);
	}
	collapsedRouteGroups.value = new Set(collapsedRouteGroups.value);
}

function expandRouteGroup(routeKey) {
	if (!collapsedRouteGroups.value.has(routeKey)) return;
	collapsedRouteGroups.value.delete(routeKey);
	collapsedRouteGroups.value = new Set(collapsedRouteGroups.value);
}

function selectRoute(routeKey) {
	sessionTreeCollapsed.value = false;
	expandRouteGroup(routeKey);
	activeTab.value = activeTab.value === routeKey && activeSession.value === null ? "" : routeKey;
	activeSession.value = null;
}

function selectSession(chain, routeKey) {
	sessionTreeCollapsed.value = false;
	expandRouteGroup(routeKey);
	activeTab.value = routeKey;
	activeSession.value = activeSession.value === chain.id ? null : chain.id;
}

function toggleChain(chainId) {
	if (expandedChains.value.has(chainId)) {
		expandedChains.value.delete(chainId);
	} else {
		expandedChains.value.add(chainId);
	}
	expandedChains.value = new Set(expandedChains.value);
}

// --- Row helpers ---
function hasRejectedVerdict(log) {
	return Array.isArray(log?.tool_verdicts) && log.tool_verdicts.some((v) => v.rejected);
}

function rowClass(log) {
	return {
		"row-error": Boolean(log?.error),
		"row-warn": isRecoveredByFailover(log) || hasRejectedVerdict(log),
	};
}

function childRowClass(log) {
	return {
		"row-error": Boolean(log?.error),
		"row-warn": isRecoveredByFailover(log) || hasRejectedVerdict(log),
	};
}

function chainRowClass(chain) {
	return {
		"row-error": chain.displayLogs.some((log) => log.error),
		"row-warn": chain.displayLogs.some((log) => isRecoveredByFailover(log) || hasRejectedVerdict(log)),
	};
}

function mobileCardClass(chain) {
	if (chain.displayLogs.some((log) => log.error)) return "mobile-log-card-error";
	if (chain.displayLogs.some((log) => isRecoveredByFailover(log) || hasRejectedVerdict(log))) return "mobile-log-card-warn";
	return "";
}

function statusText(log) {
	if (log.pending) return t("logs.streaming");
	if (log.error) return log.error;
	const parts = [];
	if (hasRejectedVerdict(log)) {
		parts.push(t("logs.toolRejected"));
	}
	const recoveredFailovers = failoverCount(log);
	if (recoveredFailovers > 0) {
		parts.push(t("logs.failoverRecovered", { n: recoveredFailovers }));
	}
	const steps = log.steps?.length;
	if (steps) parts.push(t("logs.steps", { n: steps }));
	return parts.length ? t("common.ok") + " \u00B7 " + parts.join(" \u00B7 ") : t("common.ok");
}

// --- Watchers ---

// Clear session when route tab changes
watch(activeTab, () => { activeSession.value = null; });

// Auto-expand selected session
watch(activeSession, (id) => {
	if (!id) return;
	expandedChains.value = new Set([...expandedChains.value, id]);
	const chain = chainedLogs.value.find((item) => item.id === id);
	const routeKey = chain?.logs[0]?.route || "(unknown)";
	sessionTreeCollapsed.value = false;
	expandRouteGroup(routeKey);
});

// Prune stale collapsed route groups
watch(routeKeys, (keys) => {
	const next = new Set();
	for (const key of collapsedRouteGroups.value) {
		if (keys.includes(key)) next.add(key);
	}
	if (next.size !== collapsedRouteGroups.value.size) {
		collapsedRouteGroups.value = next;
	}
});

// Clear stale activeSession after truncation
watch(chainedLogs, (chains) => {
	if (activeSession.value && !chains.some((c) => c.id === activeSession.value)) {
		activeSession.value = null;
	}
});

// Update selected log when the same request_id is updated
watch(logs, () => {
	if (selected.value) {
		const updated = logs.value.find((l) => l.request_id === selected.value.request_id);
		if (updated && updated !== selected.value) {
			selected.value = updated;
		}
	}
}, { deep: false });
</script>

<style scoped>
/* Header layout */
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

.header-row .page-title {
	margin-bottom: 0;
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
	justify-content: flex-end;
	gap: 10px;
	flex-wrap: wrap;
	margin-left: auto;
}

.logs-workspace {
	display: grid;
	grid-template-columns: minmax(260px, 320px) minmax(0, 1fr);
	gap: 20px;
	align-items: start;
}

.logs-workspace-tree-collapsed {
	grid-template-columns: 88px minmax(0, 1fr);
}

.logs-workspace-no-tree {
	grid-template-columns: minmax(0, 1fr);
}

.logs-content {
	display: flex;
	flex-direction: column;
	gap: 14px;
	min-width: 0;
}

.logs-scope {
	display: flex;
	align-items: stretch;
	justify-content: space-between;
	gap: 16px;
	padding: 16px 18px;
	background: linear-gradient(180deg, color-mix(in srgb, var(--c-surface-tint) 76%, white) 0%, var(--c-surface) 100%);
	border-color: color-mix(in srgb, var(--c-primary) 18%, var(--c-border));
}

.logs-scope-main {
	display: flex;
	flex-direction: column;
	gap: 6px;
	min-width: 0;
}

.logs-scope-title {
	margin: 0;
	font-size: 18px;
	line-height: 1.25;
}

.logs-scope-description {
	font-size: 13px;
	line-height: 1.55;
	color: var(--c-text-2);
	max-width: 58ch;
}

.logs-scope-side {
	display: flex;
	align-items: flex-start;
	justify-content: flex-end;
}

.logs-scope-pills {
	display: flex;
	flex-wrap: wrap;
	justify-content: flex-end;
	gap: 8px;
	max-width: 420px;
}

.scope-pill {
	display: inline-flex;
	align-items: center;
	gap: 6px;
	padding: 6px 10px;
	border-radius: 999px;
	border: 1px solid var(--c-border);
	background: var(--c-surface);
	color: var(--c-text-2);
	font-size: 12px;
	line-height: 1.4;
	white-space: nowrap;
}

.scope-pill-label {
	font-size: 11px;
	font-weight: 700;
	letter-spacing: 0.04em;
	text-transform: uppercase;
	color: var(--c-text-3);
}

.scope-pill-strong {
	background: var(--c-primary);
	border-color: transparent;
	color: var(--c-text-inverse);
}

.section-eyebrow {
	font-size: 11px;
	font-weight: 700;
	letter-spacing: 0.08em;
	text-transform: uppercase;
	color: var(--c-text-3);
	margin-bottom: 4px;
}

/* Table container */
.table-wrap {
	overflow: hidden;
	border-radius: var(--radius);
	background: var(--c-surface);
}

.logs-content-scoped .table-wrap {
	border-color: color-mix(in srgb, var(--c-primary) 18%, var(--c-border));
}

.logs-content-session .row-chain-head {
	background: color-mix(in srgb, var(--c-primary-bg) 52%, white);
}

.logs-content-session .row-chain-child {
	background: color-mix(in srgb, var(--c-primary-bg) 22%, var(--c-bg));
}

.data-table th {
	position: sticky;
	top: 0;
	z-index: 1;
	vertical-align: top;
}

/* Column header layout */
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
	padding: 6px 8px;
	border: 1px solid var(--c-border);
	border-radius: var(--radius-sm);
	background: var(--c-surface);
	color: var(--c-text);
	font-size: 12px;
	font-weight: 400;
	outline: none;
	box-sizing: border-box;
	transition: border-color 0.15s, box-shadow 0.15s, background-color 0.15s;
}
.col-filter:focus {
	border-color: var(--c-primary);
	box-shadow: 0 0 0 3px var(--c-primary-bg);
}
.col-filter.active {
	border-color: var(--c-primary);
	background: var(--c-primary-bg);
}

.badge {
	font-size: 11px;
	font-weight: 600;
	padding: 1px 7px;
	border-radius: 10px;
	background: var(--c-border-light);
}
.desktop-log-table {
	display: table;
}
.mobile-log-list {
	display: none;
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
.row-warn {
	background: var(--c-warning-bg);
}
.desktop-log-table tbody tr:hover {
	background: var(--c-surface-tint);
}
.desktop-log-table tbody tr.row-warn:hover {
	background: color-mix(in srgb, var(--c-warning-bg) 82%, var(--c-warning) 18%);
}
.desktop-log-table tbody tr.row-error:hover {
	background: color-mix(in srgb, var(--c-danger-bg) 82%, var(--c-danger) 18%);
}

/* Chain grouping */
.th-toggle {
	width: 88px;
	min-width: 88px;
	max-width: 88px;
}
.cell-toggle {
	text-align: left;
	width: 88px;
	padding-right: 0;
}
.cell-actions {
	width: 88px;
}
.toggle-icon {
	font-size: 10px;
	color: var(--c-text-3);
	user-select: none;
}
.toggle-btn,
.action-btn {
	min-width: 72px;
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
	color: var(--c-text-inverse);
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
.row-chain-child.row-warn {
	background: var(--c-warning-bg);
	border-left-color: var(--c-warning);
}
.cell-chain-indent {
	position: relative;
	width: 88px;
	padding: 0 8px 0 0 !important;
}
.chain-line {
	position: absolute;
	left: 18px;
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
	width: 10px;
	height: 2px;
	background: var(--c-border-light);
}
.chain-line-last {
	bottom: 50%;
}
.chain-detail-btn {
	position: relative;
	z-index: 1;
	margin-left: 22px;
}
.cell-prompt {
	max-width: 260px;
	font-size: 12px;
	color: var(--c-text-2);
}

.cell-prompt-text {
	display: block;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.row-route-chip {
	display: inline-flex;
	align-items: center;
	max-width: 100%;
	margin-bottom: 6px;
	padding: 2px 8px;
	border-radius: 999px;
	background: color-mix(in srgb, var(--c-primary-bg) 72%, white);
	color: var(--c-accent-text);
	font-size: 11px;
	font-weight: 600;
	line-height: 1.35;
}

.sr-only {
	position: absolute;
	width: 1px;
	height: 1px;
	padding: 0;
	margin: -1px;
	overflow: hidden;
	clip: rect(0, 0, 0, 0);
	white-space: nowrap;
	border: 0;
}

.mobile-log-card {
	padding: 14px;
	border: 1px solid var(--c-border);
	box-shadow: none;
}
.mobile-log-card-error {
	border-color: color-mix(in srgb, var(--c-danger) 35%, var(--c-border));
	background: color-mix(in srgb, var(--c-danger-bg) 68%, var(--c-surface));
}
.mobile-log-card-warn {
	border-color: color-mix(in srgb, var(--c-warning) 35%, var(--c-border));
	background: color-mix(in srgb, var(--c-warning-bg) 68%, var(--c-surface));
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
.mobile-log-actions {
	display: flex;
	gap: 8px;
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
.mobile-log-meta div {
	min-width: 0;
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
	display: flex;
	flex-wrap: wrap;
	gap: 6px;
	align-items: center;
}
.mobile-log-children {
	margin-top: 14px;
	padding-top: 12px;
	border-top: 1px solid var(--c-border-light);
	display: flex;
	flex-direction: column;
	gap: 8px;
}
.mobile-log-child {
	display: grid;
	grid-template-columns: 88px 1fr;
	gap: 6px 10px;
	padding: 10px 12px;
	background: var(--c-surface);
	border: 1px solid var(--c-border);
	border-radius: var(--radius);
	text-align: left;
	cursor: pointer;
}
.mobile-log-child-time,
.mobile-log-child-status {
	font-size: 12px;
	color: var(--c-text-3);
}
.mobile-log-child-main {
	font-size: 13px;
	font-weight: 500;
	overflow-wrap: anywhere;
}

@media (max-width: 768px) {
	.header-row {
		align-items: flex-start;
		gap: 12px;
		justify-content: flex-start;
	}

	.header-actions {
		width: 100%;
		justify-content: stretch;
	}

	.header-actions .btn {
		flex: 1 1 140px;
	}

	.logs-workspace {
		grid-template-columns: 1fr;
	}

	.logs-workspace-tree-collapsed {
		grid-template-columns: 1fr;
	}

	.table-wrap {
		overflow: visible;
		-webkit-overflow-scrolling: touch;
		background: transparent;
		border: none;
		box-shadow: none;
		max-height: none;
	}

	.desktop-log-table {
		display: none;
	}

	.mobile-log-list {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.logs-scope {
		flex-direction: column;
		padding: 14px;
	}

	.logs-scope-side {
		width: 100%;
		justify-content: flex-start;
	}

	.logs-scope-pills {
		justify-content: flex-start;
		max-width: none;
	}

	.mobile-log-meta {
		grid-template-columns: 1fr;
	}

	.mobile-log-child {
		grid-template-columns: 1fr;
	}
}
</style>
