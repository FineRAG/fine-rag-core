import {
  __resetDemoStoreForTests,
  buildTenantHeaders,
  getDemoDefaults,
  serializeIngestionPayloadFromLocal,
  serializeIngestionPayloadFromUri,
} from './api'
import type { TenantSession } from './types'

describe('api helpers', () => {
  beforeEach(() => {
    __resetDemoStoreForTests()
  })

  it('serializes URI ingestion payload with trimmed fields', () => {
    const payload = serializeIngestionPayloadFromUri(' s3://tenant-a/docs/file.pdf ', ' abc123 ')

    expect(payload).toEqual({
      sourceMode: 'uri',
      sourceUri: 's3://tenant-a/docs/file.pdf',
      checksum: 'abc123',
    })
  })

  it('serializes local ingestion payload with deterministic metadata', () => {
    const payload = serializeIngestionPayloadFromLocal(
      [
        {
          name: 'doc.txt',
          size: 123,
          type: 'text/plain',
          lastModified: 42,
          relativePath: 'folder/doc.txt',
        },
      ],
      ' c1 ',
    )

    expect(payload).toEqual({
      sourceMode: 'local',
      sourceUri: 'local://folder%2Fdoc.txt',
      checksum: 'c1',
      localItems: [
        {
          name: 'doc.txt',
          size: 123,
          type: 'text/plain',
          lastModified: 42,
          relativePath: 'folder/doc.txt',
        },
      ],
    })
  })

  it('adds tenant context headers for all scoped requests', () => {
    const session: TenantSession = {
      username: 'admin',
      token: 'secret-key',
      mode: 'demo',
      tenantId: 'tenant-a',
      requestId: 'req-101',
    }

    expect(buildTenantHeaders(session)).toEqual({
      Authorization: 'Bearer secret-key',
      'Content-Type': 'application/json',
      'X-Request-ID': 'req-101',
      'X-Tenant-ID': 'tenant-a',
    })
  })

  it('exposes demo defaults for local mode bootstrap', () => {
    expect(getDemoDefaults()).toEqual({
      username: 'admin',
      apiKey: 'sk-1234',
      requestId: 'req-demo-1',
      tenantId: 'tenant-1234',
    })
  })
})
