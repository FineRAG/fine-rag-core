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
  it('boots session, streams answer, and renders citations/trace', async () => {
    const fetchMock = vi.fn()
    fetchMock.mockResolvedValueOnce(
      streamResponse(
        'data: {"type":"token","token":"Hello "}\n\n' +
          'data: {"type":"token","token":"tenant"}\n\n' +
          'data: {"type":"citation","citation":{"id":"c1","title":"Doc 1","uri":"s3://tenant-a/docs/1.txt"}}\n\n' +
          'data: {"type":"done","trace":{"requestId":"req-1","ttftMs":120}}\n\n',
      ),
    )

    vi.stubGlobal('fetch', fetchMock)

    render(<App />)

    fireEvent.change(screen.getByTestId('tenant-id'), { target: { value: 'tenant-a' } })
    fireEvent.change(screen.getByTestId('request-id'), { target: { value: 'req-1' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start Session' }))

    fireEvent.change(screen.getByTestId('query-text'), { target: { value: 'What is onboarding?' } })
    fireEvent.click(screen.getByRole('button', { name: 'Stream Answer' }))

    await waitFor(() => {
      expect(screen.getByTestId('answer-panel').textContent).toContain('Hello tenant')
    })
    expect(screen.getByText('Doc 1')).toBeInTheDocument()
    expect(screen.getByTestId('trace-panel').textContent).toContain('ttftMs')

    const streamCall = fetchMock.mock.calls[0]
    expect(streamCall[0]).toContain('/api/v1/search/stream')
    expect(streamCall[1].method).toBe('POST')
    expect(streamCall[1].headers['X-Tenant-ID']).toBe('tenant-a')
  })

  it('shows retry state when stream fails', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce({ ok: false, status: 503 })
    vi.stubGlobal('fetch', fetchMock)

    render(<App />)

    fireEvent.change(screen.getByTestId('tenant-id'), { target: { value: 'tenant-a' } })
    fireEvent.change(screen.getByTestId('request-id'), { target: { value: 'req-2' } })
    fireEvent.change(screen.getByTestId('api-key'), { target: { value: 'k-999' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start Session' }))

    fireEvent.change(screen.getByTestId('query-text'), { target: { value: 'retry me' } })
    fireEvent.click(screen.getByRole('button', { name: 'Stream Answer' }))

    await screen.findByText('Streaming interrupted. Retry is available.')
  })
})
