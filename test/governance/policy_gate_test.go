package governance_test

import (
	"context"
	"testing"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/governance"
)

type memoryAuditSink struct {
	events []contracts.AuditEvent
}

func (m *memoryAuditSink) Write(_ context.Context, event contracts.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func fixedNow() time.Time {
	return time.Date(2026, 3, 9, 10, 11, 12, 0, time.UTC)
}

func newServiceWithClock(sink contracts.AuditSink) *governance.DeterministicPolicyService {
	return governance.NewDeterministicPolicyServiceWithClock(sink, fixedNow)
}

func baselineMetadata() contracts.RequestMetadata {
	return contracts.RequestMetadata{
		TenantID:  "tenant-a",
		RequestID: "req-1",
		SourceIP:  "127.0.0.1",
		UserAgent: "go-test",
	}
}

func baselineJob() contracts.IngestionJob {
	return contracts.IngestionJob{
		JobID:     "job-1",
		TenantID:  "tenant-a",
		SourceURI: "s3://tenant-a-ap-south-1/docs/file.txt",
		Checksum:  "abc123",
		CreatedAt: fixedNow(),
	}
}

func TestPolicyGateDeterministicApproved(t *testing.T) {
	sink := &memoryAuditSink{}
	svc := newServiceWithClock(sink)

	metadata := baselineMetadata()
	job := baselineJob()

	first, err := svc.EvaluateIngestion(t.Context(), metadata, job)
	if err != nil {
		t.Fatalf("first evaluation failed: %v", err)
	}
	second, err := svc.EvaluateIngestion(t.Context(), metadata, job)
	if err != nil {
		t.Fatalf("second evaluation failed: %v", err)
	}

	if first != contracts.IngestionStatusApproved || second != contracts.IngestionStatusApproved {
		t.Fatalf("expected deterministic approved decisions, got first=%s second=%s", first, second)
	}
	if len(sink.events) != 2 {
		t.Fatalf("expected two audit events, got %d", len(sink.events))
	}
	if sink.events[0].EventID != sink.events[1].EventID {
		t.Fatalf("expected deterministic event id, got %s and %s", sink.events[0].EventID, sink.events[1].EventID)
	}
}

func TestGovernancePIIRedactionQuarantine(t *testing.T) {
	sink := &memoryAuditSink{}
	svc := newServiceWithClock(sink)

	job := baselineJob()
	job.SourceURI = "s3://tenant-a-ap-south-1/docs/pii-passport.csv"

	decision, err := svc.EvaluateIngestion(t.Context(), baselineMetadata(), job)
	if err != nil {
		t.Fatalf("evaluation failed: %v", err)
	}
	if decision != contracts.IngestionStatusQuarantine {
		t.Fatalf("expected quarantine, got %s", decision)
	}
	if got := sink.events[0].Attributes["policy_code"]; got != "pii_redaction_required" {
		t.Fatalf("expected pii policy code, got %q", got)
	}
}

func TestResidencyRejectedWhenSourceRegionInvalid(t *testing.T) {
	sink := &memoryAuditSink{}
	svc := newServiceWithClock(sink)

	job := baselineJob()
	job.SourceURI = "s3://tenant-a-us-east-1/docs/file.txt"

	decision, err := svc.EvaluateIngestion(t.Context(), baselineMetadata(), job)
	if err != nil {
		t.Fatalf("evaluation failed: %v", err)
	}
	if decision != contracts.IngestionStatusRejected {
		t.Fatalf("expected rejected decision, got %s", decision)
	}
	if got := sink.events[0].Attributes["policy_code"]; got != "residency_lock_violation" {
		t.Fatalf("expected residency violation code, got %q", got)
	}
}

func TestGovernanceAuditContainsDecisionRationale(t *testing.T) {
	sink := &memoryAuditSink{}
	svc := newServiceWithClock(sink)

	decision, err := svc.EvaluateIngestion(t.Context(), baselineMetadata(), baselineJob())
	if err != nil {
		t.Fatalf("evaluation failed: %v", err)
	}
	if decision != contracts.IngestionStatusApproved {
		t.Fatalf("expected approved decision, got %s", decision)
	}

	if len(sink.events) != 1 {
		t.Fatalf("expected one audit event, got %d", len(sink.events))
	}
	event := sink.events[0]
	if event.TenantID != "tenant-a" {
		t.Fatalf("expected tenant in audit event, got %s", event.TenantID)
	}
	if event.Attributes["job_id"] != "job-1" || event.Attributes["request_id"] != "req-1" {
		t.Fatalf("missing audit rationale identifiers: %+v", event.Attributes)
	}
	if event.Attributes["policy_code"] == "" || event.Attributes["decision"] == "" || event.Attributes["timestamp"] == "" {
		t.Fatalf("missing policy rationale fields: %+v", event.Attributes)
	}
}
