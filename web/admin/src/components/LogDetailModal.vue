<template>
	<div v-if="log" class="modal-overlay" @click.self="close">
		<div
			ref="modalRef"
			class="modal"
			role="dialog"
			aria-modal="true"
			aria-labelledby="log-detail-title"
			@keydown="handleModalKeydown"
		>
			<div class="modal-header">
				<h3 id="log-detail-title">{{ $t('logs.requestDetail') }}</h3>
				<div class="modal-header-actions">
					<button ref="closeButtonRef" class="btn btn-secondary btn-sm" @click="close">{{ $t('common.close') }}</button>
				</div>
			</div>

			<div class="modal-body">
				<LogDetailPanel
					:log="selected"
					:lastUserPreview="lastUserPreview"
				/>
			</div>
		</div>
	</div>
</template>

<script setup>
import { ref, watch, nextTick, onUnmounted } from "vue";
import LogDetailPanel from "./LogDetailPanel.vue";

const props = defineProps({
	log: { default: null },
	lastUserPreview: { type: Function, required: true },
});

const emit = defineEmits(["close"]);

const modalRef = ref(null);
const closeButtonRef = ref(null);
let lastFocusedElement = null;

const selected = ref(props.log);

function close() {
	emit("close");
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
		close();
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

watch(() => props.log, async (value, oldValue) => {
	if (value && !oldValue) {
		lastFocusedElement = document.activeElement;
	}
	selected.value = value || null;
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
</script>

<style scoped>
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
	flex-shrink: 0;
}
.modal-header h3 {
	margin: 0;
}
.modal-header-actions {
	display: flex;
	align-items: center;
	gap: 8px;
}
.modal-body {
	padding: 20px;
	overflow-y: auto;
}

@media (max-width: 768px) {
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
}
</style>
