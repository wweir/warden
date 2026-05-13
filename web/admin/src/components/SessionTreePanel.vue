<template>
	<aside
		v-if="tree.length"
		class="session-tree-panel panel"
		:class="{ collapsed: collapsed }"
	>
		<div class="session-tree-header">
			<div class="session-tree-copy">
				<div class="section-eyebrow">{{ $t('logs.routes') }}</div>
				<h3 class="session-tree-title">{{ $t('logs.sessions') }}</h3>
			</div>
			<div class="session-tree-actions">
				<button
					class="btn btn-secondary btn-sm session-tree-collapse-btn"
					type="button"
					:aria-expanded="!collapsed"
					:aria-label="collapsed ? $t('logs.expandSessionTree') : $t('logs.collapseSessionTree')"
					@click="$emit('toggle-collapse')"
				>
					{{ collapsed ? "\u25B6" : "\u25C0" }}
				</button>
			</div>
		</div>

		<template v-if="!collapsed">
			<button
				class="tree-root-button"
				:class="{ active: activeRoute === '' && activeSession === '' }"
				type="button"
				@click="$emit('select-all')"
			>
				<span class="tree-root-title">{{ $t('logs.allRequests') }}</span>
				<span class="tree-root-meta">{{ logCount }} {{ $t('logs.reqs') }}</span>
			</button>

			<div class="tree-routes" role="tree">
				<div
					v-for="group in tree"
					:key="group.route"
					class="route-group"
					role="treeitem"
					:aria-expanded="isExpanded(group.route)"
				>
					<div
						class="route-branch"
						:class="{ active: activeRoute === group.route && !activeSession }"
					>
						<button
							class="route-chevron"
							:class="{ 'route-chevron-open': isExpanded(group.route) }"
							type="button"
							:aria-label="isExpanded(group.route) ? $t('logs.collapseRouteGroup') : $t('logs.expandRouteGroup')"
							@click.stop="toggleExpand(group.route)"
						>
							{{ isExpanded(group.route) ? '\u25BC' : '\u25B6' }}
						</button>
						<button
							class="route-branch-button"
							type="button"
							@click="$emit('select-route', group.route)"
						>
							<span class="route-branch-label">{{ group.route }}</span>
							<span class="badge">{{ group.sessions.length }}</span>
						</button>
					</div>

					<div v-if="isExpanded(group.route)" class="route-sessions">
						<button
							v-for="session in group.sessions"
							:key="session.fingerprint"
							class="session-button"
							:class="{
								active: activeSession === session.fingerprint,
								'session-pending': session.log.pending,
								'session-error': session.log.error,
							}"
							type="button"
							@click.stop="$emit('select-session', { route: group.route, fingerprint: session.fingerprint })"
						>
							<span class="session-preview">{{ session.preview }}</span>
							<span v-if="session.log.pending" class="session-pulse" aria-hidden="true"></span>
						</button>
					</div>
				</div>
			</div>
		</template>
	</aside>
</template>

<script setup>
import { ref } from "vue";

const props = defineProps({
	tree: { type: Array, required: true },
	activeRoute: { type: String, required: true },
	activeSession: { type: String, required: true },
	collapsed: { type: Boolean, required: true },
	logCount: { type: Number, required: true },
});

const emit = defineEmits(["select-all", "select-route", "select-session", "toggle-collapse"]);

const STORAGE_KEY = "warden:logs:collapsedRoutes";

function loadCollapsed() {
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		return raw ? new Set(JSON.parse(raw)) : new Set();
	} catch {
		return new Set();
	}
}

const collapsedRoutes = ref(loadCollapsed());

function saveCollapsed() {
	try {
		localStorage.setItem(STORAGE_KEY, JSON.stringify([...collapsedRoutes.value]));
	} catch { /* ignore */ }
}

function isExpanded(route) {
	return !collapsedRoutes.value.has(route);
}

function toggleExpand(route) {
	if (collapsedRoutes.value.has(route)) {
		collapsedRoutes.value.delete(route);
	} else {
		collapsedRoutes.value.add(route);
	}
	saveCollapsed();
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
	max-height: calc(100vh - 32px);
	overflow-y: auto;
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

.session-tree-title {
	margin: 0;
	font-size: 18px;
	line-height: 1.25;
}

.tree-root-button,
.session-button {
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

.tree-root-button:hover,
.session-button:hover {
	background: var(--c-surface-tint);
	border-color: var(--c-border);
}

.tree-root-button {
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: 4px;
	padding: 10px 12px;
	background: var(--c-surface);
	border-color: var(--c-border-light);
}

.tree-root-button.active,
.route-branch.active,
.session-button.active {
	background: var(--c-primary-bg);
	border-color: var(--c-primary);
}

.tree-root-button.active {
	padding-left: 10px;
}

.tree-root-title,
.route-branch-label {
	display: block;
	font-size: 13px;
	font-weight: 600;
	color: var(--c-text);
}

.tree-root-meta {
	font-size: 12px;
	color: var(--c-text-3);
}

.tree-routes {
	display: flex;
	flex-direction: column;
	gap: 4px;
}

.route-group {
	display: flex;
	flex-direction: column;
}

.route-branch {
	display: flex;
	align-items: center;
	gap: 2px;
	border-radius: var(--radius-sm);
	border: 1px solid transparent;
	transition: background-color 0.15s, border-color 0.15s;
}

.route-branch:hover {
	background: var(--c-surface-tint);
	border-color: var(--c-border);
}

.route-branch.active {
	padding-left: 7px;
}

.route-branch {
	cursor: pointer;
}

.route-chevron {
	font-size: 10px;
	color: var(--c-text-3);
	width: 26px;
	height: 32px;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	border-radius: var(--radius-sm);
	background: transparent;
	border: none;
	cursor: pointer;
	flex-shrink: 0;
	transition: background-color 0.15s;
}

.route-chevron:hover {
	background: var(--c-border-light);
}

.route-branch-button {
	flex: 1 1 auto;
	display: flex;
	align-items: center;
	gap: 6px;
	padding: 8px 10px 8px 0;
	font-size: 13px;
	font-weight: 600;
	background: transparent;
	border: none;
	color: inherit;
	cursor: pointer;
	text-align: left;
	min-width: 0;
}

.route-branch-label {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.route-sessions {
	display: flex;
	flex-direction: column;
	gap: 2px;
	padding-left: 20px;
	margin-top: 2px;
}

.session-button {
	display: flex;
	align-items: center;
	gap: 8px;
	padding: 6px 10px;
	font-size: 12px;
	font-weight: 500;
	color: var(--c-text-2);
}

.session-button.active {
	padding-left: 6px;
}

.session-preview {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.session-pulse {
	width: 7px;
	height: 7px;
	border-radius: 50%;
	background: var(--c-primary);
	animation: pulse 1.5s infinite;
	flex-shrink: 0;
}

@keyframes pulse {
	0% { opacity: 1; transform: scale(1); }
	50% { opacity: 0.5; transform: scale(0.85); }
	100% { opacity: 1; transform: scale(1); }
}

@media (prefers-reduced-motion: reduce) {
	.session-pulse {
		animation: none;
		opacity: 1;
	}
}

.session-error .session-preview {
	color: var(--c-danger);
}

.badge {
	font-size: 11px;
	font-weight: 600;
	padding: 1px 7px;
	border-radius: 10px;
	background: var(--c-border-light);
	flex-shrink: 0;
}

.session-tree-panel.collapsed .section-eyebrow,
.session-tree-panel.collapsed .session-tree-title,
.session-tree-panel.collapsed .badge,
.session-tree-panel.collapsed .route-chevron,
.session-tree-panel.collapsed .route-sessions,
.session-tree-panel.collapsed .tree-root-meta {
	display: none;
}

.session-tree-panel.collapsed .session-tree-header {
	justify-content: center;
}

.session-tree-panel.collapsed .tree-root-button {
	justify-content: center;
	padding-inline: 0;
}

.session-tree-panel.collapsed .route-branch {
	justify-content: center;
	padding-inline: 0;
}

.session-tree-panel.collapsed .route-branch-label,
.session-tree-panel.collapsed .route-chevron {
	display: none;
}

@media (max-width: 768px) {
	.session-tree-panel {
		position: static;
		padding: 14px;
		max-height: none;
	}

	.session-tree-collapse-btn {
		min-width: 44px;
		min-height: 44px;
	}
}
</style>
