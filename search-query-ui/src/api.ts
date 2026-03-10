import type { AuthSession, StreamEvent, TenantRecord, TenantSession } from './types'

const defaultBaseUrl =
  typeof window !== 'undefined' && window.location?.origin ? window.location.origin : 'http://localhost:8080'

export class SessionExpiredError extends Error {
  constructor(message = 'session expired') {
    super(message)
    this.name = 'SessionExpiredError'
  }
}

export function getApiBaseUrl(): string {
  const fromEnv = import.meta.env.VITE_SEARCH_API_BASE_URL
  return fromEnv && String(fromEnv).trim() ? String(fromEnv).trim() : defaultBaseUrl
}

function assertAuthSession(session: AuthSession | null | undefined): asserts session is AuthSession {
  if (!session?.token) {
    throw new Error('authenticated session is required')
  }
}

function assertTenantSession(session: TenantSession | null | undefined): asserts session is TenantSession {
  assertAuthSession(session)
  if (!session.tenantId) {
    throw new Error('tenant session is required')
  }
}

export function createRequestId(): string {
  return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

export async function login(input: {
  username: string
  apiKey: string
  requestId: string
}): Promise<AuthSession> {
  const response = await fetch(`${getApiBaseUrl()}/api/v1/auth/login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Request-ID': input.requestId.trim() || createRequestId(),
    },
    body: JSON.stringify({
      username: input.username.trim(),
      apiKey: input.apiKey.trim(),
    }),
  })

  if (response.status === 401 || response.status === 403) {
    throw new SessionExpiredError('authentication rejected')
  }
  if (!response.ok) {
    throw new Error(`failed to login: ${response.status}`)
  }

  const payload = (await response.json()) as { token?: string }
  if (!payload.token) {
    throw new Error('failed to login: missing token')
  }

  return {
    username: input.username.trim(),
    token: payload.token,
    requestId: input.requestId.trim() || createRequestId(),
  }
}

export async function listTenants(session: AuthSession): Promise<TenantRecord[]> {
  assertAuthSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants`, {
    headers: {
      Authorization: `Bearer ${session.token}`,
      'Content-Type': 'application/json',
    },
  })

  if (response.status === 401 || response.status === 403) {
    throw new SessionExpiredError('session rejected while listing tenants')
  }
  if (!response.ok) {
    throw new Error(`failed to list tenants: ${response.status}`)
  }

  return (await response.json()) as TenantRecord[]
}

export function buildTenantHeaders(session: TenantSession): HeadersInit {
  assertTenantSession(session)
  return {
    Authorization: `Bearer ${session.token}`,
    'Content-Type': 'application/json',
    'X-Tenant-ID': session.tenantId,
  }
}

type StreamCallbacks = {
  onEvent: (event: StreamEvent) => void
  onStatus: (status: string) => void
}

export async function startSearchStream(
  session: TenantSession,
  queryText: string,
  callbacks: StreamCallbacks,
  signal?: AbortSignal,
): Promise<void> {
  assertTenantSession(session)

  const response = await fetch(`${getApiBaseUrl()}/api/v1/search/stream`, {
    method: 'POST',
    headers: buildTenantHeaders(session),
    body: JSON.stringify({ queryText: queryText.trim() }),
    signal,
  })
  if (response.status === 401 || response.status === 403) {
    throw new SessionExpiredError('query session expired')
  }
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
