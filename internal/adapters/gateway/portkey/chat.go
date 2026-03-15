package portkey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"enterprise-go-rag/internal/contracts"
)

type ChatConfig struct {
	BaseURL        string
	PortkeyAPIKey  string
	OpenRouterKey  string
	Model          string
	Timeout        time.Duration
	ProviderHeader string
}

type ChatClient struct {
	cfg        ChatConfig
	httpClient *http.Client
}

func NewChatClient(cfg ChatConfig) (*ChatClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("portkey base url is required")
	}
	if strings.TrimSpace(cfg.PortkeyAPIKey) == "" {
		return nil, errors.New("portkey api key is required")
	}
	if chatRequiresOpenRouterAuth(cfg.Model) && strings.TrimSpace(cfg.OpenRouterKey) == "" {
		return nil, errors.New("openrouter api key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = "@groq-key/openai/gpt-oss-120b"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 12 * time.Second
	}
	if strings.TrimSpace(cfg.ProviderHeader) == "" && chatRequiresOpenRouterAuth(cfg.Model) {
		cfg.ProviderHeader = "openrouter"
	}
	return &ChatClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (c *ChatClient) GenerateAnswer(ctx context.Context, query string, contextText string) (string, error) {
	prompt := strings.TrimSpace(contextText)
	if prompt == "" {
		return "", errors.New("empty retrieval context")
	}

	body := map[string]any{
		"model": c.cfg.Model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a RAG answer generator. Use only the provided Context. Do not use outside knowledge. If Context is insufficient, clearly say so. Return output in this exact order and format:\n1. ANSWER: <one concise factual answer>\n2. CONFIDENCE: <High|Medium|Low>\n3. GAPS: <what is missing, or 'None'>\nRules: keep facts grounded to context, do not invent values, do not reorder sections, and do not add extra sections.",
			},
			{
				"role":    "user",
				"content": "Question:\n" + strings.TrimSpace(query) + "\n\nContext:\n" + prompt + "\n\nReturn strictly in the required 3-section order.",
			},
		},
		"temperature": 0.1,
		"max_tokens":  512,
	}
	if providerUserID, ok := contracts.ProviderUserIDFromContext(ctx); ok {
		body["user"] = providerUserID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-portkey-api-key", strings.TrimSpace(c.cfg.PortkeyAPIKey))
	if chatRequiresOpenRouterAuth(c.cfg.Model) {
		key := strings.TrimSpace(c.cfg.OpenRouterKey)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if provider := strings.TrimSpace(c.cfg.ProviderHeader); provider != "" {
		req.Header.Set("x-portkey-provider", provider)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(rawBody))
		if msg == "" {
			return "", fmt.Errorf("portkey chat request failed: model=%s status=%d", c.cfg.Model, resp.StatusCode)
		}
		return "", fmt.Errorf("portkey chat request failed: model=%s status=%d body=%s", c.cfg.Model, resp.StatusCode, truncateChatBody(msg))
	}

	content, err := extractChatChoiceText(rawBody)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		return "", errors.New("empty llm response")
	}
	return strings.TrimSpace(content), nil
}

func (c *ChatClient) RewriteQuery(ctx context.Context, tenantID contracts.TenantID, originalQuery string) (contracts.StructuredQuery, error) {
	if err := tenantID.Validate(); err != nil {
		return contracts.StructuredQuery{}, err
	}
	trimmed := strings.TrimSpace(originalQuery)
	if trimmed == "" {
		return contracts.StructuredQuery{}, errors.New("query is required")
	}

	prompt := fmt.Sprintf(`You are a Search Optimization Expert. Your goal is to take a raw user query and transform it into a structured search request for a Retrieval-Augmented Generation (RAG) system.

Instructions:

Decompose/Expand: Generate 3 distinct search queries that capture different facets of the user's intent (e.g., technical, conceptual, and summary-based).

Extract Metadata: Identify constraints mentioned in the query (e.g., date, author, category, status) and map them to the provided metadata schema.

Format: Output ONLY a valid JSON object.

Metadata Schema:

category: (e.g., 'financial', 'legal', 'technical')

date_range: (e.g., 'last_24h', '2024', 'none')

priority: (1 to 5)

User Input: "%s"

JSON Output Format:
{
"expanded_queries": ["query 1", "query 2", "query 3"],
"metadata_filters": {
"category": "value",
"date_range": "value",
"priority": integer or null
}
}
`, trimmed)

	body := map[string]any{
		"model": "@google-gemini/gemini-2.5-flash",
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.0,
		"max_tokens":  512,
	}
	if providerUserID, ok := contracts.ProviderUserIDFromContext(ctx); ok {
		body["user"] = providerUserID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return contracts.StructuredQuery{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return contracts.StructuredQuery{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-portkey-api-key", strings.TrimSpace(c.cfg.PortkeyAPIKey))
	if provider := strings.TrimSpace(c.cfg.ProviderHeader); provider != "" {
		req.Header.Set("x-portkey-provider", provider)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return contracts.StructuredQuery{}, err
	}
	defer resp.Body.Close()
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return contracts.StructuredQuery{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return contracts.StructuredQuery{}, fmt.Errorf("portkey query rewrite failed: status=%d body=%s", resp.StatusCode, truncateChatBody(string(rawBody)))
	}

	responseText, err := extractChatChoiceText(rawBody)
	if err != nil {
		return contracts.StructuredQuery{}, err
	}

	// Clean JSON if LLM added markdown blocks
	responseText = strings.TrimSpace(responseText)
	if strings.HasPrefix(responseText, "```json") {
		responseText = strings.TrimPrefix(responseText, "```json")
		responseText = strings.TrimSuffix(responseText, "```")
	} else if strings.HasPrefix(responseText, "```") {
		responseText = strings.TrimPrefix(responseText, "```")
		responseText = strings.TrimSuffix(responseText, "```")
	}
	responseText = strings.TrimSpace(responseText)

	var structured contracts.StructuredQuery
	if err := json.Unmarshal([]byte(responseText), &structured); err != nil {
		return contracts.StructuredQuery{}, fmt.Errorf("failed to parse structured query JSON: %w", err)
	}

	if len(structured.ExpandedQueries) == 0 {
		structured.ExpandedQueries = []string{trimmed}
	}

	return structured, nil
}

func chatRequiresOpenRouterAuth(model string) bool {
	return !strings.HasPrefix(strings.TrimSpace(model), "@")
}

func truncateChatBody(body string) string {
	const maxLen = 240
	if len(body) <= maxLen {
		return body
	}
	return body[:maxLen] + "..."
}

func extractChatChoiceText(rawBody []byte) (string, error) {
	var payload struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return "", err
	}
	if len(payload.Choices) == 0 {
		return "", errors.New("empty query rewrite response")
	}
	choice := payload.Choices[0]
	if text := strings.TrimSpace(choice.Text); text != "" {
		return text, nil
	}
	text := strings.TrimSpace(flattenMessageContent(choice.Message.Content))
	if text == "" {
		return "", errors.New("empty query rewrite response")
	}
	return text, nil
}

func flattenMessageContent(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			segment := strings.TrimSpace(flattenMessageContent(item))
			if segment != "" {
				parts = append(parts, segment)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	case map[string]any:
		if text, ok := v["text"]; ok {
			return flattenMessageContent(text)
		}
		if contentValue, ok := v["content"]; ok {
			return flattenMessageContent(contentValue)
		}
		if val, ok := v["value"]; ok {
			return flattenMessageContent(val)
		}
		return ""
	case json.Number:
		return v.String()
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	default:
		return ""
	}
}
