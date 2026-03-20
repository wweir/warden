<template>
  <div class="tag-editor">
    <div class="tag-list">
      <span v-for="(tag, idx) in tags" :key="idx" class="tag">
        <span v-if="allowReorder" class="tag-order">{{ idx + 1 }}</span>
        {{ tag }}
        <span class="tag-actions">
          <button
            v-if="allowReorder"
            class="tag-action"
            type="button"
            :disabled="idx === 0"
            :title="t('common.moveUp')"
            :aria-label="t('common.moveUp')"
            @click="moveTag(idx, -1)"
          >
            {{ t('common.moveUp') }}
          </button>
          <button
            v-if="allowReorder"
            class="tag-action"
            type="button"
            :disabled="idx === tags.length - 1"
            :title="t('common.moveDown')"
            :aria-label="t('common.moveDown')"
            @click="moveTag(idx, 1)"
          >
            {{ t('common.moveDown') }}
          </button>
          <button
            class="tag-remove"
            type="button"
            :title="t('common.delete')"
            :aria-label="t('common.delete')"
            @click="removeTag(idx)"
          >
            &times;
          </button>
        </span>
      </span>
      <div class="tag-input-wrap">
        <input
          :id="inputId"
          class="tag-input"
          v-model="input"
          :placeholder="tags.length === 0 ? placeholder : ''"
          role="combobox"
          aria-autocomplete="list"
          :aria-expanded="showSuggestions && filtered.length > 0 ? 'true' : 'false'"
          :aria-controls="listboxId"
          :aria-activedescendant="highlightedOptionId"
          @keydown.enter.prevent="confirmInput"
          @keydown.down.prevent="moveHighlight(1)"
          @keydown.up.prevent="moveHighlight(-1)"
          @keydown.escape="closeSuggestions"
          @keydown.tab="closeSuggestions"
          @keydown.backspace="handleBackspace"
          @input="onInput"
          @focus="openSuggestions"
          @blur="hideSuggestions"
        />
        <ul
          v-if="showSuggestions && filtered.length > 0"
          :id="listboxId"
          class="suggestions"
          role="listbox"
          :aria-labelledby="inputId"
        >
          <li
            v-for="(s, idx) in filtered" :key="s"
            :id="optionId(idx)"
            role="option"
            :aria-selected="idx === highlightedIdx ? 'true' : 'false'"
            :class="{ highlighted: idx === highlightedIdx }"
            @mouseenter="highlightedIdx = idx"
            @mousedown.prevent="selectSuggestion(s)"
          >{{ s }}</li>
        </ul>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  suggestions: { type: Array, default: () => [] },
  placeholder: { type: String, default: 'Type and press Enter' },
  allowReorder: { type: Boolean, default: false },
})
const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()

const tags = computed(() => props.modelValue || [])
const input = ref('')
const showSuggestions = ref(false)
const highlightedIdx = ref(-1)
const listboxId = `tag-list-editor-${Math.random().toString(36).slice(2)}`
const inputId = `${listboxId}-input`

const filtered = computed(() => {
  if (!props.suggestions.length) return []
  const q = input.value.toLowerCase()
  return props.suggestions.filter(
    s => !tags.value.includes(s) && (q === '' || s.toLowerCase().includes(q))
  )
})
const highlightedOptionId = computed(() =>
  highlightedIdx.value >= 0 && highlightedIdx.value < filtered.value.length
    ? optionId(highlightedIdx.value)
    : undefined,
)

function optionId(index) {
  return `${listboxId}-option-${index}`
}

function addTag() {
  const v = input.value.trim()
  if (v && !tags.value.includes(v)) {
    emit('update:modelValue', [...tags.value, v])
  }
  input.value = ''
  highlightedIdx.value = -1
}

function removeTag(idx) {
  emit('update:modelValue', tags.value.filter((_, i) => i !== idx))
}

function moveTag(idx, delta) {
  const nextIdx = idx + delta
  if (nextIdx < 0 || nextIdx >= tags.value.length) return
  const nextTags = [...tags.value]
  const [moved] = nextTags.splice(idx, 1)
  nextTags.splice(nextIdx, 0, moved)
  emit('update:modelValue', nextTags)
}

function selectSuggestion(s) {
  if (!tags.value.includes(s)) {
    emit('update:modelValue', [...tags.value, s])
  }
  input.value = ''
  closeSuggestions()
}

function openSuggestions() {
  showSuggestions.value = true
}

function closeSuggestions() {
  showSuggestions.value = false
  highlightedIdx.value = -1
}

function onInput() {
  showSuggestions.value = true
  highlightedIdx.value = filtered.value.length > 0 ? 0 : -1
}

function moveHighlight(delta) {
  if (!showSuggestions.value) {
    openSuggestions()
  }
  if (filtered.value.length === 0) return
  highlightedIdx.value = (highlightedIdx.value + delta + filtered.value.length) % filtered.value.length
}

function confirmInput() {
  if (showSuggestions.value && highlightedIdx.value >= 0 && highlightedIdx.value < filtered.value.length) {
    selectSuggestion(filtered.value[highlightedIdx.value])
    return
  }
  addTag()
  closeSuggestions()
}

function handleBackspace() {
  if (input.value !== '' || tags.value.length === 0) return
  removeTag(tags.value.length - 1)
}

function hideSuggestions() {
  setTimeout(() => { closeSuggestions() }, 150)
}

watch(filtered, (nextItems) => {
  if (nextItems.length === 0) {
    highlightedIdx.value = -1
    return
  }
  if (highlightedIdx.value >= nextItems.length) {
    highlightedIdx.value = nextItems.length - 1
  }
})
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
  gap: 6px;
  background: var(--c-primary-bg);
  color: var(--c-primary);
  border-radius: 4px;
  padding: 2px 6px;
  font-size: 12px;
  font-weight: 500;
}
.tag-order {
  min-width: 18px;
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.08);
  color: var(--c-text-2);
  text-align: center;
  font-size: 11px;
  line-height: 18px;
}
.tag-actions {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}
.tag-action,
.tag-remove {
  background: none;
  border: none;
  color: var(--c-primary);
  cursor: pointer;
  font-size: 11px;
  line-height: 1;
  min-height: 28px;
  padding: 4px 6px;
  opacity: 0.6;
  transition: opacity var(--transition);
}
.tag-action:disabled {
  cursor: not-allowed;
  opacity: 0.3;
}
.tag-action:hover:not(:disabled),
.tag-remove:hover {
  opacity: 1;
}
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
  padding: 8px 10px;
  min-height: 36px;
  cursor: pointer;
  font-size: 13px;
  transition: background var(--transition);
  display: flex;
  align-items: center;
}
.suggestions li:hover,
.suggestions li.highlighted { background: var(--c-primary-bg); }
</style>
