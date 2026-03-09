export type Session = {
  tenantId: string
  requestId: string
  apiKey: string
}

export type IngestionJob = {
  jobId: string
  sourceUri: string
  status: 'queued' | 'processing' | 'approved' | 'quarantine' | 'rejected'
  submittedAt: string
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
