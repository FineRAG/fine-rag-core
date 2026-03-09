export type AuthStatus = 'logged_out' | 'authenticating' | 'authenticated' | 'expired' | 'error'

export type SessionMode = 'demo' | 'backend'

export type AuthSession = {
  username: string
  token: string
  requestId: string
  mode: SessionMode
}

export type TenantSession = AuthSession & {
  tenantId: string
}

export type TenantRecord = {
  tenantId: string
  displayName: string
}

export type LoginInput = {
  username: string
  apiKey: string
  requestId: string
}

export type IngestionJob = {
  jobId: string
  sourceUri: string
  status: 'queued' | 'processing' | 'approved' | 'quarantine' | 'rejected'
  submittedAt: string
}

export type SourceMode = 'uri' | 'local'

export type LocalItem = {
  name: string
  size: number
  type: string
  lastModified: number
  relativePath?: string
}

export type IngestionPayload =
  | {
      sourceMode: 'uri'
      sourceUri: string
      checksum: string
    }
  | {
      sourceMode: 'local'
      sourceUri: string
      checksum: string
      localItems: LocalItem[]
    }

export type ApiKeyRecord = {
  keyId: string
  label: string
  createdAt: string
  lastUsedAt?: string
}

export type NewApiKeyResponse = {
  keyId: string
  value: string
  createdAt: string
}

export type ApiKeyPopupState =
  | 'idle'
  | 'create_dialog_open'
  | 'delete_dialog_open'
  | 'submitting'
  | 'success'
  | 'failure'
