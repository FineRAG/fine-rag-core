import { buildTenantHeaders, createRequestId, getApiBaseUrl } from './api'
import type { TenantSession } from './types'

describe('api helpers', () => {
  it('defaults base URL when search env variable is missing', () => {
    expect(getApiBaseUrl()).toMatch(/^http:\/\/localhost(?::\d+)?$/)
  })

  it('adds tenant context headers for all scoped requests', () => {
    const session: TenantSession = {
      username: 'alice',
      token: 'secret-key',
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

  it('creates deterministic request id shape', () => {
    expect(createRequestId()).toContain('req-')
  })
})
