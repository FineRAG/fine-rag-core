export type AuthStatus = 'logged_out' | 'authenticating' | 'authenticated' | 'expired' | 'error'

export type AuthSession = {
  username: string
  token: string
  requestId: string
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
  password: string
}

export type SourceMode = 'uri' | 'local'

export type LocalItem = {
  name: string
  size: number
  type: string
  lastModified: number
  relativePath: string
}

export type KnowledgeBaseRecord = {
  knowledgeBaseId: string
  name: string
  status: 'ready' | 'indexing' | 'degraded'
  documentCount: number
  chunkCount: number
  lastIngestedAt?: string
}

export type VectorStats = {
  vectorCount: number
  storageBytes: number
  updatedAt?: string
}

export type IngestionFileStatus = {
  path: string
  status: 'queued' | 'processing' | 'approved' | 'quarantine' | 'rejected' | 'failed'
  processedChunks?: number
  policyCode?: string
  policyReason?: string
}

export type IngestionJob = {
  jobId: string
  sourceUri: string
  status: 'queued' | 'processing' | 'approved' | 'quarantine' | 'rejected' | 'failed'
  stage?: string
  processedFiles?: number
  totalFiles?: number
  successfulFiles?: number
  failedFiles?: number
  policyCode?: string
  policyReason?: string
  fileStatuses?: IngestionFileStatus[]
  submittedAt: string
}

export type PresignedUploadItem = {
  relativePath: string
  objectKey: string
  uploadUrl: string
  headers?: Record<string, string>
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
      objectKeys: string[]
      localItems: LocalItem[]
    }

export type IngestionProgressEvent = {
  type: 'job' | 'progress' | 'file' | 'done'
  job?: IngestionJob
  jobId?: string
  stage?: string
  processedFiles?: number
  totalFiles?: number
  successfulFiles?: number
  failedFiles?: number
  fileStatus?: IngestionFileStatus
  policyCode?: string
  policyReason?: string
}
