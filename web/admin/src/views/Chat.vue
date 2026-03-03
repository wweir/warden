<template>
  <div class="chat-page">
    <!-- Mobile sidebar toggle -->
    <button class="sidebar-toggle" @click="sidebarOpen = !sidebarOpen">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>
    </button>
    <!-- Sidebar overlay for mobile -->
    <div v-if="sidebarOpen" class="sidebar-overlay" @click="sidebarOpen = false"></div>
    <!-- Sidebar -->
    <aside :class="['chat-sidebar', { open: sidebarOpen }]">
      <button class="btn btn-primary sidebar-new" @click="newConversation(); sidebarOpen = false">{{ $t('chat.newChat') }}</button>
      <div class="conv-list">
        <div
          v-for="c in conversations"
          :key="c.id"
          :class="['conv-item', { active: c.id === currentId }]"
          @click="switchTo(c.id); sidebarOpen = false"
        >
          <span class="conv-title">{{ c.title || $t('chat.newChat') }}</span>
          <button class="conv-del" @click.stop="deleteConversation(c.id)" :title="$t('chat.deleteChat')">&times;</button>
        </div>
        <div v-if="conversations.length === 0" class="empty" style="padding:12px">{{ $t('chat.noConversations') }}</div>
      </div>
    </aside>

    <!-- Main chat area -->
    <div class="chat-main">
      <!-- Top bar: route, provider & model selector -->
      <div class="chat-topbar">
        <div class="topbar-group">
          <label>{{ $t('chat.route') }}</label>
          <select class="form-input topbar-select" v-model="currentRoute" @change="onRouteChange">
            <option v-for="r in routes" :key="r" :value="r">{{ r }}</option>
          </select>
        </div>
        <div class="topbar-group">
          <label>{{ $t('chat.provider') }}</label>
          <select class="form-input topbar-select" v-model="currentProvider" @change="updateConvMeta">
            <option value="">{{ $t('chat.autoSelect') }}</option>
            <option v-for="p in providers" :key="p" :value="p">{{ p }}</option>
          </select>
        </div>
        <div class="topbar-group">
          <label>{{ $t('chat.model') }}</label>
          <ModelCombobox
            v-model="modelQuery"
            :models="models"
            :placeholder="$t('chat.selectModel')"
            input-class="topbar-model"
            @update:modelValue="updateConvMeta"
          />
        </div>
      </div>

      <!-- Messages -->
      <div class="chat-messages" ref="messagesRef">
        <div v-if="currentMessages.length === 0" class="chat-empty">
          <div class="chat-empty-icon">W</div>
          <p>{{ $t('chat.sendHint') }}</p>
        </div>
        <div v-for="(msg, idx) in currentMessages" :key="idx" :class="['chat-msg', 'msg-' + msg.role]">
          <div class="msg-avatar">{{ msg.role === 'user' ? 'U' : 'A' }}</div>
          <div class="msg-body">
            <div v-if="msg.role === 'assistant'" class="msg-content markdown-body" v-html="renderMarkdown(getTextContent(msg))"></div>
            <div v-else class="msg-content">
              <template v-if="Array.isArray(msg.content)">
                <template v-for="(part, pi) in msg.content" :key="pi">
                  <span v-if="part.type === 'text'">{{ part.text }}</span>
                  <img v-else-if="part.type === 'image_url'" :src="part.image_url.url" class="msg-image" />
                </template>
              </template>
              <template v-else>{{ msg.content }}</template>
            </div>
          </div>
        </div>
        <!-- Streaming indicator -->
        <div v-if="streaming" class="chat-msg msg-assistant">
          <div class="msg-avatar">A</div>
          <div class="msg-body">
            <div class="msg-content markdown-body" v-html="renderMarkdown(streamContent || '...')"></div>
          </div>
        </div>
      </div>

      <!-- Input area -->
      <div class="chat-input-area">
        <div v-if="error" class="msg msg-error" style="margin-bottom:8px">{{ error }}</div>
        <!-- Image preview -->
        <div v-if="pendingImages.length > 0" class="image-preview-row">
          <div v-for="(img, i) in pendingImages" :key="i" class="image-preview">
            <img :src="img" />
            <button class="image-preview-del" @click="pendingImages.splice(i, 1)">&times;</button>
          </div>
        </div>
        <div class="input-row">
          <button class="btn btn-secondary btn-icon input-attach" @click="triggerFileInput" :title="$t('chat.attachImage')">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48"/></svg>
          </button>
          <textarea
            ref="inputRef"
            class="chat-textarea"
            v-model="userInput"
            :placeholder="$t('chat.inputPlaceholder')"
            rows="1"
            @keydown="onKeydown"
            @paste="onPaste"
            @input="autoResize"
          ></textarea>
          <button class="btn btn-primary input-send" @click="sendMessage" :disabled="sending || (!userInput.trim() && pendingImages.length === 0)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="22" y1="2" x2="11" y2="13"/><polygon points="22 2 15 22 11 13 2 9 22 2"/></svg>
          </button>
        </div>
        <input type="file" ref="fileInputRef" accept="image/*" multiple style="display:none" @change="onFileSelect" />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import { fetchStatus, fetchRouteModels, sendRouteRequest } from '../api.js'
import ModelCombobox from '../components/ModelCombobox.vue'

const { t } = useI18n()

// --- State ---
const sidebarOpen = ref(false)
const conversations = ref([])
const currentId = ref(null)
const routes = ref([])
const routeProviders = ref({}) // route prefix -> provider names
const providers = ref([])
const models = ref([])
const currentRoute = ref('')
const currentProvider = ref('')
const modelQuery = ref('')
const messagesRef = ref(null)
const inputRef = ref(null)
const fileInputRef = ref(null)
const userInput = ref('')
const pendingImages = ref([])
const streaming = ref(false)
const streamContent = ref('')
const sending = ref(false)
const error = ref('')

const STORAGE_KEY = 'warden-chat-conversations'

// --- Markdown ---
marked.setOptions({ breaks: true, gfm: true })

function renderMarkdown(text) {
  if (!text) return ''
  return marked.parse(text)
}

function getTextContent(msg) {
  if (typeof msg.content === 'string') return msg.content
  if (Array.isArray(msg.content)) {
    return msg.content.filter(p => p.type === 'text').map(p => p.text).join('')
  }
  return ''
}

// --- Conversations ---
function loadConversations() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) conversations.value = JSON.parse(raw)
  } catch { /* ignore */ }
}

function saveConversations() {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(conversations.value))
}

function currentConv() {
  return conversations.value.find(c => c.id === currentId.value)
}

const currentMessages = computed(() => {
  const c = currentConv()
  return c ? c.messages : []
})

function genId() {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

function newConversation() {
  const conv = {
    id: genId(),
    title: '',
    route: currentRoute.value,
    provider: currentProvider.value,
    model: modelQuery.value,
    messages: [],
    createdAt: new Date().toISOString(),
  }
  conversations.value.unshift(conv)
  currentId.value = conv.id
  saveConversations()
}

function switchTo(id) {
  currentId.value = id
  const c = currentConv()
  if (c) {
    currentRoute.value = c.route || currentRoute.value
    currentProvider.value = c.provider || ''
    modelQuery.value = c.model || modelQuery.value
    onRouteChange()
  }
  nextTick(scrollToBottom)
}

function deleteConversation(id) {
  conversations.value = conversations.value.filter(c => c.id !== id)
  if (currentId.value === id) {
    currentId.value = conversations.value.length > 0 ? conversations.value[0].id : null
  }
  saveConversations()
}

function updateConvMeta() {
  const c = currentConv()
  if (!c) return
  c.route = currentRoute.value
  c.provider = currentProvider.value
  c.model = modelQuery.value
  if (!c.title && c.messages.length > 0) {
    const first = getTextContent(c.messages[0])
    c.title = first.slice(0, 40) + (first.length > 40 ? '...' : '')
  }
  saveConversations()
}

// --- Route & Model ---
async function loadRoutes() {
  try {
    const status = await fetchStatus()
    routes.value = (status.routes || []).map(r => r.prefix)
    // build route -> providers mapping
    const providerMap = {}
    for (const r of status.routes || []) {
      providerMap[r.prefix] = r.providers || []
    }
    routeProviders.value = providerMap
    if (routes.value.length > 0 && !currentRoute.value) {
      currentRoute.value = routes.value[0]
    }
    updateProviders()
    await loadModels()
  } catch (e) {
    error.value = t('chat.loadRoutesFailed', { error: e.message })
  }
}

function updateProviders() {
  if (!currentRoute.value) {
    providers.value = []
    return
  }
  providers.value = routeProviders.value[currentRoute.value] || []
  // reset provider selection if current provider not in new route
  if (currentProvider.value && !providers.value.includes(currentProvider.value)) {
    currentProvider.value = ''
  }
}

async function loadModels() {
  if (!currentRoute.value) { models.value = []; return }
  try {
    models.value = await fetchRouteModels(currentRoute.value)
  } catch {
    models.value = []
  }
}

async function onRouteChange() {
  updateProviders()
  await loadModels()
  if (models.value.length > 0 && !models.value.includes(modelQuery.value)) {
    modelQuery.value = ''
  }
}

// --- Input ---
function onKeydown(e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendMessage()
  }
}

function autoResize() {
  const el = inputRef.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = Math.min(el.scrollHeight, 200) + 'px'
}

function onPaste(e) {
  const items = e.clipboardData?.items
  if (!items) return
  for (const item of items) {
    if (item.type.startsWith('image/')) {
      e.preventDefault()
      const file = item.getAsFile()
      if (file) readImageFile(file)
    }
  }
}

function triggerFileInput() {
  fileInputRef.value?.click()
}

function onFileSelect(e) {
  for (const file of e.target.files) {
    if (file.type.startsWith('image/')) {
      readImageFile(file)
    }
  }
  e.target.value = ''
}

function readImageFile(file) {
  const reader = new FileReader()
  reader.onload = () => {
    pendingImages.value.push(reader.result)
  }
  reader.readAsDataURL(file)
}

// --- Send ---
async function sendMessage() {
  const text = userInput.value.trim()
  const images = [...pendingImages.value]
  if (!text && images.length === 0) return
  if (!modelQuery.value) { error.value = t('chat.selectModelError'); return }
  if (!currentRoute.value) { error.value = t('chat.selectRouteError'); return }

  error.value = ''

  // ensure conversation exists
  if (!currentConv()) {
    newConversation()
  }

  // build user message
  let content
  if (images.length > 0) {
    content = []
    if (text) content.push({ type: 'text', text })
    for (const img of images) {
      content.push({ type: 'image_url', image_url: { url: img } })
    }
  } else {
    content = text
  }

  const conv = currentConv()
  conv.messages.push({ role: 'user', content })
  userInput.value = ''
  pendingImages.value = []
  nextTick(() => { autoResize(); scrollToBottom() })
  updateConvMeta()

  // send request
  sending.value = true
  streaming.value = true
  streamContent.value = ''

  const body = {
    model: modelQuery.value,
    messages: conv.messages,
    stream: true,
  }

  try {
    const res = await sendRouteRequest(currentRoute.value, 'chat/completions', body, currentProvider.value)
    if (!res.ok) {
      const errText = await res.text()
      error.value = `Error ${res.status}: ${errText}`
      streaming.value = false
      sending.value = false
      return
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let fullContent = ''

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed || !trimmed.startsWith('data: ')) continue
        const dataStr = trimmed.slice(6)
        if (dataStr === '[DONE]') continue
        try {
          const obj = JSON.parse(dataStr)
          const delta = obj.choices?.[0]?.delta?.content || ''
          fullContent += delta
          streamContent.value = fullContent
          nextTick(scrollToBottom)
        } catch { /* ignore */ }
      }
    }

    // add assistant message
    conv.messages.push({ role: 'assistant', content: fullContent })
    saveConversations()
  } catch (e) {
    error.value = e.message
  } finally {
    streaming.value = false
    sending.value = false
    nextTick(scrollToBottom)
  }
}

function scrollToBottom() {
  const el = messagesRef.value
  if (el) el.scrollTop = el.scrollHeight
}

// --- Lifecycle ---
onMounted(async () => {
  loadConversations()
  await loadRoutes()
  if (conversations.value.length > 0) {
    switchTo(conversations.value[0].id)
  }
})
</script>

<style scoped>
.chat-page {
  display: flex;
  height: calc(100vh - 52px);
  margin: -28px -24px;
  overflow: hidden;
}

/* Sidebar */
.chat-sidebar {
  width: 240px;
  flex-shrink: 0;
  background: var(--c-surface);
  border-right: 1px solid var(--c-border);
  display: flex;
  flex-direction: column;
}
.sidebar-new {
  margin: 12px;
  justify-content: center;
}
.conv-list {
  flex: 1;
  overflow-y: auto;
}
.conv-item {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  cursor: pointer;
  font-size: 13px;
  border-left: 3px solid transparent;
  transition: all var(--transition);
}
.conv-item:hover {
  background: var(--c-border-light);
}
.conv-item.active {
  background: var(--c-primary-bg);
  border-left-color: var(--c-primary);
}
.conv-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.conv-del {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--c-text-3);
  font-size: 16px;
  padding: 0 4px;
  opacity: 0;
  transition: opacity var(--transition);
}
.conv-item:hover .conv-del { opacity: 1; }
.conv-del:hover { color: var(--c-danger); }

/* Main */
.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

/* Top bar */
.chat-topbar {
  display: flex;
  gap: 16px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--c-border);
  background: var(--c-surface);
  align-items: center;
  flex-shrink: 0;
}
.topbar-group {
  display: flex;
  align-items: center;
  gap: 8px;
}
.topbar-group label {
  font-size: 12px;
  font-weight: 600;
  color: var(--c-text-2);
  white-space: nowrap;
}
.topbar-select {
  width: 140px;
  padding: 5px 8px !important;
  font-size: 12px !important;
}
.topbar-model {
  width: 220px;
  padding: 5px 8px !important;
  font-size: 12px !important;
}
/* Messages */
.chat-messages {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.chat-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--c-text-3);
  gap: 12px;
}
.chat-empty-icon {
  width: 48px;
  height: 48px;
  background: var(--c-primary);
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
  font-size: 22px;
  color: #fff;
}
.chat-msg {
  display: flex;
  gap: 12px;
  max-width: 800px;
}
.msg-user {
  align-self: flex-end;
  flex-direction: row-reverse;
}
.msg-assistant {
  align-self: flex-start;
}
.msg-avatar {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 13px;
  flex-shrink: 0;
}
.msg-user .msg-avatar {
  background: var(--c-primary-bg);
  color: var(--c-primary);
}
.msg-assistant .msg-avatar {
  background: #1e293b;
  color: #fff;
}
.msg-body {
  min-width: 0;
}
.msg-content {
  padding: 10px 14px;
  border-radius: var(--radius);
  font-size: 14px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
.msg-user .msg-content {
  background: var(--c-primary);
  color: #fff;
  border-bottom-right-radius: 2px;
}
.msg-assistant .msg-content {
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-bottom-left-radius: 2px;
  white-space: normal;
}
.msg-image {
  max-width: 300px;
  max-height: 200px;
  border-radius: var(--radius-sm);
  margin-top: 6px;
  display: block;
}

/* Markdown inside assistant messages */
.markdown-body :deep(p) { margin: 0 0 8px; }
.markdown-body :deep(p:last-child) { margin-bottom: 0; }
.markdown-body :deep(pre) {
  background: #f8fafc;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 10px 12px;
  overflow-x: auto;
  font-size: 13px;
  margin: 8px 0;
}
.markdown-body :deep(code) {
  font-family: var(--font-mono);
  font-size: 0.9em;
}
.markdown-body :deep(pre code) {
  background: none;
  padding: 0;
  border-radius: 0;
}
.markdown-body :deep(code:not(pre code)) {
  background: var(--c-border-light);
  padding: 1px 5px;
  border-radius: 3px;
}
.markdown-body :deep(ul), .markdown-body :deep(ol) {
  padding-left: 20px;
  margin: 8px 0;
}
.markdown-body :deep(li) { margin: 2px 0; }
.markdown-body :deep(blockquote) {
  border-left: 3px solid var(--c-border);
  padding-left: 12px;
  color: var(--c-text-2);
  margin: 8px 0;
}
.markdown-body :deep(table) {
  border-collapse: collapse;
  margin: 8px 0;
  font-size: 13px;
}
.markdown-body :deep(th), .markdown-body :deep(td) {
  border: 1px solid var(--c-border);
  padding: 6px 10px;
}
.markdown-body :deep(h1), .markdown-body :deep(h2), .markdown-body :deep(h3),
.markdown-body :deep(h4), .markdown-body :deep(h5), .markdown-body :deep(h6) {
  margin: 12px 0 6px;
  font-weight: 600;
}
.markdown-body :deep(h1) { font-size: 1.3em; }
.markdown-body :deep(h2) { font-size: 1.2em; }
.markdown-body :deep(h3) { font-size: 1.1em; }

/* Input area */
.chat-input-area {
  padding: 12px 16px 16px;
  border-top: 1px solid var(--c-border);
  background: var(--c-surface);
  flex-shrink: 0;
}
.image-preview-row {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}
.image-preview {
  position: relative;
  display: inline-block;
}
.image-preview img {
  height: 60px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--c-border);
}
.image-preview-del {
  position: absolute;
  top: -6px;
  right: -6px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--c-danger);
  color: #fff;
  border: none;
  cursor: pointer;
  font-size: 12px;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.input-row {
  display: flex;
  gap: 8px;
  align-items: flex-end;
}
.chat-textarea {
  flex: 1;
  resize: none;
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  padding: 10px 12px;
  font-size: 14px;
  font-family: inherit;
  line-height: 1.5;
  background: var(--c-bg);
  color: var(--c-text);
  transition: border-color var(--transition), box-shadow var(--transition);
  max-height: 200px;
  overflow-y: auto;
}
.chat-textarea:focus {
  outline: none;
  border-color: var(--c-primary);
  box-shadow: 0 0 0 3px var(--c-primary-bg);
}
.input-attach, .input-send {
  flex-shrink: 0;
  width: 38px;
  height: 38px;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* Mobile sidebar toggle button */
.sidebar-toggle {
  display: none;
  position: absolute;
  top: 10px;
  left: 10px;
  z-index: 20;
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 6px;
  cursor: pointer;
  color: var(--c-text-2);
  line-height: 1;
}

.sidebar-overlay {
  display: none;
}

@media (max-width: 768px) {
  .chat-page {
    margin: -16px -12px;
    height: calc(100vh - 48px);
  }

  .sidebar-toggle {
    display: flex;
  }

  .sidebar-overlay {
    display: block;
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.3);
    z-index: 29;
  }

  .chat-sidebar {
    position: fixed;
    left: 0;
    top: 48px;
    bottom: 0;
    z-index: 30;
    transform: translateX(-100%);
    transition: transform 200ms ease;
  }

  .chat-sidebar.open {
    transform: translateX(0);
  }

  .chat-topbar {
    flex-wrap: wrap;
    padding: 10px 10px 10px 44px;
    gap: 8px;
  }

  .topbar-group {
    flex: 1;
    min-width: 0;
  }

  .topbar-select,
  .topbar-model {
    width: 100% !important;
  }

  .chat-messages {
    padding: 12px;
    gap: 12px;
  }

  .chat-msg {
    max-width: 100%;
  }

  .msg-image {
    max-width: 200px;
  }

  .chat-input-area {
    padding: 8px 10px 12px;
  }
}
</style>
