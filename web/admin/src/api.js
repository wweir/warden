// All API requests rely on the browser's native HTTP Basic Auth.
// After the browser prompts for credentials, it caches and sends them
// automatically with every same-origin request.

async function apiFetch(url, options = {}) {
  const res = await fetch(url, options)
  if (res.status === 401) throw new Error('Unauthorized')
  return res
}

async function apiJSON(url, options = {}) {
  const res = await apiFetch(url, options)
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text)
  }
  return res.json()
}

export function createStatusStream() {
  return createSSEStream('/_admin/api/status')
}

// fetchStatus fetches status once via SSE stream (reads first event then closes).
export function fetchStatus() {
  return new Promise((resolve, reject) => {
    const stop = createStatusStream().start(
      (data) => { stop(); resolve(data) },
      (e) => { reject(e) }
    )
  })
}

export function fetchConfigSource() {
  return apiJSON('/_admin/api/config/source')
}

export function fetchConfig() {
  return apiJSON('/_admin/api/config')
}

export function fetchToolHookSuggestions() {
  return apiJSON('/_admin/api/tool-hooks/suggestions')
}

export async function saveConfig(config) {
  const res = await apiFetch('/_admin/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text)
  }
  return res.json()
}

export function healthCheck(name) {
  return apiJSON('/_admin/api/providers/health', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
}

export function setProviderSuppress(name, suppress) {
  return apiJSON('/_admin/api/providers/suppress', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, suppress }),
  })
}

export function fetchProviderDetail(name) {
  return apiJSON(`/_admin/api/providers/detail?name=${encodeURIComponent(name)}`)
}

export function validateConfig(config) {
  return apiJSON('/_admin/api/config/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
}

export function restartGateway() {
  return apiJSON('/_admin/api/restart', { method: 'POST' })
}

export function fetchMcpDetail(name) {
  return apiJSON(`/_admin/api/mcp/detail?name=${encodeURIComponent(name)}`)
}

export function invokeMcpTool(mcp, tool, args) {
  return apiJSON('/_admin/api/mcp/tool-call', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mcp, tool, arguments: args }),
  })
}

export function toggleMcpTool(mcp, tool, disabled) {
  return apiJSON('/_admin/api/mcp/tool-toggle', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mcp, tool, disabled }),
  })
}

export function fetchRouteDetail(prefix) {
  return apiJSON(`/_admin/api/routes/detail?prefix=${encodeURIComponent(prefix)}`)
}

export async function fetchRouteModels(prefix) {
  try {
    const res = await fetch(`${prefix}/models`)
    if (!res.ok) return []
    const data = await res.json()
    return (data.data || []).map(m => m.id).filter(Boolean)
  } catch {
    return []
  }
}

export function sendRouteRequest(prefix, endpoint, body, provider = '') {
  const headers = { 'Content-Type': 'application/json' }
  if (provider) {
    headers['X-Provider'] = provider
  }
  return fetch(`${prefix}/${endpoint}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
}

// createSSEStream creates a generic SSE stream reader.
function createSSEStream(url) {
  return {
    start(onMessage, onError) {
      const ctrl = new AbortController()
      fetch(url, { signal: ctrl.signal }).then(res => {
        if (res.status === 401) {
          onError(new Error('Unauthorized'))
          return
        }
        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        function read() {
          reader.read().then(({ done, value }) => {
            if (done) return
            buffer += decoder.decode(value, { stream: true })
            const lines = buffer.split('\n')
            buffer = lines.pop() || ''
            for (const line of lines) {
              if (line.startsWith('data: ')) {
                try {
                  onMessage(JSON.parse(line.slice(6)))
                } catch (e) { /* ignore parse errors */ }
              }
            }
            read()
          }).catch(err => {
            if (err.name !== 'AbortError') onError(err)
          })
        }
        read()
      }).catch(err => {
        if (err.name !== 'AbortError') onError(err)
      })
      return () => ctrl.abort()
    },
  }
}

export function createLogStream() {
  return createSSEStream('/_admin/api/logs/stream')
}

export function createMetricsStream() {
  return createSSEStream('/_admin/api/metrics/stream')
}

export async function fetchMetrics() {
  const res = await apiFetch('/metrics')
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text)
  }
  return res.text()
}

export function parseMetrics(text) {
  const lines = text.split('\n')
  const metrics = {
    requestsTotal: [],
    requestDuration: [],
    providerHealth: [],
    providerSuppressed: []
  }

  for (const line of lines) {
    if (line.startsWith('#') || !line.trim()) continue

    const reqMatch = line.match(/^warden_requests_total\{route="([^"]+)",provider="([^"]+)",status="([^"]+)"\}\s+(\d+)/)
    if (reqMatch) {
      metrics.requestsTotal.push({
        route: reqMatch[1],
        provider: reqMatch[2],
        status: reqMatch[3],
        value: parseInt(reqMatch[4])
      })
      continue
    }

    const durMatch = line.match(/^warden_request_duration_ms_bucket\{route="([^"]+)",provider="([^"]+)",le="([^"]+)"\}\s+(\d+)/)
    if (durMatch) {
      metrics.requestDuration.push({
        route: durMatch[1],
        provider: durMatch[2],
        le: durMatch[3],
        value: parseInt(durMatch[4])
      })
      continue
    }

    const healthMatch = line.match(/^warden_provider_health\{provider="([^"]+)"\}\s+(\d+)/)
    if (healthMatch) {
      metrics.providerHealth.push({
        provider: healthMatch[1],
        value: parseInt(healthMatch[2])
      })
      continue
    }

    const suppMatch = line.match(/^warden_provider_suppressed\{provider="([^"]+)"\}\s+(\d+)/)
    if (suppMatch) {
      metrics.providerSuppressed.push({
        provider: suppMatch[1],
        value: parseInt(suppMatch[2])
      })
    }
  }

  return metrics
}
