package portkey

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"enterprise-go-rag/internal/contracts"
)

func TestEmbedIncludesUserFieldFromContext(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer server.Close()

	client, err := NewEmbeddingClient(EmbeddingConfig{
		BaseURL:       server.URL,
		PortkeyAPIKey: "pk-test",
		OpenRouterKey: "or-test",
		Model:         "openai/text-embedding-3-small",
	})
	if err != nil {
		t.Fatalf("new embedding client: %v", err)
	}

	tenantCtx, err := contracts.WithTenantContext(context.Background(), contracts.TenantContext{TenantID: contracts.TenantID("tenant-sample"), RequestID: "req-1"})
	if err != nil {
		t.Fatalf("tenant context: %v", err)
	}
	ctx := contracts.WithActorID(tenantCtx, "sample-user")
	if _, err := client.Embed(ctx, contracts.TenantID("tenant-sample"), []string{"hello"}); err != nil {
		t.Fatalf("embed: %v", err)
	}

	if got, _ := captured["user"].(string); got != "sample-user_tenant-sample" {
		t.Fatalf("expected user field sample-user_tenant-sample, got %v", captured["user"])
	}
}
