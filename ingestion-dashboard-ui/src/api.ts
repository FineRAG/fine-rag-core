import type {
  ApiKeyRecord,
  AuthSession,
  IngestionJob,
  IngestionPayload,
  LocalItem,
  LoginInput,
  NewApiKeyResponse,
  SessionMode,
  TenantRecord,
  TenantSession,
} from './types'

const defaultBaseUrl = 'http://localhost:8080'
const DEMO_USER = 'admin'
const DEMO_KEY = 'sk-1234'
const DEMO_TENANT = 'tenant-1234'

type DemoStore = {
  tenants: TenantRecord[]
  jobs: Record<string, IngestionJob[]>
  apiKeys: Record<string, ApiKeyRecord[]>
  keyCounter: number
  jobCounter: number
}

let demoStore: DemoStore = {
  tenants: [{ tenantId: DEMO_TENANT, displayName: 'Demo Tenant 1234' }],
  jobs: {
    [DEMO_TENANT]: [
      {
        jobId: 'job-demo-1',
        sourceUri: 's3://tenant-1234-ap-south-1/docs/welcome.txt',
        status: 'approved',
        submittedAt: new Date('2026-03-09T10:00:00Z').toISOString(),
      },
    ],
  },
  apiKeys: {
    [DEMO_TENANT]: [
      {
        keyId: 'key-demo-1',
        label: 'bootstrap-demo',
        createdAt: new Date('2026-03-09T10:00:00Z').toISOString(),
      },
    ],
  },
  keyCounter: 2,
  jobCounter: 2,
}

function nextId(prefix: string, value: number): string {
  return `${prefix}-${String(value).padStart(4, '0')}`
}

function isDemoEnabled(): boolean {
  return String(import.meta.env.VITE_DEMO_MODE ?? '').toLowerCase() === 'true'
}

function isBackendMode(): boolean {
  return String(import.meta.env.VITE_BACKEND_MODE ?? '').toLowerCase() === 'true'
}

function resolveMode(): SessionMode {
  if (isDemoEnabled() && !isBackendMode()) {
    return 'demo'
  }
  return 'backend'
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

export function getApiBaseUrl(): string {
  const fromEnv = import.meta.env.VITE_INGESTION_API_BASE_URL
  return fromEnv && String(fromEnv).trim() ? String(fromEnv).trim() : defaultBaseUrl
}

export function isDemoMode(): boolean {
  return !isBackendMode() && isDemoEnabled()
}

export function getDemoDefaults(): LoginInput & { tenantId: string } {
  return {
    username: DEMO_USER,
    apiKey: DEMO_KEY,
    requestId: 'req-demo-1',
    tenantId: DEMO_TENANT,
  }
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

export function createRequestId(): string {
  return `req-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

export async function login(input: LoginInput): Promise<AuthSession> {
  const requestId = input.requestId.trim() || createRequestId()
  const mode = resolveMode()

  if (mode === 'demo') {
    if (!isDemoEnabled()) {
      throw new Error('demo mode is disabled for this build')
    }
    if (input.username.trim() !== DEMO_USER || input.apiKey.trim() !== DEMO_KEY) {
      throw new Error('invalid credentials')
    }
    return {
      username: input.username.trim(),
      token: input.apiKey.trim(),
      requestId,
      mode,
    }
  }

  const response = await fetch(`${getApiBaseUrl()}/api/v1/auth/login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Request-ID': requestId,
    },
    body: JSON.stringify({
      username: input.username.trim(),
      apiKey: input.apiKey.trim(),
    }),
  })
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
    mode,
  }
}

export async function listTenants(session: AuthSession): Promise<TenantRecord[]> {
  assertAuthSession(session)

  if (session.mode === 'demo') {
    return [...demoStore.tenants]
  }

  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants`, {
    headers: {
      Authorization: `Bearer ${session.token}`,
      'Content-Type': 'application/json',
      'X-Request-ID': session.requestId,
    },
  })
  if (!response.ok) {
    throw new Error(`failed to list tenants: ${response.status}`)
  }

  return (await response.json()) as TenantRecord[]
}

export async function createTenant(session: AuthSession, tenantId: string): Promise<TenantRecord> {
  assertAuthSession(session)
  const normalized = tenantId.trim()
  if (!normalized) {
    throw new Error('tenant id is required')
  }

  if (session.mode === 'demo') {
    const existing = demoStore.tenants.find((item) => item.tenantId === normalized)
    if (existing) {
      return existing
    }
    const created = { tenantId: normalized, displayName: normalized }
    demoStore.tenants = [...demoStore.tenants, created]
    demoStore.jobs[normalized] = []
    demoStore.apiKeys[normalized] = []
    return created
  }

  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${session.token}`,
      'Content-Type': 'application/json',
      'X-Request-ID': session.requestId,
    },
    body: JSON.stringify({ tenantId: normalized }),
  })
  if (!response.ok) {
    throw new Error(`failed to create tenant: ${response.status}`)
  }

  return (await response.json()) as TenantRecord
}

export function serializeIngestionPayloadFromUri(sourceUri: string, checksum: string): IngestionPayload {
  return {
    sourceMode: 'uri',
    sourceUri: sourceUri.trim(),
    checksum: checksum.trim(),
  }
}

export function serializeIngestionPayloadFromLocal(items: LocalItem[], checksum: string): IngestionPayload {
  const normalizedItems = items.map((item) => ({
    name: item.name,
    size: item.size,
    type: item.type,
    lastModified: item.lastModified,
    relativePath: item.relativePath,
  }))
  const first = normalizedItems[0]
  const syntheticUri = first
    ? `local://${encodeURIComponent(first.relativePath || first.name)}`
    : 'local://empty-selection'

  return {
    sourceMode: 'local',
    sourceUri: syntheticUri,
    checksum: checksum.trim(),
    localItems: normalizedItems,
  }
}

export async function submitIngestionJob(
  session: TenantSession,
  payload: IngestionPayload,
): Promise<IngestionJob> {
  assertTenantSession(session)

  if (session.mode === 'demo') {
    const created: IngestionJob = {
      jobId: nextId('job-demo', demoStore.jobCounter++),
      sourceUri: payload.sourceUri,
      status: payload.sourceMode === 'local' ? 'processing' : 'queued',
      submittedAt: new Date().toISOString(),
    }
    demoStore.jobs[session.tenantId] = [created, ...(demoStore.jobs[session.tenantId] ?? [])]
    return created
  }

  const response = await fetch(`${getApiBaseUrl()}/api/v1/ingestion/jobs`, {
    method: 'POST',
    headers: buildTenantHeaders(session),
    body: JSON.stringify(payload),
  })
  if (!response.ok) {
    throw new Error(`failed to submit ingestion job: ${response.status}`)
  }

  return (await response.json()) as IngestionJob
}

export async function fetchIngestionJobs(session: TenantSession): Promise<IngestionJob[]> {
  assertTenantSession(session)

  if (session.mode === 'demo') {
    return [...(demoStore.jobs[session.tenantId] ?? [])]
  }

  const response = await fetch(`${getApiBaseUrl()}/api/v1/ingestion/jobs`, {
    headers: buildTenantHeaders(session),
  })
  if (!response.ok) {
    throw new Error(`failed to fetch ingestion jobs: ${response.status}`)
  }

  return (await response.json()) as IngestionJob[]
}

export async function fetchApiKeys(session: TenantSession): Promise<ApiKeyRecord[]> {
  assertTenantSession(session)

  if (session.mode === 'demo') {
    return [...(demoStore.apiKeys[session.tenantId] ?? [])]
  }

  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants/${session.tenantId}/api-keys`, {
    headers: buildTenantHeaders(session),
  })
  if (!response.ok) {
    throw new Error(`failed to fetch API keys: ${response.status}`)
  }

  return (await response.json()) as ApiKeyRecord[]
}

export async function createApiKey(session: TenantSession, label: string): Promise<NewApiKeyResponse> {
  assertTenantSession(session)

  if (session.mode === 'demo') {
    const created = {
      keyId: nextId('key-demo', demoStore.keyCounter++),
      value: `sk-demo-${Math.random().toString(36).slice(2, 10)}`,
      createdAt: new Date().toISOString(),
    }
    demoStore.apiKeys[session.tenantId] = [
      {
        keyId: created.keyId,
        label: label.trim(),
        createdAt: created.createdAt,
      },
      ...(demoStore.apiKeys[session.tenantId] ?? []),
    ]
    return created
  }

  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants/${session.tenantId}/api-keys`, {
    method: 'POST',
    headers: buildTenantHeaders(session),
    body: JSON.stringify({ label: label.trim() }),
  })
  if (!response.ok) {
    throw new Error(`failed to create API key: ${response.status}`)
  }

  return (await response.json()) as NewApiKeyResponse
}

export async function revokeApiKey(session: TenantSession, keyId: string): Promise<void> {
  assertTenantSession(session)

  if (session.mode === 'demo') {
    demoStore.apiKeys[session.tenantId] = (demoStore.apiKeys[session.tenantId] ?? []).filter(
      (item) => item.keyId !== keyId,
    )
    return
  }

  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants/${session.tenantId}/api-keys/${keyId}`, {
    method: 'DELETE',
    headers: buildTenantHeaders(session),
  })
  if (!response.ok) {
    throw new Error(`failed to revoke API key: ${response.status}`)
  }
}

export function __resetDemoStoreForTests(): void {
  demoStore = {
    tenants: [{ tenantId: DEMO_TENANT, displayName: 'Demo Tenant 1234' }],
    jobs: {
      [DEMO_TENANT]: [
        {
          jobId: 'job-demo-1',
          sourceUri: 's3://tenant-1234-ap-south-1/docs/welcome.txt',
          status: 'approved',
          submittedAt: new Date('2026-03-09T10:00:00Z').toISOString(),
        },
      ],
    },
    apiKeys: {
      [DEMO_TENANT]: [
        {
          keyId: 'key-demo-1',
          label: 'bootstrap-demo',
          createdAt: new Date('2026-03-09T10:00:00Z').toISOString(),
        },
      ],
    },
    keyCounter: 2,
    jobCounter: 2,
  }
}
