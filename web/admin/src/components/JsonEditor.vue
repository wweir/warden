<template>
  <div ref="editorRef" class="json-editor"></div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { EditorView, basicSetup } from 'codemirror'
import { json } from '@codemirror/lang-json'

const props = defineProps({
  modelValue: { type: String, default: '' },
})
const emit = defineEmits(['update:modelValue'])

const editorRef = ref(null)
let view = null

onMounted(() => {
  view = new EditorView({
    doc: props.modelValue,
    extensions: [
      basicSetup,
      json(),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) {
          emit('update:modelValue', update.state.doc.toString())
        }
      }),
    ],
    parent: editorRef.value,
  })
})

watch(() => props.modelValue, (newVal) => {
  if (view && view.state.doc.toString() !== newVal) {
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: newVal },
    })
  }
})

onUnmounted(() => {
  if (view) view.destroy()
})
</script>

<style scoped>
.json-editor {
  border: 1px solid #ddd;
  border-radius: 8px;
  overflow: hidden;
  min-height: 500px;
  background: #fff;
}
.json-editor :deep(.cm-editor) {
  min-height: 500px;
}
.json-editor :deep(.cm-scroller) {
  font-family: 'SF Mono', Monaco, Consolas, monospace;
  font-size: 13px;
  line-height: 1.5;
}
</style>
