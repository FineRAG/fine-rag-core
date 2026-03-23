package portkey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/internal/adapters/vector"
	"enterprise-go-rag/internal/contracts"
)

type TokenUsage struct {
	Input  int
	Output int
	Total  int
}

type RerankRequest struct {
	TenantID   contracts.TenantID
	RequestID  string
	QueryText  string
	Candidates []contracts.RerankCandidate
	TopN       int
	Metadata   map[string]string
}

type Client interface {
	Rerank(ctx context.Context, req RerankRequest) ([]contracts.RerankCandidate, TokenUsage, error)
}

type Config struct {
	BaseURL                 string
	APIKey                  string
	Timeout                 time.Duration
	RetryMax                int
	CircuitFailureThreshold int
	FallbackMode            string
	DirectAllowlist         []string
	Model                   string
	ProviderKey             string
	NowMillis               func() int64
}

type RerankerAdapter struct {
	client Client
	cfg    Config

	mu           sync.Mutex
	failures     int
	lastTrace    contracts.GatewayCallTrace
	lastMetadata map[string]string
}

func NewRerankerAdapter(cfg Config) (*RerankerAdapter, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("FINE_RAG_PORTKEY_BASE_URL is required when FINE_RAG_GATEWAY_PROVIDER=portkey")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("FINE_RAG_PORTKEY_API_KEY is required when FINE_RAG_GATEWAY_PROVIDER=portkey")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 12 * time.Second
	}
	if cfg.RetryMax < 0 {
		cfg.RetryMax = 0
	}
	if cfg.CircuitFailureThreshold <= 0 {
		cfg.CircuitFailureThreshold = 3
	}
	if cfg.FallbackMode == "" {
		cfg.FallbackMode = "retrieval_only"
	}
	if cfg.NowMillis == nil {
		cfg.NowMillis = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	if err := validateFallbackMode(cfg.FallbackMode); err != nil {
		return nil, err
	}
	return &RerankerAdapter{
		cfg:    cfg,
		client: &portkeyRerankClient{
			apiKey:      cfg.APIKey,
			baseURL:     cfg.BaseURL,
			model:       cfg.Model,
			providerKey: cfg.ProviderKey,
			httpClient:  &http.Client{Timeout: cfg.Timeout},
		},
	}, nil
}

func NewStubReranker() contracts.Reranker {
	return stubReranker{}
}

func (r *RerankerAdapter) WithClient(client Client) *RerankerAdapter {
	if client == nil {
		return r
	}
	r.client = client
	return r
}

func (r *RerankerAdapter) Rerank(ctx context.Context, req contracts.RerankRequest) ([]contracts.RerankCandidate, error) {
	if err := req.Validate(); err != nil {
		return nil, contracts.WrapValidationErr("rerank_request", err)
	}
	if r.isCircuitOpen() {
		reason := "gateway_circuit_open"
		r.recordTrace(contracts.GatewayCallTrace{Provider: "portkey", Status: "circuit_open", FallbackReason: reason})
		return r.applyFallback(req, reason, errors.New(reason))
	}

	lastErr := error(nil)
	for attempt := 0; attempt <= r.cfg.RetryMax; attempt++ {
		start := time.Now()
		timeoutCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
		gwReq := toGatewayRequest(req)
		r.mu.Lock()
		r.lastMetadata = map[string]string{"tenant_id": gwReq.Metadata["tenant_id"], "request_id": gwReq.Metadata["request_id"]}
		r.mu.Unlock()
		candidates, usage, err := r.client.Rerank(timeoutCtx, gwReq)
		cancel()
		if err == nil {
			r.resetFailures()
			r.recordTrace(contracts.GatewayCallTrace{
				Provider:      "portkey",
				Model:         "portkey-rerank",
				LatencyMillis: time.Since(start).Milliseconds(),
				TokenInput:    usage.Input,
				TokenOutput:   usage.Output,
				TokenTotal:    usage.Total,
				Status:        "ok",
			})
			return candidates, nil
		}
		lastErr = vector.NormalizeProviderError("portkey", "rerank", err)
	}

	r.registerFailure()
	reason := "gateway_timeout"
	if !errors.Is(lastErr, context.DeadlineExceeded) {
		reason = "gateway_error"
	}
	r.recordTrace(contracts.GatewayCallTrace{Provider: "portkey", Status: "fallback", FallbackReason: reason})
	return r.applyFallback(req, reason, lastErr)
}

func (r *RerankerAdapter) LastGatewayTrace() contracts.GatewayCallTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastTrace
}

func (r *RerankerAdapter) LastOutboundMetadata() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.lastMetadata))
	for k, v := range r.lastMetadata {
		out[k] = v
	}
	return out
}

func (r *RerankerAdapter) RedactedAPIKey() string {
	if strings.TrimSpace(r.cfg.APIKey) == "" {
		return ""
	}
	return "REDACTED"
}

func (r *RerankerAdapter) isCircuitOpen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures >= r.cfg.CircuitFailureThreshold
}

func (r *RerankerAdapter) registerFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures++
}

func (r *RerankerAdapter) resetFailures() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = 0
}

func (r *RerankerAdapter) recordTrace(trace contracts.GatewayCallTrace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastTrace = trace
}

func (r *RerankerAdapter) applyFallback(req contracts.RerankRequest, reason string, err error) ([]contracts.RerankCandidate, error) {
	switch r.cfg.FallbackMode {
	case "retrieval_only":
		return req.TopCandidates(), nil
	case "direct_allowlist":
		if isAllowlisted(req.TenantID, r.cfg.DirectAllowlist) {
			return req.TopCandidates(), nil
		}
		return nil, fmt.Errorf("fallback blocked for tenant: %w", err)
	default:
		return nil, err
	}
}

func toGatewayRequest(req contracts.RerankRequest) RerankRequest {
	metadata := map[string]string{
		"tenant_id":  string(req.TenantID),
		"request_id": req.RequestID,
	}
	return RerankRequest{
		TenantID:   req.TenantID,
		RequestID:  req.RequestID,
		QueryText:  req.QueryText,
		Candidates: req.Candidates,
		TopN:       req.TopN,
		Metadata:   metadata,
	}
}

func isAllowlisted(tenantID contracts.TenantID, allowlist []string) bool {
	for _, item := range allowlist {
		if item == string(tenantID) {
			return true
		}
	}
	return false
}

func validateFallbackMode(mode string) error {
	switch mode {
	case "fail_closed", "direct_allowlist", "retrieval_only":
		return nil
	default:
		return fmt.Errorf("unsupported FINE_RAG_GATEWAY_FALLBACK_MODE %q", mode)
	}
}

func BuildAuditAttributes(req contracts.RerankRequest, trace contracts.GatewayCallTrace) map[string]string {
	return map[string]string{
		"tenant_id":       string(req.TenantID),
		"request_id":      req.RequestID,
		"provider":        trace.Provider,
		"status":          trace.Status,
		"fallback_reason": trace.FallbackReason,
	}
}

type stubReranker struct{}

func (stubReranker) Rerank(_ context.Context, req contracts.RerankRequest) ([]contracts.RerankCandidate, error) {
	return req.TopCandidates(), nil
}

type failingClient struct{}

func (failingClient) Rerank(_ context.Context, _ RerankRequest) ([]contracts.RerankCandidate, TokenUsage, error) {
	return nil, TokenUsage{}, errors.New("portkey unavailable")
}

type portkeyRerankClient struct {
	apiKey      string
	baseURL     string
	model       string
	providerKey string
	httpClient  *http.Client
}

func (c *portkeyRerankClient) Rerank(ctx context.Context, req RerankRequest) ([]contracts.RerankCandidate, TokenUsage, error) {
	texts := make([]string, 0, len(req.Candidates))
	for _, cand := range req.Candidates {
		texts = append(texts, cand.Text)
	}

	modelName := c.model
	if modelName == "" {
		modelName = "portkey-rerank"
	}

	body := map[string]any{
		"model":     modelName,
		"query":     req.QueryText,
		"documents": texts,
	}
	if req.TopN > 0 {
		body["top_n"] = req.TopN
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, TokenUsage{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/v1/rerank", bytes.NewReader(raw))
	if err != nil {
		return nil, TokenUsage{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-portkey-api-key", strings.TrimSpace(c.apiKey))
	if c.providerKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.providerKey))
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, TokenUsage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rawErr, _ := io.ReadAll(resp.Body)
		return nil, TokenUsage{}, fmt.Errorf("portkey rerank failed: status=%d body=%s", resp.StatusCode, string(rawErr))
	}

	var payload struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, TokenUsage{}, err
	}

	if len(payload.Results) == 0 {
		return nil, TokenUsage{}, errors.New("empty rerank response")
	}

	out := make([]contracts.RerankCandidate, 0, len(payload.Results))
	for _, res := range payload.Results {
		if res.Index < 0 || res.Index >= len(req.Candidates) {
			continue
		}
		orig := req.Candidates[res.Index]
		out = append(out, contracts.RerankCandidate{
			DocumentID: orig.DocumentID,
			Text:       orig.Text,
			Score:      res.RelevanceScore,
		})
	}

	return out, TokenUsage{Total: payload.Usage.TotalTokens}, nil
}
