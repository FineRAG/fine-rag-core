package backend

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/logging"
	"enterprise-go-rag/internal/repository"

	pdf "github.com/ledongthuc/pdf"
	rscpdf "rsc.io/pdf"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.uber.org/zap"
)

type dbUser struct {
	ID           int64
	Username     string
	PasswordHash string
	APIKeyHash   string
	Active       bool
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		APIKey   string `json:"apiKey"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username_required", "username is required")
		return
	}
	user, err := s.fetchUserByUsername(r.Context(), req.Username)
	if err != nil || !user.Active {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}
	ok := false
	if strings.TrimSpace(req.Password) != "" {
		ok = verifySecret(req.Password, user.PasswordHash)
	}
	if !ok && strings.TrimSpace(req.APIKey) != "" {
		ok = verifySecret(req.APIKey, user.APIKeyHash)
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}
	now := time.Now().UTC()
	token, err := s.signJWT(authClaims{Sub: user.Username, UID: user.ID, Iat: now.Unix(), Exp: now.Add(s.cfg.TokenTTL).Unix()})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_failed", "failed to issue token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	uid, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "auth_required", "missing user context")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT t.tenant_id, t.display_name
FROM tenant_registry t
JOIN user_tenants ut ON ut.tenant_id = t.tenant_id
WHERE ut.user_id = $1 AND t.active = TRUE
ORDER BY t.updated_at DESC`, uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "tenant_list_failed", err.Error())
		return
	}
	defer rows.Close()
	out := make([]map[string]string, 0)
	for rows.Next() {
		var tenantID, displayName string
		if err := rows.Scan(&tenantID, &displayName); err != nil {
			writeError(w, http.StatusInternalServerError, "tenant_scan_failed", err.Error())
			return
		}
		out = append(out, map[string]string{"tenantId": tenantID, "displayName": displayName})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	uid, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "auth_required", "missing user context")
		return
	}
	var req struct {
		TenantID    string `json:"tenantId"`
		DisplayName string `json:"displayName"`
		PlanTier    string `json:"planTier"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.TenantID == "" || req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "invalid_tenant", "tenantId and displayName are required")
		return
	}
	if strings.TrimSpace(req.PlanTier) == "" {
		req.PlanTier = "starter"
	}
	repo := repository.NewPostgresTenantRegistryRepository(s.db, repository.PostgresConfig{})
	ctx, err := contracts.WithTenantContext(r.Context(), contracts.TenantContext{TenantID: contracts.TenantID(req.TenantID), RequestID: "tenant-create"})
	if err != nil {
		writeError(w, http.StatusBadRequest, "tenant_context_invalid", err.Error())
		return
	}
	if err := repo.Upsert(ctx, contracts.TenantRecord{TenantID: contracts.TenantID(req.TenantID), DisplayName: req.DisplayName, PlanTier: req.PlanTier, Active: true, UpdatedAt: time.Now().UTC()}); err != nil {
		writeError(w, http.StatusInternalServerError, "tenant_create_failed", err.Error())
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO user_tenants (user_id, tenant_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, req.TenantID); err != nil {
		writeError(w, http.StatusInternalServerError, "tenant_assign_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"tenantId": req.TenantID, "displayName": req.DisplayName})
}

func (s *Server) handleKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	rows, err := s.db.QueryContext(r.Context(), `SELECT source_uri, MAX(status), COUNT(*)::int, COALESCE(SUM(chunk_count),0)::int, MAX(submitted_at)
FROM ingestion_jobs
WHERE tenant_id = $1
GROUP BY source_uri
ORDER BY MAX(submitted_at) DESC`, tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "knowledge_bases_failed", err.Error())
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var sourceURI, status string
		var documentCount, chunkCount int
		var last time.Time
		if err := rows.Scan(&sourceURI, &status, &documentCount, &chunkCount, &last); err != nil {
			writeError(w, http.StatusInternalServerError, "knowledge_bases_scan_failed", err.Error())
			return
		}
		out = append(out, map[string]any{"knowledgeBaseId": hashHex(sourceURI), "name": sourceURI, "status": mapJobStatus(status), "documentCount": documentCount, "chunkCount": chunkCount, "lastIngestedAt": last.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleVectorStats(w http.ResponseWriter, r *http.Request) {
	tenantPath := strings.TrimSpace(r.PathValue("tenantId"))
	tenantHeader := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantPath != tenantHeader {
		writeError(w, http.StatusForbidden, "tenant_mismatch", "path tenant must match header")
		return
	}
	var vectorCount, storageBytes int64
	err := s.db.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(chunk_count),0)::bigint, COALESCE(SUM(payload_bytes),0)::bigint FROM ingestion_jobs WHERE tenant_id = $1`, tenantPath).Scan(&vectorCount, &storageBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vector_stats_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vectorCount": vectorCount, "storageBytes": storageBytes, "updatedAt": time.Now().UTC().Format(time.RFC3339)})
}

func (s *Server) handlePresign(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var req struct {
		Files []struct {
			Name         string `json:"name"`
			Size         int64  `json:"size"`
			Type         string `json:"type"`
			RelativePath string `json:"relativePath"`
		} `json:"files"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(req.Files) == 0 {
		writeError(w, http.StatusBadRequest, "files_required", "at least one file is required")
		return
	}
	base := strings.TrimRight(s.cfg.UploadBaseURL, "/")
	if base == "" {
		base = "http://localhost:19000"
	}
	items := make([]map[string]any, 0, len(req.Files))
	for _, file := range req.Files {
		if file.Size <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_file_size", fmt.Sprintf("file %q must include size > 0", file.Name))
			return
		}
		if file.Size > s.cfg.MaxObjectBytes {
			writeError(w, http.StatusBadRequest, "object_too_large", fmt.Sprintf("file %q exceeds max size of %d bytes", file.Name, s.cfg.MaxObjectBytes))
			return
		}
		rel := sanitizeRelativePath(file.RelativePath)
		if rel == "" {
			rel = sanitizeRelativePath(file.Name)
		}
		key := fmt.Sprintf("%s/%s/%s", tenantID, time.Now().UTC().Format("20060102"), rel)
		expiresAt := time.Now().UTC().Add(s.cfg.PresignTTL)
		presigned, err := s.presign.PresignPutObject(r.Context(), &s3.PutObjectInput{
			Bucket:      &s.cfg.UploadBucket,
			Key:         &key,
			ContentType: optionalString(strings.TrimSpace(file.Type)),
		}, func(opts *s3.PresignOptions) {
			opts.Expires = s.cfg.PresignTTL
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "presign_failed", err.Error())
			return
		}
		uploadURL := presigned.URL
		if strings.HasPrefix(uploadURL, "https://") && strings.HasPrefix(base, "http://") {
			uploadURL = strings.Replace(uploadURL, "https://", "http://", 1)
		}
		headers := map[string]string{}
		if strings.TrimSpace(file.Type) != "" {
			headers["Content-Type"] = file.Type
		}
		items = append(items, map[string]any{
			"relativePath":     rel,
			"objectKey":        key,
			"uploadUrl":        uploadURL,
			"headers":          headers,
			"expiresAt":        expiresAt.Format(time.RFC3339),
			"expiresInSeconds": int(s.cfg.PresignTTL.Seconds()),
			"maxObjectBytes":   s.cfg.MaxObjectBytes,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploads": items})
}

func (s *Server) handleSubmitJob(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var payload map[string]any
	if err := decodeJSON(r.Body, &payload); err != nil {
		logging.Logger.Error("decode ingestion payload failed", zap.Error(err), zap.String("tenantID", tenantID))
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	sourceURI := strings.TrimSpace(asString(payload["sourceUri"]))
	checksum := strings.TrimSpace(asString(payload["checksum"]))
	sourceMode := strings.TrimSpace(asString(payload["sourceMode"]))
	if sourceMode == "" {
		sourceMode = "uri"
	}
	if sourceURI == "" {
		logging.Logger.Warn("missing sourceUri in ingestion payload", zap.String("tenantID", tenantID))
		writeError(w, http.StatusBadRequest, "invalid_payload", "sourceUri is required")
		return
	}
	if checksum == "" {
		checksum = hashHex(sourceURI)
	}
	jobID := "job-" + randomString(10)
	now := time.Now().UTC()
	payloadJSON, _ := json.Marshal(payload)
	totalFiles := inferTotalFiles(payload)
	logging.Logger.Info("indexing ingestion payload", zap.String("jobID", jobID), zap.String("tenantID", tenantID), zap.String("sourceURI", sourceURI), zap.Int("totalFiles", totalFiles))
	chunkCount, payloadBytes, indexErr := s.indexPayloadArtifacts(r.Context(), contracts.TenantID(tenantID), jobID, checksum, sourceURI, payload)
	if indexErr != nil {
		logging.Logger.Error("indexing payload failed", zap.Error(indexErr), zap.String("jobID", jobID), zap.String("tenantID", tenantID))
		writeError(w, http.StatusBadRequest, "indexing_failed", indexErr.Error())
		return
	}
	indexedChunkCount := chunkCount
	if chunkCount <= 0 {
		chunkCount = maxInt(totalFiles, 1)
	}
	if payloadBytes <= 0 {
		payloadBytes = len(payloadJSON)
	}
	jobStatus := "queued"
	jobStage := "cleanup"
	processedFiles := 0
	successfulFiles := 0
	if indexedChunkCount > 0 {
		jobStatus = "indexed"
		jobStage = "indexed"
		processedFiles = totalFiles
		successfulFiles = totalFiles
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO ingestion_jobs (job_id, tenant_id, source_uri, checksum, status, stage, processed_files, total_files, successful_files, failed_files, policy_code, policy_reason, source_mode, payload_json, chunk_count, payload_bytes, submitted_at, updated_at)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,$16,$17,$18)`, jobID, tenantID, sourceURI, checksum, jobStatus, jobStage, processedFiles, totalFiles, successfulFiles, 0, "", "", sourceMode, string(payloadJSON), chunkCount, payloadBytes, now, now)
	if err != nil {
		logging.Logger.Error("failed to insert ingestion job", zap.Error(err), zap.String("jobID", jobID), zap.String("tenantID", tenantID))
		writeError(w, http.StatusInternalServerError, "job_submit_failed", err.Error())
		return
	}
	logging.Logger.Info("ingestion job submitted", zap.String("jobID", jobID), zap.String("tenantID", tenantID), zap.String("sourceURI", sourceURI), zap.String("status", jobStatus), zap.Int("chunkCount", chunkCount))
	writeJSON(w, http.StatusCreated, map[string]any{"jobId": jobID, "sourceUri": sourceURI, "status": jobStatus, "stage": jobStage, "processedFiles": processedFiles, "totalFiles": totalFiles, "successfulFiles": successfulFiles, "failedFiles": 0, "submittedAt": now.Format(time.RFC3339)})
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	rows, err := s.db.QueryContext(r.Context(), `SELECT job_id, source_uri, status, stage, processed_files, total_files, successful_files, failed_files, policy_code, policy_reason, submitted_at
FROM ingestion_jobs WHERE tenant_id = $1 ORDER BY submitted_at DESC LIMIT 100`, tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "job_list_failed", err.Error())
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var jobID, sourceURI, status, stage, policyCode, policyReason string
		var processed, total, successful, failed int
		var submitted time.Time
		if err := rows.Scan(&jobID, &sourceURI, &status, &stage, &processed, &total, &successful, &failed, &policyCode, &policyReason, &submitted); err != nil {
			writeError(w, http.StatusInternalServerError, "job_scan_failed", err.Error())
			return
		}
		out = append(out, map[string]any{"jobId": jobID, "sourceUri": sourceURI, "status": status, "stage": stage, "processedFiles": processed, "totalFiles": total, "successfulFiles": successful, "failedFiles": failed, "policyCode": policyCode, "policyReason": policyReason, "submittedAt": submitted.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleJobStream(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenantId"))
	if tenantID == "" {
		tenantID = strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	}
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "tenant_required", "tenantId is required")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "sse_unsupported", "stream unsupported")
		return
	}
	emit := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT job_id, source_uri, status, stage, processed_files, total_files, successful_files, failed_files, policy_code, policy_reason, submitted_at
FROM ingestion_jobs WHERE tenant_id = $1 ORDER BY submitted_at DESC LIMIT 25`, tenantID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var jobID, sourceURI, status, stage, policyCode, policyReason string
			var processed, total, successful, failed int
			var submitted time.Time
			if rows.Scan(&jobID, &sourceURI, &status, &stage, &processed, &total, &successful, &failed, &policyCode, &policyReason, &submitted) == nil {
				emit(map[string]any{"type": "job", "job": map[string]any{"jobId": jobID, "sourceUri": sourceURI, "status": status, "stage": stage, "processedFiles": processed, "totalFiles": total, "successfulFiles": successful, "failedFiles": failed, "policyCode": policyCode, "policyReason": policyReason, "submittedAt": submitted.UTC().Format(time.RFC3339)}})
			}
		}
	}
	emit(map[string]any{"type": "done"})
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	jobID := strings.TrimSpace(r.PathValue("jobId"))
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "job_id_required", "job id is required")
		return
	}
	_, err := s.db.ExecContext(r.Context(), `UPDATE ingestion_jobs SET status='queued', stage='cleanup', updated_at=NOW() WHERE tenant_id = $1 AND job_id = $2`, tenantID, jobID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "job_retry_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var req struct {
		QueryText string `json:"queryText"`
		TopK      int    `json:"topK"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		logging.Logger.Error("decode search request failed", zap.Error(err), zap.String("tenantID", tenantID))
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	req.QueryText = strings.TrimSpace(req.QueryText)
	if req.QueryText == "" {
		writeError(w, http.StatusBadRequest, "query_required", "queryText is required")
		return
	}
	requestID := requestIDFromContext(r.Context())
	if requestID == "" {
		requestID = "req-" + randomString(8)
	}
	query := contracts.RetrievalQuery{TenantID: contracts.TenantID(tenantID), RequestID: requestID, Text: req.QueryText, TopK: req.TopK}
	meta := contracts.RequestMetadata{TenantID: contracts.TenantID(tenantID), RequestID: query.RequestID, SourceIP: r.RemoteAddr, UserAgent: r.UserAgent()}
	logging.Logger.Info("search.step.received", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("queryText", req.QueryText), zap.Int("topK", req.TopK))
	logging.Logger.Info("search.step.embedding", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("embeddingModel", s.cfg.EmbeddingModel), zap.Bool("embedderConfigured", s.embedder != nil))
	logging.Logger.Info("search.step.vector_lookup.start", zap.String("tenantID", tenantID), zap.String("requestID", requestID))
	result, err := s.retrieval.Search(r.Context(), meta, query)
	if err != nil {
		logging.Logger.Error("search.step.failed", zap.Error(err), zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
		writeError(w, http.StatusBadRequest, "search_failed", err.Error())
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
	rankedDocs := rankDocumentsByQueryIntent(req.QueryText, result.Documents)
	topVectors := buildTopFilteredVectors(req.QueryText, rankedDocs, 5)
	answer, genErr := s.generateFinalAnswerStrict(r.Context(), "search", tenantID, requestID, req.QueryText, rankedDocs)
	if genErr != nil {
		logging.Logger.Error("search.step.llm.failed", zap.Error(genErr), zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
		writeError(w, http.StatusServiceUnavailable, "llm_unavailable", "final answer generation is unavailable")
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
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSearchStream(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var req struct {
		QueryText string `json:"queryText"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		logging.Logger.Error("decode search stream request failed", zap.Error(err), zap.String("tenantID", tenantID))
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	requestID := requestIDFromContext(r.Context())
	if requestID == "" {
		requestID = "req-" + randomString(8)
	}
	req.QueryText = strings.TrimSpace(req.QueryText)
	if req.QueryText == "" {
		writeError(w, http.StatusBadRequest, "query_required", "queryText is required")
		return
	}
	query := contracts.RetrievalQuery{TenantID: contracts.TenantID(tenantID), RequestID: requestID, Text: req.QueryText, TopK: 5}
	meta := contracts.RequestMetadata{TenantID: contracts.TenantID(tenantID), RequestID: query.RequestID, SourceIP: r.RemoteAddr, UserAgent: r.UserAgent()}
	logging.Logger.Info("search_stream.step.received", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("queryText", req.QueryText), zap.Int("topK", query.TopK))
	logging.Logger.Info("search_stream.step.embedding", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.String("embeddingModel", s.cfg.EmbeddingModel), zap.Bool("embedderConfigured", s.embedder != nil))
	logging.Logger.Info("search_stream.step.vector_lookup.start", zap.String("tenantID", tenantID), zap.String("requestID", requestID))
	result, err := s.retrieval.Search(r.Context(), meta, query)
	if err != nil {
		logging.Logger.Error("search_stream.step.failed", zap.Error(err), zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
		writeError(w, http.StatusBadRequest, "search_failed", err.Error())
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
	rankedDocs := rankDocumentsByQueryIntent(req.QueryText, result.Documents)
	topVectors := buildTopFilteredVectors(req.QueryText, rankedDocs, 5)
	answer, genErr := s.generateFinalAnswerStrict(r.Context(), "search_stream", tenantID, requestID, req.QueryText, rankedDocs)
	if genErr != nil {
		logging.Logger.Error("search_stream.step.llm.failed", zap.Error(genErr), zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Duration("elapsed", time.Since(started)))
		writeError(w, http.StatusServiceUnavailable, "llm_unavailable", "final answer generation is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		logging.Logger.Error("sse stream unsupported", zap.String("tenantID", tenantID), zap.String("requestID", requestID))
		writeError(w, http.StatusInternalServerError, "sse_unsupported", "stream unsupported")
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

func (s *Server) generateFinalAnswerStrict(ctx context.Context, flow, tenantID, requestID, queryText string, rankedDocs []contracts.RetrievalDocument) (string, error) {
	if s.llm == nil {
		return "", fmt.Errorf("llm client is not configured")
	}
	logging.Logger.Info(
		flow+".debug.original_query",
		zap.String("tenantID", tenantID),
		zap.String("requestID", requestID),
		zap.String("original-Query", strings.TrimSpace(queryText)),
	)
	logging.Logger.Info(
		flow+".debug.docs_from_vdb",
		zap.String("tenantID", tenantID),
		zap.String("requestID", requestID),
		zap.Any("Docs from VDB", docsForDebugLog(rankedDocs)),
	)

	parts := make([]string, 0, 4)
	for i, d := range rankedDocs {
		if i >= 4 {
			break
		}
		if cleaned := buildContextPart(queryText, d.Content); cleaned != "" {
			parts = append(parts, cleaned)
		}
	}

	contextText := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if contextText == "" {
		contextText = "No retrieved documents found for this query."
	}
	logging.Logger.Info(
		flow+".debug.llm_input",
		zap.String("tenantID", tenantID),
		zap.String("requestID", requestID),
		zap.String("input for oss-120B", truncateForDebugLog(contextText, 2000)),
	)

	logging.Logger.Info(flow+".step.llm.start", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Int("contextParts", len(parts)), zap.String("llmModel", s.cfg.OpenRouterModel))
	generated, err := s.llm.GenerateAnswer(ctx, queryText, contextText)
	if err != nil {
		return "", err
	}
	answer := strings.TrimSpace(generated)
	logging.Logger.Info(
		flow+".debug.llm_raw_output",
		zap.String("tenantID", tenantID),
		zap.String("requestID", requestID),
		zap.String("raw output from oss-120B", truncateForDebugLog(answer, 2000)),
	)
	if answer == "" {
		return "", fmt.Errorf("empty llm answer")
	}
	if cleaned := sanitizeStreamAnswer(answer); cleaned != "" {
		answer = cleaned
	} else {
		// Keep fail-closed for true junk, but avoid false positives from strict sanitizer heuristics.
		collapsed := strings.TrimSpace(strings.Join(strings.Fields(answer), " "))
		if hasReadableSignal(collapsed) {
			answer = truncateForDebugLog(collapsed, 1200)
			logging.Logger.Warn(
				flow+".step.llm.sanitize_bypassed",
				zap.String("tenantID", tenantID),
				zap.String("requestID", requestID),
			)
		} else {
			return "", fmt.Errorf("unreadable llm answer")
		}
	}
	logging.Logger.Info(
		flow+".debug.llm_output",
		zap.String("tenantID", tenantID),
		zap.String("requestID", requestID),
		zap.String("final output from oss-120B", truncateForDebugLog(answer, 2000)),
	)

	logging.Logger.Info(flow+".step.llm.done", zap.String("tenantID", tenantID), zap.String("requestID", requestID), zap.Int("answerChars", len(answer)))
	return answer, nil
}

func docsForDebugLog(docs []contracts.RetrievalDocument) []map[string]any {
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
			"snippet":    truncateForDebugLog(strings.TrimSpace(d.Content), 240),
			"metadata":   d.Metadata,
		})
	}
	return out
}

func truncateForDebugLog(input string, maxLen int) string {
	value := strings.TrimSpace(input)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func hasReadableSignal(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	letters := 0
	total := 0
	for _, r := range text {
		total++
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letters++
		}
	}
	if total == 0 || letters == 0 {
		return false
	}
	return float64(letters)/float64(total) >= 0.15
}

func buildTopFilteredVectors(query string, docs []contracts.RetrievalDocument, limit int) []map[string]any {
	if len(docs) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 5
	}
	ranked := rankDocumentsByQueryIntent(query, docs)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	vectors := make([]map[string]any, 0, len(ranked))
	for i, d := range ranked {
		snippet := buildAnswerSnippet(query, d.Content)
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

func buildAnswerSnippet(query string, raw string) string {
	text := strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
	if text == "" {
		return ""
	}
	needle := primaryQuerySignal(query)
	if needle != "" {
		lower := strings.ToLower(text)
		if idx := strings.Index(lower, needle); idx >= 0 {
			start := idx - 120
			if start < 0 {
				start = 0
			}
			end := idx + len(needle) + 160
			if end > len(text) {
				end = len(text)
			}
			window := strings.TrimSpace(text[start:end])
			if sanitized := sanitizeAnswerChunk(window); sanitized != "" {
				return sanitized
			}
		}
	}
	return sanitizeAnswerChunk(text)
}

func buildContextPart(query string, raw string) string {
	if cleaned := buildAnswerSnippet(query, raw); cleaned != "" {
		return cleaned
	}
	return sanitizeContextChunk(raw)
}

func sanitizeContextChunk(raw string) string {
	text := normalizeExtractedText(strings.TrimSpace(raw))
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 1800 {
		text = text[:1800]
	}
	if !hasReadableSignal(text) {
		return ""
	}
	// Only block obvious base64/binary garbage here; keep broader content for LLM grounding.
	if noisySequenceRegexp.MatchString(text) && len(strings.Fields(text)) <= 3 {
		return ""
	}
	return text
}

func primaryQuerySignal(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(q, "cgpa") {
		return "cgpa"
	}
	if strings.Contains(q, "gpa") {
		return "gpa"
	}
	if strings.Contains(q, "education") {
		return "education"
	}
	if strings.Contains(q, "qualification") {
		return "qualification"
	}
	for _, token := range strings.Fields(q) {
		if len(token) >= 4 {
			return token
		}
	}
	return ""
}

func rankDocumentsByQueryIntent(query string, docs []contracts.RetrievalDocument) []contracts.RetrievalDocument {
	if len(docs) <= 1 {
		return docs
	}
	queryTerms := tokenizeSearchTerms(query)
	if len(queryTerms) == 0 {
		ordered := make([]contracts.RetrievalDocument, len(docs))
		copy(ordered, docs)
		return ordered
	}

	ordered := make([]contracts.RetrievalDocument, len(docs))
	copy(ordered, docs)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := queryIntentScore(queryTerms, ordered[i])
		right := queryIntentScore(queryTerms, ordered[j])
		if left == right {
			return ordered[i].Score > ordered[j].Score
		}
		return left > right
	})
	return ordered
}

func queryIntentScore(queryTerms []string, doc contracts.RetrievalDocument) int {
	content := strings.ToLower(doc.Content)
	source := strings.ToLower(doc.SourceURI)
	metadata := ""
	for key, value := range doc.Metadata {
		metadata += " " + strings.ToLower(key) + " " + strings.ToLower(value)
	}

	score := 0
	for _, term := range queryTerms {
		switch {
		case strings.Contains(content, term):
			score += 8
		case strings.Contains(metadata, term):
			score += 5
		case strings.Contains(source, term):
			score += 4
		}
	}

	hasCGPAIntent := containsAny(queryTerms, "cgpa", "gpa")
	if hasCGPAIntent {
		if strings.Contains(content, "cgpa") || strings.Contains(content, "gpa") {
			score += 20
		}
		if cgpaValueRegexp.MatchString(content) {
			score += 12
		}
	}
	if containsAny(queryTerms, "education", "qualification", "degree") {
		if strings.Contains(content, "education") || strings.Contains(content, "qualification") || strings.Contains(content, "degree") {
			score += 10
		}
	}
	return score
}

func containsAny(terms []string, values ...string) bool {
	if len(terms) == 0 || len(values) == 0 {
		return false
	}
	set := map[string]struct{}{}
	for _, term := range terms {
		set[term] = struct{}{}
	}
	for _, v := range values {
		if _, ok := set[v]; ok {
			return true
		}
	}
	return false
}

func tokenizeSearchTerms(input string) []string {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		return nil
	}
	replacer := strings.NewReplacer("?", " ", "!", " ", ".", " ", ",", " ", ":", " ", ";", " ", "(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ", "'", " ", "\"", " ")
	parts := strings.Fields(replacer.Replace(value))
	stop := map[string]struct{}{
		"what": {}, "is": {}, "the": {}, "of": {}, "for": {}, "and": {}, "with": {}, "from": {}, "about": {}, "tell": {}, "me": {},
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if _, skip := stop[p]; skip {
			continue
		}
		if len(p) < 3 {
			continue
		}
		out = append(out, p)
	}
	return out
}

type vectorTenantPurger interface {
	PurgeTenant(ctx context.Context, tenantID contracts.TenantID) error
}

func (s *Server) handlePurgeTenantData(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	pathTenant := strings.TrimSpace(r.PathValue("tenantId"))
	if tenantID == "" || pathTenant == "" || tenantID != pathTenant {
		writeError(w, http.StatusBadRequest, "tenant_mismatch", "tenant header and path must match")
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.ToUpper(strings.TrimSpace(req.Confirm)) != "PURGE" {
		writeError(w, http.StatusBadRequest, "confirmation_required", "set confirm to PURGE")
		return
	}

	deletedObjects := 0
	prefix := tenantID + "/"
	p := s3.NewListObjectsV2Paginator(s.minio, &s3.ListObjectsV2Input{
		Bucket: &s.cfg.UploadBucket,
		Prefix: &prefix,
	})
	for p.HasMorePages() {
		page, err := p.NextPage(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "minio_list_failed", err.Error())
			return
		}
		for _, item := range page.Contents {
			if item.Key == nil || strings.TrimSpace(*item.Key) == "" {
				continue
			}
			_, delErr := s.minio.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: &s.cfg.UploadBucket, Key: item.Key})
			if delErr != nil {
				writeError(w, http.StatusInternalServerError, "minio_delete_failed", delErr.Error())
				return
			}
			deletedObjects++
		}
	}

	if purger, ok := s.index.(vectorTenantPurger); ok {
		if err := purger.PurgeTenant(r.Context(), contracts.TenantID(tenantID)); err != nil {
			writeError(w, http.StatusInternalServerError, "vector_purge_failed", err.Error())
			return
		}
	}

	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM ingestion_jobs WHERE tenant_id = $1`, tenantID); err != nil {
		writeError(w, http.StatusInternalServerError, "job_purge_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "purged",
		"tenantId":       tenantID,
		"deletedObjects": deletedObjects,
	})
}

func (s *Server) fetchUserByUsername(ctx context.Context, username string) (dbUser, error) {
	var u dbUser
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, api_key_hash, active FROM app_users WHERE username = $1`, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.APIKeyHash, &u.Active)
	return u, err
}

func (s *Server) userHasTenant(ctx context.Context, userID int64, tenantID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_tenants WHERE user_id = $1 AND tenant_id = $2)`, userID, tenantID).Scan(&exists)
	return exists, err
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func inferTotalFiles(payload map[string]any) int {
	items, ok := payload["localItems"].([]any)
	if !ok || len(items) == 0 {
		return 1
	}
	return len(items)
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func hashHex(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:8])
}

func mapJobStatus(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "approved", "indexed":
		return "ready"
	case "failed", "rejected":
		return "degraded"
	default:
		return "indexing"
	}
}

func sanitizeRelativePath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "file.bin"
	}
	value = strings.ReplaceAll(value, "\\", "/")
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	value = strings.TrimPrefix(value, "/")
	if strings.HasPrefix(value, "../") || value == ".." || value == "." {
		return "file.bin"
	}
	return value
}

func inferPersonHint(relativePath string) string {
	base := strings.TrimSpace(relativePath)
	if base == "" {
		return ""
	}
	if slash := strings.LastIndex(base, "/"); slash >= 0 && slash+1 < len(base) {
		base = base[slash+1:]
	}
	if dot := strings.LastIndex(base, "."); dot > 0 {
		base = base[:dot]
	}
	base = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(base)
	base = splitCamelCase(base)
	words := strings.Fields(strings.ToLower(base))
	if len(words) == 0 {
		return ""
	}
	stop := map[string]struct{}{
		"resume": {}, "cv": {}, "profile": {}, "final": {}, "latest": {}, "updated": {},
		"jan": {}, "feb": {}, "mar": {}, "apr": {}, "may": {}, "jun": {}, "jul": {},
		"aug": {}, "sep": {}, "oct": {}, "nov": {}, "dec": {},
	}
	hintWords := make([]string, 0, 2)
	for _, word := range words {
		if _, excluded := stop[word]; excluded {
			continue
		}
		allDigits := true
		for _, ch := range word {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits || len(word) < 3 {
			continue
		}
		hintWords = append(hintWords, word)
		if len(hintWords) == 2 {
			break
		}
	}
	return strings.Join(hintWords, " ")
}

func splitCamelCase(input string) string {
	if input == "" {
		return ""
	}
	var b strings.Builder
	prevLower := false
	for _, r := range input {
		isUpper := r >= 'A' && r <= 'Z'
		if isUpper && prevLower {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prevLower = r >= 'a' && r <= 'z'
	}
	return b.String()
}

func (s *Server) indexPayloadArtifacts(ctx context.Context, tenantID contracts.TenantID, jobID string, checksum string, fallbackSourceURI string, payload map[string]any) (int, int, error) {
	if s.index == nil || s.minio == nil {
		return 0, 0, nil
	}
	objectKeys := readStringArray(payload["objectKeys"])
	if len(objectKeys) == 0 {
		return 0, 0, nil
	}
	if s.embedder == nil {
		return 0, 0, fmt.Errorf("embedding provider unavailable")
	}

	localItemByPath := map[string]map[string]any{}
	if localItems, ok := payload["localItems"].([]any); ok {
		for _, item := range localItems {
			if asMap, mapOK := item.(map[string]any); mapOK {
				relativePath := strings.TrimSpace(asString(asMap["relativePath"]))
				if relativePath != "" {
					localItemByPath[relativePath] = asMap
				}
			}
		}
	}

	type pendingChunk struct {
		recordID   string
		chunkText  string
		metadata   map[string]string
		sourceURI  string
		indexedAt  time.Time
		checksum   string
		retryCount int
		tenantID   contracts.TenantID
		jobID      string
	}
	pending := make([]pendingChunk, 0, len(objectKeys))
	indexedChunks := 0
	payloadBytes := 0
	now := time.Now().UTC()

	for keyIndex, objectKey := range objectKeys {
		key := strings.TrimSpace(objectKey)
		if key == "" {
			continue
		}
		input := &s3.GetObjectInput{Bucket: &s.cfg.UploadBucket, Key: &key}
		object, err := s.minio.GetObject(ctx, input)
		if err != nil {
			return 0, 0, fmt.Errorf("read object %q: %w", key, err)
		}
		bytes, readErr := io.ReadAll(io.LimitReader(object.Body, s.cfg.MaxObjectBytes))
		_ = object.Body.Close()
		if readErr != nil {
			return 0, 0, fmt.Errorf("read object body %q: %w", key, readErr)
		}
		payloadBytes += len(bytes)

		relativePath := key
		if idx := strings.LastIndex(key, "/"); idx >= 0 && idx+1 < len(key) {
			relativePath = key[idx+1:]
		}
		personHint := inferPersonHint(relativePath)

		declaredType := ""
		for rel, item := range localItemByPath {
			if strings.HasSuffix(key, rel) || rel == relativePath {
				declaredType = strings.TrimSpace(asString(item["type"]))
				break
			}
		}

		sourceURI := fallbackSourceURI
		if sourceURI == "" {
			sourceURI = "s3://" + s.cfg.UploadBucket + "/" + key
		}

		text := ""
		if isPDFContent(declaredType, key) {
			text = extractPDFSearchableText(bytes)
		} else if isTextLikeContent(declaredType, key) {
			text = extractSearchableText(bytes)
		}
		if text == "" {
			text = "uploaded document " + relativePath + " for tenant " + string(tenantID)
			if declaredType != "" {
				text += " (" + declaredType + ")"
			}
		}
		chunks := chunkText(text, s.cfg.ChunkSizeChars, s.cfg.ChunkOverlapWords)
		for chunkIndex, chunk := range chunks {
			chunkMetadata := map[string]string{
				"object_key":    key,
				"relative_path": relativePath,
				"file_name":     relativePath,
				"source_uri":    sourceURI,
			}
			if personHint != "" {
				chunkMetadata["person_hint"] = personHint
			}
			pending = append(pending, pendingChunk{
				recordID:   fmt.Sprintf("vec-%s-%d-%d", jobID, keyIndex, chunkIndex),
				tenantID:   tenantID,
				jobID:      jobID,
				chunkText:  chunk,
				metadata:   chunkMetadata,
				indexedAt:  now,
				sourceURI:  sourceURI,
				checksum:   checksum,
				retryCount: 0,
			})
			indexedChunks++
		}
	}

	if len(pending) == 0 {
		return 0, payloadBytes, nil
	}
	chunkTexts := make([]string, 0, len(pending))
	for _, item := range pending {
		chunkTexts = append(chunkTexts, item.chunkText)
	}
	vectors, err := s.embedder.Embed(ctx, tenantID, chunkTexts)
	if err != nil {
		return 0, 0, fmt.Errorf("embed chunks: %w", err)
	}
	firstDim := 0
	if len(vectors) > 0 {
		firstDim = len(vectors[0])
	}
	logging.Logger.Info(
		"ingestion.step.embedding.response",
		zap.String("tenantID", string(tenantID)),
		zap.String("jobID", jobID),
		zap.String("embeddingModel", s.cfg.EmbeddingModel),
		zap.Int("chunkCount", len(chunkTexts)),
		zap.Int("vectorCount", len(vectors)),
		zap.Int("firstVectorDim", firstDim),
	)
	if len(vectors) != len(pending) {
		return 0, 0, fmt.Errorf("embedding count mismatch: got=%d want=%d", len(vectors), len(pending))
	}

	records := make([]contracts.VectorRecord, 0, len(pending))
	for i, item := range pending {
		records = append(records, contracts.VectorRecord{
			RecordID:   item.recordID,
			TenantID:   item.tenantID,
			JobID:      item.jobID,
			ChunkText:  item.chunkText,
			Embedding:  vectors[i],
			Metadata:   item.metadata,
			IndexedAt:  item.indexedAt,
			SourceURI:  item.sourceURI,
			Checksum:   item.checksum,
			RetryCount: item.retryCount,
		})
	}
	if err := s.index.Upsert(ctx, records); err != nil {
		return 0, 0, err
	}
	return indexedChunks, payloadBytes, nil
}

func readStringArray(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	output := make([]string, 0, len(items))
	for _, item := range items {
		if s, stringOK := item.(string); stringOK {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				output = append(output, trimmed)
			}
		}
	}
	return output
}

func extractSearchableText(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	builder := strings.Builder{}
	for _, b := range payload {
		if (b >= 32 && b <= 126) || b == '\n' || b == '\r' || b == '\t' {
			builder.WriteByte(b)
			continue
		}
		builder.WriteByte(' ')
	}
	text := strings.Join(strings.Fields(builder.String()), " ")
	if isLikelyNoisyText(text) {
		return ""
	}
	if len(text) > 12000 {
		return text[:12000]
	}
	return text
}

func sanitizeAnswerChunk(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if isLikelyNoisyText(text) {
		return ""
	}
	if len(text) > 320 {
		return text[:320]
	}
	return text
}

func sanitizeStreamAnswer(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	if isLikelyNoisyText(text) {
		return ""
	}
	if len(text) > 1200 {
		return text[:1200]
	}
	return text
}

func isTextLikeContent(contentType string, objectKey string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "xml") || strings.Contains(ct, "yaml") {
		return true
	}
	ext := strings.ToLower(strings.TrimSpace(objectKey))
	for _, suffix := range []string{".txt", ".md", ".csv", ".json", ".html", ".htm", ".xml", ".yaml", ".yml"} {
		if strings.HasSuffix(ext, suffix) {
			return true
		}
	}
	return false
}

func isPDFContent(contentType string, objectKey string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(ct, "pdf") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(objectKey)), ".pdf")
}

func extractPDFSearchableText(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	if parsed := extractPDFTextWithLibrary(payload); parsed != "" {
		parsed = normalizeExtractedText(parsed)
		if parsed == "" {
			return ""
		}
		if len(parsed) > 12000 {
			return parsed[:12000]
		}
		return parsed
	}
	if parsed := extractPDFTextWithRscLibrary(payload); parsed != "" {
		parsed = normalizeExtractedText(parsed)
		if parsed == "" {
			return ""
		}
		if len(parsed) > 12000 {
			return parsed[:12000]
		}
		return parsed
	}
	segments := make([]string, 0, 128)
	segments = append(segments, extractPDFTextSegments(payload)...)
	for _, decoded := range extractPDFFlateStreams(payload) {
		segments = append(segments, extractPDFTextSegments(decoded)...)
	}

	if len(segments) == 0 {
		return ""
	}
	unique := make([]string, 0, len(segments))
	seen := make(map[string]struct{}, len(segments))
	for _, seg := range segments {
		trimmed := strings.TrimSpace(seg)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}
	if len(unique) == 0 {
		return ""
	}
	text := normalizeExtractedText(strings.Join(unique, " "))
	if text == "" {
		return ""
	}
	if isLikelyNoisyText(text) {
		return ""
	}
	if len(text) > 12000 {
		return text[:12000]
	}
	return text
}

func extractPDFTextWithRscLibrary(payload []byte) string {
	reader, err := rscpdf.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return ""
	}
	b := strings.Builder{}
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		content := page.Content()
		for _, textPart := range content.Text {
			segment := strings.TrimSpace(textPart.S)
			if segment == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(segment)
		}
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	if len(text) < 10 || isLikelyNoisyText(text) {
		return ""
	}
	return text
}

func extractPDFTextWithLibrary(payload []byte) string {
	reader, err := pdf.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return ""
	}
	plain, err := reader.GetPlainText()
	if err != nil || plain == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(plain, 2*1024*1024))
	if err != nil || len(raw) == 0 {
		return ""
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	if len(text) < 10 {
		return ""
	}
	if isLikelyNoisyText(text) {
		return ""
	}
	return text
}

func extractPDFTextSegments(payload []byte) []string {
	segments := make([]string, 0, 64)
	current := make([]rune, 0, 64)
	inLiteral := false
	escape := false
	inHex := false
	hexCurrent := make([]rune, 0, 64)

	appendCandidate := func(candidate string) {
		candidate = strings.Join(strings.Fields(candidate), " ")
		if len(candidate) < 3 {
			return
		}
		if isLikelyNoisyText(candidate) {
			return
		}
		segments = append(segments, candidate)
	}
	flush := func() {
		if len(current) == 0 {
			return
		}
		candidate := string(current)
		current = current[:0]
		appendCandidate(candidate)
	}
	flushHex := func() {
		if len(hexCurrent) == 0 {
			return
		}
		decoded := decodePDFHexLiteral(string(hexCurrent))
		hexCurrent = hexCurrent[:0]
		if decoded == "" {
			return
		}
		appendCandidate(decoded)
	}

	for _, b := range payload {
		r := rune(b)
		if !inLiteral && !inHex {
			switch r {
			case '(':
				inLiteral = true
				escape = false
				current = current[:0]
			case '<':
				inHex = true
				hexCurrent = hexCurrent[:0]
			}
			continue
		}
		if inHex {
			if r == '>' {
				flushHex()
				inHex = false
				continue
			}
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				hexCurrent = append(hexCurrent, r)
			}
			continue
		}
		if escape {
			escape = false
			if r >= 32 && r <= 126 {
				current = append(current, r)
			}
			continue
		}
		switch r {
		case '\\':
			escape = true
		case ')':
			flush()
			inLiteral = false
		default:
			if r == '\n' || r == '\r' || r == '\t' || (r >= 32 && r <= 126) {
				current = append(current, r)
			}
		}
	}
	flush()
	flushHex()
	return segments
}

func extractPDFFlateStreams(payload []byte) [][]byte {
	streams := make([][]byte, 0, 8)
	if len(payload) == 0 {
		return streams
	}
	lower := toASCIILower(payload)
	start := 0
	for {
		if start >= len(lower) {
			break
		}
		streamIdx := bytes.Index(lower[start:], []byte("stream"))
		if streamIdx < 0 {
			break
		}
		streamIdx += start
		dataStart := streamIdx + len("stream")
		if dataStart < len(payload) {
			if payload[dataStart] == '\r' {
				dataStart++
			}
			if dataStart < len(payload) && payload[dataStart] == '\n' {
				dataStart++
			}
		}
		if dataStart >= len(lower) {
			break
		}
		endIdx := bytes.Index(lower[dataStart:], []byte("endstream"))
		if endIdx < 0 {
			break
		}
		endIdx += dataStart
		if endIdx <= dataStart {
			start = dataStart + 1
			continue
		}
		if dataStart < 0 || endIdx > len(payload) {
			start = dataStart + 1
			continue
		}
		raw := payload[dataStart:endIdx]
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err == nil {
			decoded, readErr := io.ReadAll(io.LimitReader(zr, 2*1024*1024))
			_ = zr.Close()
			if readErr == nil && len(decoded) > 0 {
				streams = append(streams, decoded)
			}
		}
		start = endIdx + len("endstream")
	}
	return streams
}

func toASCIILower(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] = out[i] + ('a' - 'A')
		}
	}
	return out
}

func decodePDFHexLiteral(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed)%2 == 1 {
		trimmed += "0"
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil || len(decoded) == 0 {
		return ""
	}
	if utf16Decoded, ok := decodeLikelyPDFUTF16(decoded); ok {
		return strings.TrimSpace(utf16Decoded)
	}
	b := make([]byte, 0, len(decoded))
	for _, ch := range decoded {
		if ch == '\n' || ch == '\r' || ch == '\t' || (ch >= 32 && ch <= 126) {
			b = append(b, ch)
		} else {
			b = append(b, ' ')
		}
	}
	return strings.TrimSpace(string(b))
}

func decodeLikelyPDFUTF16(decoded []byte) (string, bool) {
	if len(decoded) < 4 {
		return "", false
	}
	// Many PDFs encode text as UTF-16BE hex literals (e.g. <005300680061...>).
	if len(decoded)%2 == 1 {
		decoded = decoded[:len(decoded)-1]
	}
	if len(decoded) < 4 {
		return "", false
	}
	evenZero := 0
	oddZero := 0
	pairs := len(decoded) / 2
	for i := 0; i+1 < len(decoded); i += 2 {
		if decoded[i] == 0 {
			evenZero++
		}
		if decoded[i+1] == 0 {
			oddZero++
		}
	}
	// Require a clear zero-byte pattern before interpreting as UTF-16.
	if evenZero*3 < pairs && oddZero*3 < pairs {
		return "", false
	}
	words := make([]uint16, 0, pairs)
	if evenZero >= oddZero {
		for i := 0; i+1 < len(decoded); i += 2 {
			words = append(words, uint16(decoded[i])<<8|uint16(decoded[i+1]))
		}
	} else {
		for i := 0; i+1 < len(decoded); i += 2 {
			words = append(words, uint16(decoded[i+1])<<8|uint16(decoded[i]))
		}
	}
	runes := utf16.Decode(words)
	clean := strings.Builder{}
	for _, r := range runes {
		switch {
		case r == '\u0000':
			clean.WriteByte(' ')
		case r == '\n' || r == '\r' || r == '\t':
			clean.WriteRune(r)
		case r >= 32 && r <= 126:
			clean.WriteRune(r)
		default:
			clean.WriteByte(' ')
		}
	}
	text := strings.Join(strings.Fields(clean.String()), " ")
	if len(text) < 3 || isLikelyNoisyText(text) {
		return "", false
	}
	return text, true
}

func normalizeExtractedText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	// Drop replacement chars and collapse glyph-spaced runs produced by some PDF extractors.
	trimmed = strings.ReplaceAll(trimmed, "\uFFFD", " ")
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	trimmed = spacedGlyphRunRegexp.ReplaceAllStringFunc(trimmed, compactSpacedGlyphRun)
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	return trimmed
}

func compactSpacedGlyphRun(raw string) string {
	tokens := strings.Fields(raw)
	if len(tokens) < 4 {
		return raw
	}
	b := strings.Builder{}
	for _, tok := range tokens {
		for _, r := range tok {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '@' || r == '.' || r == '_' || r == '+' || r == '-' || r == '#' {
				b.WriteRune(r)
			}
		}
	}
	joined := b.String()
	if len(joined) < 4 {
		return raw
	}
	return joined
}

func isLikelyNoisyText(text string) bool {
	if text == "" {
		return true
	}
	if len(text) >= 64 {
		if noisySequenceRegexp.MatchString(text) {
			return true
		}
	}
	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return true
	}
	alpha := 0
	alnum := 0
	total := 0
	suspicious := 0
	symbol := 0
	for _, token := range tokens {
		tokenAlpha := 0
		tokenAlnum := 0
		for _, r := range token {
			total++
			switch {
			case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
				alpha++
				tokenAlpha++
				alnum++
				tokenAlnum++
			case r >= '0' && r <= '9':
				alnum++
				tokenAlnum++
			case !(r == '_' || r == '-' || r == '.' || r == ',' || r == ':' || r == ';' || r == '?' || r == '!' || r == '/' || r == '(' || r == ')' || r == '[' || r == ']' || r == '\'' || r == '"'):
				symbol++
			}
		}
		if len(token) >= 24 {
			ratio := float64(tokenAlpha) / float64(maxInt(tokenAlnum, 1))
			if ratio < 0.35 {
				suspicious++
			}
		}
	}
	if total == 0 {
		return true
	}
	alphaRatio := float64(alpha) / float64(total)
	alnumRatio := float64(alnum) / float64(total)
	suspiciousRatio := float64(suspicious) / float64(len(tokens))
	symbolRatio := float64(symbol) / float64(total)
	return alphaRatio < 0.28 || alnumRatio < 0.45 || suspiciousRatio > 0.25 || symbolRatio > 0.18
}

var noisySequenceRegexp = regexp.MustCompile(`[A-Za-z0-9+/=]{36,}|[{}<>|~^]{2,}`)
var cgpaValueRegexp = regexp.MustCompile(`\b\d{1,2}(?:\.\d{1,2})?\s*/\s*10\b|\bcgpa\s*[:=-]?\s*\d{1,2}(?:\.\d{1,2})?\b`)
var spacedGlyphRunRegexp = regexp.MustCompile(`(?:[A-Za-z0-9@._#+-]\s+){3,}[A-Za-z0-9@._#+-]`)

func chunkText(text string, maxLen int, overlapWords int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if maxLen <= 0 {
		maxLen = 900
	}
	if overlapWords < 0 {
		overlapWords = 0
	}
	words := strings.Fields(trimmed)
	if len(words) == 0 {
		return nil
	}
	chunks := make([]string, 0, len(words)/20+1)
	start := 0
	for start < len(words) {
		end := start
		currentLen := 0
		for end < len(words) {
			wordLen := len(words[end])
			candidateLen := currentLen + wordLen
			if end > start {
				candidateLen++
			}
			if candidateLen > maxLen && end > start {
				break
			}
			currentLen = candidateLen
			end++
			if currentLen >= maxLen {
				break
			}
		}
		if end <= start {
			end = start + 1
		}
		chunks = append(chunks, strings.Join(words[start:end], " "))
		if end >= len(words) {
			break
		}
		nextStart := end - overlapWords
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}
	return chunks
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
