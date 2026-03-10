package portkey

import "testing"

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
