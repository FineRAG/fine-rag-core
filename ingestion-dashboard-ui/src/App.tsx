import { useCallback, useMemo, useState } from 'react'
import {
  createApiKey,
  fetchApiKeys,
  fetchIngestionJobs,
  revokeApiKey,
  serializeIngestionPayload,
  submitIngestionJob,
} from './api'
import type { ApiKeyRecord, IngestionJob, NewApiKeyResponse, Session } from './types'

const defaultSession: Session = {
  tenantId: '',
  requestId: '',
  apiKey: '',
}

function App() {
  const [session, setSession] = useState<Session>(defaultSession)
  const [activeSession, setActiveSession] = useState<Session | null>(null)
  const [jobs, setJobs] = useState<IngestionJob[]>([])
  const [sourceUri, setSourceUri] = useState('')
  const [checksum, setChecksum] = useState('')
  const [apiKeys, setApiKeys] = useState<ApiKeyRecord[]>([])
  const [newApiKeyLabel, setNewApiKeyLabel] = useState('')
  const [newApiKeyValue, setNewApiKeyValue] = useState('')
  const [statusMessage, setStatusMessage] = useState('Session is not active.')

  const canStartSession = useMemo(() => {
    return session.tenantId.trim() && session.requestId.trim() && session.apiKey.trim()
  }, [session])

  const refreshData = useCallback(async (active: Session) => {
    const [jobsResponse, keysResponse] = await Promise.all([
      fetchIngestionJobs(active),
      fetchApiKeys(active),
    ])
    setJobs(jobsResponse)
    setApiKeys(keysResponse)
  }, [])

  function startSession() {
    const next = {
      tenantId: session.tenantId.trim(),
      requestId: session.requestId.trim(),
      apiKey: session.apiKey.trim(),
    }
    setActiveSession(next)
    setNewApiKeyValue('')
    setStatusMessage(`Active tenant session: ${next.tenantId}`)
    void refreshData(next)
  }

  async function handleSubmitJob() {
    if (!activeSession) {
      return
    }

    const payload = serializeIngestionPayload(sourceUri, checksum)
    const created = await submitIngestionJob(activeSession, payload)
    setSourceUri('')
    setChecksum('')
    setJobs((current) => [created, ...current])
    setStatusMessage(`Job ${created.jobId} queued for ${activeSession.tenantId}.`)
  }

  async function handleRefresh() {
    if (!activeSession) {
      return
    }
    await refreshData(activeSession)
    setStatusMessage('Ingestion and API-key state refreshed.')
  }

  async function handleCreateApiKey() {
    if (!activeSession || !newApiKeyLabel.trim()) {
      return
    }

    const created: NewApiKeyResponse = await createApiKey(activeSession, newApiKeyLabel)
    setApiKeys((current) => [
      {
        keyId: created.keyId,
        label: newApiKeyLabel.trim(),
        createdAt: created.createdAt,
      },
      ...current,
    ])
    setNewApiKeyLabel('')
    setNewApiKeyValue(created.value)
    setStatusMessage(`Created key ${created.keyId}. Copy value now; it will not be stored.`)
  }

  async function handleRevokeApiKey(keyId: string) {
    if (!activeSession) {
      return
    }

    await revokeApiKey(activeSession, keyId)
    setApiKeys((current) => current.filter((item) => item.keyId !== keyId))
    setStatusMessage(`Revoked API key ${keyId}.`)
  }

  return (
    <main className="dashboard-root">
      <header className="hero">
        <p className="eyebrow">Enterprise Go RAG</p>
        <h1>Ingestion Dashboard</h1>
        <p className="subtitle">Submit tenant-scoped ingestion jobs, observe outcomes, and manage API keys.</p>
      </header>

      <section className="card session-card">
        <h2>Tenant Session Bootstrap</h2>
        <div className="field-grid">
          <label>
            Tenant ID
            <input
              data-testid="tenant-id"
              value={session.tenantId}
              onChange={(event) => setSession((current) => ({ ...current, tenantId: event.target.value }))}
              placeholder="tenant-a"
            />
          </label>
          <label>
            Request ID
            <input
              data-testid="request-id"
              value={session.requestId}
              onChange={(event) => setSession((current) => ({ ...current, requestId: event.target.value }))}
              placeholder="req-001"
            />
          </label>
          <label>
            API Key
            <input
              data-testid="api-key"
              type="password"
              value={session.apiKey}
              onChange={(event) => setSession((current) => ({ ...current, apiKey: event.target.value }))}
              placeholder="tenant key"
            />
          </label>
        </div>
        <button type="button" disabled={!canStartSession} onClick={startSession}>
          Start Session
        </button>
      </section>

      <section className="layout-grid">
        <article className="card">
          <h2>Ingestion Submission</h2>
          <label>
            Source URI
            <input
              data-testid="source-uri"
              value={sourceUri}
              onChange={(event) => setSourceUri(event.target.value)}
              placeholder="s3://tenant-a-ap-south-1/docs/file.txt"
              disabled={!activeSession}
            />
          </label>
          <label>
            Checksum
            <input
              data-testid="checksum"
              value={checksum}
              onChange={(event) => setChecksum(event.target.value)}
              placeholder="sha256..."
              disabled={!activeSession}
            />
          </label>
          <div className="row-actions">
            <button
              type="button"
              onClick={() => {
                void handleSubmitJob()
              }}
              disabled={!activeSession || !sourceUri.trim() || !checksum.trim()}
            >
              Submit Job
            </button>
            <button
              type="button"
              onClick={() => {
                void handleRefresh()
              }}
              disabled={!activeSession}
            >
              Refresh
            </button>
          </div>
        </article>

        <article className="card">
          <h2>API Key Controls</h2>
          <label>
            Label
            <input
              data-testid="api-key-label"
              value={newApiKeyLabel}
              onChange={(event) => setNewApiKeyLabel(event.target.value)}
              placeholder="ops-rotation-march"
              disabled={!activeSession}
            />
          </label>
          <div className="row-actions">
            <button
              type="button"
              onClick={() => {
                void handleCreateApiKey()
              }}
              disabled={!activeSession || !newApiKeyLabel.trim()}
            >
              Create Key
            </button>
          </div>
          {newApiKeyValue ? (
            <div className="secret-box" data-testid="new-api-key-value">
              Newly issued API key (displayed once): <code>{newApiKeyValue}</code>
            </div>
          ) : null}
          <ul className="list-reset">
            {apiKeys.map((key) => (
              <li key={key.keyId} className="list-item">
                <span>
                  <strong>{key.label}</strong> <small>{key.keyId}</small>
                </span>
                <button
                  type="button"
                  className="danger"
                  onClick={() => {
                    void handleRevokeApiKey(key.keyId)
                  }}
                >
                  Revoke
                </button>
              </li>
            ))}
          </ul>
        </article>
      </section>

      <section className="card">
        <h2>Ingestion Job Status</h2>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Job ID</th>
                <th>Source</th>
                <th>Status</th>
                <th>Submitted</th>
              </tr>
            </thead>
            <tbody>
              {jobs.length === 0 ? (
                <tr>
                  <td colSpan={4}>No ingestion jobs found for current tenant session.</td>
                </tr>
              ) : (
                jobs.map((job) => (
                  <tr key={job.jobId}>
                    <td>{job.jobId}</td>
                    <td>{job.sourceUri}</td>
                    <td>
                      <span className={`pill pill-${job.status}`}>{job.status}</span>
                    </td>
                    <td>{new Date(job.submittedAt).toLocaleString()}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      <footer className="status-line" data-testid="status-message">
        {statusMessage}
      </footer>
    </main>
  )
}

export default App
