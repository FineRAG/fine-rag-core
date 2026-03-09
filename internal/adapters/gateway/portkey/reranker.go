package portkey

import (
	"context"
	"errors"
	"fmt"
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
		cfg.Timeout = 250 * time.Millisecond
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
	return &RerankerAdapter{cfg: cfg, client: failingClient{}}, nil
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
