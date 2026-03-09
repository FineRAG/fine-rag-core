import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import App from './App'

const jobsPayload = [
  {
    jobId: 'job-1',
    sourceUri: 's3://tenant-a-ap-south-1/docs/a.txt',
    status: 'queued',
    submittedAt: '2026-03-09T11:22:33Z',
  },
]

const keysPayload = [
  {
    keyId: 'key-1',
    label: 'bootstrap',
    createdAt: '2026-03-09T11:22:33Z',
  },
]

describe('ingestion dashboard integration flow', () => {
  it('boots session, submits ingestion, and refreshes status list', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => jobsPayload })
      .mockResolvedValueOnce({ ok: true, json: async () => keysPayload })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          jobId: 'job-2',
          sourceUri: 's3://tenant-a-ap-south-1/docs/new.txt',
          status: 'queued',
          submittedAt: '2026-03-09T11:23:00Z',
        }),
      })
      .mockResolvedValueOnce({ ok: true, json: async () => jobsPayload })
      .mockResolvedValueOnce({ ok: true, json: async () => keysPayload })

    vi.stubGlobal('fetch', fetchMock)

    render(<App />)

    fireEvent.change(screen.getByTestId('tenant-id'), { target: { value: 'tenant-a' } })
    fireEvent.change(screen.getByTestId('request-id'), { target: { value: 'req-1' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start Session' }))

    await screen.findByText('bootstrap')

    fireEvent.change(screen.getByTestId('source-uri'), {
      target: { value: 's3://tenant-a-ap-south-1/docs/new.txt' },
    })
    fireEvent.change(screen.getByTestId('checksum'), { target: { value: 'abc' } })
    fireEvent.click(screen.getByRole('button', { name: 'Submit Job' }))

    await screen.findByText('job-2')

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(5)
    })

    const submitCall = fetchMock.mock.calls[2]
    expect(submitCall[0]).toContain('/api/v1/ingestion/jobs')
    expect(submitCall[1].method).toBe('POST')
    expect(submitCall[1].headers['X-Tenant-ID']).toBe('tenant-a')
  })
})
