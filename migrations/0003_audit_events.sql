CREATE TABLE IF NOT EXISTS audit_events (
  event_id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  resource TEXT,
  actor TEXT,
  outcome TEXT NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL,
  attributes_json JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_occurred
  ON audit_events (tenant_id, occurred_at DESC);
