export type Session = {
  tenantId: string
  requestId: string
  apiKey: string
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
