<template>
	<div>
		<h2>{{ $t("apikeys.title") }}</h2>

		<div v-if="message" :class="['msg', messageType]">{{ message }}</div>
		<div v-if="error" class="msg error">{{ error }}</div>

		<div class="panel">
			<div class="panel-header">
				<h3>{{ $t("apikeys.manageKeys") }}</h3>
				<button class="btn btn-primary btn-sm" @click="showCreateModal = true">
					{{ $t("apikeys.createKey") }}
				</button>
			</div>

			<div v-if="loading" class="loading">{{ $t("common.loading") }}</div>

			<div v-else-if="keys.length === 0" class="empty-state">
				{{ $t("apikeys.noKeys") }}
			</div>

			<table v-else class="data-table">
				<thead>
					<tr>
						<th>{{ $t("apikeys.name") }}</th>
						<th>{{ $t("apikeys.actions") }}</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="key in keys" :key="key.name">
						<td>
							<code>{{ key.name }}</code>
						</td>
						<td>
							<button class="btn btn-danger btn-sm" @click="deleteKey(key.name)">
								{{ $t("apikeys.delete") }}
							</button>
						</td>
					</tr>
				</tbody>
			</table>
		</div>

		<!-- Create Modal -->
		<div v-if="showCreateModal" class="modal-overlay" @click.self="showCreateModal = false">
			<div class="modal">
				<h3>{{ $t("apikeys.createNewKey") }}</h3>
				<div class="form-group">
					<label>{{ $t("apikeys.keyName") }}</label>
					<input
						v-model="newKeyName"
						class="form-input"
						:placeholder="$t('apikeys.keyNamePlaceholder')"
						@keyup.enter="createKey"
					/>
				</div>
				<div class="modal-actions">
					<button class="btn btn-secondary" @click="showCreateModal = false">
						{{ $t("common.cancel") }}
					</button>
					<button class="btn btn-primary" @click="createKey" :disabled="!newKeyName.trim()">
						{{ $t("apikeys.create") }}
					</button>
				</div>
			</div>
		</div>

		<!-- Show Key Modal -->
		<div v-if="showKeyModal" class="modal-overlay" @click.self="showKeyModal = false">
			<div class="modal">
				<h3>{{ $t("apikeys.keyCreated") }}</h3>
				<p class="warning-text">{{ $t("apikeys.copyWarning") }}</p>
				<div class="key-display">
					<code>{{ createdKey }}</code>
					<button class="btn btn-sm" @click="copyKey">{{ $t("apikeys.copy") }}</button>
				</div>
				<div class="modal-actions">
					<button class="btn btn-primary" @click="showKeyModal = false">
						{{ $t("common.close") }}
					</button>
				</div>
			</div>
		</div>
	</div>
</template>

<script setup>
import { ref, onMounted } from "vue";
import { useI18n } from "vue-i18n";
import { fetchAPIKeys, createAPIKey, deleteAPIKey } from "../api.js";

const { t } = useI18n();

const keys = ref([]);
const loading = ref(true);
const message = ref("");
const messageType = ref("success");
const error = ref("");

const showCreateModal = ref(false);
const showKeyModal = ref(false);
const newKeyName = ref("");
const createdKey = ref("");

async function loadKeys() {
	loading.value = true;
	error.value = "";
	try {
		const result = await fetchAPIKeys();
		keys.value = result.keys || [];
	} catch (e) {
		error.value = e.message;
	} finally {
		loading.value = false;
	}
}

async function createKey() {
	const name = newKeyName.value.trim();
	if (!name) return;

	error.value = "";
	message.value = "";

	try {
		const result = await createAPIKey(name);
		if (result.key) {
			createdKey.value = result.key;
			showCreateModal.value = false;
			showKeyModal.value = true;
			newKeyName.value = "";
			await loadKeys();
		}
	} catch (e) {
		error.value = e.message;
	}
}

async function deleteKey(name) {
	if (!confirm(t("apikeys.confirmDelete", { name }))) return;

	error.value = "";
	message.value = "";

	try {
		await deleteAPIKey(name);
		message.value = t("apikeys.deleted", { name });
		messageType.value = "success";
		await loadKeys();
	} catch (e) {
		error.value = e.message;
	}
}

function copyKey() {
	navigator.clipboard.writeText(createdKey.value);
	message.value = t("apikeys.copied");
	messageType.value = "success";
}

onMounted(loadKeys);
</script>

<style scoped>
h2 {
	margin-bottom: 16px;
}

.panel {
	padding: 18px;
}

.panel-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	margin-bottom: 16px;
}

.panel-header h3 {
	margin: 0;
	font-size: 14px;
	font-weight: 600;
}

.loading,
.empty-state {
	color: var(--c-text-3);
	padding: 20px;
	text-align: center;
}

.modal-overlay {
	position: fixed;
	top: 0;
	left: 0;
	right: 0;
	bottom: 0;
	background: rgba(0, 0, 0, 0.5);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 100;
}

.modal {
	background: var(--c-surface);
	border-radius: var(--radius);
	padding: 24px;
	max-width: 450px;
	width: 90%;
	box-shadow: var(--shadow-md);
}

.modal h3 {
	margin: 0 0 16px;
	font-size: 16px;
	font-weight: 600;
}

.form-group {
	margin-bottom: 16px;
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
	margin-bottom: 12px;
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
</style>