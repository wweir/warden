<template>
	<div>
		<div class="breadcrumb">
			<router-link to="/">{{ $t("dashboard.title") }}</router-link>
			<span class="sep">/</span>
			<span class="current">{{ name }}</span>
		</div>

		<div v-if="error" class="msg msg-error">{{ error }}</div>

		<div v-if="detail" class="detail-layout">
			<section class="info-section">
				<h3>{{ $t("providerDetail.basicInfo") }}</h3>
				<table class="info-table">
					<tr>
						<td>{{ $t("providerDetail.name") }}</td>
						<td>{{ detail.name }}</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.url") }}</td>
						<td>
							<code>{{ detail.url }}</code>
						</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.protocol") }}</td>
						<td>{{ detail.protocol }}</td>
					</tr>
					<tr v-if="detail.protocol === 'openai'">
						<td>chat_to_responses</td>
						<td>{{ detail.chat_to_responses ? $t("common.on") : $t("common.off") }}</td>
					</tr>
					<tr v-if="detail.protocol === 'openai'">
						<td>responses_to_chat</td>
						<td>{{ detail.responses_to_chat ? $t("common.on") : $t("common.off") }}</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.timeout") }}</td>
						<td>{{ detail.timeout || $t("providerDetail.defaultTimeout") }}</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.apiKey") }}</td>
						<td>
							{{ detail.has_api_key ? $t("common.configured") : $t("common.notSet") }}
						</td>
					</tr>
				</table>
			</section>

			<section v-if="detail.status" class="info-section">
				<h3>{{ $t("providerDetail.runtimeStatus") }}</h3>
				<table class="info-table">
					<tr>
						<td>{{ $t("providerDetail.consecutiveFailures") }}</td>
						<td>{{ detail.status.consecutive_failures }}</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.manuallySuppressed") }}</td>
						<td>
							{{
								detail.status.manual_suppressed
									? $t("providerDetail.yes")
									: $t("providerDetail.no")
							}}
						</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.suppressed") }}</td>
						<td>
							{{
								detail.status.suppressed
									? $t("providerDetail.yes")
									: $t("providerDetail.no")
							}}
						</td>
					</tr>
					<tr v-if="detail.status.suppressed">
						<td>{{ $t("providerDetail.suppressedUntil") }}</td>
						<td>{{ formatTime(detail.status.suppress_until) }}</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.totalRequests") }}</td>
						<td>{{ detail.status.total_requests }}</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.success") }}</td>
						<td>{{ detail.status.success_count }}</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.failure") }}</td>
						<td>{{ detail.status.failure_count }}</td>
					</tr>
					<tr>
						<td>{{ $t("providerDetail.avgLatency") }}</td>
						<td>
							{{
								detail.status.total_requests > 0
									? detail.status.avg_latency_ms.toFixed(0) + "ms"
									: "-"
							}}
						</td>
					</tr>
				</table>
			</section>

			<section class="info-section">
				<h3>{{ $t("providerDetail.availableModels", { n: detail.models.length }) }}</h3>
				<div v-if="detail.models.length === 0" class="empty">
					{{ $t("providerDetail.noModels") }}
				</div>
				<table v-else class="data-table">
					<thead>
						<tr>
							<th>{{ $t("providerDetail.modelId") }}</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="m in parsedModels" :key="m.id">
							<td>
								<code>{{ m.id }}</code>
							</td>
						</tr>
					</tbody>
				</table>
			</section>

			<div class="actions">
				<button @click="runHealthCheck" class="btn btn-primary" :disabled="checking">
					{{
						checking ? $t("providerDetail.checking") : $t("providerDetail.healthCheck")
					}}
				</button>
				<button
					v-if="detail.status && !detail.status.manual_suppressed"
					@click="suppressProvider"
					class="btn btn-error"
				>
					{{ $t("providerDetail.suppressBtn") }}
				</button>
				<button
					v-else-if="detail.status"
					@click="unsuppressProvider"
					class="btn btn-success"
				>
					{{ $t("providerDetail.unsuppressBtn") }}
				</button>
				<span
					v-if="healthResult"
					class="health-result"
					:class="healthResult.status === 'ok' ? 'text-success' : 'text-error'"
					style="font-size: 13px"
				>
					{{
						healthResult.status === "ok"
							? t("providerDetail.healthOk", {
									latency: healthResult.latency_ms,
									count: healthResult.model_count,
								})
							: t("providerDetail.healthError", { error: healthResult.error })
					}}
				</span>
			</div>
		</div>
	</div>
</template>

<script setup>
import { ref, computed, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import { fetchProviderDetail, healthCheck, setProviderSuppress } from "../api.js";

const { t } = useI18n();

const props = defineProps({ name: String });

const detail = ref(null);
const error = ref("");
const checking = ref(false);
const healthResult = ref(null);

const parsedModels = computed(() => {
	if (!detail.value) return [];
	return detail.value.models.map((m) => {
		if (typeof m === "string") {
			try {
				return JSON.parse(m);
			} catch {
				return { id: m };
			}
		}
		return m;
	});
});

async function load() {
	try {
		detail.value = await fetchProviderDetail(props.name);
		error.value = "";
	} catch (e) {
		error.value = e.message;
	}
}

function formatTime(t) {
	if (!t) return "";
	return new Date(t).toLocaleString();
}

async function runHealthCheck() {
	checking.value = true;
	healthResult.value = null;
	try {
		healthResult.value = await healthCheck(props.name);
	} catch (e) {
		healthResult.value = { status: "error", error: e.message };
	} finally {
		checking.value = false;
	}
}

async function suppressProvider() {
	try {
		await setProviderSuppress(props.name, true);
		await load();
	} catch (e) {
		console.error("Failed to suppress provider:", e);
	}
}

async function unsuppressProvider() {
	try {
		await setProviderSuppress(props.name, false);
		await load();
	} catch (e) {
		console.error("Failed to unsuppress provider:", e);
	}
}

onMounted(load);
</script>

<style scoped>
.actions {
	display: flex;
	align-items: center;
	gap: 12px;
}
.health-result {
	font-size: 13px;
}
</style>
