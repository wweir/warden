<template>
  <div class="kv-editor">
    <div v-for="(row, idx) in rows" :key="idx" class="kv-row">
      <input
        class="form-input kv-key"
        :value="row.key"
        :placeholder="keyPlaceholder"
        :readonly="keyReadonly"
        @input="updateKey(idx, $event.target.value)"
      />
      <input
        class="form-input kv-value"
        :value="row.value"
        :placeholder="valuePlaceholder"
        @input="updateValue(idx, $event.target.value)"
      />
      <button class="btn-icon kv-del" @click="removeRow(idx)" title="Delete">&times;</button>
    </div>
    <button class="kv-add" @click="addRow">+ Add</button>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  modelValue: { type: Object, default: () => ({}) },
  keyPlaceholder: { type: String, default: 'Key' },
  valuePlaceholder: { type: String, default: 'Value' },
  keyReadonly: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])

const rows = computed(() => {
  const obj = props.modelValue || {}
  return Object.entries(obj).map(([key, value]) => ({ key, value }))
})

function emitUpdate(entries) {
  const obj = {}
  for (const { key, value } of entries) {
    if (key !== '') obj[key] = value
  }
  emit('update:modelValue', obj)
}

function updateKey(idx, newKey) {
  const entries = rows.value.map((r, i) =>
    i === idx ? { key: newKey, value: r.value } : { ...r }
  )
  emitUpdate(entries)
}

function updateValue(idx, newValue) {
  const entries = rows.value.map((r, i) =>
    i === idx ? { key: r.key, value: newValue } : { ...r }
  )
  emitUpdate(entries)
}

function addRow() {
  const obj = { ...(props.modelValue || {}) }
  let suffix = 0
  let tempKey = ''
  do { tempKey = `__new_${suffix++}` } while (tempKey in obj)
  obj[tempKey] = ''
  emit('update:modelValue', obj)
}

function removeRow(idx) {
  const entries = rows.value.filter((_, i) => i !== idx)
  emitUpdate(entries)
}
</script>

<style scoped>
.kv-editor { display: flex; flex-direction: column; gap: 6px; }
.kv-row { display: flex; gap: 6px; align-items: center; }
.kv-key { flex: 1; }
.kv-value { flex: 2; }
.kv-del {
  color: var(--c-danger);
  border-color: transparent;
  font-size: 16px;
  padding: 4px 6px;
}
.kv-del:hover { background: var(--c-danger-bg); border-color: transparent; }
.kv-add {
  align-self: flex-start;
  background: none;
  border: 1px dashed var(--c-border);
  border-radius: var(--radius-sm);
  padding: 4px 12px;
  cursor: pointer;
  font-size: 12px;
  color: var(--c-text-3);
  margin-top: 2px;
  transition: all var(--transition);
}
.kv-add:hover { border-color: var(--c-text-3); color: var(--c-text-2); }
</style>
