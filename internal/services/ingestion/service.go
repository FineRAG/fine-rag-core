package ingestion

import (
	"context"

	"enterprise-go-rag/internal/contracts"
)

// Service defines ingestion orchestration boundaries for E1 foundations.
type Service interface {
	CreateJob(ctx context.Context, metadata contracts.RequestMetadata, job contracts.IngestionJob) (contracts.IngestionJob, error)
	GetJob(ctx context.Context, metadata contracts.RequestMetadata, jobID string) (contracts.IngestionJob, error)
}
