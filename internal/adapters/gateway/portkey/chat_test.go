package portkey

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"enterprise-go-rag/internal/contracts"
)

func TestExtractChatChoiceTextFromStringContent(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"content":"rewritten query text"}}]}`)
	got, err := extractChatChoiceText(raw)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if got != "rewritten query text" {
		t.Fatalf("unexpected text: %q", got)
	}
}

func TestExtractChatChoiceTextFromStructuredContentArray(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"content":[{"type":"text","text":"candidate"},{"type":"text","text":"education cgpa details"}]}}]}`)
	got, err := extractChatChoiceText(raw)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if got != "candidate education cgpa details" {
		t.Fatalf("unexpected text: %q", got)
	}
}

func TestExtractChatChoiceTextFallsBackToChoiceText(t *testing.T) {
	raw := []byte(`{"choices":[{"text":"fallback completion text"}]}`)
	got, err := extractChatChoiceText(raw)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if got != "fallback completion text" {
		t.Fatalf("unexpected text: %q", got)
	}
}

func TestGenerateAnswerIncludesUserFieldFromContext(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := NewChatClient(ChatConfig{
		BaseURL:       server.URL,
		PortkeyAPIKey: "pk-test",
		OpenRouterKey: "or-test",
		Model:         "openai/gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("new chat client: %v", err)
	}

	tenantCtx, err := contracts.WithTenantContext(context.Background(), contracts.TenantContext{TenantID: contracts.TenantID("tenant-sample"), RequestID: "req-1"})
	if err != nil {
		t.Fatalf("tenant context: %v", err)
	}
	ctx := contracts.WithActorID(tenantCtx, "sample-user")
	if _, err := client.GenerateAnswer(ctx, "hello", "context"); err != nil {
		t.Fatalf("generate answer: %v", err)
	}

	if got, _ := captured["user"].(string); got != "sample-user_tenant-sample" {
		t.Fatalf("expected user field sample-user_tenant-sample, got %v", captured["user"])
	}
}
