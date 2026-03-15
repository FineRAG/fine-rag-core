CREATE TABLE IF NOT EXISTS tenant_registry (
  tenant_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  plan_tier TEXT NOT NULL,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at TIMESTAMPTZ NOT NULL
);
