import { useCallback, useMemo, useState } from 'react'
import {
  createRequestId,
  createApiKey,
  createTenant,
  fetchApiKeys,
  fetchIngestionJobs,
  getDemoDefaults,
  isDemoMode,
  listTenants,
  login,
  revokeApiKey,
  serializeIngestionPayloadFromLocal,
  serializeIngestionPayloadFromUri,
  submitIngestionJob,
} from './api'
import type {
  ApiKeyPopupState,
  ApiKeyRecord,
  AuthSession,
  AuthStatus,
  IngestionJob,
  LocalItem,
  NewApiKeyResponse,
  SourceMode,
  TenantRecord,
  TenantSession,
} from './types'

function App() {
  const demoDefaults = getDemoDefaults()
  const [username, setUsername] = useState(isDemoMode() ? demoDefaults.username : '')
  const [apiKeyInput, setApiKeyInput] = useState(isDemoMode() ? demoDefaults.apiKey : '')
  const [requestIdInput, setRequestIdInput] = useState(isDemoMode() ? demoDefaults.requestId : createRequestId())

  const [authStatus, setAuthStatus] = useState<AuthStatus>('logged_out')
  const [authSession, setAuthSession] = useState<AuthSession | null>(null)
  const [activeSession, setActiveSession] = useState<TenantSession | null>(null)
  const [tenants, setTenants] = useState<TenantRecord[]>([])
  const [tenantDraft, setTenantDraft] = useState(isDemoMode() ? demoDefaults.tenantId : '')
  const [jobs, setJobs] = useState<IngestionJob[]>([])
  const [sourceUri, setSourceUri] = useState('')
  const [checksum, setChecksum] = useState('')
  const [sourceMode, setSourceMode] = useState<SourceMode>('uri')
  const [localItems, setLocalItems] = useState<LocalItem[]>([])
  const [apiKeys, setApiKeys] = useState<ApiKeyRecord[]>([])
  const [newApiKeyLabel, setNewApiKeyLabel] = useState('')
  const [newApiKeyValue, setNewApiKeyValue] = useState<string | null>(null)
  const [apiKeyPopupState, setApiKeyPopupState] = useState<ApiKeyPopupState>('idle')
  const [selectedRevokeKeyId, setSelectedRevokeKeyId] = useState<string | null>(null)
  const [statusMessage, setStatusMessage] = useState('Session is not active.')

  const canLogin = useMemo(() => {
    return username.trim() && apiKeyInput.trim() && requestIdInput.trim()
  }, [username, apiKeyInput, requestIdInput])

  const tenantStage = useMemo(() => {
    if (!authSession) {
      return 'none'
    }
    if (activeSession) {
      return 'resolved'
    }
    if (tenants.length === 1) {
      return 'single'
    }
    return 'select_or_create'
  }, [activeSession, authSession, tenants])

  const refreshData = useCallback(async (active: TenantSession) => {
    const [jobsResponse, keysResponse] = await Promise.all([
      fetchIngestionJobs(active),
      fetchApiKeys(active),
    ])
    setJobs(jobsResponse)
    setApiKeys(keysResponse)
  }, [])

  function resetToLoggedOut(nextMessage = 'Session is not active.') {
    setAuthStatus('logged_out')
    setAuthSession(null)
    setActiveSession(null)
    setTenants([])
    setJobs([])
    setApiKeys([])
    setSourceUri('')
    setChecksum('')
    setLocalItems([])
    setSourceMode('uri')
    setNewApiKeyLabel('')
    setNewApiKeyValue(null)
    setApiKeyPopupState('idle')
    setSelectedRevokeKeyId(null)
    setStatusMessage(nextMessage)
    if (!isDemoMode()) {
      setUsername('')
      setApiKeyInput('')
    }
    setRequestIdInput(createRequestId())
  }

  async function startSession() {
    setAuthStatus('authenticating')

    try {
      const next = await login({
        username: username.trim(),
        apiKey: apiKeyInput.trim(),
        requestId: requestIdInput.trim(),
      })
      setAuthSession(next)
      setAuthStatus('authenticated')
      setStatusMessage('Authenticated. Resolving tenant access...')

      const resolvedTenants = await listTenants(next)
      setTenants(resolvedTenants)

      if (resolvedTenants.length === 1) {
        const autoSession = { ...next, tenantId: resolvedTenants[0].tenantId }
        setActiveSession(autoSession)
        await refreshData(autoSession)
        setStatusMessage(`Tenant auto-opened: ${resolvedTenants[0].tenantId}`)
      } else {
        setStatusMessage('Select an existing tenant or create a new tenant.')
      }
    } catch (error) {
      setAuthStatus('error')
      setStatusMessage(error instanceof Error ? error.message : 'authentication failed')
    }
  }

  async function activateTenant(tenantId: string) {
    if (!authSession) {
      return
    }

    const next = {
      ...authSession,
      tenantId,
    }
    setActiveSession(next)
    setJobs([])
    setApiKeys([])
    setSourceUri('')
    setChecksum('')
    setLocalItems([])
    setNewApiKeyValue(null)
    setApiKeyPopupState('idle')
    setSelectedRevokeKeyId(null)
    await refreshData(next)
    setStatusMessage(`Active tenant session: ${next.tenantId}`)
  }

  async function handleCreateTenant() {
    if (!authSession || !tenantDraft.trim()) {
      return
    }

    const created = await createTenant(authSession, tenantDraft)
    const nextTenants = [...tenants.filter((item) => item.tenantId !== created.tenantId), created]
    setTenants(nextTenants)
    await activateTenant(created.tenantId)
  }

  async function handleSubmitJob() {
    if (!activeSession) {
      return
    }

    const payload =
      sourceMode === 'uri'
        ? serializeIngestionPayloadFromUri(sourceUri, checksum)
        : serializeIngestionPayloadFromLocal(localItems, checksum)

    const created = await submitIngestionJob(activeSession, payload)
    setSourceUri('')
    setChecksum('')
    setLocalItems([])
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

    setApiKeyPopupState('submitting')
    try {
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
      setApiKeyPopupState('success')
      setStatusMessage(`Created key ${created.keyId}. Copy value now; it will not be stored.`)
    } catch {
      setApiKeyPopupState('failure')
      setStatusMessage('Failed to create API key.')
    }
  }

  async function handleRevokeApiKey(keyId: string) {
    if (!activeSession) {
      return
    }

    setApiKeyPopupState('submitting')
    try {
      await revokeApiKey(activeSession, keyId)
      setApiKeys((current) => current.filter((item) => item.keyId !== keyId))
      setApiKeyPopupState('success')
      setStatusMessage(`Revoked API key ${keyId}.`)
      setSelectedRevokeKeyId(null)
    } catch {
      setApiKeyPopupState('failure')
      setStatusMessage('Failed to revoke API key.')
    }
  }

  function readLocalItems(list: FileList | null): LocalItem[] {
    if (!list) {
      return []
    }
    return Array.from(list).map((file) => {
      const relativePath = 'webkitRelativePath' in file ? (file.webkitRelativePath as string) : ''
      return {
        name: file.name,
        size: file.size,
        type: file.type,
        lastModified: file.lastModified,
        relativePath: relativePath || undefined,
      }
    })
  }

  function closeCreatePopup() {
    setApiKeyPopupState('idle')
    setNewApiKeyValue(null)
  }

  function closeRevokePopup() {
    setApiKeyPopupState('idle')
    setSelectedRevokeKeyId(null)
  }

  return (
    <main className="dashboard-root">
      <header className="hero">
        <p className="eyebrow">Enterprise Go RAG</p>
        <h1>Ingestion Dashboard</h1>
        <p className="subtitle">Submit tenant-scoped ingestion jobs, observe outcomes, and manage API keys.</p>
        {isDemoMode() ? <p data-testid="demo-badge">Demo Mode</p> : null}
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
              Request ID
              <input
                data-testid="request-id"
                value={requestIdInput}
                onChange={(event) => setRequestIdInput(event.target.value)}
                placeholder="req-001"
              />
            </label>
            <label>
              API Key
              <input
                data-testid="api-key"
                type="password"
                value={apiKeyInput}
                onChange={(event) => setApiKeyInput(event.target.value)}
                placeholder="tenant key"
              />
            </label>
          </div>
          <button type="button" disabled={!canLogin || authStatus === 'authenticating'} onClick={() => void startSession()}>
            Login
          </button>
        </section>
      ) : null}

      {authStatus === 'authenticated' && authSession && tenantStage !== 'resolved' ? (
        <section className="card session-card" data-testid="tenant-resolver">
          <h2>Tenant Resolver</h2>
          {tenants.length > 0 ? (
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
          ) : (
            <p>No tenants found.</p>
          )}
          <label>
            Create Tenant
            <input
              data-testid="tenant-draft"
              value={tenantDraft}
              onChange={(event) => setTenantDraft(event.target.value)}
              placeholder="tenant-a"
            />
          </label>
          <button type="button" onClick={() => void handleCreateTenant()} disabled={!tenantDraft.trim()}>
            Create and Open
          </button>
          <button type="button" onClick={() => resetToLoggedOut('Logged out.')}>
            Logout
          </button>
        </section>
      ) : null}

      {activeSession ? <section className="layout-grid" data-testid="dashboard"> 
        <article className="card">
          <h2>Ingestion Submission</h2>
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
              Local
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
                Local Files
                <input
                  data-testid="local-files"
                  type="file"
                  multiple
                  onChange={(event) => setLocalItems(readLocalItems(event.target.files))}
                />
              </label>
              <label>
                Local Folder
                <input
                  data-testid="local-folder"
                  type="file"
                  // @ts-expect-error webkitdirectory is browser specific and required for folder selection.
                  webkitdirectory=""
                  onChange={(event) => setLocalItems(readLocalItems(event.target.files))}
                />
              </label>
              <ul data-testid="local-preview" className="list-reset">
                {localItems.map((item, index) => (
                  <li key={`${item.name}-${index}`} className="list-item">
                    {item.relativePath || item.name}
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
            <button
              type="button"
              onClick={() => {
                void handleSubmitJob()
              }}
              disabled={sourceMode === 'uri' ? !sourceUri.trim() || !checksum.trim() : localItems.length === 0 || !checksum.trim()}
            >
              Submit Job
            </button>
            <button
              type="button"
              onClick={() => {
                void handleRefresh()
              }}
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
            />
          </label>
          <div className="row-actions">
            <button
              type="button"
              onClick={() => {
                setApiKeyPopupState('create_dialog_open')
              }}
              disabled={!newApiKeyLabel.trim()}
            >
              Create Key
            </button>
          </div>
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
                    setSelectedRevokeKeyId(key.keyId)
                    setApiKeyPopupState('delete_dialog_open')
                  }}
                >
                  Revoke
                </button>
              </li>
            ))}
          </ul>
        </article>
      </section> : null}

      {activeSession ? <section className="card">
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
      </section> : null}

      {apiKeyPopupState === 'create_dialog_open' ? (
        <section className="card" data-testid="create-key-popup">
          <h3>Confirm API Key Creation</h3>
          <p>Create a new API key for tenant {activeSession?.tenantId}?</p>
          <button type="button" onClick={() => void handleCreateApiKey()}>
            Confirm Create
          </button>
          <button type="button" onClick={() => setApiKeyPopupState('idle')}>
            Cancel
          </button>
        </section>
      ) : null}

      {apiKeyPopupState === 'delete_dialog_open' ? (
        <section className="card" data-testid="revoke-key-popup">
          <h3>Confirm API Key Revoke</h3>
          <p>Revoke key {selectedRevokeKeyId}?</p>
          <button type="button" onClick={() => void handleRevokeApiKey(selectedRevokeKeyId || '')}>
            Confirm Revoke
          </button>
          <button type="button" onClick={closeRevokePopup}>
            Cancel
          </button>
        </section>
      ) : null}

      {newApiKeyValue && (apiKeyPopupState === 'success' || apiKeyPopupState === 'failure' || apiKeyPopupState === 'idle') ? (
        <section className="card secret-box" data-testid="new-api-key-value">
          Newly issued API key (displayed once): <code>{newApiKeyValue}</code>
          <button type="button" onClick={closeCreatePopup}>
            Close
          </button>
        </section>
      ) : null}

      {activeSession ? (
        <button type="button" onClick={() => resetToLoggedOut('Logged out.')}>Logout</button>
      ) : null}

      <footer className="status-line" data-testid="status-message">
        {statusMessage}
      </footer>
    </main>
  )
}

export default App
