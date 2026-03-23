package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"enterprise-go-rag/internal/contracts"
)

var ErrRecordNotFound = errors.New("record not found")

type PostgresConfig struct {
	QueryTimeout time.Duration
}

func (c PostgresConfig) withDefaults() PostgresConfig {
	if c.QueryTimeout <= 0 {
		c.QueryTimeout = 3 * time.Second
	}
	return c
}

type PostgresTenantRegistryRepository struct {
	db  *sql.DB
	cfg PostgresConfig
}

func NewPostgresTenantRegistryRepository(db *sql.DB, cfg PostgresConfig) *PostgresTenantRegistryRepository {
	return &PostgresTenantRegistryRepository{db: db, cfg: cfg.withDefaults()}
}

func (r *PostgresTenantRegistryRepository) Upsert(ctx context.Context, tenant contracts.TenantRecord) error {
	if err := tenant.Validate(); err != nil {
		return contracts.WrapValidationErr("tenant_record", err)
	}
	if err := GuardWriteScope(ctx, tenant.TenantID); err != nil {
		return err
	}
	if r.db == nil {
		return errors.New("postgres db is required")
	}

	timedCtx, cancel := context.WithTimeout(ctx, r.cfg.QueryTimeout)
	defer cancel()

	_, err := r.db.ExecContext(
		timedCtx,
		`INSERT INTO tenant_registry (tenant_id, display_name, plan_tier, active, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id) DO UPDATE SET
	display_name = EXCLUDED.display_name,
	plan_tier = EXCLUDED.plan_tier,
	active = EXCLUDED.active,
	updated_at = EXCLUDED.updated_at`,
		string(tenant.TenantID),
		tenant.DisplayName,
		tenant.PlanTier,
		tenant.Active,
		tenant.UpdatedAt,
	)
	return err
}

func (r *PostgresTenantRegistryRepository) GetByTenantID(ctx context.Context, tenantID contracts.TenantID) (contracts.TenantRecord, error) {
	if err := tenantID.Validate(); err != nil {
		return contracts.TenantRecord{}, contracts.WrapValidationErr("tenant_id", err)
	}
	if err := GuardReadScope(ctx, tenantID); err != nil {
		return contracts.TenantRecord{}, err
	}
	if r.db == nil {
		return contracts.TenantRecord{}, errors.New("postgres db is required")
	}

	timedCtx, cancel := context.WithTimeout(ctx, r.cfg.QueryTimeout)
	defer cancel()

	var record contracts.TenantRecord
	var tenantIDString string
	err := r.db.QueryRowContext(
		timedCtx,
		`SELECT tenant_id, display_name, plan_tier, active, updated_at
FROM tenant_registry
WHERE tenant_id = $1`,
		string(tenantID),
	).Scan(&tenantIDString, &record.DisplayName, &record.PlanTier, &record.Active, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.TenantRecord{}, ErrRecordNotFound
	}
	if err != nil {
		return contracts.TenantRecord{}, err
	}
	record.TenantID = contracts.TenantID(tenantIDString)
	return record, nil
}

type PostgresIngestionMetadataRepository struct {
	db  *sql.DB
	cfg PostgresConfig
}

func NewPostgresIngestionMetadataRepository(db *sql.DB, cfg PostgresConfig) *PostgresIngestionMetadataRepository {
	return &PostgresIngestionMetadataRepository{db: db, cfg: cfg.withDefaults()}
}

func (r *PostgresIngestionMetadataRepository) Save(ctx context.Context, metadata contracts.IngestionMetadata) error {
	if err := metadata.Validate(); err != nil {
		return contracts.WrapValidationErr("ingestion_metadata", err)
	}
	if err := GuardWriteScope(ctx, metadata.TenantID); err != nil {
		return err
	}
	if r.db == nil {
		return errors.New("postgres db is required")
	}

	timedCtx, cancel := context.WithTimeout(ctx, r.cfg.QueryTimeout)
	defer cancel()

	_, err := r.db.ExecContext(
		timedCtx,
		`INSERT INTO ingestion_metadata (tenant_id, checksum_sha256, source_uri, lifecycle_class, captured_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id, checksum_sha256) DO UPDATE SET
	source_uri = EXCLUDED.source_uri,
	lifecycle_class = EXCLUDED.lifecycle_class,
	captured_at = EXCLUDED.captured_at`,
		string(metadata.TenantID),
		metadata.ChecksumSHA256,
		metadata.SourceURI,
		metadata.LifecycleClass,
		metadata.CapturedAt,
	)
	return err
}

func (r *PostgresIngestionMetadataRepository) GetByChecksum(ctx context.Context, tenantID contracts.TenantID, checksumSHA256 string) (contracts.IngestionMetadata, error) {
	if err := tenantID.Validate(); err != nil {
		return contracts.IngestionMetadata{}, contracts.WrapValidationErr("tenant_id", err)
	}
	if checksumSHA256 == "" {
		return contracts.IngestionMetadata{}, errors.New("checksum_sha256 is required")
	}
	if err := GuardReadScope(ctx, tenantID); err != nil {
		return contracts.IngestionMetadata{}, err
	}
	if r.db == nil {
		return contracts.IngestionMetadata{}, errors.New("postgres db is required")
	}

	timedCtx, cancel := context.WithTimeout(ctx, r.cfg.QueryTimeout)
	defer cancel()

	var metadata contracts.IngestionMetadata
	var tenantIDString string
	err := r.db.QueryRowContext(
		timedCtx,
		`SELECT tenant_id, checksum_sha256, source_uri, lifecycle_class, captured_at
FROM ingestion_metadata
WHERE tenant_id = $1 AND checksum_sha256 = $2`,
		string(tenantID),
		checksumSHA256,
	).Scan(&tenantIDString, &metadata.ChecksumSHA256, &metadata.SourceURI, &metadata.LifecycleClass, &metadata.CapturedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.IngestionMetadata{}, ErrRecordNotFound
	}
	if err != nil {
		return contracts.IngestionMetadata{}, err
	}
	metadata.TenantID = contracts.TenantID(tenantIDString)
	return metadata, nil
}

type PostgresAuditEventRepository struct {
	db  *sql.DB
	cfg PostgresConfig
}

func NewPostgresAuditEventRepository(db *sql.DB, cfg PostgresConfig) *PostgresAuditEventRepository {
	return &PostgresAuditEventRepository{db: db, cfg: cfg.withDefaults()}
}

func (r *PostgresAuditEventRepository) Save(ctx context.Context, event contracts.AuditEvent) error {
	if err := event.Validate(); err != nil {
		return contracts.WrapValidationErr("audit_event", err)
	}
	if err := GuardWriteScope(ctx, event.TenantID); err != nil {
		return err
	}
	if r.db == nil {
		return errors.New("postgres db is required")
	}

	attrs, err := json.Marshal(event.Attributes)
	if err != nil {
		return fmt.Errorf("marshal audit attributes: %w", err)
	}

	timedCtx, cancel := context.WithTimeout(ctx, r.cfg.QueryTimeout)
	defer cancel()

	_, err = r.db.ExecContext(
		timedCtx,
		`INSERT INTO audit_events (event_id, tenant_id, event_type, resource, actor, outcome, occurred_at, attributes_json)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (event_id) DO NOTHING`,
		event.EventID,
		string(event.TenantID),
		event.EventType,
		event.Resource,
		event.Actor,
		string(event.Outcome),
		event.OccurredAt,
		string(attrs),
	)
	return err
}

func (r *PostgresAuditEventRepository) ListByTenant(ctx context.Context, tenantID contracts.TenantID, limit int) ([]contracts.AuditEvent, error) {
	if err := tenantID.Validate(); err != nil {
		return nil, contracts.WrapValidationErr("tenant_id", err)
	}
	if err := GuardReadScope(ctx, tenantID); err != nil {
		return nil, err
	}
	if r.db == nil {
		return nil, errors.New("postgres db is required")
	}
	if limit <= 0 {
		limit = 50
	}

	timedCtx, cancel := context.WithTimeout(ctx, r.cfg.QueryTimeout)
	defer cancel()

	rows, err := r.db.QueryContext(
		timedCtx,
		`SELECT event_id, tenant_id, event_type, resource, actor, outcome, occurred_at, attributes_json
FROM audit_events
WHERE tenant_id = $1
ORDER BY occurred_at DESC
LIMIT $2`,
		string(tenantID),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]contracts.AuditEvent, 0, limit)
	for rows.Next() {
		var event contracts.AuditEvent
		var tenantIDString string
		var outcome string
		var attrsJSON string
		if err := rows.Scan(&event.EventID, &tenantIDString, &event.EventType, &event.Resource, &event.Actor, &outcome, &event.OccurredAt, &attrsJSON); err != nil {
			return nil, err
		}
		event.TenantID = contracts.TenantID(tenantIDString)
		event.Outcome = contracts.AuditOutcome(outcome)
		if attrsJSON != "" {
			event.Attributes = map[string]string{}
			if err := json.Unmarshal([]byte(attrsJSON), &event.Attributes); err != nil {
				return nil, fmt.Errorf("unmarshal audit attributes: %w", err)
			}
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

type PostgresAuditSink struct {
	repo contracts.AuditEventRepository
}

func NewPostgresAuditSink(repo contracts.AuditEventRepository) *PostgresAuditSink {
	return &PostgresAuditSink{repo: repo}
}

func (s *PostgresAuditSink) Write(ctx context.Context, event contracts.AuditEvent) error {
	if s.repo == nil {
		return nil
	}
	return s.repo.Save(ctx, event)
}
