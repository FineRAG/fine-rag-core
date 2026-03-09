import { useMemo, useRef, useState } from 'react'
import { startSearchStream } from './api'
import type { SearchCitation, SearchTrace, Session, StreamEvent } from './types'

const defaultSession: Session = {
  tenantId: '',
  requestId: '',
  apiKey: '',
}

function App() {
  const [session, setSession] = useState<Session>(defaultSession)
  const [activeSession, setActiveSession] = useState<Session | null>(null)
  const [queryText, setQueryText] = useState('')
  const [answer, setAnswer] = useState('')
  const [citations, setCitations] = useState<SearchCitation[]>([])
  const [trace, setTrace] = useState<SearchTrace | null>(null)
  const [lastError, setLastError] = useState<string | null>(null)
  const [isStreaming, setIsStreaming] = useState(false)
  const abortControllerRef = useRef<AbortController | null>(null)
  const [statusMessage, setStatusMessage] = useState('Session is not active.')

  const canStartSession = useMemo(() => {
    return session.tenantId.trim() && session.requestId.trim() && session.apiKey.trim()
  }, [session])

  function startSession() {
    const next = {
      tenantId: session.tenantId.trim(),
      requestId: session.requestId.trim(),
      apiKey: session.apiKey.trim(),
    }
    setActiveSession(next)
    setLastError(null)
    setStatusMessage(`Active tenant session: ${next.tenantId}`)
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
      await startSearchStream(activeSession, queryText, {
        onEvent: handleStreamEvent,
        onStatus: setStatusMessage,
      }, controller.signal)
    } catch (error) {
      if (controller.signal.aborted) {
        setStatusMessage('Streaming stopped by user.')
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
          <h2>Query Panel</h2>
          <label>
            Query Text
            <textarea
              data-testid="query-text"
              value={queryText}
              onChange={(event) => setQueryText(event.target.value)}
              placeholder="Ask a question about your tenant corpus..."
              disabled={!activeSession}
              rows={5}
            />
          </label>
          <div className="row-actions">
            <button
              type="button"
              onClick={() => {
                void runQuery()
              }}
              disabled={!activeSession || !queryText.trim() || isStreaming}
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
      </section>

      <section className="card">
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
      </section>

      <footer className="status-line" data-testid="status-message">
        {statusMessage}
      </footer>
    </main>
  )
}

export default App
