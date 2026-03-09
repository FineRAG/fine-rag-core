import type { ApiKeyRecord, IngestionJob, NewApiKeyResponse, Session } from './types'

export type IngestionPayload = {
  sourceUri: string
  checksum: string
}

const defaultBaseUrl = 'http://localhost:8080'

export function getApiBaseUrl(): string {
  const fromEnv = import.meta.env.VITE_INGESTION_API_BASE_URL
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

export function serializeIngestionPayload(sourceUri: string, checksum: string): IngestionPayload {
  return {
    sourceUri: sourceUri.trim(),
    checksum: checksum.trim(),
  }
}

export async function submitIngestionJob(
  session: Session,
  payload: IngestionPayload,
): Promise<IngestionJob> {
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

export async function fetchIngestionJobs(session: Session): Promise<IngestionJob[]> {
  const response = await fetch(`${getApiBaseUrl()}/api/v1/ingestion/jobs`, {
    headers: buildTenantHeaders(session),
  })
  if (!response.ok) {
    throw new Error(`failed to fetch ingestion jobs: ${response.status}`)
  }

  return (await response.json()) as IngestionJob[]
}

export async function fetchApiKeys(session: Session): Promise<ApiKeyRecord[]> {
  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants/${session.tenantId}/api-keys`, {
    headers: buildTenantHeaders(session),
  })
  if (!response.ok) {
    throw new Error(`failed to fetch API keys: ${response.status}`)
  }

  return (await response.json()) as ApiKeyRecord[]
}

export async function createApiKey(session: Session, label: string): Promise<NewApiKeyResponse> {
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

export async function revokeApiKey(session: Session, keyId: string): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/api/v1/tenants/${session.tenantId}/api-keys/${keyId}`, {
    method: 'DELETE',
    headers: buildTenantHeaders(session),
  })
  if (!response.ok) {
    throw new Error(`failed to revoke API key: ${response.status}`)
  }
}
