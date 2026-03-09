import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  fetchIngestionJobs,
  fetchKnowledgeBases,
  fetchVectorStats,
  listTenants,
  login,
  requestPresignedUploads,
  retryFailedIngestionFile,
  serializeIngestionPayloadFromLocal,
  serializeIngestionPayloadFromUri,
  SessionExpiredError,
  startIngestionProgressStream,
  submitIngestionJob,
  uploadLocalFilesToMinio,
} from './api'
import type {
  AuthSession,
  AuthStatus,
  IngestionJob,
  IngestionProgressEvent,
  KnowledgeBaseRecord,
  LocalItem,
  SourceMode,
  TenantRecord,
  TenantSession,
  VectorStats,
} from './types'

type LocalSelectionResult = {
  items: LocalItem[]
  files: Map<string, File>
  skipped: string[]
}

const ALLOWED_EXTENSIONS = new Set(['.txt', '.md', '.pdf', '.csv', '.json', '.html', '.doc', '.docx'])

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / 1024 ** index
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[index]}`
}

function getExtension(name: string): string {
  const lastDot = name.lastIndexOf('.')
  return lastDot >= 0 ? name.slice(lastDot).toLowerCase() : ''
}

function App() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')

  const [authStatus, setAuthStatus] = useState<AuthStatus>('logged_out')
  const [authSession, setAuthSession] = useState<AuthSession | null>(null)
  const [activeSession, setActiveSession] = useState<TenantSession | null>(null)
  const [tenants, setTenants] = useState<TenantRecord[]>([])
  const [resumeTenantId, setResumeTenantId] = useState<string | null>(null)

  const [sourceMode, setSourceMode] = useState<SourceMode>('uri')
  const [sourceUri, setSourceUri] = useState('')
  const [checksum, setChecksum] = useState('')
  const [localItems, setLocalItems] = useState<LocalItem[]>([])
  const [filesByRelativePath, setFilesByRelativePath] = useState<Map<string, File>>(new Map())
  const [isSubmitting, setIsSubmitting] = useState(false)

  const [jobs, setJobs] = useState<IngestionJob[]>([])
  const [knowledgeBases, setKnowledgeBases] = useState<KnowledgeBaseRecord[]>([])
  const [vectorStats, setVectorStats] = useState<VectorStats | null>(null)

  const [statusMessage, setStatusMessage] = useState('Session is not active.')

  const canLogin = useMemo(() => username.trim() && password, [username, password])

  const kbSummary = useMemo(() => {
    return knowledgeBases.reduce(
      (acc, kb) => {
        acc.documents += kb.documentCount
        acc.chunks += kb.chunkCount
        return acc
      },
      { documents: 0, chunks: 0 },
    )
  }, [knowledgeBases])

  const canSubmit = useMemo(() => {
    if (sourceMode === 'uri') {
      return !!sourceUri.trim() && !!checksum.trim() && !isSubmitting
    }
    return localItems.length > 0 && !!checksum.trim() && !isSubmitting
  }, [checksum, isSubmitting, localItems.length, sourceMode, sourceUri])

  const mergeProgressEvent = useCallback((event: IngestionProgressEvent) => {
    if (event.job) {
      setJobs((current) => {
        const index = current.findIndex((item) => item.jobId === event.job?.jobId)
        if (index < 0) {
          return [event.job as IngestionJob, ...current]
        }
        const next = [...current]
        next[index] = { ...next[index], ...(event.job as IngestionJob) }
        return next
      })
      return
    }

    if (!event.jobId) {
      return
    }

    setJobs((current) => {
      const index = current.findIndex((item) => item.jobId === event.jobId)
      if (index < 0) {
        const created: IngestionJob = {
          jobId: event.jobId as string,
          sourceUri: 'stream://unknown',
          status: 'processing',
          submittedAt: new Date().toISOString(),
        }
        return [created, ...current]
      }

      const next = [...current]
      const target = { ...next[index] }
      if (event.stage) {
        target.stage = event.stage
      }
      if (typeof event.processedFiles === 'number') {
        target.processedFiles = event.processedFiles
      }
      if (typeof event.totalFiles === 'number') {
        target.totalFiles = event.totalFiles
      }
      if (typeof event.successfulFiles === 'number') {
        target.successfulFiles = event.successfulFiles
      }
      if (typeof event.failedFiles === 'number') {
        target.failedFiles = event.failedFiles
      }
      if (event.policyCode) {
        target.policyCode = event.policyCode
      }
      if (event.policyReason) {
        target.policyReason = event.policyReason
      }
      if (event.fileStatus) {
        const existing = target.fileStatuses ?? []
        const fileIndex = existing.findIndex((item) => item.path === event.fileStatus?.path)
        if (fileIndex < 0) {
          target.fileStatuses = [...existing, event.fileStatus]
        } else {
          const files = [...existing]
          files[fileIndex] = { ...files[fileIndex], ...event.fileStatus }
          target.fileStatuses = files
        }
      }

      next[index] = target
      return next
    })
  }, [])

  const refreshData = useCallback(async (session: TenantSession) => {
    const [jobsResponse, kbResponse, vectorResponse] = await Promise.all([
      fetchIngestionJobs(session),
      fetchKnowledgeBases(session),
      fetchVectorStats(session),
    ])
    setJobs(jobsResponse)
    setKnowledgeBases(kbResponse)
    setVectorStats(vectorResponse)
  }, [])

  const clearLocalSelection = useCallback(() => {
    setLocalItems([])
    setFilesByRelativePath(new Map())
  }, [])

  const resetToLoggedOut = useCallback(
    (nextMessage = 'Session is not active.', preserveTenantId: string | null = null) => {
      setAuthStatus('logged_out')
      setAuthSession(null)
      setActiveSession(null)
      setTenants([])
      setResumeTenantId(preserveTenantId)
      setJobs([])
      setKnowledgeBases([])
      setVectorStats(null)
      setSourceUri('')
      setChecksum('')
      clearLocalSelection()
      setSourceMode('uri')
      setPassword('')
      setStatusMessage(nextMessage)
    },
    [clearLocalSelection],
  )

  async function activateTenant(tenantId: string, sessionSource = authSession) {
    if (!sessionSource) {
      return
    }
    const next: TenantSession = {
      ...sessionSource,
      tenantId,
    }
    setActiveSession(next)
    clearLocalSelection()
    setSourceUri('')
    setChecksum('')
    await refreshData(next)
    setStatusMessage(`Active tenant session: ${tenantId}`)
  }

  async function startSession() {
    setAuthStatus('authenticating')
    try {
      const next = await login({
        username: username.trim(),
        password,
      })

      setAuthSession(next)
      setAuthStatus('authenticated')
      setStatusMessage('Authenticated. Resolving tenant access...')

      const resolvedTenants = await listTenants(next)
      setTenants(resolvedTenants)

      if (resolvedTenants.length === 0) {
        setStatusMessage('No tenant assigned to this account.')
        return
      }

      if (resumeTenantId && resolvedTenants.some((item) => item.tenantId === resumeTenantId)) {
        await activateTenant(resumeTenantId, next)
        setResumeTenantId(null)
        setStatusMessage(`Session resumed on tenant: ${resumeTenantId}`)
        return
      }

      if (resolvedTenants.length === 1) {
        await activateTenant(resolvedTenants[0].tenantId, next)
        setStatusMessage(`Tenant auto-opened: ${resolvedTenants[0].tenantId}`)
        return
      }

      setStatusMessage('Select a tenant to open the dashboard.')
    } catch (error) {
      if (error instanceof SessionExpiredError) {
        resetToLoggedOut('Session expired. Please log in again.', resumeTenantId)
        return
      }
      setAuthStatus('error')
      setStatusMessage(error instanceof Error ? error.message : 'authentication failed')
    }
  }

  function readLocalSelection(list: FileList | null): LocalSelectionResult {
    const items: LocalItem[] = []
    const files = new Map<string, File>()
    const skipped: string[] = []

    if (!list) {
      return { items, files, skipped }
    }

    for (const file of Array.from(list)) {
      const relativePath = 'webkitRelativePath' in file && file.webkitRelativePath ? file.webkitRelativePath : file.name
      const extension = getExtension(relativePath)
      if (!ALLOWED_EXTENSIONS.has(extension)) {
        skipped.push(relativePath)
        continue
      }

      items.push({
        name: file.name,
        size: file.size,
        type: file.type,
        lastModified: file.lastModified,
        relativePath,
      })
      files.set(relativePath, file)
    }

    return { items, files, skipped }
  }

  function handleLocalSelection(list: FileList | null) {
    const selected = readLocalSelection(list)
    setLocalItems(selected.items)
    setFilesByRelativePath(selected.files)
    if (selected.skipped.length > 0) {
      setStatusMessage(`Skipped ${selected.skipped.length} unsupported file(s).`)
    }
  }

  async function handleSubmitJob() {
    if (!activeSession || !canSubmit) {
      return
    }

    setIsSubmitting(true)
    try {
      if (sourceMode === 'uri') {
        const payload = serializeIngestionPayloadFromUri(sourceUri, checksum)
        const created = await submitIngestionJob(activeSession, payload)
        setJobs((current) => [created, ...current])
        setSourceUri('')
        setChecksum('')
        setStatusMessage(`Job ${created.jobId} queued for ${activeSession.tenantId}.`)
        return
      }

      const presignedUploads = await requestPresignedUploads(activeSession, localItems)
      await uploadLocalFilesToMinio(presignedUploads, filesByRelativePath)
      const localPayload = serializeIngestionPayloadFromLocal(
        localItems,
        checksum,
        presignedUploads.map((item) => item.objectKey),
      )

      const created = await submitIngestionJob(activeSession, localPayload)
      setJobs((current) => [created, ...current])
      clearLocalSelection()
      setChecksum('')
      setStatusMessage(`Uploaded ${presignedUploads.length} file(s) and queued job ${created.jobId}.`)
    } catch (error) {
      if (error instanceof SessionExpiredError) {
        resetToLoggedOut('Session expired. Please log in again.', activeSession.tenantId)
        return
      }
      setStatusMessage(error instanceof Error ? error.message : 'failed to submit ingestion job')
    } finally {
      setIsSubmitting(false)
    }
  }

  async function handleRefresh() {
    if (!activeSession) {
      return
    }

    try {
      await refreshData(activeSession)
      setStatusMessage('Dashboard data refreshed.')
    } catch (error) {
      if (error instanceof SessionExpiredError) {
        resetToLoggedOut('Session expired. Please log in again.', activeSession.tenantId)
        return
      }
      setStatusMessage(error instanceof Error ? error.message : 'refresh failed')
    }
  }

  async function handleRetryFailedFile(jobId: string, relativePath: string) {
    if (!activeSession) {
      return
    }

    try {
      await retryFailedIngestionFile(activeSession, jobId, relativePath)
      setStatusMessage(`Retry requested for ${relativePath}.`)
    } catch (error) {
      if (error instanceof SessionExpiredError) {
        resetToLoggedOut('Session expired. Please log in again.', activeSession.tenantId)
        return
      }
      setStatusMessage(error instanceof Error ? error.message : 'retry request failed')
    }
  }

  useEffect(() => {
    if (!activeSession || import.meta.env.MODE === 'test') {
      return
    }

    const controller = new AbortController()
    let stopped = false

    const run = async () => {
      while (!stopped && !controller.signal.aborted) {
        try {
          await startIngestionProgressStream(
            activeSession,
            {
              onEvent: mergeProgressEvent,
              onStatus: setStatusMessage,
            },
            controller.signal,
          )
          if (controller.signal.aborted) {
            break
          }
          await delay(400)
        } catch (error) {
          if (controller.signal.aborted) {
            break
          }
          if (error instanceof SessionExpiredError) {
            resetToLoggedOut('Session expired. Please log in again.', activeSession.tenantId)
            break
          }
          setStatusMessage('Ingestion progress stream interrupted. Retrying...')
          await delay(1500)
        }
      }
    }

    void run()

    return () => {
      stopped = true
      controller.abort()
    }
  }, [activeSession, mergeProgressEvent, resetToLoggedOut])

  return (
    <main className="dashboard-root">
      <header className="hero">
        <p className="eyebrow">Enterprise Go RAG</p>
        <h1>Ingestion Dashboard</h1>
        <p className="subtitle">Login, upload to MinIO, and monitor ingestion lifecycle in real time.</p>
      </header>

      {authStatus !== 'authenticated' || !authSession ? (
        <section className="card session-card" data-testid="login-gate">
          <h2>Login</h2>
          <div className="field-grid">
            <label>
              Username
              <input
                data-testid="username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                placeholder="admin"
              />
            </label>
            <label>
              Password
              <input
                data-testid="password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder="password"
              />
            </label>
          </div>
          <button type="button" disabled={!canLogin || authStatus === 'authenticating'} onClick={() => void startSession()}>
            Login
          </button>
        </section>
      ) : null}

      {authStatus === 'authenticated' && authSession && !activeSession && tenants.length > 1 ? (
        <section className="card" data-testid="tenant-resolver">
          <h2>Select Tenant</h2>
          <ul className="list-reset">
            {tenants.map((tenant) => (
              <li key={tenant.tenantId} className="list-item">
                <span>{tenant.displayName}</span>
                <button type="button" onClick={() => void activateTenant(tenant.tenantId)}>
                  Open
                </button>
              </li>
            ))}
          </ul>
          <button type="button" onClick={() => resetToLoggedOut('Logged out.', resumeTenantId)}>
            Logout
          </button>
        </section>
      ) : null}

      {activeSession ? (
        <section className="card" data-testid="dashboard">
          <h2>Tenant Workspace</h2>
          <div className="field-grid">
            <label>
              Active Tenant
              <select
                data-testid="tenant-switcher"
                value={activeSession.tenantId}
                onChange={(event) => void activateTenant(event.target.value)}
              >
                {tenants.map((tenant) => (
                  <option key={tenant.tenantId} value={tenant.tenantId}>
                    {tenant.displayName}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Vector Count
              <input readOnly value={String(vectorStats?.vectorCount ?? 0)} />
            </label>
            <label>
              Vector Storage
              <input readOnly value={formatBytes(vectorStats?.storageBytes ?? 0)} />
            </label>
          </div>
          <div className="row-actions">
            <button type="button" onClick={() => void handleRefresh()}>
              Refresh
            </button>
            <button type="button" onClick={() => resetToLoggedOut('Logged out.', activeSession.tenantId)}>
              Logout
            </button>
          </div>
        </section>
      ) : null}

      {activeSession ? (
        <section className="layout-grid">
          <article className="card">
            <h2>Knowledge Base</h2>
            <div className="field-grid">
              <label>
                Total Documents
                <input readOnly value={String(kbSummary.documents)} />
              </label>
              <label>
                Total Chunks
                <input readOnly value={String(kbSummary.chunks)} />
              </label>
              <label>
                Knowledge Bases
                <input readOnly value={String(knowledgeBases.length)} />
              </label>
            </div>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Status</th>
                    <th>Documents</th>
                    <th>Chunks</th>
                  </tr>
                </thead>
                <tbody>
                  {knowledgeBases.length === 0 ? (
                    <tr>
                      <td colSpan={4}>No knowledge base found for this tenant.</td>
                    </tr>
                  ) : (
                    knowledgeBases.map((kb) => (
                      <tr key={kb.knowledgeBaseId}>
                        <td>{kb.name}</td>
                        <td>{kb.status}</td>
                        <td>{kb.documentCount}</td>
                        <td>{kb.chunkCount}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </article>

          <article className="card">
            <h2>Upload and Ingest</h2>
            <fieldset>
              <legend>Source Mode</legend>
              <label>
                <input
                  type="radio"
                  name="source-mode"
                  checked={sourceMode === 'uri'}
                  onChange={() => setSourceMode('uri')}
                />
                URI
              </label>
              <label>
                <input
                  type="radio"
                  name="source-mode"
                  checked={sourceMode === 'local'}
                  onChange={() => setSourceMode('local')}
                />
                File/Folder Upload
              </label>
            </fieldset>

            {sourceMode === 'uri' ? (
              <label>
                Source URI
                <input
                  data-testid="source-uri"
                  value={sourceUri}
                  onChange={(event) => setSourceUri(event.target.value)}
                  placeholder="s3://tenant-a-ap-south-1/docs/file.txt"
                />
              </label>
            ) : (
              <>
                <label>
                  Upload Files
                  <input
                    data-testid="local-files"
                    type="file"
                    multiple
                    onChange={(event) => handleLocalSelection(event.target.files)}
                  />
                </label>
                <label>
                  Upload Folder
                  <input
                    data-testid="local-folder"
                    type="file"
                    // @ts-expect-error browser-specific directory selection attribute
                    webkitdirectory=""
                    onChange={(event) => handleLocalSelection(event.target.files)}
                  />
                </label>
                <ul data-testid="local-preview" className="list-reset">
                  {localItems.map((item) => (
                    <li key={item.relativePath} className="list-item">
                      <span>{item.relativePath}</span>
                      <small>{formatBytes(item.size)}</small>
                    </li>
                  ))}
                </ul>
              </>
            )}

            <label>
              Checksum
              <input
                data-testid="checksum"
                value={checksum}
                onChange={(event) => setChecksum(event.target.value)}
                placeholder="sha256..."
              />
            </label>

            <div className="row-actions">
              <button type="button" disabled={!canSubmit} onClick={() => void handleSubmitJob()}>
                {isSubmitting ? 'Submitting...' : 'Submit Job'}
              </button>
            </div>
          </article>
        </section>
      ) : null}

      {activeSession ? (
        <section className="card">
          <h2>Ingestion Progress</h2>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Job ID</th>
                  <th>Status / Stage</th>
                  <th>Source</th>
                  <th>Files</th>
                  <th>Governance</th>
                  <th>Submitted</th>
                </tr>
              </thead>
              <tbody>
                {jobs.length === 0 ? (
                  <tr>
                    <td colSpan={6}>No ingestion jobs found for current tenant session.</td>
                  </tr>
                ) : (
                  jobs.map((job) => (
                    <tr key={job.jobId}>
                      <td>{job.jobId}</td>
                      <td>
                        <span className={`pill pill-${job.status}`}>{job.status}</span>
                        <div>{job.stage || 'pending'}</div>
                      </td>
                      <td>{job.sourceUri}</td>
                      <td>
                        {`${job.processedFiles ?? 0}/${job.totalFiles ?? 0}`}
                        <div>{`ok:${job.successfulFiles ?? 0} fail:${job.failedFiles ?? 0}`}</div>
                      </td>
                      <td>
                        <div>{job.policyCode || '-'}</div>
                        <small>{job.policyReason || '-'}</small>
                      </td>
                      <td>{new Date(job.submittedAt).toLocaleString()}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>

          {jobs.some((job) => (job.fileStatuses ?? []).some((item) => item.status === 'failed')) ? (
            <>
              <h3>Failed Files</h3>
              <ul className="list-reset" data-testid="failed-files-list">
                {jobs.flatMap((job) =>
                  (job.fileStatuses ?? [])
                    .filter((item) => item.status === 'failed')
                    .map((item) => (
                      <li key={`${job.jobId}-${item.path}`} className="list-item">
                        <span>
                          {item.path}
                          {item.policyCode ? ` (${item.policyCode})` : ''}
                        </span>
                        <button type="button" onClick={() => void handleRetryFailedFile(job.jobId, item.path)}>
                          Retry
                        </button>
                      </li>
                    )),
                )}
              </ul>
            </>
          ) : null}
        </section>
      ) : null}

      <footer className="status-line" data-testid="status-message">
        {statusMessage}
      </footer>
    </main>
  )
}

export default App
