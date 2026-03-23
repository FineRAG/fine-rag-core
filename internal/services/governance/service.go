package governance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"enterprise-go-rag/internal/contracts"
)

// Service defines policy checks and audit boundaries used by ingestion and retrieval.
type Service interface {
	EvaluateIngestion(ctx context.Context, metadata contracts.RequestMetadata, job contracts.IngestionJob) (contracts.IngestionStatus, error)
	RecordAuditEvent(ctx context.Context, event contracts.AuditEvent) error
}

type DeterministicPolicyService struct {
	auditSink       contracts.AuditSink
	clock           func() time.Time
	residencyRegion string
}

func NewDeterministicPolicyService(auditSink contracts.AuditSink) *DeterministicPolicyService {
	return NewDeterministicPolicyServiceWithClock(auditSink, func() time.Time { return time.Now().UTC().Round(0) })
}

func NewDeterministicPolicyServiceWithRepository(repo contracts.AuditEventRepository) *DeterministicPolicyService {
	if repo == nil {
		return NewDeterministicPolicyService(nil)
	}
	return NewDeterministicPolicyService(auditRepositorySink{repo: repo})
}

func NewDeterministicPolicyServiceWithClock(auditSink contracts.AuditSink, clock func() time.Time) *DeterministicPolicyService {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC().Round(0) }
	}
	return &DeterministicPolicyService{
		auditSink:       auditSink,
		clock:           clock,
		residencyRegion: "ap-south-1",
	}
}

func (s *DeterministicPolicyService) EvaluateIngestion(ctx context.Context, metadata contracts.RequestMetadata, job contracts.IngestionJob) (contracts.IngestionStatus, error) {
	if err := metadata.Validate(); err != nil {
		return contracts.IngestionStatusRejected, contracts.WrapValidationErr("request_metadata", err)
	}
	if err := job.Validate(); err != nil {
		return contracts.IngestionStatusRejected, contracts.WrapValidationErr("ingestion_job", err)
	}
	if metadata.TenantID != job.TenantID {
		return contracts.IngestionStatusRejected, fmt.Errorf("tenant mismatch: metadata=%s job=%s", metadata.TenantID, job.TenantID)
	}

	decision := contracts.IngestionStatusApproved
	policyCode := "policy_approved"
	now := s.clock()

	if !containsRequiredResidency(job.SourceURI, s.residencyRegion) {
		decision = contracts.IngestionStatusRejected
		policyCode = "residency_lock_violation"
	} else if hasPIIMarker(job.SourceURI) {
		decision = contracts.IngestionStatusQuarantine
		policyCode = "pii_redaction_required"
	}

	auditEvent := contracts.AuditEvent{
		EventID:    deterministicEventID(metadata, job, decision, policyCode),
		TenantID:   metadata.TenantID,
		EventType:  "governance.ingestion.policy_decision",
		Resource:   job.SourceURI,
		Actor:      "governance.policy_engine",
		Outcome:    toAuditOutcome(decision),
		OccurredAt: now,
		Attributes: map[string]string{
			"request_id":  metadata.RequestID,
			"job_id":      job.JobID,
			"source_uri":  job.SourceURI,
			"policy_code": policyCode,
			"decision":    string(decision),
			"timestamp":   now.Format(time.RFC3339Nano),
		},
	}

	if err := s.RecordAuditEvent(ctx, auditEvent); err != nil {
		return contracts.IngestionStatusRejected, err
	}

	return decision, nil
}

func (s *DeterministicPolicyService) RecordAuditEvent(ctx context.Context, event contracts.AuditEvent) error {
	if err := event.Validate(); err != nil {
		return contracts.WrapValidationErr("audit_event", err)
	}
	if s.auditSink == nil {
		return nil
	}
	return s.auditSink.Write(ctx, event)
}

func containsRequiredResidency(sourceURI, requiredRegion string) bool {
	return strings.Contains(strings.ToLower(sourceURI), strings.ToLower(requiredRegion))
}

func hasPIIMarker(sourceURI string) bool {
	normalized := strings.ToLower(sourceURI)
	return strings.Contains(normalized, "pii") || strings.Contains(normalized, "ssn") || strings.Contains(normalized, "passport")
}

func deterministicEventID(metadata contracts.RequestMetadata, job contracts.IngestionJob, decision contracts.IngestionStatus, policyCode string) string {
	raw := fmt.Sprintf("%s|%s|%s|%s|%s", metadata.TenantID, metadata.RequestID, job.JobID, decision, policyCode)
	sum := sha256.Sum256([]byte(raw))
	return "evt_" + hex.EncodeToString(sum[:8])
}

func toAuditOutcome(decision contracts.IngestionStatus) contracts.AuditOutcome {
	if decision == contracts.IngestionStatusApproved {
		return contracts.AuditOutcomeAllowed
	}
	return contracts.AuditOutcomeDenied
}

type auditRepositorySink struct {
	repo contracts.AuditEventRepository
}

func (s auditRepositorySink) Write(ctx context.Context, event contracts.AuditEvent) error {
	return s.repo.Save(ctx, event)
}
