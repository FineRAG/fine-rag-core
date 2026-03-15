CREATE TABLE IF NOT EXISTS ingestion_metadata (
  tenant_id TEXT NOT NULL,
  checksum_sha256 TEXT NOT NULL,
  source_uri TEXT NOT NULL,
  lifecycle_class TEXT NOT NULL,
  captured_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, checksum_sha256)
);
