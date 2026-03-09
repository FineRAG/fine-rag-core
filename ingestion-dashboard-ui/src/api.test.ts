import { buildTenantHeaders, serializeIngestionPayload } from './api'
import type { Session } from './types'

describe('api helpers', () => {
  it('serializes ingestion payload with trimmed fields', () => {
    const payload = serializeIngestionPayload(' s3://tenant-a/docs/file.pdf ', ' abc123 ')

    expect(payload).toEqual({
      sourceUri: 's3://tenant-a/docs/file.pdf',
      checksum: 'abc123',
    })
  })

  it('adds tenant context headers for all scoped requests', () => {
    const session: Session = {
      tenantId: 'tenant-a',
      requestId: 'req-101',
      apiKey: 'secret-key',
    }

    expect(buildTenantHeaders(session)).toEqual({
      Authorization: 'Bearer secret-key',
      'Content-Type': 'application/json',
      'X-Request-ID': 'req-101',
      'X-Tenant-ID': 'tenant-a',
    })
  })
})
