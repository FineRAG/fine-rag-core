package governance

import (
	"context"

	"enterprise-go-rag/internal/contracts"
)

// Service defines policy checks and audit boundaries used by ingestion and retrieval.
type Service interface {
	EvaluateIngestion(ctx context.Context, metadata contracts.RequestMetadata, job contracts.IngestionJob) (contracts.IngestionStatus, error)
	RecordAuditEvent(ctx context.Context, event contracts.AuditEvent) error
}
