package ingestion

import (
	"context"
	"errors"

	"enterprise-go-rag/internal/contracts"
)

// MetadataRecorder persists ingestion metadata for deterministic replay and audit joins.
type MetadataRecorder struct {
	repo contracts.IngestionMetadataRepository
}

func NewMetadataRecorder(repo contracts.IngestionMetadataRepository) *MetadataRecorder {
	return &MetadataRecorder{repo: repo}
}

func (r *MetadataRecorder) Record(ctx context.Context, profile contracts.IngestionProfile) error {
	if err := profile.Validate(); err != nil {
		return contracts.WrapValidationErr("ingestion_profile", err)
	}
	if r.repo == nil {
		return errors.New("metadata repository is required")
	}
	return r.repo.Save(ctx, profile.Metadata)
}
