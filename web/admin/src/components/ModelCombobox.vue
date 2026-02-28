<template>
  <div class="model-combobox" ref="rootRef">
    <input
      class="form-input"
      :class="inputClass"
      v-model="query"
      :placeholder="placeholder"
      @focus="open = true"
      @keydown.down.prevent="move(1)"
      @keydown.up.prevent="move(-1)"
      @keydown.enter.prevent="confirm"
      @keydown.escape="open = false"
    />
    <ul v-if="open && filtered.length > 0" class="model-dropdown">
      <li
        v-for="(m, i) in filtered"
        :key="m"
        :class="{ highlighted: i === idx }"
        @mousedown.prevent="select(m)"
      >{{ m }}</li>
    </ul>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'

const props = defineProps({
  modelValue: { type: String, default: '' },
  models: { type: Array, default: () => [] },
  placeholder: { type: String, default: 'Select model' },
  inputClass: { type: String, default: '' },
})

const emit = defineEmits(['update:modelValue'])

const rootRef = ref(null)
const open = ref(false)
const idx = ref(-1)

const query = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const filtered = computed(() => {
  const q = query.value.toLowerCase()
  if (!q) return props.models
  return props.models.filter(m => m.toLowerCase().includes(q))
})

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
  padding: 6px 10px;
  font-size: 12px;
  font-family: var(--font-mono);
  cursor: pointer;
  transition: background var(--transition);
}
.model-dropdown li:hover,
.model-dropdown li.highlighted {
  background: var(--c-primary-bg);
}
</style>
