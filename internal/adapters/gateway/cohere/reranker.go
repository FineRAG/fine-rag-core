package cohere

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/logging"

	"go.uber.org/zap"
)

type Config struct {
	APIKey                  string
	Model                   string
	Timeout                 time.Duration
	RetryMax                int
	CircuitFailureThreshold int
}

type CohereReranker struct {
	cfg        Config
	httpClient *http.Client

	mu        sync.Mutex
	failures  int
	lastTrace contracts.GatewayCallTrace
}

func NewCohereReranker(cfg Config) (*CohereReranker, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("cohere api key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = "rerank-v4.0-pro"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 12 * time.Second
	}
	if cfg.CircuitFailureThreshold <= 0 {
		cfg.CircuitFailureThreshold = 3
	}
	return &CohereReranker{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (r *CohereReranker) Rerank(ctx context.Context, req contracts.RerankRequest) ([]contracts.RerankCandidate, error) {
	if err := req.Validate(); err != nil {
		return nil, contracts.WrapValidationErr("cohere_rerank_request", err)
	}

	if len(req.Candidates) < 3 {
		logging.Logger.Info("cohere.rerank.skipped", 
			zap.String("requestID", req.RequestID), 
			zap.Int("candidateCount", len(req.Candidates)), 
			zap.String("reason", "below_minimum_threshold"))
		return req.TopCandidates(), nil
	}

	if r.isCircuitOpen() {
		return req.TopCandidates(), nil
	}

	started := time.Now()
	
	texts := make([]string, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		texts = append(texts, c.Text)
	}

	payload := map[string]any{
		"model":     r.cfg.Model,
		"query":     req.QueryText,
		"documents": texts,
		"top_n":     req.TopN,
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.cohere.com/v2/rerank", bytes.NewReader(rawPayload))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(r.cfg.APIKey))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		r.recordFailure()
		r.recordTrace("timeout", time.Since(started).Milliseconds(), 0)
		logging.Logger.Error("cohere.rerank.failed", zap.String("requestID", req.RequestID), zap.Error(err))
		return req.TopCandidates(), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	logging.Logger.Info("cohere.rerank.debug", 
		zap.String("requestID", req.RequestID),
		zap.String("requestPayload", string(rawPayload)),
		zap.Int("statusCode", resp.StatusCode),
		zap.String("responseBody", string(body)))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		r.recordFailure()
		r.recordTrace("error", time.Since(started).Milliseconds(), 0)
		logging.Logger.Error("cohere.rerank.error_status", 
			zap.String("requestID", req.RequestID), 
			zap.Int("statusCode", resp.StatusCode),
			zap.String("body", string(body)))
		return req.TopCandidates(), nil
	}

	r.resetFailures()

	var result struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
		Meta struct {
			BilledUnits struct {
				SearchUnits int `json:"search_units"`
			} `json:"billed_units"`
		} `json:"meta"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		logging.Logger.Error("cohere.rerank.unmarshal_failed", zap.String("requestID", req.RequestID), zap.Error(err))
		return req.TopCandidates(), nil
	}

	if len(result.Results) == 0 {
		return req.TopCandidates(), nil
	}

	out := make([]contracts.RerankCandidate, 0, len(result.Results))
	for _, res := range result.Results {
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

	r.recordTrace("ok", time.Since(started).Milliseconds(), result.Meta.BilledUnits.SearchUnits)
	return out, nil
}

func (r *CohereReranker) LastGatewayTrace() contracts.GatewayCallTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastTrace
}

func (r *CohereReranker) isCircuitOpen() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures >= r.cfg.CircuitFailureThreshold
}

func (r *CohereReranker) recordFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures++
}

func (r *CohereReranker) resetFailures() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = 0
}

func (r *CohereReranker) recordTrace(status string, latency int64, units int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastTrace = contracts.GatewayCallTrace{
		Provider:      "cohere",
		Model:         r.cfg.Model,
		Status:        status,
		LatencyMillis: latency,
		TokenTotal:    units,
	}
}
