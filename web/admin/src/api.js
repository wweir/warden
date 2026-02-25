// All API requests rely on the browser's native HTTP Basic Auth.
// After the browser prompts for credentials, it caches and sends them
// automatically with every same-origin request.

export async function fetchStatus() {
  const res = await fetch('/_admin/api/status')
  if (res.status === 401) throw new Error('Unauthorized')
  return res.json()
}

export async function fetchConfig() {
  const res = await fetch('/_admin/api/config')
  if (res.status === 401) throw new Error('Unauthorized')
  return res.json()
}

export async function saveConfig(config) {
  const res = await fetch('/_admin/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  if (res.status === 401) throw new Error('Unauthorized')
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text)
  }
  return res.json()
}

export async function healthCheck(name) {
  const res = await fetch('/_admin/api/providers/health', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (res.status === 401) throw new Error('Unauthorized')
  return res.json()
}

export async function fetchProviderDetail(name) {
  const res = await fetch(`/_admin/api/providers/detail?name=${encodeURIComponent(name)}`)
  if (res.status === 401) throw new Error('Unauthorized')
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text)
  }
  return res.json()
}

export async function validateConfig(config) {
  const res = await fetch('/_admin/api/config/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
  if (res.status === 401) throw new Error('Unauthorized')
  return res.json()
}

export async function restartGateway() {
  const res = await fetch('/_admin/api/restart', {
    method: 'POST',
  })
  if (res.status === 401) throw new Error('Unauthorized')
  return res.json()
}

export async function fetchMcpDetail(name) {
  const res = await fetch(`/_admin/api/mcp/detail?name=${encodeURIComponent(name)}`)
  if (res.status === 401) throw new Error('Unauthorized')
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text)
  }
  return res.json()
}

export async function invokeMcpTool(mcp, tool, args) {
  const res = await fetch('/_admin/api/mcp/tool-call', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mcp, tool, arguments: args }),
  })
  if (res.status === 401) throw new Error('Unauthorized')
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text)
  }
  return res.json()
}

export async function fetchRouteDetail(prefix) {
  const res = await fetch(`/_admin/api/routes/detail?prefix=${encodeURIComponent(prefix)}`)
  if (res.status === 401) throw new Error('Unauthorized')
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text)
  }
  return res.json()
}

export async function fetchRouteModels(prefix) {
  const res = await fetch(`${prefix}/models`)
  if (!res.ok) return []
  const data = await res.json()
  return (data.data || []).map(m => m.id).filter(Boolean)
}

export async function sendRouteRequest(prefix, endpoint, body) {
  const res = await fetch(`${prefix}/${endpoint}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return res
}

export function createLogStream() {
  const url = '/_admin/api/logs/stream'
  return {
    start(onMessage, onError) {
      const ctrl = new AbortController()
      fetch(url, {
        signal: ctrl.signal,
      }).then(res => {
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
                  const data = JSON.parse(line.slice(6))
                  onMessage(data)
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
