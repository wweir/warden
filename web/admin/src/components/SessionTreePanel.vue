<template>
	<aside v-if="tree.length" class="session-tree-panel panel">
		<div class="session-tree-header">
			<div class="session-tree-copy">
				<div class="section-eyebrow">{{ $t('logs.routes') }}</div>
				<h3 class="session-tree-title">{{ $t('logs.sessions') }}</h3>
			</div>
		</div>

		<button
			class="tree-root-button"
			:class="{ active: activeRoute === '' && activeSession === '' && activePrefix === '' }"
			type="button"
			@click="$emit('select-all')"
		>
			<span class="tree-root-title">{{ $t('logs.allRequests') }}</span>
			<span class="tree-root-meta">{{ logCount }} {{ $t('logs.reqs') }}</span>
		</button>

		<div class="tree-routes" role="tree">
			<SessionTreeNode
				v-for="group in tree"
				:key="group.route"
				:node="group"
				:route="group.route"
				:active-route="activeRoute"
				:active-session="activeSession"
				:active-prefix="activePrefix"
				:depth="0"
				@select-route="$emit('select-route', $event)"
				@select-prefix="$emit('select-prefix', $event)"
				@select-session="$emit('select-session', $event)"
			/>
		</div>
	</aside>
</template>

<script setup>
import { computed, defineComponent, h, ref } from "vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();

const props = defineProps({
	tree: { type: Array, required: true },
	activeRoute: { type: String, required: true },
	activeSession: { type: String, required: true },
	activePrefix: { type: String, required: true },
	logCount: { type: Number, required: true },
});

defineEmits(["select-all", "select-route", "select-prefix", "select-session"]);

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
	} catch {
		// ignore
	}
}

function isExpanded(key) {
	return !collapsedRoutes.value.has(key);
}

function toggleExpand(key) {
	if (collapsedRoutes.value.has(key)) {
		collapsedRoutes.value.delete(key);
	} else {
		collapsedRoutes.value.add(key);
	}
	saveCollapsed();
}

function nodeKey(route, node, depth) {
	if (depth === 0) return route;
	return `${route}:${node.prefix || node.key}`;
}

const SessionTreeNode = defineComponent({
	name: "SessionTreeNode",
	props: {
		node: { type: Object, required: true },
		route: { type: String, required: true },
		activeRoute: { type: String, required: true },
		activeSession: { type: String, required: true },
		activePrefix: { type: String, required: true },
		depth: { type: Number, required: true },
	},
	emits: ["select-route", "select-prefix", "select-session"],
	setup(nodeProps, { emit }) {
			const isRoute = computed(() => nodeProps.depth === 0);
			const key = computed(() => nodeKey(nodeProps.route, nodeProps.node, nodeProps.depth));
			const expanded = computed(() => isExpanded(key.value));
			const hasChildren = computed(() => Array.isArray(nodeProps.node.children) && nodeProps.node.children.length > 0);
			const routeActive = computed(() => nodeProps.activeRoute === nodeProps.route);
			const prefixActive = computed(() => !isRoute.value && (nodeProps.activePrefix || "").startsWith(nodeProps.node.prefix || ""));
			const label = computed(() => nodeProps.node.label || nodeProps.node.prefix || nodeProps.route);
			const childCount = computed(() => {
				if (hasChildren.value) return nodeProps.node.children.length;
				if (nodeProps.node.sessions) return nodeProps.node.sessions.length;
				return 0;
			});

			function toggle() {
				toggleExpand(key.value);
			}

			function selectNode() {
				if (isRoute.value) {
					emit("select-route", nodeProps.route);
					return;
				}
				emit("select-prefix", { route: nodeProps.route, prefix: nodeProps.node.prefix || "" });
			}

			function selectSession(session) {
				emit("select-session", { route: nodeProps.route, fingerprint: session.fingerprint });
			}

			const indentPx = nodeProps.depth === 0 ? 10 : 10;

			return () => h(
				"div",
				{
					class: "route-group",
					role: "treeitem",
					"aria-expanded": isRoute.value || hasChildren.value ? expanded.value : undefined,
					"aria-selected": isRoute.value ? routeActive.value : prefixActive.value,
				},
				[
					isRoute.value
						? h(
								"div",
								{ class: ["route-branch", { active: routeActive.value }] },
								[
									h(
										"button",
										{
											class: ["route-chevron", { "route-chevron-open": expanded.value }],
											type: "button",
											"aria-label": expanded.value ? t("logs.collapseRouteGroup") : t("logs.expandRouteGroup"),
											onClick: (event) => {
												event.stopPropagation();
												toggle();
											},
										},
										expanded.value ? "▼" : "▶",
									),
									h(
										"button",
										{
											class: "route-branch-button",
											type: "button",
											onClick: selectNode,
										},
										[
											h("span", { class: "route-branch-label" }, label.value),
											h("span", { class: "badge" }, String(nodeProps.node.count || 0)),
										],
									),
								],
							)
							: (!isRoute.value && nodeProps.node.sessions?.length === 1)
								? null
								: h(
										"button",
										{
											class: ["prefix-row", { active: prefixActive.value }],
											type: "button",
											onClick: selectNode,
										},
										[
											h("span", { class: "prefix-label" }, label.value),
											h("span", { class: "prefix-badge" }, String(childCount.value)),
										],
									),
					(isRoute.value ? expanded.value : true)
						? h(
								"div",
								{
									class: "route-sessions",
									style: { paddingLeft: `${indentPx}px` },
								},
								hasChildren.value
									? nodeProps.node.children.map((child) =>
											h(SessionTreeNode, {
												key: child.key,
												node: child,
												route: nodeProps.route,
												activeRoute: nodeProps.activeRoute,
												activeSession: nodeProps.activeSession,
												activePrefix: nodeProps.activePrefix,
												depth: nodeProps.depth + 1,
												onSelectRoute: (value) => emit("select-route", value),
												onSelectPrefix: (value) => emit("select-prefix", value),
												onSelectSession: (value) => emit("select-session", value),
											}),
										)
									: nodeProps.node.sessions.map((session) =>
											h(
												"button",
												{
													key: session.fingerprint,
													class: [
														"session-button",
														{
															active: nodeProps.activeSession === session.fingerprint,
															"session-pending": session.log.pending,
															"session-error": session.log.error,
														},
													],
													type: "button",
													onClick: () => selectSession(session),
												},
												[
														h("span", { class: "session-preview" }, session.preview),
														session.log.pending ? h("span", { class: "session-pulse", "aria-hidden": "true" }) : null,
													],
												),
											),
								)
						: null,
				],
			);
		},
	});
</script>

<style>
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

.session-tree-header {
	display: flex;
	align-items: flex-start;
	justify-content: flex-start;
}

.session-tree-copy {
	display: flex;
	flex-direction: column;
	gap: 4px;
	min-width: 0;
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
	min-height: 44px;
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

/* route-branch active styling handled by background/border */

.route-branch {
	cursor: pointer;
}

.route-chevron {
	font-size: 10px;
	color: var(--c-text-3);
	width: 32px;
	height: 44px;
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

.route-chevron-spacer {
	cursor: default;
}

.route-chevron:hover {
	background: var(--c-border-light);
}

.route-branch-button {
	flex: 1 1 auto;
	display: flex;
	align-items: center;
	gap: 6px;
	min-height: 44px;
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

.prefix-row {
	display: flex;
	align-items: center;
	gap: 6px;
	padding: 2px 8px 2px 0;
	min-height: 28px;
	font-size: 11px;
	font-weight: 500;
	color: var(--c-text-3);
	cursor: pointer;
	border-radius: var(--radius-sm);
	border: 1px solid transparent;
	user-select: none;
}
.prefix-row:hover {
	color: var(--c-text-2);
	background: var(--c-surface-tint);
}
.prefix-row.active {
	color: var(--c-primary);
	background: var(--c-primary-bg);
	border-color: var(--c-primary);
}
.prefix-label {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.prefix-badge {
	font-size: 10px;
	font-weight: 600;
	padding: 0 5px;
	border-radius: 8px;
	background: var(--c-border-light);
	color: var(--c-text-3);
	flex-shrink: 0;
}

.route-sessions {
	display: flex;
	flex-direction: column;
	gap: 2px;
	margin-top: 1px;
}

.session-button {
	display: flex;
	align-items: center;
	gap: 8px;
	min-height: 44px;
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
	will-change: opacity, transform;
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

@media (max-width: 768px) {
	.session-tree-panel {
		position: static;
		padding: 14px;
		max-height: none;
	}
}
</style>
