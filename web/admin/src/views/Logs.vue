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

		<div class="logs-workspace" :class="{ 'logs-workspace-tree-collapsed': sessionTreeCollapsed }">
			<aside
				v-if="routeTree.length"
				class="session-tree-panel panel"
				:class="{ collapsed: sessionTreeCollapsed }"
			>
				<div class="session-tree-header">
					<div>
						<div class="section-eyebrow">{{ $t('logs.sessions') }}</div>
						<h3 class="session-tree-title">{{ $t('logs.sessionExplorer') }}</h3>
					</div>
					<div class="session-tree-actions">
						<span class="badge">{{ chainedLogs.length }}</span>
						<button
							class="btn btn-secondary btn-sm session-tree-collapse-btn"
							type="button"
							:aria-expanded="!sessionTreeCollapsed"
							:aria-label="sessionTreeCollapsed ? $t('logs.expandSessionTree') : $t('logs.collapseSessionTree')"
							@click="toggleSessionTree"
						>
							{{ sessionTreeCollapsed ? "▶" : "◀" }}
						</button>
					</div>
				</div>

				<template v-if="!sessionTreeCollapsed">
					<button
						class="tree-root-button"
						:class="{ active: activeTab === '' && activeSession === null }"
						type="button"
						@click="selectAllLogs"
					>
						<span class="tree-root-title">{{ $t('logs.allRequests') }}</span>
						<span class="tree-root-meta">{{ logs.length }} {{ $t('logs.reqs') }}</span>
					</button>

					<div class="tree-scroll" role="tree" :aria-label="$t('logs.sessionExplorer')">
					<section
						v-for="group in routeTree"
						:key="group.key"
						class="route-branch"
					>
						<div class="route-branch-header">
							<button
								class="route-branch-button"
								:class="{ active: activeTab === group.key && activeSession === null }"
								type="button"
								@click="selectRoute(group.key)"
							>
								<span class="route-branch-label">{{ group.key }}</span>
								<span class="badge">{{ group.chains.length }}</span>
							</button>
							<button
								class="route-branch-toggle"
								type="button"
								:aria-expanded="isRouteGroupExpanded(group.key)"
								:aria-label="isRouteGroupExpanded(group.key) ? $t('logs.collapseRouteGroup') : $t('logs.expandRouteGroup')"
								@click="toggleRouteGroup(group.key)"
							>
								{{ isRouteGroupExpanded(group.key) ? "−" : "+" }}
							</button>
						</div>

						<ul v-if="isRouteGroupExpanded(group.key)" class="session-tree-list" role="group">
							<li
								v-for="chain in group.chains"
								:key="chain.id"
								class="session-tree-item"
							>
								<button
									class="session-node-button"
									:class="{ active: activeSession === chain.id }"
									type="button"
									:aria-pressed="activeSession === chain.id"
									@click="selectSession(chain, group.key)"
								>
									<span class="session-node-main">
										<span class="session-node-title">{{ sessionName(chain) }}</span>
										<span class="session-node-meta">
											{{ formatTime(chain.logs[0].timestamp) }} · {{ chain.logs.length }} {{ $t('logs.reqs') }}
										</span>
									</span>
								</button>
							</li>
						</ul>
					</section>
					</div>
				</template>
			</aside>

			<section class="logs-content">
				<div class="logs-scope">
					<div>
						<div class="section-eyebrow">{{ activeSession ? $t('logs.selectedSession') : $t('logs.currentScope') }}</div>
						<h3 class="logs-scope-title">{{ scopeTitle }}</h3>
					</div>
					<div class="logs-scope-meta">
						<span class="badge">{{ filteredLogCount }} {{ $t('logs.reqs') }}</span>
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
							<td class="cell-prompt">{{ lastUserPreview(chain.displayLogs[0]) }}</td>
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
											expandedChains.has(chain.id) ? "▼" : "▶"
										}}</span>
										<span class="sr-only">
											{{ expandedChains.has(chain.id) ? $t('logs.collapseSession') : $t('logs.expandSession') }}
										</span>
									</button>
								</td>
								<td>{{ formatTime(chain.displayLogs[0].timestamp) }}</td>
								<td class="cell-prompt">{{ lastUserPreview(chain.displayLogs[0]) }}</td>
								<td>{{ chain.displayLogs[0].model }}</td>
								<td>-</td>
								<td>{{ formatDuration(chainTotalDuration(chain)) }}</td>
								<td>
									<span class="badge badge-chain"
										>{{ chain.displayLogs.length }} {{ $t('logs.reqs') }}</span
									>
									{{ chainStatus(chain) !== $t('common.ok') ? " · " + chainStatus(chain) : "" }}
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
									<td class="cell-prompt">{{ lastUserPreview(log) }}</td>
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
								<dd>{{ chain.displayLogs[0].model || "—" }}</dd>
							</div>
							<div>
								<dt>{{ $t('logs.provider') }}</dt>
								<dd>{{ chain.displayLogs[0].provider || "—" }}</dd>
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
									<span>{{ chain.displayLogs.length > 1 ? chainStatus(chain) : statusText(chain.displayLogs[0]) }}</span>
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
		<div v-if="selected" class="modal-overlay" @click.self="closeDetail">
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
						<button class="btn btn-secondary btn-sm" @click="copyJSON">{{ copied ? '✓' : $t('common.copy') }}</button>
						<button ref="closeButtonRef" class="btn btn-secondary btn-sm" @click="closeDetail">{{ $t('common.close') }}</button>
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
								<span class="detail-status-pill" :class="responseClass(selected)">
									{{ selected.error || responseStatusText(selected) }}
								</span>
							</div>

							<div class="detail-meta-grid">
								<div class="detail-meta-item">
									<span>{{ $t('logs.requestId') }}</span>
									<code>{{ selected.request_id }}</code>
								</div>
								<div class="detail-meta-item">
									<span>{{ $t('logs.route') }}</span>
									<code>{{ selected.route }}</code>
								</div>
								<div class="detail-meta-item">
									<span>{{ $t('logs.model') }}</span>
									<strong>{{ selected.model }}</strong>
								</div>
								<div class="detail-meta-item">
									<span>{{ $t('logs.provider') }}</span>
									<strong>{{ selected.provider }}</strong>
								</div>
								<div class="detail-meta-item">
									<span>{{ $t('logs.duration') }}</span>
									<strong>{{ formatDuration(selected.duration_ms) }}</strong>
								</div>
								<div v-if="selected.fingerprint" class="detail-meta-item detail-meta-item-wide">
									<span>{{ $t('logs.session') }}</span>
									<code class="fp-str">{{ selected.fingerprint }}</code>
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

										<!-- text preview (system gets single-line truncated style) -->
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
											<pre class="code-block code-block-raw">{{ renderEscapes(formatJSON(selected.request)) }}</pre>
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

							<div class="response-block" :class="responseClass(selected)">
								<span class="response-status">{{ selected.error || responseStatusText(selected) }}</span>
								<div v-if="selected.response" class="response-pane">
									<div class="pane-label">{{ $t('logs.response') }}</div>
									<!-- tool_use blocks from Anthropic response -->
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
										<pre class="code-block code-block-raw">{{ renderEscapes(formatJSON(selected.response)) }}</pre>
									</details>
								</div>
							</div>
						</section>
					</div>
				</div>
			</div>
		</div>
	</div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from "vue";
import { useI18n } from "vue-i18n";
import { createLogStream } from "../api.js";
import { formatDuration } from "../utils.js";

const { t, locale } = useI18n();
const logs = ref([]);
const paused = ref(false);
const error = ref("");
const modalRef = ref(null);
const closeButtonRef = ref(null);
const selected = ref(null);
const detailView = ref("timeline"); // "timeline" | "json"
const copied = ref(false);
let copyTimer = null;
const filters = ref({ prompt: "", model: "", provider: "", status: "" });
let stopStream = null;
const MAX_LOGS = 500;
const detailTitleId = "log-detail-title";
let lastFocusedElement = null;
let autoScrollFrame = 0;
let flushFrame = 0;
let pendingLogs = [];
const requestIndexMap = new Map();

const activeTab = ref("");
const activeSession = ref(null);
const sessionTreeCollapsed = ref(false);
const collapsedRouteGroups = ref(new Set());

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

function formatJSON(data) {
	if (!data) return "";
	try {
		const obj = typeof data === "string" ? JSON.parse(data) : data;
		return JSON.stringify(obj, null, 2);
	} catch {
		return String(data);
	}
}

function maybeParseJSONObjectString(value) {
	if (typeof value !== "string") return value;
	const trimmed = value.trim();
	if (!trimmed) return value;
	if (!(trimmed.startsWith("{") || trimmed.startsWith("["))) return value;
	try {
		return JSON.parse(trimmed);
	} catch {
		return value;
	}
}

function normalizeLogJSON(value) {
	if (Array.isArray(value)) {
		return value.map((item) => normalizeLogJSON(item));
	}
	if (!value || typeof value !== "object") {
		return maybeParseJSONObjectString(value);
	}

	const normalized = {};
	for (const [key, raw] of Object.entries(value)) {
		const parsed = maybeParseJSONObjectString(raw);
		normalized[key] = parsed && typeof parsed === "object"
			? normalizeLogJSON(parsed)
			: parsed;
	}
	return normalized;
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

function closeDetail() {
	selected.value = null;
}

function showDetail(log, trigger = null) {
	lastFocusedElement = trigger instanceof HTMLElement ? trigger : document.activeElement;
	selected.value = log;
	detailView.value = log.error ? "json" : "timeline";
}

function selectAllLogs() {
	activeTab.value = "";
	activeSession.value = null;
}

function toggleSessionTree() {
	sessionTreeCollapsed.value = !sessionTreeCollapsed.value;
}

function isRouteGroupExpanded(routeKey) {
	return !collapsedRouteGroups.value.has(routeKey);
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

function mobileCardClass(chain) {
	if (chain.displayLogs.some((log) => log.error)) return "mobile-log-card-error";
	if (chain.displayLogs.some((log) => isRecoveredByFailover(log))) return "mobile-log-card-warn";
	return "";
}

function resetLogs() {
	logs.value = [];
	requestIndexMap.clear();
	selected.value = null;
	activeSession.value = null;
	expandedChains.value = new Set();
	collapsedRouteGroups.value = new Set();
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
	pendingLogs = [];
	resetLogs();
}

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
	if (selected.value?.request_id === log.request_id) {
		selected.value = log;
	}
	if (logs.value.length > MAX_LOGS) {
		logs.value = logs.value.slice(-MAX_LOGS);
		rebuildRequestIndex();
	}
}

const selectedJSON = computed(() => {
	if (!selected.value) return "";
	return JSON.stringify(normalizeLogJSON(selected.value), null, 2);
});

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
		const sysText = Array.isArray(req.system)
			? req.system.filter((b) => b.type === "text").map((b) => b.text).join(" ")
			: req.system;
		nodes.push({
			role: "system",
			raw: { role: "system", content: req.system },
			toolCalls: null,
			toolCallId: "",
			preview: truncate(sysText.replace(/\s+/g, " "), 60),
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
	// Anthropic: has "system" field (string or array) OR messages with content blocks containing tool_use/tool_result
	if (typeof req.system === "string" || Array.isArray(req.system)) return "anthropic";
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
		// support OpenAI/Responses/Anthropic text-like parts
		const text = c
			.filter((part) => ["text", "input_text", "output_text"].includes(part?.type) && typeof part.text === "string")
			.map((part) => part.text)
			.join(" ");
		if (text) return truncate(text, 120);
		const types = [...new Set(c.map((p) => p.type))];
		return "[" + types.join(", ") + "]";
	}
	return "";
}

function extractUserMessageText(message) {
	if (message == null) return "";
	if (typeof message === "string") return message;

	const content = message.content;
	if (typeof content === "string") return content;
	if (Array.isArray(content)) {
		const text = content
			.filter((part) => ["text", "input_text", "output_text"].includes(part?.type) && typeof part.text === "string")
			.map((part) => part.text)
			.join(" ");
		if (text) return text;
	}

	if (Array.isArray(message.input_text)) {
		const text = message.input_text
			.filter((part) => typeof part === "string")
			.join(" ");
		if (text) return text;
	}

	if (typeof message.input_text === "string") return message.input_text;
	if (typeof message.text === "string") return message.text;
	return "";
}

function truncate(s, n) {
	return s.length > n ? s.slice(0, n) + "..." : s;
}

function roleLabel(role) {
	switch (role) {
		case "system":
			return t("logs.system");
		case "user":
			return t("logs.user");
		case "assistant":
			return t("logs.assistant");
		case "tool":
			return t("logs.tool");
		default:
			return role || t("logs.unknown");
	}
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
			// assistant with tool_calls: always emit assistant node, then emit paired tool nodes
			nodes.push({
				type: "message",
				dotType: "assistant",
				label: t("logs.assistant"),
				preview: msg.preview,
				raw: msg.raw,
				defaultOpen: isLastSection,
			});
			for (const tc of msg.toolCalls) {
				const callId = tc.id || tc.tool_call_id;
				const result = callId ? toolResultMap.get(callId) : null;
				if (result) pairedToolIds.add(callId);
				nodes.push({
					type: "tool-pair",
					dotType: "tool",
					label: tc.function?.name || tc.name || t("logs.tool"),
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
			type: "message", dotType: msg.role, label: roleLabel(msg.role),
			preview: msg.preview, raw: msg.raw, defaultOpen: isLastSection,
		});
	}

	// append gateway steps
	for (const step of selected.value.steps || []) {
		const stepNode = {
			type: "step",
			dotType: "step",
			label: t("logs.gatewayStep", { n: step.iteration }),
		};
		nodes.push(stepNode);
		if (step.tool_calls?.length) {
			for (const tc of step.tool_calls) {
				const tr = step.tool_results?.find((r) => r.tool_call_id === tc.id);
				nodes.push({
					type: "tool-pair",
					dotType: "tool",
					label: tc.name || t("logs.tool"),
					toolName: tc.name,
					toolArgs: tc.arguments,
					toolResult: tr ? tr.output : undefined,
					toolError: tr?.is_error || false,
				});
			}
		}
	}

	return nodes;
});

// --- conversation chain grouping ---

const EMPTY_HASHES = Object.freeze([]);

// per-log caches to avoid repeated parse work in session grouping/filtering
const parsedRequestCache = new WeakMap();
const parsedResponseCache = new WeakMap();
const previewCache = new WeakMap();
const fingerprintCache = new WeakMap();
const timestampCache = new WeakMap();
const previousResponseIDCache = new WeakMap();
const responseIDCache = new WeakMap();

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
	if (!log || typeof log !== "object") return null;
	if (parsedRequestCache.has(log)) return parsedRequestCache.get(log);

	let req = log.request;
	if (!req) {
		parsedRequestCache.set(log, null);
		return null;
	}
	if (typeof req === "string") {
		try {
			req = JSON.parse(req);
		} catch {
			parsedRequestCache.set(log, null);
			return null;
		}
	}
	parsedRequestCache.set(log, req);
	return req;
}

function parseResponse(log) {
	if (!log || typeof log !== "object") return null;
	if (parsedResponseCache.has(log)) return parsedResponseCache.get(log);

	let resp = log.response;
	if (!resp) {
		parsedResponseCache.set(log, null);
		return null;
	}
	if (typeof resp === "string") {
		try {
			resp = JSON.parse(resp);
		} catch {
			parsedResponseCache.set(log, null);
			return null;
		}
	}
	parsedResponseCache.set(log, resp);
	return resp;
}

function getPreviousResponseID(log) {
	if (!log || typeof log !== "object") return "";
	if (previousResponseIDCache.has(log)) return previousResponseIDCache.get(log);
	const req = parseRequest(log);
	const id = req && typeof req.previous_response_id === "string" ? req.previous_response_id : "";
	previousResponseIDCache.set(log, id);
	return id;
}

function getResponseID(log) {
	if (!log || typeof log !== "object") return "";
	if (responseIDCache.has(log)) return responseIDCache.get(log);
	const resp = parseResponse(log);
	const id = resp && typeof resp.id === "string" ? resp.id : "";
	responseIDCache.set(log, id);
	return id;
}

// extract a short preview of the last user message in a request
function lastUserPreview(log) {
	if (!log || typeof log !== "object") return "";
	if (previewCache.has(log)) return previewCache.get(log);

	const req = parseRequest(log);
	if (!req) {
		previewCache.set(log, "");
		return "";
	}

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
		if (!lastMsg && typeof req.system === "string") {
			const preview = truncate(req.system, 40);
			previewCache.set(log, preview);
			return preview;
		}
	} else if (req.input != null) {
		if (typeof req.input === "string") {
			const preview = truncate(req.input, 40);
			previewCache.set(log, preview);
			return preview;
		}
		if (Array.isArray(req.input)) {
			const users = req.input.filter((m) => m.role === "user" || typeof m === "string");
			if (users.length) lastMsg = users[users.length - 1];
		}
	}

	let preview = "";
	if (!lastMsg) {
		preview = "";
	} else if (typeof lastMsg === "string") {
		preview = truncate(lastMsg, 40);
	} else {
		preview = truncate(extractUserMessageText(lastMsg) || extractPreview(lastMsg), 40);
	}
	previewCache.set(log, preview);
	return preview;
}

function latestContentPreview(log) {
	if (!log || typeof log !== "object") return "";

	const responseText = extractAssembledText(log).trim();
	if (responseText && responseText !== formatJSON(log.response)) {
		return truncate(responseText.replace(/\s+/g, " "), 120);
	}

	const steps = Array.isArray(log.steps) ? log.steps : [];
	for (let i = steps.length - 1; i >= 0; i--) {
		const step = steps[i];
		const stepResponse = step?.llm_response;
		if (!stepResponse) continue;
		const stepText = extractAssembledText({ response: stepResponse }).trim();
		if (stepText && stepText !== formatJSON(stepResponse)) {
			return truncate(stepText.replace(/\s+/g, " "), 120);
		}
	}

	return lastUserPreview(log);
}

function sessionTitlePreview(chain) {
	if (!chain?.logs?.length) return "";

	let lastUserText = "";
	for (const log of chain.logs) {
		const req = parseRequest(log);
		if (!req) continue;

		if (Array.isArray(req.messages)) {
			for (const message of req.messages) {
				if (message?.role === "assistant") {
					return truncate(lastUserText, 40);
				}
				if (message?.role !== "user") continue;
				if (Array.isArray(message.content) && message.content.length > 0 &&
					message.content.every((part) => part?.type === "tool_result")) {
					continue;
				}
				const text = extractUserMessageText(message).trim();
				if (text) lastUserText = text;
			}
			continue;
		}

		if (req.input == null) continue;
		if (typeof req.input === "string") {
			lastUserText = req.input.trim() || lastUserText;
			continue;
		}
		if (!Array.isArray(req.input)) continue;

		for (const item of req.input) {
			if (typeof item === "string") {
				const text = item.trim();
				if (text) lastUserText = text;
				continue;
			}
			if (item?.role === "assistant" || item?.type === "function_call" || item?.type === "message" && item?.role === "assistant") {
				return truncate(lastUserText, 40);
			}
			if (item?.role !== "user" && item?.type !== "message") continue;
			const text = extractUserMessageText(item).trim();
			if (text) lastUserText = text;
		}
	}

	if (lastUserText) return truncate(lastUserText, 40);
	for (const log of chain.logs) {
		const preview = lastUserPreview(log);
		if (preview) return preview;
	}
	return "";
}

function getTimestampMs(log) {
	if (!log || typeof log !== "object") return 0;
	if (timestampCache.has(log)) return timestampCache.get(log);
	const ts = new Date(log.timestamp).getTime();
	const normalized = Number.isFinite(ts) ? ts : 0;
	timestampCache.set(log, normalized);
	return normalized;
}

function getParsedFingerprint(log) {
	if (!log || typeof log !== "object") return null;
	if (fingerprintCache.has(log)) return fingerprintCache.get(log);
	const parsed = parseFingerprint(log.fingerprint);
	const normalized = parsed && parsed.fsm.length > 0 ? parsed : null;
	fingerprintCache.set(log, normalized);
	return normalized;
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
// Prefer explicit Responses stateful links (previous_response_id -> response.id),
// then fingerprint FSM prefix matching scoped to the same route.
const chainedLogs = computed(() => {
	const items = logs.value;
	if (!items.length) return [];

	const sorted = [...items].sort((a, b) => getTimestampMs(a) - getTimestampMs(b));

	// key = response.id -> chain index
	const statefulChainsByResponseID = new Map();
	// key = "{route}\0{sysHash}" -> chain indexes
	const fpChainsByKey = new Map();
	const chains = [];

	function insertChainIndex(indexMap, key, chainIdx) {
		if (!key) return;
		let arr = indexMap.get(key);
		if (!arr) {
			indexMap.set(key, [chainIdx]);
			return;
		}
		if (arr.includes(chainIdx)) return;
		if (arr.length === 0 || arr[arr.length - 1] < chainIdx) {
			arr.push(chainIdx);
			return;
		}
		for (let i = 0; i < arr.length; i++) {
			if (arr[i] > chainIdx) {
				arr.splice(i, 0, chainIdx);
				return;
			}
		}
		arr.push(chainIdx);
	}

	function routeKey(log) {
		return String(log?.route || "(unknown)");
	}

	function fpKey(route, sysHash) {
		return route + "\u0000" + sysHash;
	}

	function upgradeFingerprintIndex(chain, parsed, route) {
		if (!parsed || parsed.fsm.length === 0) return;
		if (!chain.fpKey) {
			chain.fpKey = fpKey(route, parsed.sysHash);
			insertChainIndex(fpChainsByKey, chain.fpKey, chain.idx);
		}
		chain.routeKey = route;
		chain.lastParsed = parsed;
	}

	function maybeUpgradeFingerprintIndex(chain, parsed, route) {
		if (!parsed || parsed.fsm.length === 0) return;
		if (!chain.lastParsed) {
			upgradeFingerprintIndex(chain, parsed, route);
			return;
		}
		if (chain.routeKey !== route) {
			return;
		}
		if (chain.fpKey && chain.fpKey !== fpKey(route, parsed.sysHash)) {
			return;
		}
		if (isFSMPrefix(chain.lastParsed.fsm, parsed.fsm)) {
			upgradeFingerprintIndex(chain, parsed, route);
		}
	}

	function appendToChain(chain, log, parsed) {
		chain.logs.push(log);
		const responseID = getResponseID(log);
		if (responseID) {
			statefulChainsByResponseID.set(responseID, chain.idx);
		}
		maybeUpgradeFingerprintIndex(chain, parsed, routeKey(log));
	}

	for (const log of sorted) {
		const parsed = getParsedFingerprint(log);
		const currentRouteKey = routeKey(log);
		const previousResponseID = getPreviousResponseID(log);
		let matched = false;

		if (previousResponseID) {
			const chainIdx = statefulChainsByResponseID.get(previousResponseID);
			const chain = chainIdx == null ? null : chains[chainIdx];
			if (chain && chain.routeKey === currentRouteKey) {
				appendToChain(chain, log, parsed);
				matched = true;
			}
		}

		if (!matched && parsed) {
			// fingerprint path: match by (route + sys_hash, FSM prefix)
			const candidates = fpChainsByKey.get(fpKey(currentRouteKey, parsed.sysHash)) || EMPTY_HASHES;
			for (let i = candidates.length - 1; i >= 0; i--) {
				const chain = chains[candidates[i]];
				const lastParsed = chain.lastParsed;
				if (!lastParsed) continue;
				if (chain.routeKey !== currentRouteKey) continue;
				if (isFSMPrefix(lastParsed.fsm, parsed.fsm)) {
					appendToChain(chain, log, parsed);
					matched = true;
					break;
				}
			}
		}

		if (!matched) {
			const chain = {
				idx: chains.length,
				id: (log.request_id || "") + "_" + chains.length,
				logs: [log],
				routeKey: currentRouteKey,
				lastParsed: null,
				fpKey: "",
			};
			chains.push(chain);
			maybeUpgradeFingerprintIndex(chain, parsed, currentRouteKey);
			const responseID = getResponseID(log);
			if (responseID) {
				statefulChainsByResponseID.set(responseID, chain.idx);
			}
		}
	}

	return chains;
});

function chainTotalDuration(chain) {
	return chain.logs.reduce((sum, l) => sum + (l.duration_ms || 0), 0);
}

function failoverCount(log) {
	return Array.isArray(log?.failovers) ? log.failovers.length : 0;
}

function isRecoveredByFailover(log) {
	return !log?.pending && !log?.error && failoverCount(log) > 0;
}

function rowClass(log) {
	return {
		"row-error": Boolean(log?.error),
		"row-warn": isRecoveredByFailover(log),
	};
}

function childRowClass(log) {
	return {
		"row-error": Boolean(log?.error),
		"row-warn": isRecoveredByFailover(log),
	};
}

function chainRowClass(chain) {
	return {
		"row-error": chain.displayLogs.some((log) => log.error),
		"row-warn": chain.displayLogs.some((log) => isRecoveredByFailover(log)),
	};
}

function chainStatus(chain) {
	const errors = chain.logs.filter((l) => l.error);
	const recoveredFailovers = chain.logs.reduce((sum, log) => sum + failoverCount(log), 0);
	if (errors.length === 0 && recoveredFailovers > 0) {
		return t("logs.failoverRecovered", { n: recoveredFailovers });
	}
	if (errors.length === 0) return t("common.ok");
	if (errors.length === chain.logs.length) return t("logs.failedAll");
	return t("logs.failed", { n: errors.length, total: chain.logs.length });
}

// single-log status text: includes step count if any
function statusText(log) {
	if (log.pending) return t("logs.streaming");
	if (log.error) return log.error;
	const parts = [];
	const recoveredFailovers = failoverCount(log);
	if (recoveredFailovers > 0) {
		parts.push(t("logs.failoverRecovered", { n: recoveredFailovers }));
	}
	const steps = log.steps?.length;
	if (steps) parts.push(t("logs.steps", { n: steps }));
	return parts.length ? t("common.ok") + " · " + parts.join(" · ") : t("common.ok");
}

function responseClass(log) {
	if (log?.error) return "response-error";
	if (isRecoveredByFailover(log)) return "response-warn";
	return "response-ok";
}

function responseStatusText(log) {
	if (isRecoveredByFailover(log)) {
		return t("logs.failoverRecovered", { n: failoverCount(log) });
	}
	return t("common.ok");
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

const routeTree = computed(() => routeKeys.value.map((key) => ({
	key,
	chains: chainedLogs.value.filter((chain) => (chain.logs[0]?.route || "(unknown)") === key),
})));

const filteredLogCount = computed(() =>
	filteredChains.value.reduce((sum, chain) => sum + chain.displayLogs.length, 0),
);

const scopeTitle = computed(() => {
	if (activeSession.value) {
		const chain = chainedLogs.value.find((item) => item.id === activeSession.value);
		if (chain) return sessionName(chain);
	}
	if (activeTab.value) return activeTab.value;
	return t("logs.allRequests");
});

function sessionName(chain) {
	const preview = sessionTitlePreview(chain);
	return preview || formatTime(chain.logs[0].timestamp);
}

const detailTitle = computed(() => {
	if (!selected.value) return "";
	return latestContentPreview(selected.value) || selected.value.request_id || t("logs.requestDetail");
});

// clear session when route tab changes
watch(activeTab, () => { activeSession.value = null; });

// auto-expand selected session
watch(activeSession, (id) => {
	if (!id) return;
	expandedChains.value = new Set([...expandedChains.value, id]);
	const chain = chainedLogs.value.find((item) => item.id === id);
	const routeKey = chain?.logs[0]?.route || "(unknown)";
	sessionTreeCollapsed.value = false;
	expandRouteGroup(routeKey);
});

watch(routeKeys, (keys) => {
	const next = new Set();
	for (const key of collapsedRouteGroups.value) {
		if (keys.includes(key)) next.add(key);
	}
	if (next.size !== collapsedRouteGroups.value.size) {
		collapsedRouteGroups.value = next;
	}
});

watch(paused, (value) => {
	if (!value && pendingLogs.length > 0) {
		flushPendingLogs();
	}
});

watch(selected, async (value) => {
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

function getFocusableElements() {
	if (!modalRef.value) return [];
	return [...modalRef.value.querySelectorAll(
		'button, summary, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
	)].filter((el) => !el.hasAttribute("disabled") && el.getAttribute("aria-hidden") !== "true");
}

function handleModalKeydown(event) {
	if (event.key === "Escape") {
		event.preventDefault();
		closeDetail();
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

onMounted(() => {
	const stream = createLogStream();
	stopStream = stream.start(
		(data) => {
			if (paused.value) return;
			enqueueLog(data);
		},
		(err) => {
			error.value = err.message;
		},
	);
});

onUnmounted(() => {
	if (stopStream) stopStream();
	if (flushFrame) cancelAnimationFrame(flushFrame);
	if (autoScrollFrame) cancelAnimationFrame(autoScrollFrame);
	clearTimeout(copyTimer);
});
</script>

<style scoped>
/* Header layout */
.header-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 12px;
	margin-bottom: 18px;
}

.logs-workspace {
	display: grid;
	grid-template-columns: minmax(260px, 320px) minmax(0, 1fr);
	gap: 18px;
	align-items: start;
}

.logs-workspace-tree-collapsed {
	grid-template-columns: 88px minmax(0, 1fr);
}

.session-tree-panel {
	position: sticky;
	top: 16px;
	padding: 16px;
	display: flex;
	flex-direction: column;
	gap: 14px;
}

.session-tree-panel.collapsed {
	padding: 14px 10px;
}

.session-tree-header,
.logs-scope,
.detail-summary-head,
.detail-section-head {
	display: flex;
	align-items: flex-start;
	justify-content: space-between;
	gap: 12px;
}

.session-tree-actions {
	display: flex;
	align-items: center;
	gap: 8px;
}

.session-tree-collapse-btn {
	min-width: 36px;
	padding-inline: 0;
}

.section-eyebrow {
	font-size: 11px;
	font-weight: 700;
	letter-spacing: 0.08em;
	text-transform: uppercase;
	color: var(--c-text-3);
	margin-bottom: 4px;
}

.session-tree-title,
.logs-scope-title,
.detail-summary-title,
.detail-section-title {
	margin: 0;
	font-size: 18px;
	line-height: 1.25;
}

.tree-root-button,
.route-branch-button,
.session-node-button {
	width: 100%;
	text-align: left;
	border: 1px solid transparent;
	border-radius: var(--radius-sm);
	background: transparent;
	color: inherit;
	cursor: pointer;
	transition:
		background-color 0.15s,
		border-color 0.15s,
		color 0.15s;
}

.tree-root-button,
.route-branch-button,
.session-node-button {
	padding: 10px 12px;
}

.tree-root-button:hover,
.route-branch-button:hover,
.session-node-button:hover {
	background: var(--c-surface-tint);
	border-color: var(--c-border);
}

.tree-root-button.active,
.route-branch-button.active,
.session-node-button.active {
	background: var(--c-primary-bg);
	border-color: color-mix(in srgb, var(--c-primary) 24%, var(--c-border));
}

.tree-root-title,
.route-branch-label,
.session-node-title {
	display: block;
	font-size: 13px;
	font-weight: 600;
	color: var(--c-text);
}

.tree-root-meta,
.session-node-meta,
.logs-scope-meta {
	font-size: 12px;
	color: var(--c-text-3);
}

.tree-scroll {
	display: flex;
	flex-direction: column;
	gap: 12px;
	max-height: calc(100vh - 180px);
	overflow-y: auto;
	padding-right: 4px;
}

.route-branch {
	display: flex;
	flex-direction: column;
	gap: 6px;
}

.route-branch-header {
	display: grid;
	grid-template-columns: minmax(0, 1fr) 36px;
	gap: 8px;
	align-items: stretch;
}

.route-branch-button {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 12px;
}

.route-branch-toggle {
	border: 1px solid var(--c-border);
	border-radius: var(--radius-sm);
	background: var(--c-surface);
	color: var(--c-text-2);
	cursor: pointer;
	font-size: 18px;
	line-height: 1;
	transition:
		background-color 0.15s,
		border-color 0.15s,
		color 0.15s;
}

.route-branch-toggle:hover {
	background: var(--c-surface-tint);
	border-color: var(--c-primary);
	color: var(--c-text);
}

.session-tree-panel.collapsed .section-eyebrow,
.session-tree-panel.collapsed .session-tree-title,
.session-tree-panel.collapsed .badge {
	display: none;
}

.session-tree-panel.collapsed .session-tree-header {
	justify-content: center;
}

.session-tree-list {
	list-style: none;
	padding: 0;
	margin: 0;
}

.session-tree-list {
	display: flex;
	flex-direction: column;
	gap: 8px;
	padding-left: 10px;
	border-left: 1px solid var(--c-border-light);
}

.session-tree-item {
	display: flex;
	flex-direction: column;
	gap: 6px;
}

.session-node-main {
	display: flex;
	flex-direction: column;
	gap: 2px;
}

.logs-content {
	display: flex;
	flex-direction: column;
	gap: 12px;
	min-width: 0;
}

.logs-scope {
	padding: 0 2px;
}

/* Table container */
.table-wrap {
	overflow: visible;
	border-radius: var(--radius);
	background: var(--c-surface);
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
	background: var(--c-text-3);
}
.dot-user {
	background: var(--c-primary);
}
.dot-assistant {
	background: var(--c-success);
}
.dot-tool {
	background: var(--c-warning);
}
.dot-step {
	background: var(--c-text-3);
}
.dot-ok {
	background: var(--c-success);
}
.dot-error {
	background: var(--c-danger);
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
	color: var(--c-success);
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
		flex-wrap: wrap;
		gap: 8px;
		justify-content: flex-start;
	}

	.logs-workspace {
		grid-template-columns: 1fr;
	}

	.logs-workspace-tree-collapsed {
		grid-template-columns: 1fr;
	}

	.session-tree-panel {
		position: static;
		padding: 14px;
	}

	.tree-scroll {
		max-height: 360px;
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
	.detail-section-head,
	.logs-scope {
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

	.mobile-log-meta {
		grid-template-columns: 1fr;
	}

	.mobile-log-child {
		grid-template-columns: 1fr;
	}

}
</style>
