package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/repository"
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
		rel := sanitizeRelativePath(file.RelativePath)
		if rel == "" {
			rel = sanitizeRelativePath(file.Name)
		}
		key := fmt.Sprintf("%s/%s/%s", tenantID, time.Now().UTC().Format("20060102"), rel)
		uploadURL := fmt.Sprintf("%s/%s/%s", base, s.cfg.UploadBucket, key)
		headers := map[string]string{}
		if strings.TrimSpace(file.Type) != "" {
			headers["Content-Type"] = file.Type
		}
		items = append(items, map[string]any{"relativePath": rel, "objectKey": key, "uploadUrl": uploadURL, "headers": headers})
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploads": items})
}

func (s *Server) handleSubmitJob(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var payload map[string]any
	if err := decodeJSON(r.Body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	sourceURI := strings.TrimSpace(asString(payload["sourceUri"]))
	checksum := strings.TrimSpace(asString(payload["checksum"]))
	sourceMode := strings.TrimSpace(asString(payload["sourceMode"]))
	if sourceMode == "" {
		sourceMode = "uri"
	}
	if sourceURI == "" || checksum == "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", "sourceUri and checksum are required")
		return
	}
	jobID := "job-" + randomString(10)
	now := time.Now().UTC()
	payloadJSON, _ := json.Marshal(payload)
	totalFiles := inferTotalFiles(payload)
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO ingestion_jobs (job_id, tenant_id, source_uri, checksum, status, stage, processed_files, total_files, successful_files, failed_files, policy_code, policy_reason, source_mode, payload_json, chunk_count, payload_bytes, submitted_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,$16,$17,$18)`, jobID, tenantID, sourceURI, checksum, "queued", "cleanup", 0, totalFiles, 0, 0, "", "", sourceMode, string(payloadJSON), maxInt(totalFiles, 1)*8, len(payloadJSON), now, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "job_submit_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"jobId": jobID, "sourceUri": sourceURI, "status": "queued", "stage": "cleanup", "processedFiles": 0, "totalFiles": totalFiles, "successfulFiles": 0, "failedFiles": 0, "submittedAt": now.Format(time.RFC3339)})
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
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var req struct {
		QueryText string `json:"queryText"`
		TopK      int    `json:"topK"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.TopK <= 0 {
		req.TopK = 5
	}
	query := contracts.RetrievalQuery{TenantID: contracts.TenantID(tenantID), RequestID: "req-" + randomString(8), Text: strings.TrimSpace(req.QueryText), TopK: req.TopK}
	meta := contracts.RequestMetadata{TenantID: contracts.TenantID(tenantID), RequestID: query.RequestID, SourceIP: r.RemoteAddr, UserAgent: r.UserAgent()}
	result, err := s.retrieval.Search(r.Context(), meta, query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "search_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSearchStream(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var req struct {
		QueryText string `json:"queryText"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	query := contracts.RetrievalQuery{TenantID: contracts.TenantID(tenantID), RequestID: "req-" + randomString(8), Text: strings.TrimSpace(req.QueryText), TopK: 5}
	meta := contracts.RequestMetadata{TenantID: contracts.TenantID(tenantID), RequestID: query.RequestID, SourceIP: r.RemoteAddr, UserAgent: r.UserAgent()}
	result, err := s.retrieval.Search(r.Context(), meta, query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "search_failed", err.Error())
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
	answer := "No relevant chunks found."
	if len(result.Documents) > 0 {
		parts := make([]string, 0, 3)
		for i, d := range result.Documents {
			if i >= 3 {
				break
			}
			parts = append(parts, strings.TrimSpace(d.Content))
		}
		answer = strings.Join(parts, " ")
	}
	for _, tok := range strings.Fields(answer) {
		emit(map[string]any{"type": "token", "token": tok + " "})
	}
	citations := make([]map[string]string, 0)
	for _, d := range result.Documents {
		c := map[string]string{"id": d.DocumentID, "title": "Source", "uri": d.SourceURI}
		citations = append(citations, c)
		emit(map[string]any{"type": "citation", "citation": c})
	}
	trace := map[string]any{"requestId": result.RequestID, "retrievalMs": result.Trace.DurationMillis, "rerankApplied": result.Trace.RerankApplied}
	emit(map[string]any{"type": "trace", "trace": trace})
	emit(map[string]any{"type": "done", "citations": citations, "trace": trace})
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
