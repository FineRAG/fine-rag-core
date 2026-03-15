package governance_test

import (
	"context"
	"testing"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/governance"
)

type memoryAuditRepository struct {
	events []contracts.AuditEvent
}

func (m *memoryAuditRepository) Save(_ context.Context, event contracts.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *memoryAuditRepository) ListByTenant(_ context.Context, tenantID contracts.TenantID, limit int) ([]contracts.AuditEvent, error) {
	result := make([]contracts.AuditEvent, 0, limit)
	for _, event := range m.events {
		if event.TenantID == tenantID {
			result = append(result, event)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func TestGovernanceRepositoryWiringPersistsAuditEvent(t *testing.T) {
	repo := &memoryAuditRepository{}
	svc := governance.NewDeterministicPolicyServiceWithRepository(repo)

	metadata := contracts.RequestMetadata{
		TenantID:  "tenant-a",
		RequestID: "req-1",
		SourceIP:  "127.0.0.1",
		UserAgent: "go-test",
	}
	job := contracts.IngestionJob{
		JobID:     "job-1",
		TenantID:  "tenant-a",
		SourceURI: "s3://tenant-a-ap-south-1/docs/file.txt",
		Checksum:  "abc123",
		CreatedAt: time.Date(2026, 3, 9, 10, 11, 12, 0, time.UTC),
	}

	decision, err := svc.EvaluateIngestion(t.Context(), metadata, job)
	if err != nil {
		t.Fatalf("evaluate ingestion: %v", err)
	}
	if decision != contracts.IngestionStatusApproved {
		t.Fatalf("expected approved, got %s", decision)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected one persisted audit event, got %d", len(repo.events))
	}
	if repo.events[0].Attributes["job_id"] != "job-1" {
		t.Fatalf("expected job id linkage in persisted audit event, got %+v", repo.events[0].Attributes)
	}
}
