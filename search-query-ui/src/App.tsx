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

type ChatTurn = {
  id: string
  question: string
  answer: string
  citations: SearchCitation[]
  trace: SearchTrace | null
}

function App() {
  const [username, setUsername] = useState('')
  const [apiKeyInput, setApiKeyInput] = useState('')

  const [authStatus, setAuthStatus] = useState<AuthStatus>('logged_out')
  const [authSession, setAuthSession] = useState<AuthSession | null>(null)
  const [tenants, setTenants] = useState<TenantRecord[]>([])
  const [activeSession, setActiveSession] = useState<TenantSession | null>(null)

  const [queryText, setQueryText] = useState('')
  const [turns, setTurns] = useState<ChatTurn[]>([])
  const [lastError, setLastError] = useState<string | null>(null)
  const [isStreaming, setIsStreaming] = useState(false)
  const abortControllerRef = useRef<AbortController | null>(null)
  const [statusMessage, setStatusMessage] = useState('Session is not active.')

  const canLogin = useMemo(() => username.trim() && apiKeyInput.trim(), [username, apiKeyInput])

  function clearSession(message: string, expired = false) {
    setAuthStatus(expired ? 'expired' : 'logged_out')
    setAuthSession(null)
    setActiveSession(null)
    setTenants([])
    setTurns([])
    setQueryText('')
    setIsStreaming(false)
    setLastError(null)
    setStatusMessage(message)
  }

  async function startSession() {
    setAuthStatus('authenticating')

    try {
      const next = await login({
        username: username.trim(),
        apiKey: apiKeyInput.trim(),
        requestId: createRequestId(),
      })
      setAuthSession(next)
      setAuthStatus('authenticated')
      setStatusMessage('Authenticated. Resolving tenant access...')

      const tenantList = await listTenants(next)
      setTenants(tenantList)

      if (tenantList.length === 1) {
        setActiveSession({ ...next, tenantId: tenantList[0].tenantId })
        setStatusMessage(`Tenant auto-opened: ${tenantList[0].displayName}`)
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
    setStatusMessage(`Active tenant session: ${tenantId}`)
  }

  function applyStreamEvent(turnId: string, event: StreamEvent) {
    setTurns((current) =>
      current.map((turn) => {
        if (turn.id !== turnId) {
          return turn
        }
        if (event.type === 'token') {
          return { ...turn, answer: `${turn.answer}${event.token}` }
        }
        if (event.type === 'citation') {
          return { ...turn, citations: [...turn.citations, event.citation] }
        }
        if (event.type === 'trace') {
          return { ...turn, trace: event.trace }
        }
        if (event.type === 'done') {
          return {
            ...turn,
            citations: event.citations ?? turn.citations,
            trace: event.trace ?? turn.trace,
          }
        }
        return turn
      }),
    )
  }

  async function runQuery() {
    if (!activeSession || !queryText.trim()) {
      return
    }

    const question = queryText.trim()
    const turnId = `turn-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`

    setQueryText('')
    setLastError(null)
    setIsStreaming(true)
    setTurns((current) => [
      ...current,
      {
        id: turnId,
        question,
        answer: '',
        citations: [],
        trace: null,
      },
    ])

    abortControllerRef.current?.abort()
    const controller = new AbortController()
    abortControllerRef.current = controller

    try {
      await startSearchStream(
        activeSession,
        question,
        {
          onEvent: (event) => applyStreamEvent(turnId, event),
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
    <main className="chat-root">
      <header className="chat-header">
        <p className="eyebrow">Enterprise FineR</p>
        <h1>Query</h1>
        <p className="subtitle">Ask one focused question and stream answers from your tenant knowledge base.</p>
      </header>

      {authStatus !== 'authenticated' || !authSession ? (
        <section className="login-card" data-testid="login-gate">
          <h2>Sign In</h2>
          <label>
            Username
            <input data-testid="username" value={username} onChange={(event) => setUsername(event.target.value)} placeholder="admin" />
          </label>
          <label>
            API Key
            <input
              data-testid="api-key"
              type="password"
              value={apiKeyInput}
              onChange={(event) => setApiKeyInput(event.target.value)}
              placeholder="Enter API key"
            />
          </label>
          <button type="button" disabled={!canLogin || authStatus === 'authenticating'} onClick={() => void startSession()}>
            Login
          </button>
        </section>
      ) : null}

      {authStatus === 'authenticated' && authSession && !activeSession ? (
        <section className="login-card" data-testid="tenant-resolver">
          <h2>Select Tenant</h2>
          <ul className="tenant-list">
            {tenants.map((tenant) => (
              <li key={tenant.tenantId}>
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

      {activeSession ? (
        <section className="chat-shell" data-testid="query-workspace">
          <div className="chat-toolbar">
            <span>{`Tenant: ${tenants.find((tenant) => tenant.tenantId === activeSession.tenantId)?.displayName ?? activeSession.tenantId}`}</span>
            {tenants.length > 1 ? (
              <select
                data-testid="tenant-switcher"
                value={activeSession.tenantId}
                onChange={(event) => activateTenant(event.target.value)}
              >
                {tenants.map((tenant) => (
                  <option key={tenant.tenantId} value={tenant.tenantId}>
                    {tenant.displayName}
                  </option>
                ))}
              </select>
            ) : null}
            <button type="button" onClick={() => clearSession('Logged out.')}>Logout</button>
          </div>

          <section className="chat-stream" data-testid="answer-panel">
            {turns.length === 0 ? <p className="muted">No conversations yet. Ask your first question.</p> : null}
            {turns.map((turn) => (
              <article key={turn.id} className="turn">
                <div className="bubble question">{turn.question}</div>
                <div className="bubble answer">{turn.answer || (isStreaming ? 'Streaming response...' : 'No response.')}</div>
                {turn.citations.length > 0 ? (
                  <div className="citations">
                    {turn.citations.map((citation) => (
                      <div key={citation.id} className="citation-item">
                        <strong>{citation.title}</strong>
                        <span>{citation.uri}</span>
                      </div>
                    ))}
                  </div>
                ) : null}
                {turn.trace ? (
                  <pre className="trace-panel" data-testid="trace-panel">
                    {JSON.stringify(turn.trace, null, 2)}
                  </pre>
                ) : null}
              </article>
            ))}
          </section>

          <div className="composer">
            <input
              data-testid="query-text"
              value={queryText}
              onChange={(event) => setQueryText(event.target.value)}
              placeholder="Ask about your documents..."
              onKeyDown={(event) => {
                if (event.key === 'Enter' && !event.shiftKey) {
                  event.preventDefault()
                  void runQuery()
                }
              }}
            />
            <button type="button" onClick={() => void runQuery()} disabled={!queryText.trim() || isStreaming}>
              Send
            </button>
            <button type="button" onClick={stopQuery} disabled={!isStreaming}>
              Stop
            </button>
          </div>
          {lastError ? <p className="error-text">{lastError}</p> : null}
        </section>
      ) : null}

      <footer className="status-line" data-testid="status-message">
        {statusMessage}
      </footer>
    </main>
  )
}

export default App
