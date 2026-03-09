export type AuthStatus = 'logged_out' | 'authenticating' | 'authenticated' | 'expired' | 'error'

export type AuthSession = {
  username: string
  token: string
  requestId: string
}

export type TenantSession = AuthSession & {
  tenantId: string
}

export type TenantRecord = {
  tenantId: string
  displayName: string
}

export type SearchTrace = {
  requestId: string
  ttftMs?: number
  retrievalMs?: number
  rerankApplied?: boolean
}

export type SearchCitation = {
  id: string
  title: string
  uri: string
}

export type StreamEvent =
  | { type: 'token'; token: string }
  | { type: 'citation'; citation: SearchCitation }
  | { type: 'trace'; trace: SearchTrace }
  | { type: 'done'; citations?: SearchCitation[]; trace?: SearchTrace }
