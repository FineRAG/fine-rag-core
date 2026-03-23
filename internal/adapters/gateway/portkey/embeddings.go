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
	"sync"
	"time"

	"enterprise-go-rag/internal/contracts"
)

type EmbeddingConfig struct {
	BaseURL        string
	PortkeyAPIKey  string
	OpenRouterKey  string
	Model          string
	FallbackModel  string
	Timeout        time.Duration
	ProviderHeader string
	MinInterval    time.Duration
	RetryMax       int
}

type EmbeddingClient struct {
	cfg        EmbeddingConfig
	httpClient *http.Client

	rateMu      sync.Mutex
	lastRequest time.Time
}

func NewEmbeddingClient(cfg EmbeddingConfig) (*EmbeddingClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("portkey base url is required")
	}
	if strings.TrimSpace(cfg.PortkeyAPIKey) == "" {
		return nil, errors.New("portkey api key is required")
	}
	if requiresOpenRouterAuth(cfg.Model) && strings.TrimSpace(cfg.OpenRouterKey) == "" {
		return nil, errors.New("openrouter api key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = "@openrouter-key/nvidia/llama-nemotron-embed-vl-1b-v2:free"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 12 * time.Second
	}
	if cfg.MinInterval <= 0 {
		cfg.MinInterval = 900 * time.Millisecond
	}
	if cfg.RetryMax < 0 {
		cfg.RetryMax = 0
	}
	if strings.TrimSpace(cfg.ProviderHeader) == "" && requiresOpenRouterAuth(cfg.Model) {
		cfg.ProviderHeader = "openrouter"
	}
	if strings.TrimSpace(cfg.FallbackModel) != "" && requiresOpenRouterAuth(cfg.FallbackModel) && strings.TrimSpace(cfg.OpenRouterKey) == "" {
		return nil, errors.New("openrouter api key is required for embedding fallback model")
	}
	return &EmbeddingClient{cfg: cfg, httpClient: &http.Client{Timeout: cfg.Timeout}}, nil
}

func (c *EmbeddingClient) Embed(ctx context.Context, tenantID contracts.TenantID, chunks []string) ([][]float32, error) {
	if err := tenantID.Validate(); err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, errors.New("at least one chunk is required")
	}

	input := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		trimmed := strings.TrimSpace(chunk)
		if trimmed != "" {
			input = append(input, trimmed)
		}
	}
	if len(input) == 0 {
		return nil, errors.New("at least one non-empty chunk is required")
	}

	vectors, err := c.embedWithModel(ctx, input, c.cfg.Model)
	if err != nil {
		fallback := strings.TrimSpace(c.cfg.FallbackModel)
		if fallback == "" || fallback == strings.TrimSpace(c.cfg.Model) || !isEmbeddingUnsupportedError(err) {
			return nil, err
		}
		vectors, err = c.embedWithModel(ctx, input, fallback)
		if err != nil {
			return nil, fmt.Errorf("primary embedding failed and fallback failed: primary_model=%s fallback_model=%s err=%w", c.cfg.Model, fallback, err)
		}
	}

	if len(vectors) != len(input) {
		return nil, fmt.Errorf("embedding count mismatch: got=%d want=%d", len(vectors), len(input))
	}

	return vectors, nil
}

func (c *EmbeddingClient) embedWithModel(ctx context.Context, input []string, model string) ([][]float32, error) {
	body := map[string]any{
		"model":           model,
		"input":           input,
		"encoding_format": "float",
	}
	if providerUserID, ok := contracts.ProviderUserIDFromContext(ctx); ok {
		body["user"] = providerUserID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	var lastErr error
	for attempt := 0; attempt <= c.cfg.RetryMax; attempt++ {
		if err := c.waitTurn(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/v1/embeddings", bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-portkey-api-key", strings.TrimSpace(c.cfg.PortkeyAPIKey))
		if requiresOpenRouterAuth(model) {
			key := strings.TrimSpace(c.cfg.OpenRouterKey)
			if key != "" {
				req.Header.Set("Authorization", "Bearer "+key)
			}
		}
		if provider := providerHeaderForModel(model, c.cfg.ProviderHeader); provider != "" {
			req.Header.Set("x-portkey-provider", provider)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryDelay := c.retryDelay(resp.Header.Get("Retry-After"), attempt)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("portkey embedding request rate-limited: status=429")
			if attempt < c.cfg.RetryMax {
				if err := sleepContext(ctx, retryDelay); err != nil {
					return nil, err
				}
				continue
			}
			break
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			rawErr, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if msg := strings.TrimSpace(string(rawErr)); msg != "" {
				return nil, fmt.Errorf("portkey embedding request failed: model=%s status=%d body=%s", model, resp.StatusCode, truncateErrorBody(msg))
			}
			return nil, fmt.Errorf("portkey embedding request failed: model=%s status=%d", model, resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, lastErr
	}
	if len(payload.Data) != len(input) {
		return nil, fmt.Errorf("embedding count mismatch: got=%d want=%d", len(payload.Data), len(input))
	}

	vectors := make([][]float32, 0, len(payload.Data))
	for _, item := range payload.Data {
		if len(item.Embedding) == 0 {
			return nil, errors.New("empty embedding in response")
		}
		vectors = append(vectors, item.Embedding)
	}
	return vectors, nil
}

func requiresOpenRouterAuth(model string) bool {
	return !strings.HasPrefix(strings.TrimSpace(model), "@")
}

func truncateErrorBody(body string) string {
	const maxLen = 240
	if len(body) <= maxLen {
		return body
	}
	return body[:maxLen] + "..."
}

func isEmbeddingUnsupportedError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "embed is not supported") || strings.Contains(msg, "embedding is not supported")
}

func providerHeaderForModel(model, configured string) string {
	if requiresOpenRouterAuth(model) {
		if strings.TrimSpace(configured) != "" {
			return strings.TrimSpace(configured)
		}
		return "openrouter"
	}
	return ""
}

func (c *EmbeddingClient) waitTurn(ctx context.Context) error {
	c.rateMu.Lock()
	now := time.Now()
	next := c.lastRequest.Add(c.cfg.MinInterval)
	if next.After(now) {
		delay := next.Sub(now)
		c.rateMu.Unlock()
		if err := sleepContext(ctx, delay); err != nil {
			return err
		}
		c.rateMu.Lock()
	}
	c.lastRequest = time.Now()
	c.rateMu.Unlock()
	return nil
}

func (c *EmbeddingClient) retryDelay(retryAfterHeader string, attempt int) time.Duration {
	if parsed := strings.TrimSpace(retryAfterHeader); parsed != "" {
		if secs, err := strconv.Atoi(parsed); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
		if when, err := http.ParseTime(parsed); err == nil {
			d := time.Until(when)
			if d > 0 {
				return d
			}
		}
	}
	base := 900 * time.Millisecond
	if attempt > 0 {
		base = base * time.Duration(1<<attempt)
	}
	maxDelay := 8 * time.Second
	if base > maxDelay {
		return maxDelay
	}
	return base
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
