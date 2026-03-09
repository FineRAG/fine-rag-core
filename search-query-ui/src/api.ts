import type { Session, StreamEvent } from './types'

const defaultBaseUrl = 'http://localhost:8080'

export function getApiBaseUrl(): string {
  const fromEnv = import.meta.env.VITE_SEARCH_API_BASE_URL
  return fromEnv && String(fromEnv).trim() ? String(fromEnv).trim() : defaultBaseUrl
}

export function buildTenantHeaders(session: Session): HeadersInit {
  return {
    Authorization: `Bearer ${session.apiKey}`,
    'Content-Type': 'application/json',
    'X-Request-ID': session.requestId,
    'X-Tenant-ID': session.tenantId,
  }
}

type StreamCallbacks = {
  onEvent: (event: StreamEvent) => void
  onStatus: (status: string) => void
}

export async function startSearchStream(
  session: Session,
  queryText: string,
  callbacks: StreamCallbacks,
  signal?: AbortSignal,
): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/api/v1/search/stream`, {
    method: 'POST',
    headers: buildTenantHeaders(session),
    body: JSON.stringify({ queryText: queryText.trim() }),
    signal,
  })
  if (!response.ok) {
    throw new Error(`failed to start search stream: ${response.status}`)
  }
  if (!response.body) {
    throw new Error('search stream body is empty')
  }

  callbacks.onStatus('Streaming response...')

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) {
      break
    }

    buffer += decoder.decode(value, { stream: true })
    const frames = buffer.split('\n\n')
    buffer = frames.pop() ?? ''

    for (const frame of frames) {
      const event = parseSseFrame(frame)
      if (!event) {
        continue
      }
      callbacks.onEvent(event)
      if (event.type === 'done') {
        callbacks.onStatus('Search completed.')
      }
    }
  }

  if (buffer.trim()) {
    const event = parseSseFrame(buffer)
    if (event) {
      callbacks.onEvent(event)
    }
  }

  callbacks.onStatus('Search stream closed.')
}

function parseSseFrame(frame: string): StreamEvent | null {
  const lines = frame
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trim())

  if (lines.length === 0) {
    return null
  }

  const payload = lines.join('')
  try {
    return JSON.parse(payload) as StreamEvent
  } catch {
    return { type: 'token', token: payload }
  }
}
