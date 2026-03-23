package ingestion_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/ingestion"
)

type memoryMetadataRepo struct {
	records []contracts.IngestionMetadata
}

func (m *memoryMetadataRepo) Save(_ context.Context, metadata contracts.IngestionMetadata) error {
	m.records = append(m.records, metadata)
	return nil
}

func (m *memoryMetadataRepo) GetByChecksum(_ context.Context, tenantID contracts.TenantID, checksum string) (contracts.IngestionMetadata, error) {
	for _, record := range m.records {
		if record.TenantID == tenantID && record.ChecksumSHA256 == checksum {
			return record, nil
		}
	}
	return contracts.IngestionMetadata{}, errors.New("metadata not found")
}

func TestMetadataRecorderPersistsProfileMetadata(t *testing.T) {
	repo := &memoryMetadataRepo{}
	recorder := ingestion.NewMetadataRecorder(repo)

	profile := contracts.IngestionProfile{
		Metadata: contracts.IngestionMetadata{
			TenantID:       "tenant-a",
			ChecksumSHA256: "abc123",
			SourceURI:      "s3://tenant-a-ap-south-1/docs/file.txt",
			LifecycleClass: "standard",
			CapturedAt:     time.Date(2026, 3, 9, 14, 0, 0, 0, time.UTC),
		},
		PayloadBytes:   10,
		ContentType:    "text/plain",
		LineCount:      1,
		WordCount:      2,
		Classification: contracts.PayloadClassValid,
	}

	if err := recorder.Record(t.Context(), profile); err != nil {
		t.Fatalf("record metadata: %v", err)
	}

	if len(repo.records) != 1 {
		t.Fatalf("expected one metadata record, got %d", len(repo.records))
	}
	if repo.records[0].TenantID != "tenant-a" || repo.records[0].ChecksumSHA256 != "abc123" {
		t.Fatalf("unexpected metadata persisted: %+v", repo.records[0])
	}
}
