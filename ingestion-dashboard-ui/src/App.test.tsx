import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import App from './App'

describe('ingestion dashboard integration flow', () => {
  it('shows login gate before dashboard and supports single-tenant auto-open', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
    vi.stubGlobal('fetch', fetchMock)

    render(<App />)

    expect(screen.getByTestId('login-gate')).toBeInTheDocument()
    expect(screen.queryByTestId('dashboard')).not.toBeInTheDocument()

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByTestId('request-id'), { target: { value: 'req-1' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await waitFor(() => {
      expect(screen.getByTestId('dashboard')).toBeInTheDocument()
    })
    expect(screen.getByTestId('status-message').textContent).toContain('Tenant auto-opened')
  })

  it('submits URI job and preserves auth headers', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          jobId: 'job-2',
          sourceUri: 's3://tenant-a-ap-south-1/docs/new.txt',
          status: 'queued',
          submittedAt: '2026-03-09T11:23:00Z',
        }),
      })

    vi.stubGlobal('fetch', fetchMock)

    render(<App />)

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByTestId('request-id'), { target: { value: 'req-1' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await screen.findByTestId('dashboard')

    fireEvent.change(screen.getByTestId('source-uri'), {
      target: { value: 's3://tenant-a-ap-south-1/docs/new.txt' },
    })
    fireEvent.change(screen.getByTestId('checksum'), { target: { value: 'abc' } })
    fireEvent.click(screen.getByRole('button', { name: 'Submit Job' }))

    await screen.findByText('job-2')

    const submitCall = fetchMock.mock.calls[4]
    expect(submitCall[0]).toContain('/api/v1/ingestion/jobs')
    expect(submitCall[1].method).toBe('POST')
    expect(submitCall[1].headers['X-Tenant-ID']).toBe('tenant-a')
    expect(submitCall[1].headers.Authorization).toBe('Bearer token-a')
  })

  it('requires popup confirmation for create/revoke API key and clears displayed secret on close', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ keyId: 'key-1', label: 'bootstrap', createdAt: '2026-03-09T11:22:33Z' }],
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ keyId: 'key-2', value: 'sk-new', createdAt: '2026-03-09T12:00:00Z' }),
      })
      .mockResolvedValueOnce({ ok: true, json: async () => ({}) })

    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByTestId('request-id'), { target: { value: 'req-1' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await screen.findByTestId('dashboard')

    fireEvent.change(screen.getByTestId('api-key-label'), { target: { value: 'rotation' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create Key' }))
    expect(screen.getByTestId('create-key-popup')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm Create' }))

    await screen.findByTestId('new-api-key-value')

    const createCall = fetchMock.mock.calls[4]
    expect(createCall[1].method).toBe('POST')

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByTestId('new-api-key-value')).not.toBeInTheDocument()

    fireEvent.click(screen.getAllByRole('button', { name: 'Revoke' })[0])
    expect(screen.getByTestId('revoke-key-popup')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm Revoke' }))

    await waitFor(() => {
      expect(fetchMock.mock.calls[5][1].method).toBe('DELETE')
    })
  })

  it('supports local mode preview and local-mode payload serialization strategy', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          jobId: 'job-local',
          sourceUri: 'local://doc.txt',
          status: 'processing',
          submittedAt: '2026-03-09T11:23:00Z',
        }),
      })

    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByTestId('request-id'), { target: { value: 'req-1' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await screen.findByTestId('dashboard')

    fireEvent.click(screen.getByRole('radio', { name: 'Local' }))
    const file = new File(['hello'], 'doc.txt', { type: 'text/plain', lastModified: 100 })
    fireEvent.change(screen.getByTestId('local-files'), { target: { files: [file] } })
    expect(screen.getByTestId('local-preview').textContent).toContain('doc.txt')

    fireEvent.change(screen.getByTestId('checksum'), { target: { value: 'abc' } })
    fireEvent.click(screen.getByRole('button', { name: 'Submit Job' }))

    await screen.findByText('job-local')
    const submitCall = fetchMock.mock.calls[4]
    const body = JSON.parse(submitCall[1].body as string)
    expect(body.sourceMode).toBe('local')
    expect(body.localItems).toHaveLength(1)
  })
})
