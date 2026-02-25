<template>
  <div class="tag-editor">
    <div class="tag-list">
      <span v-for="(tag, idx) in tags" :key="idx" class="tag">
        {{ tag }}
        <button class="tag-remove" @click="removeTag(idx)">&times;</button>
      </span>
      <div class="tag-input-wrap" ref="wrapRef">
        <input
          class="tag-input"
          v-model="input"
          :placeholder="tags.length === 0 ? placeholder : ''"
          @keydown.enter.prevent="addTag"
          @input="onInput"
          @focus="showSuggestions = true"
          @blur="hideSuggestions"
        />
        <ul v-if="showSuggestions && filtered.length > 0" class="suggestions">
          <li
            v-for="s in filtered" :key="s"
            @mousedown.prevent="selectSuggestion(s)"
          >{{ s }}</li>
        </ul>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'

const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  suggestions: { type: Array, default: () => [] },
  placeholder: { type: String, default: 'Type and press Enter' },
})
const emit = defineEmits(['update:modelValue'])

const tags = computed(() => props.modelValue || [])
const input = ref('')
const showSuggestions = ref(false)

const filtered = computed(() => {
  if (!props.suggestions.length) return []
  const q = input.value.toLowerCase()
  return props.suggestions.filter(
    s => !tags.value.includes(s) && (q === '' || s.toLowerCase().includes(q))
  )
})

function addTag() {
  const v = input.value.trim()
  if (v && !tags.value.includes(v)) {
    emit('update:modelValue', [...tags.value, v])
  }
  input.value = ''
}

function removeTag(idx) {
  emit('update:modelValue', tags.value.filter((_, i) => i !== idx))
}

function selectSuggestion(s) {
  if (!tags.value.includes(s)) {
    emit('update:modelValue', [...tags.value, s])
  }
  input.value = ''
  showSuggestions.value = false
}

function onInput() {
  showSuggestions.value = true
}

function hideSuggestions() {
  setTimeout(() => { showSuggestions.value = false }, 150)
}
</script>

<style scoped>
.tag-editor { width: 100%; }
.tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  padding: 5px 8px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  background: var(--c-surface);
  min-height: 36px;
  transition: border-color var(--transition), box-shadow var(--transition);
}
.tag-list:focus-within {
  border-color: var(--c-primary);
  box-shadow: 0 0 0 3px var(--c-primary-bg);
}
.tag {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  background: var(--c-primary-bg);
  color: var(--c-primary);
  border-radius: 4px;
  padding: 2px 6px;
  font-size: 12px;
  font-weight: 500;
}
.tag-remove {
  background: none;
  border: none;
  color: var(--c-primary);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  padding: 0 2px;
  opacity: 0.6;
  transition: opacity var(--transition);
}
.tag-remove:hover { opacity: 1; }
.tag-input-wrap { position: relative; flex: 1; min-width: 80px; }
.tag-input {
  width: 100%;
  border: none;
  outline: none;
  font-size: 13px;
  padding: 2px 0;
  background: transparent;
  font-family: var(--font-mono);
  color: var(--c-text);
}
.suggestions {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  list-style: none;
  margin: 4px 0 0;
  padding: 4px 0;
  max-height: 160px;
  overflow-y: auto;
  z-index: 10;
  box-shadow: var(--shadow-md);
}
.suggestions li {
  padding: 6px 10px;
  cursor: pointer;
  font-size: 13px;
  transition: background var(--transition);
}
.suggestions li:hover { background: var(--c-primary-bg); }
</style>
