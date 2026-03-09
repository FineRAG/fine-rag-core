package ingestion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"enterprise-go-rag/internal/contracts"
)

// Service defines ingestion orchestration boundaries for E1 foundations.
type Service interface {
	CreateJob(ctx context.Context, metadata contracts.RequestMetadata, job contracts.IngestionJob) (contracts.IngestionJob, error)
	GetJob(ctx context.Context, metadata contracts.RequestMetadata, jobID string) (contracts.IngestionJob, error)
}

type ProfileInput struct {
	TenantID       contracts.TenantID
	SourceURI      string
	LifecycleClass string
	ContentType    string
	Payload        []byte
}

type Profiler interface {
	Profile(ctx context.Context, in ProfileInput) (contracts.IngestionProfile, error)
}

type DeterministicProfiler struct{}

func (DeterministicProfiler) Profile(_ context.Context, in ProfileInput) (contracts.IngestionProfile, error) {
	if err := in.TenantID.Validate(); err != nil {
		return invalidProfile(in, contracts.PayloadClassInvalidTenant, "tenant_id_missing"), nil
	}
	if strings.TrimSpace(in.SourceURI) == "" {
		return invalidProfile(in, contracts.PayloadClassInvalidSource, "source_uri_missing"), nil
	}
	if len(in.Payload) == 0 {
		return invalidProfile(in, contracts.PayloadClassInvalidPayload, "payload_empty"), nil
	}

	sum := sha256.Sum256(in.Payload)
	payloadText := string(in.Payload)

	profile := contracts.IngestionProfile{
		Metadata: contracts.IngestionMetadata{
			TenantID:       in.TenantID,
			ChecksumSHA256: hex.EncodeToString(sum[:]),
			SourceURI:      in.SourceURI,
			LifecycleClass: fallbackLifecycleClass(in.LifecycleClass),
			CapturedAt:     time.Now().UTC().Round(0),
		},
		PayloadBytes:   len(in.Payload),
		ContentType:    fallbackContentType(in.ContentType),
		LineCount:      countLines(payloadText),
		WordCount:      len(strings.Fields(payloadText)),
		Classification: contracts.PayloadClassValid,
	}

	if err := profile.Validate(); err != nil {
		return contracts.IngestionProfile{}, fmt.Errorf("profile validation failed: %w", err)
	}

	return profile, nil
}

func invalidProfile(in ProfileInput, class contracts.PayloadClassification, reason string) contracts.IngestionProfile {
	checksum := ""
	if len(in.Payload) > 0 {
		sum := sha256.Sum256(in.Payload)
		checksum = hex.EncodeToString(sum[:])
	}

	return contracts.IngestionProfile{
		Metadata: contracts.IngestionMetadata{
			TenantID:       in.TenantID,
			ChecksumSHA256: checksum,
			SourceURI:      in.SourceURI,
			LifecycleClass: fallbackLifecycleClass(in.LifecycleClass),
			CapturedAt:     time.Now().UTC().Round(0),
		},
		PayloadBytes:   len(in.Payload),
		ContentType:    fallbackContentType(in.ContentType),
		LineCount:      countLines(string(in.Payload)),
		WordCount:      len(strings.Fields(string(in.Payload))),
		Classification: class,
		ErrorReason:    reason,
	}
}

func fallbackLifecycleClass(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "standard"
	}
	return v
}

func fallbackContentType(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "application/octet-stream"
	}
	return v
}

func countLines(v string) int {
	if v == "" {
		return 0
	}
	return strings.Count(v, "\n") + 1
}
