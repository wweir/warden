<template>
	<div>
		<div class="page-header">
			<div class="page-header-main">
				<h2 class="page-title">{{ $t("routes.title") }}</h2>
				<router-link to="/routes/new" class="btn btn-primary btn-sm">
					{{ $t("routes.addRoute") }}
				</router-link>
			</div>
			<input
				v-model="search"
				class="form-input search-input"
				:placeholder="$t('routes.searchPlaceholder')"
			/>
		</div>
		<div v-if="error" class="msg msg-error">{{ error }}</div>

		<div v-if="metricsData" class="route-stats">
			<div class="route-stat-card">
				<div class="metric-header">
					<span class="route-stat-title">{{ $t("routes.trafficTrend") }}</span>
					<span class="metric-count"
						>{{ requestTrendSeries.length }} · {{ $t("dashboard.trendWindow") }}</span
					>
				</div>
				<div class="trend-chart">
					<RealtimeLineChart
						:points="requestTrendPoints"
						:series="requestTrendSeries"
						:empty-text="$t('common.noData')"
						:group="chartGroup"
						:time-range="chartTimeRange"
						:y-formatter="formatCountAxis"
					/>
				</div>
				<div class="metric-stats">
					<div
						v-for="item in requestTrendLegend"
						:key="item.route"
						class="trend-legend-row"
					>
						<span class="trend-dot" :style="{ background: item.color }"></span>
						<span class="trend-route">{{ item.route }}</span>
						<span class="trend-value">{{ item.value }}</span>
					</div>
					<div v-if="requestTrendLegend.length === 0" class="stat-empty">
						{{ $t("common.noData") }}
					</div>
				</div>
			</div>

			<div class="route-stat-card">
				<div class="metric-header">
					<span class="route-stat-title">{{ $t("routes.errorTrend") }}</span>
					<span class="metric-count"
						>{{ errorTrendSeries.length }} · {{ $t("dashboard.trendWindow") }}</span
					>
				</div>
				<div class="trend-chart">
					<RealtimeLineChart
						:points="errorTrendPoints"
						:series="errorTrendSeries"
						:empty-text="$t('common.noData')"
						:group="chartGroup"
						:time-range="chartTimeRange"
						:y-formatter="formatPercentAxis"
					/>
				</div>
				<div class="metric-stats">
					<div
						v-for="item in errorTrendLegend"
						:key="item.route"
						class="trend-legend-row"
					>
						<span class="trend-dot" :style="{ background: item.color }"></span>
						<span class="trend-route">{{ item.route }}</span>
						<span class="trend-value">{{ item.value }}</span>
					</div>
					<div v-if="errorTrendLegend.length === 0" class="stat-empty">
						{{ $t("common.noData") }}
					</div>
				</div>
			</div>

			<div class="route-stat-card">
				<div class="metric-header">
					<span class="route-stat-title">{{ $t("routes.outputTrend") }}</span>
					<span class="metric-count"
						>{{ outputTrendSeries.length }} · {{ $t("dashboard.trendWindow") }}</span
					>
				</div>
				<div class="trend-chart">
					<RealtimeLineChart
						:points="outputTrendPoints"
						:series="outputTrendSeries"
						:empty-text="$t('common.noData')"
						:group="chartGroup"
						:time-range="chartTimeRange"
						:y-formatter="formatTPSAxis"
					/>
				</div>
				<div class="metric-stats">
					<div
						v-for="item in outputTrendLegend"
						:key="item.route"
						class="trend-legend-row"
					>
						<span class="trend-dot" :style="{ background: item.color }"></span>
						<span class="trend-route">{{ item.route }}</span>
						<span class="trend-value">{{ item.value }}</span>
					</div>
					<div v-if="outputTrendLegend.length === 0" class="stat-empty">
						{{ $t("common.noData") }}
					</div>
				</div>
			</div>
		</div>

		<div v-if="status" class="panel" style="padding: 18px">
			<table class="data-table">
				<thead>
					<tr>
						<th>{{ $t("routes.prefix") }}</th>
						<th>{{ $t("routes.protocol") }}</th>
						<th>{{ $t("routes.models") }}</th>
						<th>{{ $t("routes.providers") }}</th>
						<th>{{ $t("routes.requests") }}</th>
						<th>{{ $t("routes.failures") }}</th>
						<th>{{ $t("routes.successRate") }}</th>
						<th>{{ $t("routes.outputRate") }}</th>
						<th>{{ $t("routes.latencyP95") }}</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="r in filteredRoutes" :key="r.prefix">
						<td>
							<router-link :to="'/routes' + r.prefix" class="resource-link"
								><code>{{ r.prefix }}</code></router-link
							>
						</td>
						<td>
							<span class="badge" :class="r.protocol ? 'badge-ok' : 'badge-warn'">
								{{ r.protocol || "-" }}
							</span>
						</td>
						<td class="metric-cell">{{ (r.models || []).length }}</td>
						<td>
							<template v-for="(p, i) in r.providers || []" :key="p">
								<span v-if="i > 0">, </span>
								<router-link
									:to="'/providers/' + encodeURIComponent(p)"
									class="resource-link"
									>{{ p }}</router-link
								>
							</template>
						</td>
						<td class="metric-cell">{{ fmtNum(r.requests) }}</td>
						<td class="metric-cell">{{ fmtNum(r.failures) }}</td>
						<td
							class="metric-cell"
							:class="
								r.successRate >= 99
									? 'text-success'
									: r.successRate >= 95
										? 'text-warning'
										: 'text-error'
							"
						>
							{{ r.successRate > 0 ? r.successRate.toFixed(1) + "%" : "-" }}
						</td>
						<td class="metric-cell">{{ formatTPS(r.outputRate) }}</td>
						<td class="metric-cell">{{ formatMs(r.latencyP95) }}</td>
					</tr>
					<tr v-if="filteredRoutes.length === 0">
						<td colspan="10" class="empty" style="padding: 16px 0">
							{{ $t("routes.noMatch", { query: search }) }}
						</td>
					</tr>
				</tbody>
			</table>
		</div>
	</div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from "vue";
import RealtimeLineChart from "../components/RealtimeLineChart.vue";
import { createStatusStream, createMetricsStream } from "../api.js";
import { fmtNum } from "../utils.js";

const status = ref(null);
const metricsData = ref(null);
const error = ref("");
const search = ref("");
const chartGroup = "routes-time";
let statusStop = null;
let metricsStop = null;

const routePalette = [
	"#2563eb",
	"#7c3aed",
	"#ea580c",
	"#0f766e",
	"#dc2626",
	"#0891b2",
	"#ca8a04",
	"#4f46e5",
];

const filtered = computed(() => {
	const routes = status.value?.routes ?? [];
	const q = search.value.trim().toLowerCase();
	if (!q) return routes;
	return routes.filter(
		(r) =>
			r.prefix.toLowerCase().includes(q) ||
			String(r.protocol || "").toLowerCase().includes(q) ||
			(r.models || []).some((m) => m.toLowerCase().includes(q)) ||
			(r.providers || []).some((p) => p.toLowerCase().includes(q)),
	);
});

const realtimeRouteRequestHistory = computed(
	() => metricsData.value?.realtime?.routes?.requests ?? [],
);
const realtimeRouteErrorHistory = computed(() => metricsData.value?.realtime?.routes?.errors ?? []);
const realtimeRouteOutputHistory = computed(
	() => metricsData.value?.realtime?.routes?.output ?? [],
);

const chartTimeRange = computed(() => {
	const windowSeconds = Number(metricsData.value?.realtime?.window_seconds || 0);
	const latestTs = Math.max(
		realtimeRouteRequestHistory.value[realtimeRouteRequestHistory.value.length - 1]?.ts || 0,
		realtimeRouteErrorHistory.value[realtimeRouteErrorHistory.value.length - 1]?.ts || 0,
		realtimeRouteOutputHistory.value[realtimeRouteOutputHistory.value.length - 1]?.ts || 0,
	);
	if (!latestTs || !windowSeconds) return null;
	return {
		start: latestTs - windowSeconds * 1000,
		end: latestTs,
	};
});

function routeColor(route) {
	let hash = 0;
	for (let i = 0; i < route.length; i += 1) {
		hash = (hash << 5) - hash + route.charCodeAt(i);
		hash |= 0;
	}
	return routePalette[Math.abs(hash) % routePalette.length];
}

function pickTopRouteNames(points, limit = 4) {
	const latestPoint = points[points.length - 1]?.routes ?? {};
	const latestValues = new Map();
	const peakValues = new Map();

	for (const point of points) {
		for (const [route, value] of Object.entries(point.routes ?? {})) {
			const numeric = Number(value || 0);
			peakValues.set(route, Math.max(peakValues.get(route) ?? 0, numeric));
		}
	}

	for (const [route, value] of Object.entries(latestPoint)) {
		latestValues.set(route, Number(value || 0));
	}

	return Array.from(new Set([...peakValues.keys(), ...latestValues.keys()]))
		.filter((route) => (peakValues.get(route) ?? 0) > 0 || (latestValues.get(route) ?? 0) > 0)
		.sort((a, b) => {
			const latestDiff = (latestValues.get(b) ?? 0) - (latestValues.get(a) ?? 0);
			if (latestDiff !== 0) return latestDiff;
			const peakDiff = (peakValues.get(b) ?? 0) - (peakValues.get(a) ?? 0);
			if (peakDiff !== 0) return peakDiff;
			return a.localeCompare(b);
		})
		.slice(0, limit);
}

function buildRoutePoints(points, routeNames) {
	return points.map((point) => {
		const row = { ts: point.ts };
		for (const route of routeNames) {
			row[route] = Number(point.routes?.[route] || 0);
		}
		return row;
	});
}

function buildRouteSeries(routeNames) {
	return routeNames.map((route) => ({
		key: route,
		name: route,
		color: routeColor(route),
	}));
}

function buildRouteLegend(routeNames, points, formatter) {
	const latestPoint = points[points.length - 1]?.routes ?? {};
	return routeNames.map((route) => ({
		route,
		color: routeColor(route),
		value: formatter(Number(latestPoint[route] || 0)),
	}));
}

const requestTrendRoutes = computed(() => pickTopRouteNames(realtimeRouteRequestHistory.value));
const requestTrendPoints = computed(() =>
	buildRoutePoints(realtimeRouteRequestHistory.value, requestTrendRoutes.value),
);
const requestTrendSeries = computed(() => buildRouteSeries(requestTrendRoutes.value));
const requestTrendLegend = computed(() =>
	buildRouteLegend(requestTrendRoutes.value, realtimeRouteRequestHistory.value, formatPerMinute),
);

const errorTrendRoutes = computed(() => pickTopRouteNames(realtimeRouteErrorHistory.value));
const errorTrendPoints = computed(() =>
	buildRoutePoints(realtimeRouteErrorHistory.value, errorTrendRoutes.value),
);
const errorTrendSeries = computed(() => buildRouteSeries(errorTrendRoutes.value));
const errorTrendLegend = computed(() =>
	buildRouteLegend(errorTrendRoutes.value, realtimeRouteErrorHistory.value, formatPercent),
);

const outputTrendRoutes = computed(() => pickTopRouteNames(realtimeRouteOutputHistory.value));
const outputTrendPoints = computed(() =>
	buildRoutePoints(realtimeRouteOutputHistory.value, outputTrendRoutes.value),
);
const outputTrendSeries = computed(() => buildRouteSeries(outputTrendRoutes.value));
const outputTrendLegend = computed(() =>
	buildRouteLegend(outputTrendRoutes.value, realtimeRouteOutputHistory.value, formatTPSValue),
);

function quantileFromHistogramBuckets(buckets, quantile) {
	const levels = Object.keys(buckets)
		.map(Number)
		.filter(Number.isFinite)
		.sort((a, b) => a - b);
	if (!levels.length) return { value: 0, count: 0 };
	const total = buckets[levels[levels.length - 1]];
	if (!total) return { value: 0, count: 0 };
	const rank = total * quantile;
	let prevLe = 0;
	let prevCount = 0;
	for (const le of levels) {
		const cum = buckets[le];
		if (cum >= rank) {
			const bucketCount = cum - prevCount;
			if (bucketCount <= 0) return { value: le, count: total };
			const ratio = Math.max(0, Math.min(1, (rank - prevCount) / bucketCount));
			return { value: prevLe + (le - prevLe) * ratio, count: total };
		}
		prevLe = le;
		prevCount = cum;
	}
	return { value: levels[levels.length - 1], count: total };
}

const routeMetrics = computed(() => {
	const map = {};
	for (const row of metricsData.value?.route_requests_total ?? []) {
		if (!map[row.route]) {
			map[row.route] = {
				route: row.route,
				success: 0,
				failure: 0,
				requests: 0,
				latencyP95: 0,
				ttftWeighted: 0,
				ttftSamples: 0,
				throughputWeighted: 0,
				throughputSamples: 0,
				outputRate: 0,
			};
		}
		if (row.status === "failure") map[row.route].failure += row.value;
		else map[row.route].success += row.value;
	}

	const durationBuckets = {};
	for (const row of metricsData.value?.route_request_duration ?? []) {
		if (!durationBuckets[row.route]) durationBuckets[row.route] = {};
		const le = Number(row.le);
		if (!Number.isFinite(le)) continue;
		durationBuckets[row.route][le] =
			(durationBuckets[row.route][le] ?? 0) + Number(row.value || 0);
	}

	for (const route of Object.keys(durationBuckets)) {
		if (!map[route]) {
			map[route] = {
				route,
				success: 0,
				failure: 0,
				requests: 0,
				latencyP95: 0,
				ttftWeighted: 0,
				ttftSamples: 0,
				throughputWeighted: 0,
				throughputSamples: 0,
				outputRate: 0,
			};
		}
		const q = quantileFromHistogramBuckets(durationBuckets[route], 0.95);
		map[route].latencyP95 = q.value;
	}

	for (const row of metricsData.value?.route_stream_ttft_p95_ms ?? []) {
		if (!map[row.route]) {
			map[row.route] = {
				route: row.route,
				success: 0,
				failure: 0,
				requests: 0,
				latencyP95: 0,
				ttftWeighted: 0,
				ttftSamples: 0,
				throughputWeighted: 0,
				throughputSamples: 0,
				outputRate: 0,
			};
		}
		const count = Number(row.count || 0);
		map[row.route].ttftWeighted += Number(row.value || 0) * count;
		map[row.route].ttftSamples += count;
	}

	for (const row of metricsData.value?.route_throughput_p99_tokens ?? []) {
		if (!map[row.route]) {
			map[row.route] = {
				route: row.route,
				success: 0,
				failure: 0,
				requests: 0,
				latencyP95: 0,
				ttftWeighted: 0,
				ttftSamples: 0,
				throughputWeighted: 0,
				throughputSamples: 0,
				outputRate: 0,
			};
		}
		const count = Number(row.count || 0);
		map[row.route].throughputWeighted += Number(row.value || 0) * count;
		map[row.route].throughputSamples += count;
	}

	for (const row of metricsData.value?.route_token_rate ?? []) {
		if (row.type !== "completion") continue;
		if (!row.route) continue;
		if (!map[row.route]) {
			map[row.route] = {
				route: row.route,
				success: 0,
				failure: 0,
				requests: 0,
				latencyP95: 0,
				ttftWeighted: 0,
				ttftSamples: 0,
				throughputWeighted: 0,
				throughputSamples: 0,
				outputRate: 0,
			};
		}
		map[row.route].outputRate = Math.max(map[row.route].outputRate, Number(row.value || 0));
	}

	return Object.values(map).map((item) => {
		const requests = item.success + item.failure;
		return {
			route: item.route,
			requests,
			successRate: requests > 0 ? (item.success / requests) * 100 : 0,
			latencyP95: item.latencyP95,
			ttftP95: item.ttftSamples > 0 ? item.ttftWeighted / item.ttftSamples : 0,
			throughputP99:
				item.throughputSamples > 0 ? item.throughputWeighted / item.throughputSamples : 0,
			outputRate: item.outputRate || 0,
			failure: item.failure,
			total: requests,
		};
	});
});

const routeMetricMap = computed(() => {
	const map = {};
	for (const item of routeMetrics.value) map[item.route] = item;
	return map;
});

const filteredRoutes = computed(() => {
	return filtered.value
		.map((route) => {
			const metric = routeMetricMap.value[route.prefix] || {};
			return {
				...route,
				requests: metric.requests || 0,
				failures: metric.failure || 0,
				successRate: metric.successRate || 0,
				outputRate: metric.outputRate || 0,
				latencyP95: metric.latencyP95 || 0,
				ttftP95: metric.ttftP95 || 0,
				throughputP99: metric.throughputP99 || 0,
			};
		})
		.sort((a, b) => b.requests - a.requests || a.prefix.localeCompare(b.prefix));
});

function formatMs(value) {
	if (!value || value < 0) return "-";
	return `${Math.round(value)}ms`;
}

function formatTPS(value) {
	if (!value || value < 0) return "-";
	return `${value.toFixed(1)}/s`;
}

function formatPerMinute(value) {
	return `${Number(value || 0).toFixed(1)}/min`;
}

function formatPercent(value) {
	return `${Number(value || 0).toFixed(1)}%`;
}

function formatTPSValue(value) {
	return `${Number(value || 0).toFixed(1)}/s`;
}

function formatCountAxis(value) {
	if (value >= 1000) return `${(value / 1000).toFixed(1)}k`;
	return Number(value || 0).toFixed(0);
}

function formatPercentAxis(value) {
	return `${Number(value || 0).toFixed(0)}%`;
}

function formatTPSAxis(value) {
	return `${Number(value || 0).toFixed(0)}/s`;
}

onMounted(() => {
	statusStop = createStatusStream().start(
		(data) => {
			status.value = data;
			error.value = "";
		},
		(e) => {
			error.value = e.message;
		},
	);
	metricsStop = createMetricsStream().start(
		(data) => {
			metricsData.value = data;
		},
		() => {
			/* ignore errors */
		},
	);
});

onUnmounted(() => {
	if (statusStop) statusStop();
	if (metricsStop) metricsStop();
});
</script>

<style scoped>
.page-header {
	display: flex;
	flex-direction: column;
	align-items: stretch;
	gap: 12px;
	margin-bottom: 20px;
}

.page-header-main {
	display: flex;
	justify-content: space-between;
	align-items: center;
	gap: 12px;
}

.page-header .page-title {
	margin-bottom: 0;
	flex-shrink: 0;
}
.search-input {
	max-width: 280px;
	font-family: inherit;
}

.route-stats {
	display: grid;
	grid-template-columns: repeat(3, 1fr);
	gap: 12px;
	margin-bottom: 16px;
}
.route-stat-card {
	background: var(--c-surface);
	border: 1px solid var(--c-border);
	border-radius: var(--radius);
	box-shadow: var(--shadow);
	padding: 14px 16px;
}
.route-stat-title {
	font-size: 11px;
	font-weight: 600;
	color: var(--c-text-3);
	text-transform: uppercase;
	letter-spacing: 0.04em;
}

.metric-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	gap: 12px;
	margin-bottom: 12px;
}

.metric-count {
	font-size: 12px;
	color: var(--c-text-3);
}

.trend-chart {
	height: 148px;
	margin-bottom: 8px;
	border: 1px solid var(--c-border-light);
	border-radius: 10px;
	background: linear-gradient(180deg, #ffffff 0%, #f8fbff 100%);
	overflow: hidden;
	position: relative;
}

.metric-stats {
	display: flex;
	flex-direction: column;
	gap: 6px;
}

.trend-legend-row {
	display: flex;
	align-items: center;
	gap: 8px;
	font-size: 12px;
}

.trend-dot {
	width: 8px;
	height: 8px;
	border-radius: 999px;
	flex-shrink: 0;
}

.trend-route {
	color: var(--c-text);
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	flex: 1;
	min-width: 0;
}

.trend-value {
	color: var(--c-text-3);
	font-family: monospace;
	font-size: 11px;
	flex-shrink: 0;
}

.stat-empty {
	font-size: 12px;
	color: var(--c-text-3);
}
.metric-cell {
	color: var(--c-text-2);
	font-family: var(--font-mono);
	font-size: 12px;
}

@media (max-width: 768px) {
	.page-header-main {
		flex-direction: column;
		align-items: flex-start;
		gap: 10px;
	}
	.search-input {
		max-width: 100%;
	}
	.route-stats {
		grid-template-columns: 1fr;
	}
}

@media (max-width: 480px) {
	.route-stats {
		grid-template-columns: 1fr;
	}
}
</style>
