<template>
	<aside
		v-if="routeTree.length"
		class="session-tree-panel panel"
		:class="{ collapsed: sessionTreeCollapsed }"
	>
		<div class="session-tree-header">
			<div class="session-tree-copy">
				<div class="section-eyebrow">{{ $t('logs.sessions') }}</div>
				<h3 class="session-tree-title">{{ $t('logs.sessionExplorer') }}</h3>
				<p class="section-note">{{ $t('logs.sessionExplorerHint') }}</p>
			</div>
			<div class="session-tree-actions">
				<span class="badge">{{ chainedLogs.length }}</span>
				<button
					class="btn btn-secondary btn-sm session-tree-collapse-btn"
					type="button"
					:aria-expanded="!sessionTreeCollapsed"
					:aria-label="sessionTreeCollapsed ? $t('logs.expandSessionTree') : $t('logs.collapseSessionTree')"
					@click="$emit('toggle-collapse')"
				>
					{{ sessionTreeCollapsed ? "\u25B6" : "\u25C0" }}
				</button>
			</div>
		</div>

		<template v-if="!sessionTreeCollapsed">
			<button
				class="tree-root-button"
				:class="{ active: activeTab === '' && activeSession === null }"
				type="button"
				@click="$emit('select-all')"
			>
				<span class="tree-root-title">{{ $t('logs.allRequests') }}</span>
				<span class="tree-root-meta">{{ logCount }} {{ $t('logs.reqs') }}</span>
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
						@click="$emit('select-route', group.key)"
					>
						<span class="route-branch-label">{{ group.key }}</span>
						<span class="badge">{{ group.chains.length }}</span>
					</button>
					<button
						class="route-branch-toggle"
						type="button"
						:aria-expanded="isRouteGroupExpanded(group.key)"
						:aria-label="isRouteGroupExpanded(group.key) ? $t('logs.collapseRouteGroup') : $t('logs.expandRouteGroup')"
						@click="$emit('toggle-route-group', group.key)"
					>
						{{ isRouteGroupExpanded(group.key) ? "\u2212" : "+" }}
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
							@click="$emit('select-session', chain, group.key)"
						>
							<span class="session-node-main">
								<span class="session-node-title">{{ sessionName(chain) }}</span>
								<span class="session-node-meta">
									{{ formatTime(chain.logs[0].timestamp) }} &middot; {{ chain.logs.length }} {{ $t('logs.reqs') }}
								</span>
							</span>
						</button>
					</li>
				</ul>
			</section>
			</div>
		</template>
	</aside>
</template>

<script setup>
import { useI18n } from "vue-i18n";

const { locale } = useI18n();

const props = defineProps({
	routeTree: { type: Array, required: true },
	chainedLogs: { type: Array, required: true },
	activeTab: { type: String, required: true },
	activeSession: { default: null },
	sessionTreeCollapsed: { type: Boolean, required: true },
	collapsedRouteGroups: { type: Set, required: true },
	logCount: { type: Number, required: true },
	sessionTitlePreview: { type: Function, required: true },
});

defineEmits(["select-all", "select-route", "select-session", "toggle-collapse", "toggle-route-group"]);

function isRouteGroupExpanded(routeKey) {
	return !props.collapsedRouteGroups.has(routeKey);
}

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

function sessionName(chain) {
	const preview = props.sessionTitlePreview(chain);
	return preview || formatTime(chain.logs[0].timestamp);
}
</script>

<style scoped>
.session-tree-panel {
	position: sticky;
	top: 16px;
	padding: 14px;
	display: flex;
	flex-direction: column;
	gap: 12px;
}

.session-tree-panel.collapsed {
	padding: 14px 10px;
}

.session-tree-header {
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

.session-tree-copy {
	display: flex;
	flex-direction: column;
	gap: 4px;
	min-width: 0;
}

.session-tree-collapse-btn {
	min-width: 40px;
	min-height: 40px;
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

.section-note {
	font-size: 12px;
	line-height: 1.5;
	color: var(--c-text-3);
	max-width: 28ch;
}

.session-tree-title {
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
	padding: 10px 12px;
	transition:
		background-color 0.15s,
		border-color 0.15s,
		color 0.15s;
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
	background: color-mix(in srgb, var(--c-primary-bg) 82%, white);
	border-color: color-mix(in srgb, var(--c-primary) 24%, var(--c-border));
	box-shadow: inset 3px 0 0 var(--c-primary);
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
.session-node-meta {
	font-size: 12px;
	color: var(--c-text-3);
}

.tree-root-button {
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: 4px;
	background: color-mix(in srgb, var(--c-surface-tint) 58%, white);
	border-color: var(--c-border-light);
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
	display: inline-flex;
	align-items: center;
	justify-content: center;
	border: 1px solid var(--c-border);
	border-radius: var(--radius-sm);
	background: var(--c-surface);
	color: var(--c-text-2);
	cursor: pointer;
	font-size: 18px;
	line-height: 1;
	min-width: 40px;
	min-height: 40px;
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
	gap: 4px;
}

.badge {
	font-size: 11px;
	font-weight: 600;
	padding: 1px 7px;
	border-radius: 10px;
	background: var(--c-border-light);
}

@media (max-width: 768px) {
	.session-tree-panel {
		position: static;
		padding: 14px;
	}

	.session-tree-collapse-btn,
	.route-branch-toggle {
		min-width: 44px;
		min-height: 44px;
	}

	.tree-scroll {
		max-height: 360px;
	}

	.section-note {
		max-width: none;
	}
}
</style>
