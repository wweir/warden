<template>
	<div>
		<h2>{{ $t("config.title") }}</h2>

		<div v-if="configSource && !configSource.source_type?.file" class="msg warning">
			{{ $t("config.nonFileWarning", { path: configSource.config_path || "remote" }) }}
		</div>

		<div v-if="configFileChanged" class="msg warning">
			{{ $t("config.externalChange") }}
			<button @click="load" class="btn btn-sm">{{ $t("common.reload") }}</button>
		</div>

		<div v-if="message" :class="['msg', messageType]">{{ message }}</div>
		<div v-if="error" class="msg error">{{ error }}</div>

		<div class="actions">
			<button
				@click="apply"
				class="btn btn-primary"
				:disabled="applying || (configSource && !configSource.source_type?.file)"
			>
				{{
					applying
						? waitingAlive
							? $t("config.waitingService", { n: waitingElapsed })
							: $t("config.applying")
						: $t("config.apply")
				}}
			</button>
			<button v-if="dirty && !applying" @click="discard" class="btn btn-secondary">
				{{ $t("config.discardChanges") }}
			</button>
		</div>

		<!-- General -->
		<section class="config-section">
			<h3>{{ $t("config.general") }}</h3>
			<div class="form-grid">
				<label>addr</label>
				<input v-model="config.addr" class="form-input" placeholder=":9832" />

				<label>admin_password</label>
				<div class="secret-field">
					<input
						:type="showAdminPw ? 'text' : 'password'"
						:value="adminPwDisplay"
						@input="onAdminPwInput($event.target.value)"
						class="form-input"
						placeholder="(not set)"
					/>
					<button class="btn-icon" @click="showAdminPw = !showAdminPw" type="button">
						{{ showAdminPw ? "🙈" : "👁" }}
					</button>
					<span :class="['badge', adminPwConfigured ? 'badge-ok' : 'badge-none']">
						{{ adminPwConfigured ? "Configured" : "Not set" }}
					</span>
				</div>
			</div>

			<div class="subsection-header">
				<div>
					<span>{{ $t("config.apiKeysSection") }}</span>
					<p class="section-note">{{ $t("config.apiKeysHint") }}</p>
				</div>
				<button class="btn btn-sm" @click="showAPIKeyCreateModal = true">
					{{ $t("config.createApiKey") }}
				</button>
			</div>
			<div v-if="apiKeyRows.length === 0" class="empty-state">
				{{ $t("config.noApiKeys") }}
			</div>
			<table v-else class="data-table api-key-table">
				<thead>
					<tr>
						<th>{{ $t("config.apiKeyRoute") }}</th>
						<th>{{ $t("config.apiKeyName") }}</th>
						<th>{{ $t("config.apiKeyStatus") }}</th>
						<th>{{ $t("config.apiKeyRequests") }}</th>
						<th>{{ $t("config.apiKeySuccess") }}</th>
						<th>{{ $t("config.apiKeyFailure") }}</th>
						<th>{{ $t("config.apiKeyPromptTokens") }}</th>
						<th>{{ $t("config.apiKeyCompletionTokens") }}</th>
						<th>{{ $t("config.apiKeyCacheTokens") }}</th>
						<th>{{ $t("config.apiKeyActions") }}</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="row in apiKeyRows" :key="row.route + ':' + row.name">
						<td><code>{{ row.route }}</code></td>
						<td><code>{{ row.name }}</code></td>
						<td>
							<span :class="['badge', apiKeyConfigured(row.value) ? 'badge-ok' : 'badge-none']">
								{{
									apiKeyConfigured(row.value)
										? $t("common.configured")
										: $t("common.notSet")
								}}
							</span>
						</td>
						<td>{{ row.usage.total_requests }}</td>
						<td>{{ row.usage.success_requests }}</td>
						<td>{{ row.usage.failure_requests }}</td>
						<td>{{ row.usage.prompt_tokens }}</td>
						<td>{{ row.usage.completion_tokens }}</td>
						<td>{{ row.usage.cache_tokens }}</td>
						<td>
							<button class="btn btn-danger btn-sm" @click="deleteAPIKey(row.route, row.name)">
								{{ $t("common.delete") }}
							</button>
						</td>
					</tr>
				</tbody>
			</table>

			<!-- log targets -->
			<div class="subsection-header">
				<span>log.targets</span>
				<button class="btn btn-sm" @click="addLogTarget">
					{{ $t("config.addTarget") }}
				</button>
			</div>
			<div v-for="(t, i) in config.log?.targets || []" :key="i" class="card">
				<div class="card-header" @click="toggleCard('log-target', i)">
					<strong>{{ t.type || "file" }}</strong>
					<span v-if="t.type === 'file'" class="tag-proto">{{
						t.dir || "(no dir)"
					}}</span>
					<span v-else class="tag-proto">{{ t.webhook || "(no webhook)" }}</span>
					<span class="chevron">{{ isCardOpen("log-target", i) ? "▼" : "▶" }}</span>
				</div>
				<div v-show="isCardOpen('log-target', i)" class="card-body">
					<div class="form-grid">
						<label>type <span class="req">*</span></label>
						<select v-model="t.type" class="form-input">
							<option value="file">file</option>
							<option value="http">http</option>
						</select>

						<template v-if="t.type === 'file'">
							<label>dir <span class="req">*</span></label>
							<input v-model="t.dir" class="form-input" placeholder="./logs" />
						</template>

						<template v-else>
							<label>webhook <span class="req">*</span></label>
							<select v-model="t.webhook" class="form-input">
								<option value="">(none)</option>
								<option v-for="w in webhookNames" :key="w" :value="w">
									{{ w }}
								</option>
							</select>
						</template>
					</div>
					<button class="btn btn-danger btn-sm" @click="removeLogTarget(i)">
						Delete
					</button>
				</div>
			</div>
		</section>

		<!-- Webhook -->
		<section class="config-section">
			<div class="section-header" @click="webhookOpen = !webhookOpen">
				<h3>
					{{ $t("config.webhook") }} <span class="count">({{ webhookCount }})</span>
				</h3>
				<span class="chevron">{{ webhookOpen ? "▼" : "▶" }}</span>
			</div>
			<div v-show="webhookOpen">
				<div class="add-row">
					<template v-if="addingSection === 'webhook'">
						<input
							ref="addInputRef"
							v-model="addingKey"
							class="form-input add-input"
							placeholder="Webhook name"
							@keyup.enter="confirmAdd"
							@keyup.esc="cancelAdd"
						/>
						<button class="btn btn-sm" @click="confirmAdd">Confirm</button>
						<button class="btn btn-secondary btn-sm" @click="cancelAdd">Cancel</button>
					</template>
					<button v-else class="btn btn-sm" @click="startAdd('webhook')">+ Add</button>
				</div>
				<div v-for="(cfg, name) in config.webhook" :key="'webhook-' + name" class="card">
					<div class="card-header" @click="toggleCard('webhook', name)">
						<strong>{{ name }}</strong>
						<span class="tag-proto">{{ cfg.url || "(no url)" }}</span>
						<span class="chevron">{{ isCardOpen("webhook", name) ? "▼" : "▶" }}</span>
					</div>
					<div v-show="isCardOpen('webhook', name)" class="card-body">
						<div class="form-grid">
							<label>url <span class="req">*</span></label>
							<input
								v-model="cfg.url"
								class="form-input"
								placeholder="https://your-log-sink/api/ingest"
							/>

							<label>method</label>
							<select v-model="cfg.method" class="form-input">
								<option value="">POST (default)</option>
								<option value="POST">POST</option>
								<option value="PUT">PUT</option>
								<option value="PATCH">PATCH</option>
							</select>

							<label>headers</label>
							<KeyValueEditor
								v-model="cfg.headers"
								keyPlaceholder="Header name"
								valuePlaceholder="Value"
							/>

							<label>body_template</label>
							<textarea
								v-model="cfg.body_template"
								class="form-input form-textarea"
								placeholder='Go template; omit to send record as plain JSON&#10;Example: {"id": "{{ .Record.RequestID }}"}'
							/>

							<label>timeout</label>
							<input v-model="cfg.timeout" class="form-input" placeholder="5s" />

							<label>retry</label>
							<input
								v-model.number="cfg.retry"
								class="form-input"
								type="number"
								placeholder="2"
							/>
						</div>
						<button
							class="btn btn-danger btn-sm"
							@click="deleteMapEntry('webhook', name)"
						>
							Delete
						</button>
					</div>
				</div>
			</div>
		</section>

		<div
			v-if="showAPIKeyCreateModal"
			class="modal-overlay"
			@click.self="showAPIKeyCreateModal = false"
		>
			<div class="modal">
				<h3>{{ $t("config.createApiKeyModalTitle") }}</h3>
				<div class="form-group">
					<label>{{ $t("config.apiKeyRoute") }}</label>
					<select v-model="newAPIKeyRoute" class="form-select">
						<option value="" disabled>{{ $t("config.apiKeyRoutePlaceholder") }}</option>
						<option v-for="prefix in routePrefixes" :key="prefix" :value="prefix">{{ prefix }}</option>
					</select>
				</div>
				<div class="form-group">
					<label>{{ $t("config.apiKeyName") }}</label>
					<input
						v-model="newAPIKeyName"
						class="form-input"
						:placeholder="$t('config.apiKeyNamePlaceholder')"
						@keyup.enter="createAPIKey"
					/>
				</div>
				<div class="modal-actions">
					<button class="btn btn-secondary" @click="showAPIKeyCreateModal = false">
						{{ $t("common.cancel") }}
					</button>
					<button class="btn btn-primary" @click="createAPIKey" :disabled="!newAPIKeyRoute || !newAPIKeyName.trim()">
						{{ $t("common.confirm") }}
					</button>
				</div>
			</div>
		</div>

		<div v-if="showAPIKeyModal" class="modal-overlay" @click.self="showAPIKeyModal = false">
			<div class="modal">
				<h3>{{ $t("config.apiKeyCreatedTitle") }}</h3>
				<p class="warning-text">{{ $t("config.apiKeyCreatedHint") }}</p>
				<div class="key-display">
					<code>{{ generatedAPIKey }}</code>
					<button class="btn btn-sm" @click="copyGeneratedAPIKey">
						{{ $t("common.copy") }}
					</button>
				</div>
				<div class="modal-actions">
					<button class="btn btn-primary" @click="showAPIKeyModal = false">
						{{ $t("common.close") }}
					</button>
				</div>
			</div>
		</div>

	</div>
</template>

<script setup>
import { ref, computed, reactive, watch, nextTick, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import {
	fetchConfig,
	fetchConfigSource,
	fetchAPIKeys,
	saveConfig,
	validateConfig,
	restartGateway,
	fetchStatus,
} from "../api.js";
import KeyValueEditor from "../components/KeyValueEditor.vue";

const { t } = useI18n();

const REDACTED = "__REDACTED__";

const config = ref({});
const configSource = ref(null); // { source_type: { file: bool }, config_path: string, config_hash: string }
const message = ref("");
const messageType = ref("success");
const error = ref("");
const applying = ref(false);
const dirty = ref(false);
const loading = ref(false);
const configFileChanged = ref(false); // true if config file changed externally

// track config modifications via deep watch
watch(
	config,
	() => {
		if (!loading.value) dirty.value = true;
	},
	{ deep: true },
);

const webhookOpen = ref(false);
const showAdminPw = ref(false);
const showAPIKeyCreateModal = ref(false);
const showAPIKeyModal = ref(false);
const newAPIKeyRoute = ref("");
const newAPIKeyName = ref("");
const generatedAPIKey = ref("");
const apiKeyUsage = ref({});

// per-card collapse state
const openCards = reactive({});

const webhookCount = computed(() => Object.keys(config.value.webhook || {}).length);
const webhookNames = computed(() => Object.keys(config.value.webhook || {}));
const routePrefixes = computed(() => Object.keys(config.value.route || {}).sort());
const apiKeyRows = computed(() =>
	routePrefixes.value.flatMap((route) =>
		Object.keys(config.value.route?.[route]?.api_keys || {})
			.sort()
			.map((name) => ({
				route,
				name,
				value: config.value.route[route].api_keys[name],
				usage: apiKeyUsage.value[apiKeyUsageKey(route, name)] || emptyAPIKeyUsage(),
			})),
	),
);

// admin password handling
const adminPwEdited = ref(false);
const adminPwValue = ref("");

const adminPwConfigured = computed(() => {
	if (adminPwEdited.value) return adminPwValue.value !== "";
	const raw = config.value.admin_password;
	return raw && raw !== "" && raw !== REDACTED ? true : raw === REDACTED;
});

const adminPwDisplay = computed(() => {
	if (adminPwEdited.value) return adminPwValue.value;
	return config.value.admin_password === REDACTED ? REDACTED : config.value.admin_password || "";
});

function onAdminPwInput(val) {
	adminPwEdited.value = true;
	adminPwValue.value = val;
	config.value.admin_password = val;
}

function emptyAPIKeyUsage() {
	return {
		total_requests: 0,
		success_requests: 0,
		failure_requests: 0,
		prompt_tokens: 0,
		completion_tokens: 0,
		cache_tokens: 0,
	};
}

function apiKeyUsageKey(route, name) {
	return route + "\0" + name;
}

function apiKeyConfigured(value) {
	return value === REDACTED || (typeof value === "string" && value !== "");
}

function generateAPIKeyValue() {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
	const bytes = new Uint8Array(32);
	if (globalThis.crypto?.getRandomValues) {
		globalThis.crypto.getRandomValues(bytes);
	} else {
		for (let i = 0; i < bytes.length; i += 1) {
			bytes[i] = Math.floor(Math.random() * 256);
		}
	}
	return (
		"wk_" +
		Array.from(bytes, (b) => alphabet[b % alphabet.length]).join("")
	);
}

function createAPIKey() {
	const route = newAPIKeyRoute.value;
	const name = newAPIKeyName.value.trim();
	if (!route || !name) return;
	if (!config.value.route?.[route]) return;
	if (!config.value.route[route].api_keys) config.value.route[route].api_keys = {};
	if (name in config.value.route[route].api_keys) {
		error.value = t("config.apiKeyExists", { route, name });
		return;
	}

	const key = generateAPIKeyValue();
	config.value.route[route].api_keys = {
		...config.value.route[route].api_keys,
		[name]: key,
	};
	apiKeyUsage.value = {
		...apiKeyUsage.value,
		[apiKeyUsageKey(route, name)]: apiKeyUsage.value[apiKeyUsageKey(route, name)] || emptyAPIKeyUsage(),
	};
	newAPIKeyRoute.value = "";
	newAPIKeyName.value = "";
	generatedAPIKey.value = key;
	showAPIKeyCreateModal.value = false;
	showAPIKeyModal.value = true;
	message.value = t("config.apiKeyPendingApply", { route, name });
	messageType.value = "success";
	error.value = "";
}

function deleteAPIKey(route, name) {
	if (!confirm(t("config.confirmDeleteApiKey", { route, name }))) return;
	if (!config.value.route?.[route]?.api_keys) return;
	const next = { ...config.value.route[route].api_keys };
	delete next[name];
	config.value.route[route].api_keys = next;
	message.value = t("config.apiKeyDeletePendingApply", { route, name });
	messageType.value = "success";
	error.value = "";
}

function copyGeneratedAPIKey() {
	navigator.clipboard.writeText(generatedAPIKey.value);
	message.value = t("config.apiKeyCopied");
	messageType.value = "success";
}

// log targets helpers
function addLogTarget() {
	if (!config.value.log) config.value.log = { targets: [] };
	if (!config.value.log.targets) config.value.log.targets = [];
	const i = config.value.log.targets.length;
	config.value.log.targets.push({ type: "file", dir: "" });
	nextTick(() => {
		openCards["log-target/" + i] = true;
	});
}
function removeLogTarget(i) {
	config.value.log.targets.splice(i, 1);
}

// card toggle
function toggleCard(section, key) {
	const id = section + "/" + key;
	openCards[id] = !openCards[id];
}
function isCardOpen(section, key) {
	return !!openCards[section + "/" + key];
}

// add/delete map entries
const addingSection = ref("");
const addingKey = ref("");
const addInputRef = ref(null);

function startAdd(section) {
	addingSection.value = section;
	addingKey.value = "";
	// open the section and focus input after render
	if (section === "webhook") webhookOpen.value = true;
	nextTick(() => addInputRef.value?.focus());
}

function confirmAdd() {
	const section = addingSection.value;
	const key = addingKey.value.trim();
	if (!key) return;
	const map = config.value[section];
	if (map && key in map) {
		error.value = t("config.alreadyExists", { section, key });
		return;
	}
	if (!config.value[section]) config.value[section] = {};
	const defaults = {
		webhook: { url: "", method: "POST" },
	};
	config.value[section][key] = defaults[section] || {};
	openCards[section + "/" + key] = true;
	addingSection.value = "";
	addingKey.value = "";
}

function cancelAdd() {
	addingSection.value = "";
	addingKey.value = "";
}

function deleteMapEntry(section, key) {
	if (!confirm(t("config.confirmDelete", { section, key }))) return;
	delete config.value[section][key];
	// force reactivity
	config.value[section] = { ...config.value[section] };
}

// clean config before sending: remove empty/null maps, strip __new_ keys from KV editors
function cleanConfig(obj) {
	if (obj === null || obj === undefined) return obj;
	if (Array.isArray(obj)) return obj;
	if (typeof obj !== "object") return obj;

	const out = {};
	for (const [k, v] of Object.entries(obj)) {
		if (v === null || v === undefined) continue;
		if (typeof v === "object" && !Array.isArray(v)) {
			const cleaned = {};
			for (const [ik, iv] of Object.entries(v)) {
				if (ik.startsWith("__new_")) continue;
				cleaned[ik] = cleanConfig(iv);
			}
			if (Object.keys(cleaned).length > 0) out[k] = cleaned;
		} else {
			out[k] = v;
		}
	}
	return out;
}

// load config from server, reset dirty state
async function load() {
	loading.value = true;
	try {
		const [cfg, source, keyData] = await Promise.all([
			fetchConfig(),
			fetchConfigSource(),
			fetchAPIKeys(),
		]);
		config.value = {
			...cfg,
		};
		configSource.value = source;
		apiKeyUsage.value = Object.fromEntries(
			(keyData.keys || []).map((item) => [
				apiKeyUsageKey(item.route, item.name),
				item.usage || emptyAPIKeyUsage(),
			]),
		);
		adminPwEdited.value = false;
		adminPwValue.value = "";
		showAPIKeyCreateModal.value = false;
		showAPIKeyModal.value = false;
		newAPIKeyRoute.value = "";
		newAPIKeyName.value = "";
		generatedAPIKey.value = "";
		error.value = "";
		message.value = "";
		configFileChanged.value = false;
		await nextTick(); // let deep watcher run while loading=true
		dirty.value = false;
	} catch (e) {
		error.value = e.message;
	} finally {
		loading.value = false;
	}
}

// discard local edits, reload running config
function discard() {
	if (!confirm(t("config.confirmDiscard"))) return;
	load();
}

// validate → save → restart → poll until alive → reload
const waitingAlive = ref(false);
const waitingElapsed = ref(0);

async function pollUntilAlive(timeoutMs = 60000, intervalMs = 1500) {
	const deadline = Date.now() + timeoutMs;
	waitingAlive.value = true;
	waitingElapsed.value = 0;
	const startMs = Date.now();
	const ticker = setInterval(() => {
		waitingElapsed.value = Math.floor((Date.now() - startMs) / 1000);
	}, 500);
	try {
		// first wait a short period so the old process has time to shut down
		await new Promise((r) => setTimeout(r, 800));
		while (Date.now() < deadline) {
			try {
				await fetchStatus();
				return true;
			} catch {
				await new Promise((r) => setTimeout(r, intervalMs));
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
	applying.value = true;
	message.value = "";
	error.value = "";
	try {
		// check if config source is file-based
		if (!configSource.value?.source_type?.file) {
			error.value = t("config.savingDisabled");
			return;
		}

		// step 1: validate
		const result = await validateConfig(cleanConfig(config.value));
		if (!result.valid) {
			error.value = t("config.validationFailed", { error: result.error });
			return;
		}
		// step 2: save to file
		await saveConfig(cleanConfig(config.value));
		// step 3: restart gateway to apply
		const restart = await restartGateway();
		if (restart.status !== "ok") {
			error.value = t("config.savedButRestartFailed", {
				error: restart.error || "unknown error",
			});
			return;
		}
		// step 4: poll until service is back up (max 60s)
		const alive = await pollUntilAlive();
		if (!alive) {
			error.value = t("config.serviceTimeout");
			return;
		}
		// step 5: reload fresh state
		await load();
		message.value = t("config.applied");
		messageType.value = "success";
	} catch (e) {
		if (e.message?.includes("config file changed externally")) {
			configFileChanged.value = true;
			error.value = t("config.externalChangeError");
		} else {
			error.value = e.message;
		}
	} finally {
		applying.value = false;
	}
}

onMounted(load);
</script>

<style scoped>
h2 {
	margin-bottom: 16px;
}
h3 {
	margin: 0;
	font-size: 14px;
	font-weight: 600;
}

.actions {
	display: flex;
	gap: 10px;
	margin-bottom: 20px;
	align-items: center;
}

.config-section {
	background: var(--c-surface);
	border: 1px solid var(--c-border);
	border-radius: var(--radius);
	padding: 18px;
	margin-bottom: 16px;
	box-shadow: var(--shadow);
}

.section-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	cursor: pointer;
	user-select: none;
}

.chevron {
	color: var(--c-text-3);
	font-size: 12px;
	margin-left: auto;
}
.count {
	color: var(--c-text-3);
	font-weight: normal;
	font-size: 13px;
}

.form-grid {
	display: grid;
	grid-template-columns: 140px 1fr;
	gap: 10px 14px;
	align-items: start;
	margin: 10px 0;
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

.patch-row {
	display: flex;
	gap: 6px;
	align-items: center;
	margin-bottom: 6px;
}
.patch-row .form-input {
	flex: 1;
}
.form-input-sm {
	flex: 0 0 90px !important;
}
.btn-danger-icon {
	color: var(--c-danger);
}

.url-field {
	display: flex;
	gap: 6px;
}
.url-field .form-input {
	flex: 1;
}
.url-proxy {
	flex: 0 0 210px !important;
	color: var(--c-text-3);
}

.secret-field {
	display: flex;
	gap: 6px;
	align-items: center;
}
.secret-field .form-input {
	flex: 1;
}

.badge-none {
	background: var(--c-border-light);
	color: var(--c-text-3);
}

.card {
	border: 1px solid var(--c-border);
	border-radius: var(--radius-sm);
	margin: 8px 0;
	overflow: hidden;
}

.card-header {
	display: flex;
	align-items: center;
	gap: 8px;
	padding: 10px 14px;
	background: #f8fafc;
	cursor: pointer;
	user-select: none;
	transition: background var(--transition);
}
.card-header:hover {
	background: var(--c-border-light);
}

.card-body {
	padding: 14px;
	border-top: 1px solid var(--c-border);
}

.add-row {
	display: flex;
	align-items: center;
	gap: 8px;
	margin-bottom: 6px;
}
.add-input {
	width: 220px;
	flex: 0 0 220px;
}

.tag-proto {
	font-size: 11px;
	background: var(--c-primary-bg);
	color: var(--c-primary);
	padding: 1px 6px;
	border-radius: 3px;
	font-weight: 500;
}

.subsection-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	margin: 14px 0 4px;
	font-size: 12px;
	color: var(--c-text-2);
	font-family: var(--font-mono);
}

.section-note {
	margin: 4px 0 0;
	font-family: inherit;
	font-size: 12px;
	color: var(--c-text-3);
}

.empty-state {
	color: var(--c-text-3);
	padding: 16px 0;
}

.api-key-table {
	margin-bottom: 10px;
}

.modal-overlay {
	position: fixed;
	inset: 0;
	background: rgba(15, 23, 42, 0.45);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 100;
}

.modal {
	background: var(--c-surface);
	border-radius: var(--radius);
	padding: 24px;
	max-width: 460px;
	width: min(92vw, 460px);
	box-shadow: var(--shadow-md);
}

.form-group {
	margin-top: 16px;
}

.form-group label {
	display: block;
	font-size: 12px;
	font-weight: 600;
	color: var(--c-text-2);
	margin-bottom: 6px;
}

.modal-actions {
	display: flex;
	justify-content: flex-end;
	gap: 10px;
	margin-top: 20px;
}

.warning-text {
	color: var(--c-warning);
	font-size: 13px;
	margin: 12px 0;
}

.key-display {
	display: flex;
	gap: 10px;
	align-items: center;
	background: var(--c-border-light);
	padding: 12px;
	border-radius: var(--radius-sm);
}

.key-display code {
	flex: 1;
	word-break: break-all;
}

.form-textarea {
	width: 100%;
	min-height: 90px;
	resize: vertical;
	font-family: var(--font-mono);
	font-size: 12px;
	box-sizing: border-box;
}

.form-hint-row {
	display: flex;
	align-items: center;
	gap: 8px;
	padding-top: 6px;
}

.form-checkbox {
	width: 16px;
	height: 16px;
	cursor: pointer;
}

@media (max-width: 768px) {
	.form-grid {
		grid-template-columns: 1fr;
		gap: 4px 0;
	}

	.form-grid > label {
		padding-top: 4px;
		padding-bottom: 0;
	}

	.config-section {
		padding: 14px;
	}

	.secret-field {
		flex-wrap: wrap;
	}

	.actions {
		flex-wrap: wrap;
	}
}
</style>
