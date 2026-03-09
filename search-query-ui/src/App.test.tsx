import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import App from './App'

function streamResponse(body: string): Response {
  const encoder = new TextEncoder()
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode(body))
      controller.close()
    },
  })
  return new Response(stream, { status: 200 })
}

describe('search query UI integration flow', () => {
  it('blocks workspace before login and resolves tenant before query stream', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }],
      })
      .mockResolvedValueOnce(
        streamResponse(
          'data: {"type":"token","token":"Hello "}\n\n' +
            'data: {"type":"token","token":"tenant"}\n\n' +
            'data: {"type":"citation","citation":{"id":"c1","title":"Doc 1","uri":"s3://tenant-a/docs/1.txt"}}\n\n' +
            'data: {"type":"done","trace":{"requestId":"req-1","ttftMs":120}}\n\n',
        ),
      )

    vi.stubGlobal('fetch', fetchMock)

    render(<App />)

    expect(screen.getByTestId('login-gate')).toBeInTheDocument()
    expect(screen.queryByTestId('query-workspace')).not.toBeInTheDocument()

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await screen.findByTestId('query-workspace')

    fireEvent.change(screen.getByTestId('query-text'), { target: { value: 'What is onboarding?' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => {
      expect(screen.getByTestId('answer-panel').textContent).toContain('Hello tenant')
    })
    expect(screen.getByText('Doc 1')).toBeInTheDocument()
    expect(screen.getByTestId('trace-panel').textContent).toContain('ttftMs')

    const streamCall = fetchMock.mock.calls[2]
    expect(streamCall[0]).toContain('/api/v1/search/stream')
    expect(streamCall[1].method).toBe('POST')
    expect(streamCall[1].headers['X-Tenant-ID']).toBe('tenant-a')
    expect(streamCall[1].headers.Authorization).toBe('Bearer token-a')
  })

  it('resets session to login gate when stream receives 401', async () => {
    const fetchMock = vi.fn()
    fetchMock
      .mockResolvedValueOnce({ ok: true, json: async () => ({ token: 'token-a' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => [{ tenantId: 'tenant-a', displayName: 'Tenant A' }],
      })
      .mockResolvedValueOnce({ ok: false, status: 401 })

    vi.stubGlobal('fetch', fetchMock)

    render(<App />)

    fireEvent.change(screen.getByTestId('username'), { target: { value: 'alice' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-999' } })
    fireEvent.click(screen.getByRole('button', { name: 'Login' }))

    await screen.findByTestId('query-workspace')

    fireEvent.change(screen.getByTestId('query-text'), { target: { value: 'retry me' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))

    await screen.findByText('Session expired. Please log in again.')
    expect(screen.getByTestId('login-gate')).toBeInTheDocument()
  })
})
