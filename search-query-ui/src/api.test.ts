import { buildTenantHeaders, getApiBaseUrl } from './api'
import type { Session } from './types'

describe('api helpers', () => {
  it('defaults base URL when search env variable is missing', () => {
    expect(getApiBaseUrl()).toBe('http://localhost:8080')
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
