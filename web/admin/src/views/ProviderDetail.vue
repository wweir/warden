<template>
	<div>
		<div class="breadcrumb">
			<router-link to="/">{{ $t("dashboard.title") }}</router-link>
			<span class="sep">/</span>
			<router-link to="/providers">{{ $t("providers.title") }}</router-link>
			<span class="sep">/</span>
			<span class="current">{{ pageTitle }}</span>
		</div>

		<h2 class="page-title">{{ pageTitle }}</h2>

		<div v-if="configSource && !configSource.source_type?.file" class="msg warning">
			{{ $t("config.nonFileWarning", { path: configSource.config_path || "remote" }) }}
		</div>

		<div v-if="configFileChanged" class="msg warning">
			{{ $t("config.externalChange") }}
			<button @click="load" class="btn btn-sm">{{ $t("common.reload") }}</button>
		</div>

		<div v-if="message" class="msg success">{{ message }}</div>
		<div v-if="error" class="msg error">{{ error }}</div>

		<div v-if="loading" class="msg">{{ $t("common.loading") }}</div>
		<div v-else class="detail-layout">
			<section class="info-section">
				<div class="section-top">
					<div>
						<h3>{{ $t("providerDetail.configEditor") }}</h3>
						<p class="section-desc">{{ $t("providerDetail.configEditorDesc") }}</p>
					</div>
					<div class="actions">
						<button
							@click="apply"
							class="btn btn-primary"
							:disabled="saving || (configSource && !configSource.source_type?.file)"
						>
							{{
								saving
									? waitingAlive
										? $t("config.waitingService", { n: waitingElapsed })
										: $t("providerDetail.saving")
									: $t("providerDetail.saveApply")
							}}
						</button>
						<button
							v-if="dirty && !saving"
							@click="discard"
							class="btn btn-secondary"
						>
							{{ $t("config.discardChanges") }}
						</button>
						<button
							v-if="!create && !saving"
							@click="deleteProvider"
							class="btn btn-danger"
						>
							{{ $t("providerDetail.deleteProvider") }}
						</button>
					</div>
				</div>

				<div class="form-grid">
					<label>{{ $t("providerDetail.name") }} <span class="req">*</span></label>
					<input
						v-if="create"
						v-model.trim="providerName"
						class="form-input"
						:placeholder="$t('providerDetail.namePlaceholder')"
					/>
					<input v-else :value="providerName" class="form-input" readonly />

					<label>{{ $t("providerDetail.family") }} <span class="req">*</span></label>
					<select v-model="providerConfig.family" class="form-input">
						<option value="">{{ $t("providerDetail.selectFamily") }}</option>
						<option value="openai">openai</option>
						<option value="anthropic">anthropic</option>
						<option value="qwen">qwen</option>
						<option value="copilot">copilot</option>
					</select>

					<template v-if="providerFamily(providerConfig) === 'openai'">
						<label>backend</label>
						<select v-model="providerConfig.backend" class="form-input">
							<option value="">default</option>
							<option value="cliproxy">cliproxy</option>
						</select>

						<template v-if="providerBackend(providerConfig) === 'cliproxy'">
							<label>backend_provider <span class="req">*</span></label>
							<input
								v-model="providerConfig.backend_provider"
								class="form-input"
								placeholder="codex"
							/>
						</template>
					</template>

					<template
						v-if="providerFamily(providerConfig) && !['qwen', 'copilot'].includes(providerFamily(providerConfig))"
					>
						<label>url <span class="req">*</span></label>
						<div class="url-field">
							<input
								v-model="providerConfig.url"
								class="form-input"
								:placeholder="providerUrlPlaceholder(providerFamily(providerConfig))"
							/>
							<input
								v-model="providerConfig.proxy"
								class="form-input url-proxy"
								placeholder="proxy (socks5://...)"
							/>
						</div>

						<label>api_key</label>
						<div class="secret-field">
							<input
								:type="showAPIKey ? 'text' : 'password'"
								:value="secretDisplay(providerConfig.api_key)"
								@input="apiKeyTouched = true; providerConfig.api_key = $event.target.value"
								class="form-input"
								placeholder="(not set)"
							/>
							<button class="btn-icon" @click="showAPIKey = !showAPIKey" type="button" :aria-label="$t('providerDetail.toggleApiKeyVisibility')">
								{{ showAPIKey ? "🙈" : "👁" }}
							</button>
							<span
								:class="[
									'badge',
									isSecretConfigured(providerConfig.api_key)
										? 'badge-ok'
										: 'badge-none',
								]"
							>
								{{
									isSecretConfigured(providerConfig.api_key)
										? $t("common.configured")
										: $t("common.notSet")
								}}
							</span>
						</div>
					</template>

					<template v-if="['qwen', 'copilot'].includes(providerFamily(providerConfig))">
						<label>config_dir</label>
						<input
							v-model="providerConfig.config_dir"
							class="form-input"
							:placeholder="
								providerFamily(providerConfig) === 'qwen'
									? '~/.qwen'
									: '~/.config/github-copilot'
							"
						/>

						<label>proxy</label>
						<input
							v-model="providerConfig.proxy"
							class="form-input"
							placeholder="socks5://127.0.0.1:1080"
						/>
					</template>

					<label>timeout</label>
					<input v-model="providerConfig.timeout" class="form-input" placeholder="60s" />

					<label>service_protocols</label>
					<div class="service-protocols-editor">
						<TagListEditor
							v-model="providerConfig.service_protocols"
							:suggestions="serviceProtocolSuggestions"
							placeholder="adapter defaults"
						/>
						<p class="hint service-protocols-hint">empty = adapter defaults</p>
					</div>

					<template v-if="providerFamily(providerConfig) === 'openai'">
						<label>responses_to_chat</label>
						<div class="form-hint-row">
							<input
								type="checkbox"
								v-model="providerConfig.responses_to_chat"
								class="form-checkbox"
							/>
							<span class="hint">{{ $t("config.responsesToChatHint") }}</span>
						</div>

						<label>anthropic_to_chat</label>
						<div class="form-hint-row">
							<input
								type="checkbox"
								v-model="providerConfig.anthropic_to_chat"
								class="form-checkbox"
							/>
							<span class="hint">{{ $t("config.anthropicToChatHint") }}</span>
						</div>
					</template>

					<template
						v-if="providerFamily(providerConfig) && !['qwen', 'copilot'].includes(providerFamily(providerConfig))"
					>
						<label>headers</label>
						<KeyValueEditor
							v-model="providerConfig.headers"
							keyPlaceholder="Header name"
							valuePlaceholder="Value"
						/>
					</template>

					<label>models</label>
					<div class="models-editor">
						<div class="models-toolbar">
							<div class="models-copy">
								<p class="models-guide">{{ $t("providerDetail.modelsGuide") }}</p>
								<div class="models-meta">
									<span class="badge badge-none">
										{{ $t("providerDetail.modelsOptional") }}
									</span>
									<span class="hint">
										{{ $t("providerDetail.modelsConfiguredCount", { n: providerConfig.models.length }) }}
									</span>
									<span v-if="!create" class="hint">
										{{ $t("providerDetail.modelsDiscoveredCount", { n: discoveredModelIds.length }) }}
									</span>
								</div>
							</div>
							<button
								v-if="missingDiscoveredModelIds.length > 0"
								type="button"
								class="btn btn-secondary btn-sm"
								@click="appendDiscoveredModels"
							>
								{{ $t("providerDetail.addDiscoveredModels", { n: missingDiscoveredModelIds.length }) }}
							</button>
						</div>
						<p class="hint models-hint">
							{{
								discoveredModelIds.length > 0
									? $t("providerDetail.modelsSuggestionHint", { n: discoveredModelIds.length })
									: $t("providerDetail.modelsNoSuggestionHint")
							}}
						</p>
						<TagListEditor
							v-model="providerConfig.models"
							:suggestions="discoveredModelIds"
							:placeholder="$t('providerDetail.modelsPlaceholder')"
						/>
						<p class="hint models-hint">
							{{ $t("providerDetail.modelsBehaviorHint") }}
						</p>
					</div>

				</div>
			</section>

			<div v-if="!create && detail">
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
							<td>{{ $t("providerDetail.family") }}</td>
							<td>{{ detail.family || detail.protocol }}</td>
						</tr>
						<tr v-if="detail.backend">
							<td>backend</td>
							<td>{{ detail.backend }}</td>
						</tr>
						<tr v-if="detail.backend_provider">
							<td>backend_provider</td>
							<td>{{ detail.backend_provider }}</td>
						</tr>
						<tr>
							<td>{{ $t("providerDetail.supportedProtocols") }}</td>
							<td>{{ (detail.supported_protocols || []).join(", ") || "-" }}</td>
						</tr>
						<tr>
							<td>{{ $t("providerDetail.candidateProtocols") }}</td>
							<td>{{ (detail.candidate_protocols || []).join(", ") || "-" }}</td>
						</tr>
						<tr>
							<td>{{ $t("providerDetail.configuredProtocols") }}</td>
							<td>{{ (detail.configured_protocols || []).join(", ") || "-" }}</td>
						</tr>
						<tr>
							<td>{{ $t("providerDetail.displayProtocols") }}</td>
							<td>{{ (detail.display_protocols || []).join(", ") || "-" }}</td>
						</tr>
						<tr v-if="(detail.family || detail.protocol) === 'openai'">
							<td>responses_to_chat</td>
							<td>{{ detail.responses_to_chat ? $t("common.on") : $t("common.off") }}</td>
						</tr>
						<tr v-if="(detail.family || detail.protocol) === 'openai'">
							<td>anthropic_to_chat</td>
							<td>{{ detail.anthropic_to_chat ? $t("common.on") : $t("common.off") }}</td>
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

				<section class="info-section">
					<h3>{{ $t("providerDetail.protocolDetection") }}</h3>
					<div class="runtime-actions">
						<button
							@click="runProtocolDetect"
							class="btn btn-secondary"
							:disabled="detectingProtocols"
						>
							{{ detectingProtocols ? $t("providerDetail.detecting") : $t("providerDetail.detectDisplayProtocols") }}
						</button>
						<span v-if="detail.last_protocol_probe" class="hint">
							{{ detail.last_protocol_probe.status }} · {{ formatTime(detail.last_protocol_probe.checked_at) }}
							<span v-if="detail.last_protocol_probe.error"> · {{ detail.last_protocol_probe.error }}</span>
						</span>
					</div>

					<div class="probe-grid">
						<select v-model="selectedProbeModel" class="form-input">
							<option value="">{{ $t("providerDetail.selectModel") }}</option>
							<option v-for="model in probeableModels" :key="model" :value="model">
								{{ model }}
							</option>
						</select>
						<select v-model="selectedProbeProtocol" class="form-input">
							<option value="chat">chat</option>
							<option value="responses_stateless">responses_stateless</option>
							<option value="responses_stateful">responses_stateful</option>
							<option value="anthropic">anthropic</option>
						</select>
						<button
							@click="runExactProtocolProbe"
							class="btn btn-primary"
							:disabled="exactProbing || !selectedProbeModel"
						>
							{{ exactProbing ? $t("providerDetail.probing") : $t("providerDetail.probeModelProtocol") }}
						</button>
					</div>

					<div v-if="protocolProbeResult" class="hint" :class="protocolProbeResult.status === 'supported' ? 'text-success' : protocolProbeResult.status === 'unsupported' ? 'text-error' : 'text-warning'">
						{{ protocolProbeResult.model }} · {{ protocolProbeResult.protocol }} · {{ protocolProbeResult.status }}
						<span v-if="protocolProbeResult.error"> · {{ protocolProbeResult.error }}</span>
					</div>

					<table v-if="exactProbeResults.length > 0" class="data-table probe-results-table">
						<thead>
							<tr>
								<th>{{ $t("providerDetail.probeColModel") }}</th>
								<th>{{ $t("providerDetail.probeColProtocol") }}</th>
								<th>{{ $t("providerDetail.probeColStatus") }}</th>
								<th>{{ $t("providerDetail.probeColCheckedAt") }}</th>
								<th>{{ $t("providerDetail.probeColError") }}</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="probe in exactProbeResults" :key="`${probe.model}/${probe.protocol}`">
								<td><code>{{ probe.model }}</code></td>
								<td><code>{{ probe.protocol }}</code></td>
								<td>{{ probe.status }}</td>
								<td>{{ formatTime(probe.checked_at) }}</td>
								<td>{{ probe.error || "-" }}</td>
							</tr>
						</tbody>
					</table>
				</section>

				<div class="actions runtime-actions">
					<button @click="runHealthCheck" class="btn btn-primary" :disabled="checking">
						{{
							checking ? $t("providerDetail.checking") : $t("providerDetail.healthCheck")
						}}
					</button>
					<button
						v-if="detail.status && !detail.status.manual_suppressed"
						@click="suppressProvider"
						class="btn btn-danger"
					>
						{{ $t("providerDetail.suppressBtn") }}
					</button>
					<button
						v-else-if="detail.status"
						@click="unsuppressProvider"
						class="btn btn-secondary"
					>
						{{ $t("providerDetail.unsuppressBtn") }}
					</button>
					<span
						v-if="healthResult"
						class="health-result"
						:class="healthResult.status === 'ok' ? 'text-success' : 'text-error'"
					>
						{{
							healthResult.status === "ok"
								? $t("providerDetail.healthOk", {
										latency: healthResult.latency_ms,
										count: healthResult.model_count,
									})
								: $t("providerDetail.healthError", { error: healthResult.error })
						}}
					</span>
				</div>
			</div>
		</div>
	</div>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";
import {
	fetchConfig,
	fetchConfigSource,
	saveConfig,
	validateConfig,
	restartGateway,
	fetchStatus,
	fetchProviderDetail,
	healthCheck,
	setProviderSuppress,
	detectProviderProtocols,
	probeProviderModelProtocol,
} from "../api.js";
import KeyValueEditor from "../components/KeyValueEditor.vue";
import TagListEditor from "../components/TagListEditor.vue";

const { t } = useI18n();
const router = useRouter();

const REDACTED = "__REDACTED__";

const props = defineProps({
	name: { type: String, default: "" },
	create: { type: Boolean, default: false },
});

const detail = ref(null);
const configDoc = ref({});
const configSource = ref(null);
const providerName = ref("");
const providerConfig = ref(createEmptyProviderConfig());
const error = ref("");
const message = ref("");
const checking = ref(false);
const healthResult = ref(null);
const saving = ref(false);
const loading = ref(false);
const dirty = ref(false);
const suppressDirty = ref(false);
const showAPIKey = ref(false);
const apiKeyTouched = ref(false);
const configFileChanged = ref(false);
const waitingAlive = ref(false);
const waitingElapsed = ref(0);
const detectingProtocols = ref(false);
const selectedProbeModel = ref("");
const selectedProbeProtocol = ref("chat");
const protocolProbeResult = ref(null);
const exactProbing = ref(false);

watch(
	[providerName, providerConfig],
	() => {
		if (!suppressDirty.value) dirty.value = true;
	},
	{ deep: true },
);

watch(
	() => [props.name, props.create],
	() => {
		load();
	},
	{ immediate: true },
);

const pageTitle = computed(() =>
	props.create ? t("providerDetail.newProviderTitle") : providerName.value || props.name,
);

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

const discoveredModelIds = computed(() => {
	const ids = [];
	for (const model of parsedModels.value) {
		const id = typeof model?.id === "string" ? model.id.trim() : "";
		if (!id || ids.includes(id)) continue;
		ids.push(id);
	}
	return ids;
});

const missingDiscoveredModelIds = computed(() =>
	discoveredModelIds.value.filter((id) => !(providerConfig.value.models || []).includes(id)),
);

const probeableModels = computed(() =>
	discoveredModelIds.value.length > 0 ? discoveredModelIds.value : [...(providerConfig.value.models || [])],
);

const exactProbeResults = computed(() => {
	const entries = detail.value?.model_protocol_probes || [];
	return [...entries].sort((a, b) => {
		const modelCmp = String(a.model || "").localeCompare(String(b.model || ""));
		if (modelCmp !== 0) return modelCmp;
		return String(a.protocol || "").localeCompare(String(b.protocol || ""));
	});
});

function cloneData(value) {
	return JSON.parse(JSON.stringify(value ?? {}));
}

function createEmptyProviderConfig() {
	return {
		url: "",
		family: "",
		backend: "",
		backend_provider: "",
		service_protocols: [],
		models: [],
		responses_to_chat: false,
		anthropic_to_chat: false,
	};
}

function providerFamily(provider) {
	return String(provider?.family || "").trim().toLowerCase();
}

function providerBackend(provider) {
	return String(provider?.backend || "").trim().toLowerCase();
}

const serviceProtocolSuggestions = computed(() => {
	switch (providerFamily(providerConfig.value)) {
		case "openai": {
			const protocols = ["chat", "responses_stateless", "responses_stateful", "embeddings"];
			if (providerConfig.value?.anthropic_to_chat) protocols.push("anthropic");
			return protocols;
		}
		case "anthropic":
			return ["chat", "anthropic"];
		case "qwen":
		case "copilot":
			return ["chat"];
		default:
			return [];
	}
});

function normalizeServiceProtocols(protocols) {
	const out = [];
	const seen = new Set();
	for (const raw of protocols || []) {
		const protocol = String(raw || "").trim().toLowerCase();
		if (!protocol || seen.has(protocol)) continue;
		seen.add(protocol);
		out.push(protocol);
		if (protocol === "responses_stateful" && !seen.has("responses_stateless")) {
			seen.add("responses_stateless");
			out.push("responses_stateless");
		}
	}
	return out;
}

function secretDisplay(val) {
	return val === REDACTED ? REDACTED : val || "";
}

function isSecretConfigured(val) {
	return !!val;
}

function providerUrlPlaceholder(protocol) {
	switch (protocol) {
		case "":
			return "Select family first";
		case "anthropic":
			return "https://api.anthropic.com";
		case "qwen":
			return "(defaults to dashscope or portal.qwen.ai)";
		case "copilot":
			return "(defaults to api.githubcopilot.com)";
		default:
			return "https://api.openai.com/v1";
	}
}

function appendDiscoveredModels() {
	if (missingDiscoveredModelIds.value.length === 0) return;
	providerConfig.value.models = [
		...(providerConfig.value.models || []),
		...missingDiscoveredModelIds.value,
	];
}

function formatTime(timeValue) {
	if (!timeValue) return "";
	return new Date(timeValue).toLocaleString();
}

function cleanConfig(obj) {
	if (obj === null || obj === undefined) return obj;
	if (Array.isArray(obj)) return obj;
	if (typeof obj !== "object") return obj;

	const out = {};
	for (const [key, value] of Object.entries(obj)) {
		if (value === null || value === undefined) continue;
		if (typeof value === "object" && !Array.isArray(value)) {
			const cleaned = {};
			for (const [innerKey, innerValue] of Object.entries(value)) {
				if (innerKey.startsWith("__new_")) continue;
				cleaned[innerKey] = cleanConfig(innerValue);
			}
			if (Object.keys(cleaned).length > 0) out[key] = cleaned;
		} else {
			out[key] = value;
		}
	}
	return out;
}

function pruneProviderReferences(nextConfig, targetProvider) {
	if (!nextConfig?.route || typeof nextConfig.route !== "object") return;

	for (const [prefix, route] of Object.entries(nextConfig.route)) {
		if (!route || typeof route !== "object") continue;

		const nextExactModels = {};
		for (const [modelName, modelCfg] of Object.entries(route.exact_models || {})) {
			if (!modelCfg || typeof modelCfg !== "object") continue;
			const upstreams = (modelCfg.upstreams || []).filter(
				(upstream) => upstream?.provider !== targetProvider,
			);
			if (upstreams.length === 0) continue;
			nextExactModels[modelName] = { ...modelCfg, upstreams };
		}

		const nextWildcardModels = {};
		for (const [pattern, modelCfg] of Object.entries(route.wildcard_models || {})) {
			if (!modelCfg || typeof modelCfg !== "object") continue;
			const providers = (modelCfg.providers || []).filter(
				(provider) => provider !== targetProvider,
			);
			if (providers.length === 0) continue;
			nextWildcardModels[pattern] = { ...modelCfg, providers };
		}

		if (Object.keys(nextExactModels).length === 0 && Object.keys(nextWildcardModels).length === 0) {
			delete nextConfig.route[prefix];
			continue;
		}

		route.exact_models = nextExactModels;
		route.wildcard_models = nextWildcardModels;
	}
}

async function load() {
	loading.value = true;
	suppressDirty.value = true;
	error.value = "";
	healthResult.value = null;
	configFileChanged.value = false;
	try {
		const [cfg, source] = await Promise.all([fetchConfig(), fetchConfigSource()]);
		configDoc.value = cfg;
		configSource.value = source;
		showAPIKey.value = false;
		apiKeyTouched.value = false;

		if (props.create) {
			providerName.value = "";
			providerConfig.value = createEmptyProviderConfig();
			detail.value = null;
		} else {
			providerName.value = props.name;
			const provider = cfg.provider?.[props.name];
			if (!provider) {
				throw new Error(t("providerDetail.providerConfigMissing", { name: props.name }));
			}
			providerConfig.value = {
				...createEmptyProviderConfig(),
				...cloneData(provider),
				family: provider.family || provider.protocol || "",
				service_protocols: [...(provider.service_protocols || [])],
				models: [...(provider.models || [])],
			};
			detail.value = await fetchProviderDetail(props.name);
			if (!selectedProbeModel.value && discoveredModelIds.value.length > 0) {
				selectedProbeModel.value = discoveredModelIds.value[0];
			}
		}

		dirty.value = false;
	} catch (e) {
		error.value = e.message;
	} finally {
		suppressDirty.value = false;
		loading.value = false;
	}
}

function discard() {
	if (!confirm(t("config.confirmDiscard"))) return;
	load();
}

async function pollUntilAlive(timeoutMs = 60000, intervalMs = 1500) {
	const deadline = Date.now() + timeoutMs;
	waitingAlive.value = true;
	waitingElapsed.value = 0;
	const startMs = Date.now();
	const ticker = setInterval(() => {
		waitingElapsed.value = Math.floor((Date.now() - startMs) / 1000);
	}, 500);
	try {
		await new Promise((resolve) => setTimeout(resolve, 800));
		while (Date.now() < deadline) {
			try {
				await fetchStatus();
				return true;
			} catch {
				await new Promise((resolve) => setTimeout(resolve, intervalMs));
			}
		}
		return false;
	} finally {
		clearInterval(ticker);
		waitingAlive.value = false;
		waitingElapsed.value = 0;
	}
}

async function apply() {
	saving.value = true;
	message.value = "";
	error.value = "";
	try {
		if (!configSource.value?.source_type?.file) {
			error.value = t("config.savingDisabled");
			return;
		}

		const name = providerName.value.trim();
		if (!name) {
			error.value = t("providerDetail.nameRequired");
			return;
		}
		if (!providerFamily(providerConfig.value)) {
			error.value = t("providerDetail.familyRequired");
			return;
		}
		const family = providerFamily(providerConfig.value);
		if (!['qwen', 'copilot'].includes(family) && !providerConfig.value.url?.trim()) {
			error.value = t("providerDetail.urlRequired");
			return;
		}
		const backend = family === "openai" ? providerBackend(providerConfig.value) : "";
		if (backend === "cliproxy") {
			if (!String(providerConfig.value.backend_provider || "").trim()) {
				error.value = t("providerDetail.backendProviderRequired");
				return;
			}
			if ((providerConfig.value.service_protocols || []).length === 0) {
				error.value = t("providerDetail.backendServiceProtocolsRequired");
				return;
			}
		}

		const nextConfig = cloneData(configDoc.value);
		nextConfig.provider = nextConfig.provider || {};

		if (props.create && nextConfig.provider[name]) {
			error.value = t("providerDetail.providerExists", { name });
			return;
		}

		const nextProviderConfig = cloneData(providerConfig.value);
		nextProviderConfig.family = providerFamily(nextProviderConfig);
		nextProviderConfig.backend = nextProviderConfig.family === "openai" ? providerBackend(nextProviderConfig) : "";
		nextProviderConfig.backend_provider = String(nextProviderConfig.backend_provider || "").trim().toLowerCase();
		if (!nextProviderConfig.backend) {
			delete nextProviderConfig.backend;
			delete nextProviderConfig.backend_provider;
		}
		nextProviderConfig.service_protocols = normalizeServiceProtocols(nextProviderConfig.service_protocols);
		if (nextProviderConfig.service_protocols.length === 0) {
			delete nextProviderConfig.service_protocols;
		}
		delete nextProviderConfig.protocol;
		if (!apiKeyTouched.value) {
			delete nextProviderConfig.api_key;
		}
		nextConfig.provider[name] = nextProviderConfig;

		const cleaned = cleanConfig(nextConfig);
		const result = await validateConfig(cleaned);
		if (!result.valid) {
			error.value = t("config.validationFailed", { error: result.error });
			return;
		}

		await saveConfig(cleaned);
		const restart = await restartGateway();
		if (restart.status !== "ok") {
			error.value = t("config.savedButRestartFailed", {
				error: restart.error || "unknown error",
			});
			return;
		}

		const alive = await pollUntilAlive();
		if (!alive) {
			error.value = t("config.serviceTimeout");
			return;
		}

		if (props.create) {
			await router.replace(`/providers/${encodeURIComponent(name)}`);
		} else {
			await load();
		}

		message.value = t("providerDetail.savedMsg", { name });
	} catch (e) {
		if (e.message?.includes("config file changed externally")) {
			configFileChanged.value = true;
			error.value = t("config.externalChangeError");
		} else {
			error.value = e.message;
		}
	} finally {
		saving.value = false;
	}
}

async function deleteProvider() {
	if (!confirm(t("providerDetail.confirmDeleteProvider", { name: providerName.value }))) return;

	saving.value = true;
	message.value = "";
	error.value = "";
	try {
		if (!configSource.value?.source_type?.file) {
			error.value = t("config.savingDisabled");
			return;
		}

		const nextConfig = cloneData(configDoc.value);
		delete nextConfig.provider?.[providerName.value];
		pruneProviderReferences(nextConfig, providerName.value);

		const cleaned = cleanConfig(nextConfig);
		const result = await validateConfig(cleaned);
		if (!result.valid) {
			error.value = t("config.validationFailed", { error: result.error });
			return;
		}

		await saveConfig(cleaned);
		const restart = await restartGateway();
		if (restart.status !== "ok") {
			error.value = t("config.savedButRestartFailed", {
				error: restart.error || "unknown error",
			});
			return;
		}

		const alive = await pollUntilAlive();
		if (!alive) {
			error.value = t("config.serviceTimeout");
			return;
		}

		await router.push("/providers");
	} catch (e) {
		if (e.message?.includes("config file changed externally")) {
			configFileChanged.value = true;
			error.value = t("config.externalChangeError");
		} else {
			error.value = e.message;
		}
	} finally {
		saving.value = false;
	}
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

async function runProtocolDetect() {
	if (!props.name) return;
	detectingProtocols.value = true;
	error.value = "";
	try {
		await detectProviderProtocols(props.name);
		await load();
	} catch (e) {
		error.value = e.message;
	} finally {
		detectingProtocols.value = false;
	}
}

async function runExactProtocolProbe() {
	if (!props.name || !selectedProbeModel.value) return;
	exactProbing.value = true;
	error.value = "";
	protocolProbeResult.value = null;
	try {
		protocolProbeResult.value = await probeProviderModelProtocol(
			props.name,
			selectedProbeModel.value,
			selectedProbeProtocol.value,
		);
		await load();
	} catch (e) {
		error.value = e.message;
	} finally {
		exactProbing.value = false;
	}
}

async function suppressProvider() {
	try {
		await setProviderSuppress(props.name, true);
		await load();
	} catch (e) {
		error.value = e.message;
	}
}

async function unsuppressProvider() {
	try {
		await setProviderSuppress(props.name, false);
		await load();
	} catch (e) {
		error.value = e.message;
	}
}
</script>

<style scoped>
.section-top {
	display: flex;
	justify-content: space-between;
	align-items: flex-start;
	gap: 16px;
	margin-bottom: 14px;
}

.section-desc {
	margin-top: 6px;
	font-size: 13px;
	color: var(--c-text-3);
	max-width: 720px;
}

.actions {
	display: flex;
	align-items: center;
	gap: 12px;
	flex-wrap: wrap;
}

.runtime-actions {
	margin-top: -8px;
}

.health-result {
	font-size: 13px;
}

.probe-grid {
	display: grid;
	grid-template-columns: minmax(0, 1fr) minmax(0, 220px) auto;
	gap: 10px;
	align-items: center;
	margin-top: 12px;
}

.probe-results-table {
	margin-top: 12px;
}

.form-grid {
	display: grid;
	grid-template-columns: 140px 1fr;
	gap: 10px 14px;
	align-items: start;
}

.form-grid > label {
	padding-top: 7px;
	font-size: 12px;
	color: var(--c-text-2);
	font-family: var(--font-mono);
}

.req {
	color: var(--c-danger);
}

.hint {
	color: var(--c-text-3);
	font-size: 11px;
	font-weight: normal;
}

.url-field {
	display: flex;
	gap: 6px;
}

.url-proxy {
	max-width: 220px;
}

.secret-field {
	display: flex;
	gap: 8px;
	align-items: center;
}

.secret-field .form-input {
	flex: 1;
}

.form-hint-row {
	display: flex;
	align-items: center;
	gap: 10px;
	min-height: 34px;
}

.service-protocols-editor {
	display: flex;
	flex-direction: column;
	gap: 6px;
}

.service-protocols-hint {
	margin: 0;
}

.models-editor {
	display: flex;
	flex-direction: column;
	gap: 8px;
}

.models-toolbar {
	display: flex;
	justify-content: space-between;
	align-items: flex-start;
	gap: 12px;
}

.models-copy {
	display: flex;
	flex-direction: column;
	gap: 6px;
}

.models-guide {
	margin: 0;
	font-size: 13px;
	color: var(--c-text);
}

.models-meta {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	gap: 8px;
}

.models-hint {
	display: block;
	margin: 0;
}

.form-checkbox {
	width: 16px;
	height: 16px;
	margin-top: 1px;
}

.badge-none {
	background: var(--c-border-light);
	color: var(--c-text-3);
}

@media (max-width: 768px) {
	.section-top {
		flex-direction: column;
	}

	.form-grid {
		grid-template-columns: 1fr;
	}

	.probe-grid {
		grid-template-columns: 1fr;
	}

	.form-grid > label {
		padding-top: 0;
	}

	.url-field,
	.secret-field,
	.models-toolbar {
		flex-direction: column;
		align-items: stretch;
	}

	.url-proxy {
		max-width: none;
	}
}
</style>
