import type {
  AuthSession,
  IngestionJob,
  IngestionPayload,
  IngestionProgressEvent,
  KnowledgeBaseRecord,
  LocalItem,
  LoginInput,
  PresignedUploadItem,
  TenantRecord,
  TenantSession,
  VectorStats,
} from './types'

const defaultBaseUrl =
  typeof window !== 'undefined' && window.location?.origin ? window.location.origin : 'http://localhost:8080'

export class SessionExpiredError extends Error {
  constructor(message = 'session expired') {
    super(message)
    this.name = 'SessionExpiredError'
  }
}

function assertAuthSession(session: AuthSession | null | undefined): asserts session is AuthSession {
  if (!session?.token || !session.requestId) {
    throw new Error('authenticated session is required')
  }
}

function assertTenantSession(session: TenantSession | null | undefined): asserts session is TenantSession {
  assertAuthSession(session)
  if (!session.tenantId) {
    throw new Error('tenant session is required')
  }
}

function assertNotExpired(response: Response): void {
  if (response.status === 401 || response.status === 403) {
    throw new SessionExpiredError('session expired; please log in again')
  }
}

export function getApiBaseUrl(): string {
  const fromEnv = import.meta.env.VITE_INGESTION_API_BASE_URL
  return fromEnv && String(fromEnv).trim() ? String(fromEnv).trim() : defaultBaseUrl
}

export function createRequestId(): string {
  return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

export function buildTenantHeaders(session: TenantSession): HeadersInit {
  assertTenantSession(session)
  return {
    Authorization: `Bearer ${session.token}`,
    'Content-Type': 'application/json',
    'X-Request-ID': session.requestId,
    'X-Tenant-ID': session.tenantId,
  }
}

export async function login(input: LoginInput): Promise<AuthSession> {
  const requestId = createRequestId()
  const response = await fetch(`${getApiBaseUrl()}/api/v1/auth/login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Request-ID': requestId,
    },
    body: JSON.stringify({
      username: input.username.trim(),
      apiKey: input.password,
    }),
  })

  assertNotExpired(response)
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
    requestId,
  }
}

export async function purgeTenantData(session: TenantSession): Promise<{ status: string; tenantId: string; deletedObjects: number }> {
  assertTenantSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants/${session.tenantId}/purge`, {
    method: 'POST',
    headers: buildTenantHeaders(session),
    body: JSON.stringify({ confirm: 'PURGE' }),
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to purge tenant data: ${response.status}`)
  }

  return (await response.json()) as { status: string; tenantId: string; deletedObjects: number }
}

export async function listTenants(session: AuthSession): Promise<TenantRecord[]> {
  assertAuthSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants`, {
    headers: {
      Authorization: `Bearer ${session.token}`,
      'Content-Type': 'application/json',
      'X-Request-ID': session.requestId,
    },
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to list tenants: ${response.status}`)
  }

  return (await response.json()) as TenantRecord[]
}

export async function fetchKnowledgeBases(session: TenantSession): Promise<KnowledgeBaseRecord[]> {
  assertTenantSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/knowledge-bases`, {
    headers: buildTenantHeaders(session),
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to fetch knowledge bases: ${response.status}`)
  }

  return (await response.json()) as KnowledgeBaseRecord[]
}

export async function fetchVectorStats(session: TenantSession): Promise<VectorStats> {
  assertTenantSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants/${session.tenantId}/vector-stats`, {
    headers: buildTenantHeaders(session),
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to fetch vector stats: ${response.status}`)
  }

  return (await response.json()) as VectorStats
}

export function serializeIngestionPayloadFromUri(sourceUri: string): IngestionPayload {
  return {
    sourceMode: 'uri',
    sourceUri: sourceUri.trim(),
  }
}

export function serializeIngestionPayloadFromLocal(
  items: LocalItem[],
  objectKeys: string[],
): IngestionPayload {
  const normalizedItems = items.map((item) => ({
    name: item.name,
    size: item.size,
    type: item.type,
    lastModified: item.lastModified,
    relativePath: item.relativePath,
  }))

  const first = normalizedItems[0]
  const syntheticUri = first ? `local://${encodeURIComponent(first.relativePath)}` : 'local://empty-selection'

  return {
    sourceMode: 'local',
    sourceUri: syntheticUri,
    objectKeys,
    localItems: normalizedItems,
  }
}

export async function requestPresignedUploads(
  session: TenantSession,
  items: LocalItem[],
): Promise<PresignedUploadItem[]> {
  assertTenantSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/uploads/presign`, {
    method: 'POST',
    headers: buildTenantHeaders(session),
    body: JSON.stringify({
      files: items.map((item) => ({
        name: item.name,
        size: item.size,
        type: item.type,
        relativePath: item.relativePath,
      })),
    }),
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to create presigned uploads: ${response.status}`)
  }

  const payload = (await response.json()) as { uploads?: PresignedUploadItem[] }
  if (!payload.uploads || payload.uploads.length === 0) {
    throw new Error('failed to create presigned uploads: no uploads returned')
  }
  return payload.uploads
}

export async function uploadLocalFilesToMinio(
  uploads: PresignedUploadItem[],
  filesByRelativePath: Map<string, File>,
): Promise<void> {
  for (const upload of uploads) {
    const sourceFile = filesByRelativePath.get(upload.relativePath)
    if (!sourceFile) {
      throw new Error(`missing selected file for ${upload.relativePath}`)
    }

    const response = await fetch(upload.uploadUrl, {
      method: 'PUT',
      headers: upload.headers ?? {},
      body: sourceFile,
    })
    if (!response.ok) {
      throw new Error(`failed to upload ${upload.relativePath}: ${response.status}`)
    }
  }
}

export async function submitIngestionJob(session: TenantSession, payload: IngestionPayload): Promise<IngestionJob> {
  assertTenantSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/ingestion/jobs`, {
    method: 'POST',
    headers: buildTenantHeaders(session),
    body: JSON.stringify(payload),
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to submit ingestion job: ${response.status}`)
  }

  return (await response.json()) as IngestionJob
}

export async function fetchIngestionJobs(session: TenantSession): Promise<IngestionJob[]> {
  assertTenantSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/ingestion/jobs`, {
    headers: buildTenantHeaders(session),
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to fetch ingestion jobs: ${response.status}`)
  }

  return (await response.json()) as IngestionJob[]
}

export async function retryFailedIngestionFile(
  session: TenantSession,
  jobId: string,
  relativePath: string,
): Promise<void> {
  assertTenantSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/ingestion/jobs/${jobId}/retry`, {
    method: 'POST',
    headers: buildTenantHeaders(session),
    body: JSON.stringify({ relativePath }),
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to retry failed file: ${response.status}`)
  }
}

export async function startIngestionProgressStream(
  session: TenantSession,
  callbacks: {
    onEvent: (event: IngestionProgressEvent) => void
    onStatus: (status: string) => void
  },
  signal?: AbortSignal,
): Promise<void> {
  assertTenantSession(session)
  const response = await fetch(`${getApiBaseUrl()}/api/v1/ingestion/jobs/stream?tenantId=${encodeURIComponent(session.tenantId)}`, {
    method: 'GET',
    headers: buildTenantHeaders(session),
    signal,
  })

  assertNotExpired(response)
  if (!response.ok) {
    throw new Error(`failed to open ingestion progress stream: ${response.status}`)
  }
  if (!response.body) {
    throw new Error('ingestion progress stream body is empty')
  }

  callbacks.onStatus('Streaming ingestion progress...')
  const decoder = new TextDecoder()
  const reader = response.body.getReader()
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
      if (event) {
        callbacks.onEvent(event)
      }
    }
  }

  if (buffer.trim()) {
    const event = parseSseFrame(buffer)
    if (event) {
      callbacks.onEvent(event)
    }
  }
  callbacks.onStatus('Ingestion stream closed.')
}

function parseSseFrame(frame: string): IngestionProgressEvent | null {
  const lines = frame
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)

  const dataLines = lines
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trim())

  if (dataLines.length === 0) {
    return null
  }

  const payload = dataLines.join('\n')
  try {
    return JSON.parse(payload) as IngestionProgressEvent
  } catch {
    return null
  }
}
