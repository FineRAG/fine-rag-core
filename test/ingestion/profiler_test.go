package ingestion_test

import (
	"reflect"
	"testing"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/ingestion"
)

func TestIngestionProfileDeterministicForIdenticalInput(t *testing.T) {
	p := ingestion.DeterministicProfiler{}
	in := ingestion.ProfileInput{
		TenantID:       "tenant-a",
		SourceURI:      "s3://bucket/object.txt",
		LifecycleClass: "standard",
		ContentType:    "text/plain",
		Payload:        []byte("hello world\nline2"),
	}

	a, err := p.Profile(t.Context(), in)
	if err != nil {
		t.Fatalf("profile a: %v", err)
	}
	b, err := p.Profile(t.Context(), in)
	if err != nil {
		t.Fatalf("profile b: %v", err)
	}

	if a.Metadata.ChecksumSHA256 != b.Metadata.ChecksumSHA256 || a.LineCount != b.LineCount || a.WordCount != b.WordCount || a.Classification != b.Classification {
		t.Fatalf("non-deterministic profile output: a=%+v b=%+v", a, b)
	}
}

func TestMetadataSchemaIncludesRequiredGovernanceFields(t *testing.T) {
	p := ingestion.DeterministicProfiler{}
	result, err := p.Profile(t.Context(), ingestion.ProfileInput{
		TenantID:       "tenant-a",
		SourceURI:      "s3://bucket/object.txt",
		LifecycleClass: "hot",
		Payload:        []byte("payload"),
	})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := result.Metadata.Validate(); err != nil {
		t.Fatalf("metadata schema invalid: %v", err)
	}

	policyInput := result.ToPolicyEngineInput()
	if policyInput.TenantID != "tenant-a" || policyInput.SourceURI == "" || policyInput.LifecycleClass == "" {
		t.Fatalf("policy input missing required fields: %+v", policyInput)
	}
}

func TestProfilerClassifiesInvalidPayloadWithExplicitReason(t *testing.T) {
	p := ingestion.DeterministicProfiler{}
	result, err := p.Profile(t.Context(), ingestion.ProfileInput{TenantID: "tenant-a", SourceURI: "s3://bucket/object.txt"})
	if err != nil {
		t.Fatalf("profile invalid payload: %v", err)
	}
	if result.Classification != contracts.PayloadClassInvalidPayload {
		t.Fatalf("expected invalid payload classification, got %s", result.Classification)
	}
	if result.ErrorReason == "" {
		t.Fatal("expected explicit error reason for invalid payload")
	}
}

func TestProfilerOutputsPolicyCompatibleContract(t *testing.T) {
	p := ingestion.DeterministicProfiler{}
	result, err := p.Profile(t.Context(), ingestion.ProfileInput{
		TenantID:       "tenant-z",
		SourceURI:      "s3://bucket/object.txt",
		LifecycleClass: "archive",
		Payload:        []byte("payload text"),
	})
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	got := result.ToPolicyEngineInput()
	want := contracts.PolicyEngineInput{
		TenantID:       "tenant-z",
		ChecksumSHA256: result.Metadata.ChecksumSHA256,
		SourceURI:      "s3://bucket/object.txt",
		LifecycleClass: "archive",
		Classification: contracts.PayloadClassValid,
		ErrorReason:    "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("policy contract mismatch: got %+v want %+v", got, want)
	}
}
