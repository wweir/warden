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

const RESPONSE_ROUTE_PROTOCOLS = new Set(['responses_stateless', 'responses_stateful'])
const STATEFUL_RESPONSES_PROTOCOL = 'responses_stateful'

// --- State ---
const sidebarOpen = ref(false)
const conversations = ref([])
const currentId = ref(null)
const routes = ref([])
const routeProviders = ref({}) // route prefix -> provider names
const routeProtocols = ref({}) // route prefix -> configured protocol
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
  return getTextContentFromValue(msg?.content)
}

function normalizeConversation(conv) {
  if (!conv || typeof conv !== 'object') return null
  return {
    ...conv,
    route: typeof conv.route === 'string' ? conv.route : '',
    provider: typeof conv.provider === 'string' ? conv.provider : '',
    model: typeof conv.model === 'string' ? conv.model : '',
    messages: Array.isArray(conv.messages) ? conv.messages : [],
    stateful_response_id: typeof conv.stateful_response_id === 'string' ? conv.stateful_response_id : '',
  }
}

function getTextContentFromValue(content) {
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .filter(part => part?.type === 'text' || part?.type === 'output_text' || part?.type === 'input_text')
      .map(part => part?.text || '')
      .join('')
  }
  return ''
}

function normalizeContentParts(content) {
  if (typeof content === 'string') {
    return content ? [{ type: 'text', text: content }] : []
  }
  if (!Array.isArray(content)) {
    return content == null ? [] : [{ type: 'text', text: String(content) }]
  }

  const parts = []
  for (const part of content) {
    if (!part || typeof part !== 'object') continue
    if (part.type === 'text' && typeof part.text === 'string') {
      parts.push({ type: 'text', text: part.text })
      continue
    }
    if (part.type === 'image_url') {
      const url = typeof part.image_url === 'string' ? part.image_url : part.image_url?.url
      if (url) {
        parts.push({ type: 'image_url', url })
      }
    }
  }
  return parts
}

function parseDataURL(url) {
  if (typeof url !== 'string') return null
  const matched = url.match(/^data:([^;,]+);base64,(.+)$/)
  if (!matched) return null
  return {
    mediaType: matched[1],
    data: matched[2],
  }
}

function resolveRouteProtocol(route = currentRoute.value) {
  return routeProtocols.value[route] || 'chat'
}

function isStatefulResponsesProtocol(protocol) {
  return protocol === STATEFUL_RESPONSES_PROTOCOL
}

function clearConversationStatefulResponse(conv) {
  if (!conv || typeof conv !== 'object') return
  conv.stateful_response_id = ''
}

function currentConversationStatefulResponseID() {
  return currentConv()?.stateful_response_id || ''
}

function toResponsesContent(content, role) {
  const parts = normalizeContentParts(content)
  if (parts.length === 0) return ''
  if (typeof content === 'string' && parts.every(part => part.type === 'text')) {
    return parts.map(part => part.text).join('')
  }
  const textType = role === 'assistant' ? 'output_text' : 'input_text'
  return parts.map(part => (
    part.type === 'text'
      ? { type: textType, text: part.text }
      : { type: 'input_image', image_url: part.url }
  ))
}

function toResponsesInputItem(message) {
  return {
    type: 'message',
    role: message.role,
    content: toResponsesContent(message.content, message.role),
  }
}

function buildResponsesInput(messages) {
  return messages
    .filter(message => ['system', 'user', 'assistant'].includes(message?.role))
    .map(toResponsesInputItem)
}

function latestResponsesTurnInput(messages) {
  for (let idx = messages.length - 1; idx >= 0; idx -= 1) {
    const message = messages[idx]
    if (!['system', 'user', 'assistant'].includes(message?.role)) continue
    return [toResponsesInputItem(message)]
  }
  return []
}

function buildResponsesRequest(protocol, model, messages, previousResponseID = '') {
  const body = {
    model,
    stream: true,
  }

  if (isStatefulResponsesProtocol(protocol) && previousResponseID) {
    body.input = latestResponsesTurnInput(messages)
    body.previous_response_id = previousResponseID
  } else {
    body.input = buildResponsesInput(messages)
  }

  return {
    endpoint: 'responses',
    body,
  }
}

function toAnthropicContent(content) {
  const parts = normalizeContentParts(content)
  if (parts.length === 0) return ''
  if (typeof content === 'string' && parts.every(part => part.type === 'text')) {
    return parts.map(part => part.text).join('')
  }
  return parts.map(part => {
    if (part.type === 'text') {
      return { type: 'text', text: part.text }
    }
    const dataURL = parseDataURL(part.url)
    if (!dataURL) {
      throw new Error(t('chat.unsupportedAnthropicImage'))
    }
    return {
      type: 'image',
      source: {
        type: 'base64',
        media_type: dataURL.mediaType,
        data: dataURL.data,
      },
    }
  })
}

function buildAnthropicRequest(model, messages) {
  const systemTexts = []
  const anthropicMessages = []

  for (const message of messages) {
    if (message?.role === 'system') {
      const text = getTextContent(message)
      if (text) systemTexts.push(text)
      continue
    }
    if (!['user', 'assistant'].includes(message?.role)) continue
    anthropicMessages.push({
      role: message.role,
      content: toAnthropicContent(message.content),
    })
  }

  const body = {
    model,
    messages: anthropicMessages,
    stream: true,
    max_tokens: 4096,
  }
  if (systemTexts.length === 1) {
    body.system = systemTexts[0]
  } else if (systemTexts.length > 1) {
    body.system = systemTexts.join('\n\n')
  }

  return {
    endpoint: 'messages',
    body,
  }
}

function buildProtocolRequest(protocol, model, messages) {
  if (protocol === 'chat') {
    return {
      endpoint: 'chat/completions',
      body: {
        model,
        messages,
        stream: true,
      },
    }
  }
  if (RESPONSE_ROUTE_PROTOCOLS.has(protocol)) {
    return buildResponsesRequest(protocol, model, messages, currentConversationStatefulResponseID())
  }
  if (protocol === 'anthropic') {
    return buildAnthropicRequest(model, messages)
  }
  throw new Error(t('chat.unsupportedProtocol', { protocol }))
}

function extractChatResponseText(payload) {
  const message = payload?.choices?.[0]?.message
  if (!message) return ''
  return getTextContentFromValue(message.content)
}

function extractResponsesResponseText(payload) {
  const response = payload?.response || payload
  const output = response?.output
  if (!Array.isArray(output)) return ''

  const parts = []
  for (const item of output) {
    if (typeof item === 'string') {
      parts.push(item)
      continue
    }
    if (item?.type === 'message') {
      if (typeof item.content === 'string') {
        parts.push(item.content)
        continue
      }
      if (Array.isArray(item.content)) {
        parts.push(
          item.content
            .filter(part => part?.type === 'text' || part?.type === 'output_text' || part?.type === 'input_text')
            .map(part => part?.text || '')
            .join(''),
        )
      }
    }
  }
  return parts.join('\n')
}

function extractResponsesResponseID(payload) {
  const response = payload?.response || payload
  return typeof response?.id === 'string' ? response.id : ''
}

function extractAnthropicResponseText(payload) {
  const message = payload?.message || payload
  const content = message?.content
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''
  return content
    .filter(part => part?.type === 'text')
    .map(part => part?.text || '')
    .join('')
}

function extractProtocolResponseText(payload, protocol) {
  if (protocol === 'chat') return extractChatResponseText(payload)
  if (RESPONSE_ROUTE_PROTOCOLS.has(protocol)) return extractResponsesResponseText(payload)
  if (protocol === 'anthropic') return extractAnthropicResponseText(payload)
  return ''
}

function extractProtocolResponseID(payload, protocol) {
  if (RESPONSE_ROUTE_PROTOCOLS.has(protocol)) return extractResponsesResponseID(payload)
  return ''
}

function parseJSON(value) {
  if (typeof value !== 'string') return null
  try {
    return JSON.parse(value)
  } catch {
    return null
  }
}

function sseDataLine(line) {
  const trimmed = line.trim()
  if (!trimmed.startsWith('data: ')) return null
  return trimmed.slice(6)
}

function extractProtocolDeltaText(payload, protocol) {
  if (protocol === 'chat') {
    return getTextContentFromValue(payload?.choices?.[0]?.delta?.content)
  }
  if (RESPONSE_ROUTE_PROTOCOLS.has(protocol)) {
    if (payload?.type === 'response.output_text.delta' && typeof payload.delta === 'string') {
      return payload.delta
    }
    return ''
  }
  if (protocol === 'anthropic') {
    if (payload?.type === 'content_block_delta' && payload?.delta?.type === 'text_delta') {
      return payload.delta?.text || ''
    }
  }
  return ''
}

function extractProtocolFinalEventText(payload, protocol) {
  if (protocol === 'chat') {
    return extractChatResponseText(payload)
  }
  if (RESPONSE_ROUTE_PROTOCOLS.has(protocol)) {
    return extractResponsesResponseText(payload)
  }
  if (protocol === 'anthropic') {
    return extractAnthropicResponseText(payload)
  }
  return ''
}

function createStreamState(protocol) {
  return {
    protocol,
    buffer: '',
    raw: '',
    content: '',
    finalText: '',
    responseID: '',
  }
}

function consumeProtocolStreamLine(state, line) {
  const data = sseDataLine(line)
  if (data == null || data === '[DONE]') return

  const payload = parseJSON(data)
  if (!payload) return

  const deltaText = extractProtocolDeltaText(payload, state.protocol)
  if (deltaText) {
    state.content += deltaText
    state.sawDelta = true
  }

  const finalText = extractProtocolFinalEventText(payload, state.protocol)
  if (finalText) {
    state.finalText = finalText
  }

  const responseID = extractProtocolResponseID(payload, state.protocol)
  if (responseID) {
    state.responseID = responseID
  }
}

function consumeProtocolStreamChunk(state, chunk) {
  state.raw += chunk
  state.buffer += chunk
  const lines = state.buffer.split('\n')
  state.buffer = lines.pop() || ''
  for (const line of lines) {
    consumeProtocolStreamLine(state, line)
  }
}

function flushProtocolStream(state) {
  if (!state.buffer) return
  consumeProtocolStreamLine(state, state.buffer)
  state.buffer = ''
}

function extractProtocolTextFromRaw(rawText, protocol) {
  const trimmed = rawText.trim()
  if (!trimmed) return { text: '', responseID: '' }

  if (trimmed.startsWith('data:') || trimmed.startsWith('event:')) {
    const state = createStreamState(protocol)
    consumeProtocolStreamChunk(state, trimmed)
    flushProtocolStream(state)
    return {
      text: state.content || state.finalText,
      responseID: state.responseID,
    }
  }

  const payload = parseJSON(trimmed)
  if (!payload) return { text: '', responseID: '' }
  return {
    text: extractProtocolResponseText(payload, protocol),
    responseID: extractProtocolResponseID(payload, protocol),
  }
}

async function readProtocolStream(res, protocol) {
  const reader = res.body?.getReader()
  if (!reader) return { text: '', responseID: '' }

  const decoder = new TextDecoder()
  const state = createStreamState(protocol)

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    const chunk = decoder.decode(value, { stream: true })
    consumeProtocolStreamChunk(state, chunk)
    streamContent.value = state.content || state.finalText || '...'
    nextTick(scrollToBottom)
  }

  const tail = decoder.decode()
  if (tail) {
    consumeProtocolStreamChunk(state, tail)
  }
  flushProtocolStream(state)

  if (state.content || state.finalText || state.responseID) {
    return {
      text: state.content || state.finalText,
      responseID: state.responseID,
    }
  }
  return extractProtocolTextFromRaw(state.raw, protocol)
}

// --- Conversations ---
function loadConversations() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      conversations.value = Array.isArray(parsed)
        ? parsed.map(normalizeConversation).filter(Boolean)
        : []
    }
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
    stateful_response_id: '',
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
  const metaChanged = c.route !== currentRoute.value || c.provider !== currentProvider.value || c.model !== modelQuery.value
  c.route = currentRoute.value
  c.provider = currentProvider.value
  c.model = modelQuery.value
  if (metaChanged) {
    clearConversationStatefulResponse(c)
  }
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
    const protocolMap = {}
    for (const r of status.routes || []) {
      providerMap[r.prefix] = r.providers || []
      protocolMap[r.prefix] = r.protocol || 'chat'
    }
    routeProviders.value = providerMap
    routeProtocols.value = protocolMap
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
  updateConvMeta()
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
  const protocol = resolveRouteProtocol(currentRoute.value)

  try {
    const request = buildProtocolRequest(protocol, modelQuery.value, conv.messages)
    const res = await sendRouteRequest(currentRoute.value, request.endpoint, request.body, currentProvider.value)
    if (!res.ok) {
      const errText = await res.text()
      error.value = `Error ${res.status}: ${errText}`
      streaming.value = false
      sending.value = false
      return
    }

    const result = await readProtocolStream(res, protocol)
    const fullContent = result.text

    // add assistant message
    conv.messages.push({ role: 'assistant', content: fullContent })
    if (isStatefulResponsesProtocol(protocol)) {
      conv.stateful_response_id = result.responseID || ''
    } else {
      clearConversationStatefulResponse(conv)
    }
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
