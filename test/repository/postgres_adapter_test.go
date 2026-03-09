package repository_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/repository"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func scopedCtx(t *testing.T, tenantID contracts.TenantID) context.Context {
	t.Helper()
	ctx, err := contracts.WithTenantContext(t.Context(), contracts.TenantContext{TenantID: tenantID, RequestID: "req-1"})
	if err != nil {
		t.Fatalf("seed tenant context: %v", err)
	}
	return ctx
}

func TestPostgresTenantRegistryRepositoryUpsertUsesScopedTenantWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	repo := repository.NewPostgresTenantRegistryRepository(db, repository.PostgresConfig{})
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	tenant := contracts.TenantRecord{
		TenantID:    "tenant-a",
		DisplayName: "Tenant A",
		PlanTier:    "enterprise",
		Active:      true,
		UpdatedAt:   now,
	}

	query := `INSERT INTO tenant_registry (tenant_id, display_name, plan_tier, active, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id) DO UPDATE SET
	display_name = EXCLUDED.display_name,
	plan_tier = EXCLUDED.plan_tier,
	active = EXCLUDED.active,
	updated_at = EXCLUDED.updated_at`
	mock.ExpectExec(regexp.QuoteMeta(query)).
		WithArgs("tenant-a", "Tenant A", "enterprise", true, now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := scopedCtx(t, "tenant-a")
	if err := repo.Upsert(ctx, tenant); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPostgresIngestionMetadataRepositoryRejectsCrossTenantScope(t *testing.T) {
	repo := repository.NewPostgresIngestionMetadataRepository(nil, repository.PostgresConfig{})
	metadata := contracts.IngestionMetadata{
		TenantID:       "tenant-b",
		ChecksumSHA256: "sum",
		SourceURI:      "s3://tenant-b-ap-south-1/docs/file.txt",
		LifecycleClass: "standard",
		CapturedAt:     time.Now().UTC().Round(0),
	}
	ctx, err := contracts.WithTenantContext(t.Context(), contracts.TenantContext{TenantID: "tenant-a", RequestID: "req-1"})
	if err != nil {
		t.Fatalf("tenant context: %v", err)
	}

	err = repo.Save(ctx, metadata)
	if err == nil {
		t.Fatal("expected cross-tenant save to fail")
	}
	if !errors.Is(err, repository.ErrUnscopedRepositoryAccess) {
		t.Fatalf("expected ErrUnscopedRepositoryAccess, got: %v", err)
	}
}

func TestPostgresMetadataAndAuditRoundTripQueries(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	metadataRepo := repository.NewPostgresIngestionMetadataRepository(db, repository.PostgresConfig{})
	auditRepo := repository.NewPostgresAuditEventRepository(db, repository.PostgresConfig{})

	captured := time.Date(2026, 3, 9, 12, 1, 2, 0, time.UTC)
	metadata := contracts.IngestionMetadata{
		TenantID:       "tenant-a",
		ChecksumSHA256: "abc123",
		SourceURI:      "s3://tenant-a-ap-south-1/docs/file.txt",
		LifecycleClass: "standard",
		CapturedAt:     captured,
	}

	insertMetadata := `INSERT INTO ingestion_metadata (tenant_id, checksum_sha256, source_uri, lifecycle_class, captured_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (tenant_id, checksum_sha256) DO UPDATE SET
	source_uri = EXCLUDED.source_uri,
	lifecycle_class = EXCLUDED.lifecycle_class,
	captured_at = EXCLUDED.captured_at`
	mock.ExpectExec(regexp.QuoteMeta(insertMetadata)).
		WithArgs("tenant-a", "abc123", metadata.SourceURI, "standard", captured).
		WillReturnResult(sqlmock.NewResult(1, 1))

	selectMetadata := `SELECT tenant_id, checksum_sha256, source_uri, lifecycle_class, captured_at
FROM ingestion_metadata
WHERE tenant_id = $1 AND checksum_sha256 = $2`
	metadataRows := sqlmock.NewRows([]string{"tenant_id", "checksum_sha256", "source_uri", "lifecycle_class", "captured_at"}).
		AddRow("tenant-a", "abc123", metadata.SourceURI, "standard", captured)
	mock.ExpectQuery(regexp.QuoteMeta(selectMetadata)).WithArgs("tenant-a", "abc123").WillReturnRows(metadataRows)

	auditEvent := contracts.AuditEvent{
		EventID:    "evt-1",
		TenantID:   "tenant-a",
		EventType:  "governance.ingestion.policy_decision",
		Resource:   metadata.SourceURI,
		Actor:      "governance.policy_engine",
		Outcome:    contracts.AuditOutcomeAllowed,
		OccurredAt: captured,
		Attributes: map[string]string{"policy_code": "policy_approved", "request_id": "req-1"},
	}
	insertAudit := `INSERT INTO audit_events (event_id, tenant_id, event_type, resource, actor, outcome, occurred_at, attributes_json)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (event_id) DO NOTHING`
	mock.ExpectExec(regexp.QuoteMeta(insertAudit)).
		WithArgs("evt-1", "tenant-a", auditEvent.EventType, metadata.SourceURI, "governance.policy_engine", "allowed", captured, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	listAudit := `SELECT event_id, tenant_id, event_type, resource, actor, outcome, occurred_at, attributes_json
FROM audit_events
WHERE tenant_id = $1
ORDER BY occurred_at DESC
LIMIT $2`
	auditRows := sqlmock.NewRows([]string{"event_id", "tenant_id", "event_type", "resource", "actor", "outcome", "occurred_at", "attributes_json"}).
		AddRow("evt-1", "tenant-a", auditEvent.EventType, metadata.SourceURI, "governance.policy_engine", "allowed", captured, `{"policy_code":"policy_approved","request_id":"req-1"}`)
	mock.ExpectQuery(regexp.QuoteMeta(listAudit)).WithArgs("tenant-a", 2).WillReturnRows(auditRows)

	ctx, err := contracts.WithTenantContext(t.Context(), contracts.TenantContext{TenantID: "tenant-a", RequestID: "req-1"})
	if err != nil {
		t.Fatalf("tenant context: %v", err)
	}

	if err := metadataRepo.Save(ctx, metadata); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
	loadedMetadata, err := metadataRepo.GetByChecksum(ctx, "tenant-a", "abc123")
	if err != nil {
		t.Fatalf("load metadata: %v", err)
	}
	if loadedMetadata.SourceURI != metadata.SourceURI || loadedMetadata.TenantID != "tenant-a" {
		t.Fatalf("unexpected loaded metadata: %+v", loadedMetadata)
	}

	if err := auditRepo.Save(ctx, auditEvent); err != nil {
		t.Fatalf("save audit event: %v", err)
	}
	loadedAudit, err := auditRepo.ListByTenant(ctx, "tenant-a", 2)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(loadedAudit) != 1 {
		t.Fatalf("expected one audit event, got %d", len(loadedAudit))
	}
	if loadedAudit[0].Attributes["policy_code"] != "policy_approved" {
		t.Fatalf("expected policy code in loaded audit event, got %+v", loadedAudit[0].Attributes)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPostgresAuditSinkDelegatesToRepository(t *testing.T) {
	sink := repository.NewPostgresAuditSink(stubAuditRepo{})
	event := contracts.AuditEvent{
		EventID:    "evt-2",
		TenantID:   "tenant-a",
		EventType:  "auth.apikey.validate",
		Outcome:    contracts.AuditOutcomeAllowed,
		OccurredAt: time.Now().UTC().Round(0),
	}
	if err := sink.Write(t.Context(), event); err != nil {
		t.Fatalf("audit sink write: %v", err)
	}
}

type stubAuditRepo struct{}

func (stubAuditRepo) Save(_ context.Context, _ contracts.AuditEvent) error { return nil }

func (stubAuditRepo) ListByTenant(_ context.Context, _ contracts.TenantID, _ int) ([]contracts.AuditEvent, error) {
	return nil, nil
}
