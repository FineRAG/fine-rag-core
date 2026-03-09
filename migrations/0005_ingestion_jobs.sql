CREATE TABLE IF NOT EXISTS ingestion_jobs (
  job_id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  source_uri TEXT NOT NULL,
  checksum TEXT NOT NULL,
  status TEXT NOT NULL,
  stage TEXT,
  processed_files INT NOT NULL DEFAULT 0,
  total_files INT NOT NULL DEFAULT 0,
  successful_files INT NOT NULL DEFAULT 0,
  failed_files INT NOT NULL DEFAULT 0,
  policy_code TEXT,
  policy_reason TEXT,
  source_mode TEXT NOT NULL DEFAULT 'uri',
  payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  chunk_count INT NOT NULL DEFAULT 0,
  payload_bytes BIGINT NOT NULL DEFAULT 0,
  submitted_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_tenant_submitted
  ON ingestion_jobs (tenant_id, submitted_at DESC);
