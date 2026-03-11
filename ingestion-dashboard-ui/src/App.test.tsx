import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import App from './App'

describe('ingestion dashboard integration flow', () => {
  it('shows username/password login gate and auto-opens single tenant dashboard', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ vectorCount: 55, storageBytes: 1024 }) })

    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    expect(screen.getByTestId('login-gate')).toBeInTheDocument()
    expect(screen.queryByTestId('request-id')).not.toBeInTheDocument()
    expect(screen.queryByTestId('api-key')).not.toBeInTheDocument()

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByTestId('password'), { target: { value: 'sk-1234' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await waitFor(() => {
      expect(screen.getByTestId('dashboard')).toBeInTheDocument()
    })

    expect(screen.getByRole('heading', { name: 'Tenant A Workspace' })).toBeInTheDocument()
    expect(screen.getByText('55')).toBeInTheDocument()
    expect(screen.getByTestId('status-message').textContent).toContain('Tenant auto-opened')
  })

  it('submits URI ingestion using auth headers', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }] })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ vectorCount: 1, storageBytes: 1 }) })
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

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByTestId('password'), { target: { value: 'sk-1234' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await screen.findByTestId('dashboard')

    fireEvent.change(screen.getByTestId('source-uri'), {
      target: { value: 's3://tenant-a-ap-south-1/docs/new.txt' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Submit Job' }))

    await screen.findByText('job-2')

    const submitCall = fetchMock.mock.calls[5]
    expect(submitCall[0]).toContain('/api/v1/ingestion/jobs')
    expect(submitCall[1].method).toBe('POST')
    expect(submitCall[1].headers['X-Tenant-ID']).toBe('tenant-a')
    expect(submitCall[1].headers.Authorization).toBe('Bearer token-a')
    expect(submitCall[1].headers['X-Request-ID']).toContain('req-')
  })

  it('executes presigned local upload flow before local ingestion submit', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }] })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ vectorCount: 1, storageBytes: 1 }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          uploads: [
            {
              relativePath: 'docs/doc.txt',
              objectKey: 'tenant-a/uploads/docs/doc.txt',
              uploadUrl: 'https://s3.local/upload/doc',
              headers: { 'Content-Type': 'text/plain' },
            },
          ],
        }),
      })
      .mockResolvedValueOnce({ ok: true, text: async () => '' })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          jobId: 'job-local-1',
          sourceUri: 'local://docs%2Fdoc.txt',
          status: 'processing',
          submittedAt: '2026-03-09T11:24:00Z',
        }),
      })

    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByTestId('password'), { target: { value: 'sk-1234' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await screen.findByTestId('dashboard')

    fireEvent.click(screen.getByRole('radio', { name: 'File/Folder Upload' }))
    const file = new File(['hello'], 'doc.txt', { type: 'text/plain', lastModified: 100 })
    fireEvent.change(screen.getByTestId('local-folder'), {
      target: {
        files: [
          Object.assign(file, {
            webkitRelativePath: 'docs/doc.txt',
          }),
        ],
      },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Submit Job' }))

    await screen.findByText('job-local-1')

    expect(fetchMock.mock.calls[5][0]).toContain('/api/v1/uploads/presign')
      expect(fetchMock.mock.calls[6][0]).toBe('https://s3.local/upload/doc')
    expect(fetchMock.mock.calls[6][1].method).toBe('PUT')
    expect(fetchMock.mock.calls[7][0]).toContain('/api/v1/ingestion/jobs')
  })

  it('retries failed file from failed-file list', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({ ok: true, json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }] })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [
          {
            jobId: 'job-fail',
            sourceUri: 's3://tenant-a/docs/batch',
            status: 'failed',
            stage: 'classification',
            processedFiles: 1,
            totalFiles: 2,
            successfulFiles: 1,
            failedFiles: 1,
            submittedAt: '2026-03-09T11:25:00Z',
            fileStatuses: [{ path: 'docs/bad.txt', status: 'failed', policyCode: 'pii_redaction_required' }],
          },
        ],
      })
      .mockResolvedValueOnce({ ok: true, json: async () => [] })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ vectorCount: 1, storageBytes: 1 }) })
      .mockResolvedValueOnce({ ok: true, json: async () => ({}) })

    vi.stubGlobal('fetch', fetchMock)
    render(<App />)

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByTestId('password'), { target: { value: 'sk-1234' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await screen.findByTestId('failed-files-list')
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => {
      expect(fetchMock.mock.calls[5][0]).toContain('/api/v1/ingestion/jobs/job-fail/retry')
      expect(fetchMock.mock.calls[5][1].method).toBe('POST')
    })
  })
})
