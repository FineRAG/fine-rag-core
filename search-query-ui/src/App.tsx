import { useMemo, useRef, useState } from 'react'
import { createRequestId, listTenants, login, SessionExpiredError, startSearchStream } from './api'
import type {
  AuthSession,
  AuthStatus,
  SearchCitation,
  SearchTrace,
  StreamEvent,
  TenantRecord,
  TenantSession,
} from './types'

function App() {
  const [username, setUsername] = useState('')
  const [apiKeyInput, setApiKeyInput] = useState('')
  const [requestIdInput, setRequestIdInput] = useState(createRequestId())

  const [authStatus, setAuthStatus] = useState<AuthStatus>('logged_out')
  const [authSession, setAuthSession] = useState<AuthSession | null>(null)
  const [tenants, setTenants] = useState<TenantRecord[]>([])
  const [activeSession, setActiveSession] = useState<TenantSession | null>(null)
  const [queryText, setQueryText] = useState('')
  const [answer, setAnswer] = useState('')
  const [citations, setCitations] = useState<SearchCitation[]>([])
  const [trace, setTrace] = useState<SearchTrace | null>(null)
  const [lastError, setLastError] = useState<string | null>(null)
  const [isStreaming, setIsStreaming] = useState(false)
  const abortControllerRef = useRef<AbortController | null>(null)
  const [statusMessage, setStatusMessage] = useState('Session is not active.')

  const canLogin = useMemo(() => {
    return username.trim() && apiKeyInput.trim() && requestIdInput.trim()
  }, [username, apiKeyInput, requestIdInput])

  function clearSession(message: string, expired = false) {
    setAuthStatus(expired ? 'expired' : 'logged_out')
    setAuthSession(null)
    setActiveSession(null)
    setTenants([])
    setAnswer('')
    setCitations([])
    setTrace(null)
    setQueryText('')
    setIsStreaming(false)
    setRequestIdInput(createRequestId())
    setLastError(null)
    setStatusMessage(message)
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

      const tenantList = await listTenants(next)
      setTenants(tenantList)

      if (tenantList.length === 1) {
        setActiveSession({ ...next, tenantId: tenantList[0].tenantId })
        setStatusMessage(`Tenant auto-opened: ${tenantList[0].tenantId}`)
      } else {
        setStatusMessage('Select a tenant to start querying.')
      }
    } catch (error) {
      if (error instanceof SessionExpiredError) {
        clearSession('Session expired. Please log in again.', true)
        return
      }

      setAuthStatus('error')
      setStatusMessage(error instanceof Error ? error.message : 'authentication failed')
    }
  }

  function activateTenant(tenantId: string) {
    if (!authSession) {
      return
    }
    setActiveSession({ ...authSession, tenantId })
    setAnswer('')
    setCitations([])
    setTrace(null)
    setStatusMessage(`Active tenant session: ${tenantId}`)
  }

  function handleStreamEvent(event: StreamEvent) {
    if (event.type === 'token') {
      setAnswer((current) => `${current}${event.token}`)
      return
    }
    if (event.type === 'citation') {
      setCitations((current) => [...current, event.citation])
      return
    }
    if (event.type === 'trace') {
      setTrace(event.trace)
      return
    }
    if (event.type === 'done') {
      if (event.citations) {
        setCitations(event.citations)
      }
      if (event.trace) {
        setTrace(event.trace)
      }
    }
  }

  async function runQuery() {
    if (!activeSession || !queryText.trim()) {
      return
    }

    setAnswer('')
    setCitations([])
    setTrace(null)
    setLastError(null)
    setIsStreaming(true)

    abortControllerRef.current?.abort()
    const controller = new AbortController()
    abortControllerRef.current = controller

    try {
      await startSearchStream(
        activeSession,
        queryText,
        {
          onEvent: handleStreamEvent,
          onStatus: setStatusMessage,
        },
        controller.signal,
      )
    } catch (error) {
      if (controller.signal.aborted) {
        setStatusMessage('Streaming stopped by user.')
      } else if (error instanceof SessionExpiredError) {
        clearSession('Session expired. Please log in again.', true)
      } else {
        const message = error instanceof Error ? error.message : 'query stream failed'
        setLastError(message)
        setStatusMessage('Streaming interrupted. Retry is available.')
      }
    } finally {
      setIsStreaming(false)
    }
  }

  function stopQuery() {
    abortControllerRef.current?.abort()
    setIsStreaming(false)
  }

  return (
    <main className="dashboard-root">
      <header className="hero">
        <p className="eyebrow">Enterprise Go RAG</p>
        <h1>Search Query UI</h1>
        <p className="subtitle">Run tenant-scoped search, stream answers, and inspect citations and trace metadata.</p>
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

      {authStatus === 'authenticated' && authSession && !activeSession ? (
        <section className="card session-card" data-testid="tenant-resolver">
          <h2>Tenant Resolver</h2>
          {tenants.length === 0 ? <p>No tenant available for this account.</p> : null}
          <ul className="list-reset">
            {tenants.map((tenant) => (
              <li key={tenant.tenantId} className="list-item">
                <span>{tenant.displayName}</span>
                <button type="button" onClick={() => activateTenant(tenant.tenantId)}>
                  Open
                </button>
              </li>
            ))}
          </ul>
          <button type="button" onClick={() => clearSession('Logged out.')}>Logout</button>
        </section>
      ) : null}

      {activeSession ? <section className="layout-grid" data-testid="query-workspace">
        <article className="card">
          <h2>Query Panel</h2>
          <label>
            Query Text
            <textarea
              data-testid="query-text"
              value={queryText}
              onChange={(event) => setQueryText(event.target.value)}
              placeholder="Ask a question about your tenant corpus..."
              rows={5}
            />
          </label>
          <div className="row-actions">
            <button
              type="button"
              onClick={() => {
                void runQuery()
              }}
              disabled={!queryText.trim() || isStreaming}
            >
              Stream Answer
            </button>
            <button
              type="button"
              onClick={() => {
                stopQuery()
              }}
              disabled={!isStreaming}
            >
              Stop Stream
            </button>
          </div>
          {lastError ? <p className="error-text">{lastError}</p> : null}
        </article>
      </section> : null}

      {activeSession ? <section className="card">
        <h2>Streaming Answer</h2>
        <pre className="answer-panel" data-testid="answer-panel">
          {answer || 'No streamed answer yet.'}
        </pre>

        <h3>Citations</h3>
        {citations.length === 0 ? (
          <p className="muted">No citations emitted yet.</p>
        ) : (
          <ul className="list-reset">
            {citations.map((citation) => (
              <li key={citation.id} className="list-item">
                <div>
                  <strong>{citation.title}</strong>
                  <div>{citation.uri}</div>
                </div>
              </li>
            ))}
          </ul>
        )}

        <h3>Trace Metadata</h3>
        <pre className="trace-panel" data-testid="trace-panel">
          {trace ? JSON.stringify(trace, null, 2) : 'No trace metadata yet.'}
        </pre>
      </section> : null}

      {activeSession ? <button type="button" onClick={() => clearSession('Logged out.')}>Logout</button> : null}

      <footer className="status-line" data-testid="status-message">
        {statusMessage}
      </footer>
    </main>
  )
}

export default App
