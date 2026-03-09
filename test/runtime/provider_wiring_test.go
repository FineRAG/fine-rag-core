package runtime_test

import (
	"os"
	"testing"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/runtime"
)

func TestVectorProviderConfigFailFastOnUnknownProvider(t *testing.T) {
	cfg := runtime.VectorConfig{Provider: "unknown"}
	if _, _, _, err := runtime.BuildVectorAdapters(cfg); err == nil {
		t.Fatal("expected fail-fast error for unknown vector provider")
	}
}

func TestVectorProviderConfigFailFastOnMissingMilvusConfig(t *testing.T) {
	cfg := runtime.VectorConfig{Provider: "milvus", TLS: true}
	if _, _, _, err := runtime.BuildVectorAdapters(cfg); err == nil {
		t.Fatal("expected fail-fast error for missing milvus config")
	}
}

func TestVectorProviderSwitchStubAndMilvus(t *testing.T) {
	stubCfg := runtime.VectorConfig{Provider: "stub"}
	idx, searcher, provider, err := runtime.BuildVectorAdapters(stubCfg)
	if err != nil {
		t.Fatalf("build stub adapters: %v", err)
	}
	if provider != "stub" || idx == nil || searcher == nil {
		t.Fatalf("unexpected stub provider wiring: provider=%s", provider)
	}

	milvusCfg := runtime.VectorConfig{
		Provider:   "milvus",
		Endpoint:   "https://milvus.example.internal",
		Database:   "tenantdb",
		Collection: "vectors",
		TLS:        true,
	}
	idx, searcher, provider, err = runtime.BuildVectorAdapters(milvusCfg)
	if err != nil {
		t.Fatalf("build milvus adapters: %v", err)
	}
	if provider != "milvus" || idx == nil || searcher == nil {
		t.Fatalf("unexpected milvus provider wiring: provider=%s", provider)
	}
}

func TestGatewayProviderFailFastUnknownProvider(t *testing.T) {
	cfg := runtime.GatewayConfig{Provider: "unknown", FallbackMode: "retrieval_only"}
	if _, _, err := runtime.BuildGatewayReranker(cfg, nil); err == nil {
		t.Fatal("expected fail-fast error for unknown gateway provider")
	}
}

func TestGatewayProviderFailFastMissingPortkeyConfig(t *testing.T) {
	cfg := runtime.GatewayConfig{Provider: "portkey", FallbackMode: "retrieval_only", Timeout: 100 * time.Millisecond}
	if _, _, err := runtime.BuildGatewayReranker(cfg, nil); err == nil {
		t.Fatal("expected fail-fast error for missing portkey config")
	}
}

func TestGatewayStubProviderBuilds(t *testing.T) {
	cfg := runtime.GatewayConfig{Provider: "stub", FallbackMode: "retrieval_only"}
	reranker, provider, err := runtime.BuildGatewayReranker(cfg, nil)
	if err != nil {
		t.Fatalf("build stub gateway reranker: %v", err)
	}
	if provider != "stub" || reranker == nil {
		t.Fatalf("unexpected gateway wiring: provider=%s", provider)
	}

	out, err := reranker.Rerank(t.Context(), contracts.RerankRequest{
		TenantID:  "tenant-a",
		RequestID: "req-1",
		QueryText: "policy",
		TopN:      1,
		Candidates: []contracts.RerankCandidate{{
			DocumentID: "doc-1",
			Text:       "policy",
			Score:      1,
		}},
	})
	if err != nil || len(out) != 1 {
		t.Fatalf("stub reranker should return top candidates, err=%v len=%d", err, len(out))
	}
}

func TestConfigRedactionDoesNotExposeSecrets(t *testing.T) {
	vectorCfg := runtime.VectorConfig{Provider: "milvus", Password: "super-secret"}
	if vectorCfg.RedactedPassword() != "REDACTED" {
		t.Fatal("expected redacted vector password")
	}
	gatewayCfg := runtime.GatewayConfig{Provider: "portkey", PortkeyAPIKey: "pk_live_abc"}
	if gatewayCfg.RedactedAPIKey() != "REDACTED" {
		t.Fatal("expected redacted gateway api key")
	}
}

func TestLoadConfigsFromEnv(t *testing.T) {
	t.Setenv("FINE_RAG_VECTOR_PROVIDER", "stub")
	t.Setenv("FINE_RAG_GATEWAY_PROVIDER", "stub")
	v := runtime.LoadVectorConfigFromEnv(os.LookupEnv)
	g := runtime.LoadGatewayConfigFromEnv(os.LookupEnv)
	if v.Provider != "stub" || g.Provider != "stub" {
		t.Fatalf("expected providers from env, got vector=%s gateway=%s", v.Provider, g.Provider)
	}
}
