<template>
  <div class="model-combobox" ref="rootRef">
    <input
      :id="inputId"
      class="form-input"
      :class="inputClass"
      v-model="query"
      :placeholder="placeholder"
      :aria-label="ariaLabel || undefined"
      role="combobox"
      aria-autocomplete="list"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="listboxId"
      :aria-activedescendant="highlightedOptionId"
      @focus="open = true"
      @keydown.down.prevent="move(1)"
      @keydown.up.prevent="move(-1)"
      @keydown.enter.prevent="confirm"
      @keydown.escape="open = false"
      @keydown.tab="open = false"
    />
    <ul
      v-if="open && filtered.length > 0"
      :id="listboxId"
      class="model-dropdown"
      role="listbox"
      :aria-labelledby="inputId"
    >
      <li
        v-for="(m, i) in filtered"
        :key="m"
        :id="optionId(i)"
        :class="{ highlighted: i === idx }"
        role="option"
        :aria-selected="i === idx ? 'true' : 'false'"
        @mouseenter="idx = i"
        @mousedown.prevent="select(m)"
      >{{ m }}</li>
    </ul>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'

const props = defineProps({
  modelValue: { type: String, default: '' },
  models: { type: Array, default: () => [] },
  placeholder: { type: String, default: 'Select model' },
  inputClass: { type: String, default: '' },
  ariaLabel: { type: String, default: '' },
})

const emit = defineEmits(['update:modelValue'])

const rootRef = ref(null)
const open = ref(false)
const idx = ref(-1)
const listboxId = `model-combobox-${Math.random().toString(36).slice(2)}`
const inputId = `${listboxId}-input`

const query = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const filtered = computed(() => {
  const q = query.value.toLowerCase()
  if (!q) return props.models
  return props.models.filter(m => m.toLowerCase().includes(q))
})
const highlightedOptionId = computed(() =>
  idx.value >= 0 && idx.value < filtered.value.length ? optionId(idx.value) : undefined,
)

function optionId(index) {
  return `${listboxId}-option-${index}`
}

function select(m) {
  emit('update:modelValue', m)
  open.value = false
  idx.value = -1
}

function move(dir) {
  if (!open.value) { open.value = true; return }
  const len = filtered.value.length
  if (len === 0) return
  idx.value = (idx.value + dir + len) % len
}

function confirm() {
  if (idx.value >= 0 && idx.value < filtered.value.length) {
    select(filtered.value[idx.value])
  } else {
    open.value = false
  }
}

function onClickOutside(e) {
  if (rootRef.value && !rootRef.value.contains(e.target)) {
    open.value = false
  }
}

watch(filtered, (nextItems) => {
  if (nextItems.length === 0) {
    idx.value = -1
    return
  }
  if (idx.value >= nextItems.length) {
    idx.value = nextItems.length - 1
  }
})

watch(open, (nextOpen) => {
  if (!nextOpen) idx.value = -1
})

onMounted(() => document.addEventListener('click', onClickOutside))
onUnmounted(() => document.removeEventListener('click', onClickOutside))
</script>

<style scoped>
.model-combobox { position: relative; }
.model-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  margin: 2px 0 0;
  padding: 0;
  list-style: none;
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  max-height: 260px;
  overflow-y: auto;
  z-index: 20;
  box-shadow: var(--shadow-md);
}
.model-dropdown li {
  padding: 8px 10px;
  min-height: 36px;
  font-size: 12px;
  font-family: var(--font-mono);
  cursor: pointer;
  transition: background var(--transition);
  display: flex;
  align-items: center;
}
.model-dropdown li:hover,
.model-dropdown li.highlighted {
  background: var(--c-primary-bg);
}
</style>
