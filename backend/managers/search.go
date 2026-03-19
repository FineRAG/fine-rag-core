package managers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	util "enterprise-go-rag/backend/util/apiutil"
	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/logging"
	"enterprise-go-rag/internal/services/retrieval"

	"go.uber.org/zap"
)

type LLMAnswerGenerator interface {
	GenerateAnswer(ctx context.Context, query string, contextText string) (string, error)
}

type SearchManager struct {
	Retrieval      *retrieval.DeterministicRetrievalService
	LLM            LLMAnswerGenerator
	EmbeddingModel string
	OpenRouterModel string
}

func (m *SearchManager) HandleSearch(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var req struct {
		QueryText string `json:"queryText"`
		TopK      int    `json:"topK"`
	}
	if err := util.DecodeJSON(r.Body, &req); err != nil {
		logging.Logger.Error("decode search request failed", zap.Error(err), zap.String("tenantID", tenantID))
		util.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	req.QueryText = strings.TrimSpace(req.QueryText)
	if req.QueryText == "" {
		util.WriteError(w, http.StatusBadRequest, "query_required", "queryText is required")
		return
	}
	requestID := util.RequestIDFromContext(r.Context())
	if requestID == "" {
		requestID = "req-" + util.RandomString(8)
	}
	query := contracts.RetrievalQuery{TenantID: contracts.TenantID(tenantID), RequestID: requestID, Text: req.QueryText, TopK: req.TopK}
	meta := contracts.RequestMetadata{TenantID: contracts.TenantID(tenantID), RequestID: query.RequestID, SourceIP: r.RemoteAddr, UserAgent: r.UserAgent()}
	
	logging.Logger.Info("search.step.received", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("queryText", req.QueryText), zap.Int("topK", req.TopK))
	logging.Logger.Info("search.step.embedding", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("embeddingModel", m.EmbeddingModel))
	logging.Logger.Info("search.step.vector_lookup.start", zap.String("tenantID", tenantID), zap.String("requestID", requestID))
	
	result, err := m.Retrieval.Search(r.Context(), meta, query)
	if err != nil {
		logging.Logger.Error("search.step.failed", zap.Error(err), zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
		util.WriteError(w, http.StatusBadRequest, "search_failed", err.Error())
		return
	}
	logging.Logger.Info("search.step.vector_lookup.done",
		zap.String("tenantID", tenantID),
		zap.String("requestID", requestID),
		zap.Int("docCount", len(result.Documents)),
		zap.String("vectorProvider", result.Trace.VectorProvider),
		zap.String("vectorStatus", result.Trace.VectorStatus),
		zap.Int64("vectorLatencyMs", result.Trace.VectorLatencyMillis),
		zap.Bool("rerankApplied", result.Trace.RerankApplied),
		zap.String("fallbackReason", result.Trace.FallbackReason),
	)
	
	rankedDocs := util.RankDocumentsByQueryIntent(req.QueryText, result.Documents)
	topVectors := m.buildTopFilteredVectors(req.QueryText, rankedDocs, 5)
	
	answer, genErr := m.generateFinalAnswerStrict(r.Context(), "search", tenantID, requestID, req.QueryText, rankedDocs)
	if genErr != nil {
		logging.Logger.Error("search.step.llm.failed", zap.Error(genErr), zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
		util.WriteError(w, http.StatusServiceUnavailable, "llm_unavailable", "final answer generation is unavailable")
		return
	}
	
	response := struct {
		contracts.RetrievalResult
		AnswerText string           `json:"answerText"`
		TopVectors []map[string]any `json:"topVectors"`
	}{
		RetrievalResult: result,
		AnswerText:      answer,
		TopVectors:      topVectors,
	}
	logging.Logger.Info("search.step.response", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
	util.WriteJSON(w, http.StatusOK, response)
}

func (m *SearchManager) HandleSearchStream(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var req struct {
		QueryText string `json:"queryText"`
	}
	if err := util.DecodeJSON(r.Body, &req); err != nil {
		logging.Logger.Error("decode search stream request failed", zap.Error(err), zap.String("tenantID", tenantID))
		util.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	requestID := util.RequestIDFromContext(r.Context())
	if requestID == "" {
		requestID = "req-" + util.RandomString(8)
	}
	req.QueryText = strings.TrimSpace(req.QueryText)
	if req.QueryText == "" {
		util.WriteError(w, http.StatusBadRequest, "query_required", "queryText is required")
		return
	}
	query := contracts.RetrievalQuery{TenantID: contracts.TenantID(tenantID), RequestID: requestID, Text: req.QueryText, TopK: 5}
	meta := contracts.RequestMetadata{TenantID: contracts.TenantID(tenantID), RequestID: query.RequestID, SourceIP: r.RemoteAddr, UserAgent: r.UserAgent()}
	
	logging.Logger.Info("search_stream.step.received", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("queryText", req.QueryText), zap.Int("topK", query.TopK))
	logging.Logger.Info("search_stream.step.embedding", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("embeddingModel", m.EmbeddingModel))
	logging.Logger.Info("search_stream.step.vector_lookup.start", zap.String("tenantID", tenantID), zap.String("requestID", requestID))
	
	result, err := m.Retrieval.Search(r.Context(), meta, query)
	if err != nil {
		logging.Logger.Error("search_stream.step.failed", zap.Error(err), zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
		util.WriteError(w, http.StatusBadRequest, "search_failed", err.Error())
		return
	}
	logging.Logger.Info("search_stream.step.vector_lookup.done",
		zap.String("tenantID", tenantID),
		zap.String("requestID", requestID),
		zap.Int("docCount", len(result.Documents)),
		zap.String("vectorProvider", result.Trace.VectorProvider),
		zap.String("vectorStatus", result.Trace.VectorStatus),
		zap.Int64("vectorLatencyMs", result.Trace.VectorLatencyMillis),
		zap.Bool("rerankApplied", result.Trace.RerankApplied),
		zap.String("fallbackReason", result.Trace.FallbackReason),
	)
	
	rankedDocs := util.RankDocumentsByQueryIntent(req.QueryText, result.Documents)
	topVectors := m.buildTopFilteredVectors(req.QueryText, rankedDocs, 5)
	
	answer, genErr := m.generateFinalAnswerStrict(r.Context(), "search_stream", tenantID, requestID, req.QueryText, rankedDocs)
	if genErr != nil {
		logging.Logger.Error("search_stream.step.llm.failed", zap.Error(genErr), zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
		util.WriteError(w, http.StatusServiceUnavailable, "llm_unavailable", "final answer generation is unavailable")
		return
	}
	
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		logging.Logger.Error("sse stream unsupported", zap.String("tenantID", tenantID), zap.String("requestID", requestID))
		util.WriteError(w, http.StatusInternalServerError, "sse_unsupported", "stream unsupported")
		return
	}
	emit := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}
	emit(map[string]any{"type": "top_vectors", "topVectors": topVectors})
	
	for _, tok := range strings.Fields(answer) {
		emit(map[string]any{"type": "token", "token": tok + " "})
	}

	citations := make([]map[string]string, 0)
	for _, d := range rankedDocs {
		c := map[string]string{"id": d.DocumentID, "title": "Source", "uri": d.SourceURI}
		citations = append(citations, c)
		emit(map[string]any{"type": "citation", "citation": c})
	}
	trace := map[string]any{"requestId": result.RequestID, "retrievalMs": result.Trace.DurationMillis, "rerankApplied": result.Trace.RerankApplied}
	emit(map[string]any{"type": "trace", "trace": trace})
	emit(map[string]any{"type": "done", "citations": citations, "trace": trace, "topVectors": topVectors})
	logging.Logger.Info("search_stream.step.done", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Int("tokenCount", len(strings.Fields(answer))), zap.Int("citationCount", len(citations)), zap.Duration("elapsed", time.Since(started)))
}

func (m *SearchManager) generateFinalAnswerStrict(ctx context.Context, flow, tenantID, requestID, queryText string, rankedDocs []contracts.RetrievalDocument) (string, error) {
	if m.LLM == nil {
		return "", fmt.Errorf("llm client is not configured")
	}
	logging.Logger.Info(flow+".debug.original_query", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("original-Query", strings.TrimSpace(queryText)))
	logging.Logger.Info(flow+".debug.docs_from_vdb", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Any("Docs from VDB", m.docsForDebugLog(rankedDocs)))

	parts := make([]string, 0, 4)
	for i, d := range rankedDocs {
		if i >= 4 {
			break
		}
		if cleaned := util.BuildContextPart(queryText, d.Content); cleaned != "" {
			parts = append(parts, cleaned)
		}
	}

	contextText := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if contextText == "" {
		contextText = "No retrieved documents found for this query."
	}
	logging.Logger.Info(flow+".debug.llm_input", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("input for oss-120B", util.TruncateForDebugLog(contextText, 2000)))

	logging.Logger.Info(flow+".step.llm.start", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Int("contextParts", len(parts)), zap.String("llmModel", m.OpenRouterModel))
	generated, err := m.LLM.GenerateAnswer(ctx, queryText, contextText)
	if err != nil {
		return "", err
	}
	answer := strings.TrimSpace(generated)
	logging.Logger.Info(flow+".debug.llm_raw_output", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("raw output from oss-120B", util.TruncateForDebugLog(answer, 2000)))
	if answer == "" {
		return "", fmt.Errorf("empty llm answer")
	}
	
	// Sanitization heuristics
	collapsed := strings.TrimSpace(strings.Join(strings.Fields(answer), " "))
	if util.HasReadableSignal(collapsed) {
		answer = util.TruncateForDebugLog(collapsed, 1200)
	} else {
		return "", fmt.Errorf("unreadable llm answer")
	}
	
	logging.Logger.Info(flow+".debug.llm_output", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("final output from oss-120B", util.TruncateForDebugLog(answer, 2000)))
	logging.Logger.Info(flow+".step.llm.done", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Int("answerChars", len(answer)))
	return answer, nil
}

func (m *SearchManager) buildTopFilteredVectors(query string, docs []contracts.RetrievalDocument, limit int) []map[string]any {
	if len(docs) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}
	ranked := util.RankDocumentsByQueryIntent(query, docs)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	vectors := make([]map[string]any, 0, len(ranked))
	for i, d := range ranked {
		snippet := util.BuildAnswerSnippet(query, d.Content)
		vectors = append(vectors, map[string]any{
			"rank":       i + 1,
			"documentId": d.DocumentID,
			"sourceUri":  d.SourceURI,
			"score":      d.Score,
			"snippet":    snippet,
			"metadata":   d.Metadata,
		})
	}
	return vectors
}

func (m *SearchManager) docsForDebugLog(docs []contracts.RetrievalDocument) []map[string]any {
	if len(docs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(docs))
	for i, d := range docs {
		out = append(out, map[string]any{
			"rank":       i + 1,
			"documentId": d.DocumentID,
			"sourceUri":  d.SourceURI,
			"score":      d.Score,
			"snippet":    util.TruncateForDebugLog(strings.TrimSpace(d.Content), 240),
			"metadata":   d.Metadata,
		})
	}
	return out
}
