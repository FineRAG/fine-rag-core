package managers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	util "enterprise-go-rag/backend/util/apiutil"
	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/logging"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.uber.org/zap"
)

type IngestionManager struct {
	DB                *sql.DB
	Index             contracts.VectorIndex
	ObjectStore       *s3.Client
	Embedder          contracts.EmbeddingProvider
	UploadBucket      string
	MaxObjectBytes    int64
	ChunkSizeChars    int
	ChunkOverlapWords int
}

func (m *IngestionManager) HandleSubmitJob(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var payload map[string]any
	if err := util.DecodeJSON(r.Body, &payload); err != nil {
		logging.Logger.Error("decode ingestion payload failed", zap.Error(err), zap.String("tenantID", tenantID))
		util.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	sourceURI := strings.TrimSpace(m.asString(payload["sourceUri"]))
	checksum := strings.TrimSpace(m.asString(payload["checksum"]))
	sourceMode := strings.TrimSpace(m.asString(payload["sourceMode"]))
	if sourceMode == "" {
		sourceMode = "uri"
	}
	if sourceURI == "" {
		util.WriteError(w, http.StatusBadRequest, "invalid_payload", "sourceUri is required")
		return
	}
	if checksum == "" {
		checksum = m.hashHex(sourceURI)
	}
	jobID := "job-" + util.RandomString(10)
	now := time.Now().UTC()
	payloadJSON, _ := json.Marshal(payload)
	totalFiles := m.inferTotalFiles(payload)
	
	logging.Logger.Info("indexing ingestion payload", zap.String("jobID", jobID), zap.String("tenantID", tenantID), zap.String("sourceURI", sourceURI), zap.Int("totalFiles", totalFiles))
	chunkCount, payloadBytes, indexErr := m.indexPayloadArtifacts(r.Context(), contracts.TenantID(tenantID), jobID, checksum, sourceURI, payload)
	if indexErr != nil {
		logging.Logger.Error("indexing payload failed", zap.Error(indexErr), zap.String("jobID", jobID), zap.String("tenantID", tenantID))
		util.WriteError(w, http.StatusBadRequest, "indexing_failed", indexErr.Error())
		return
	}
	
	jobStatus := "queued"
	jobStage := "cleanup"
	processedFiles, successfulFiles := 0, 0
	if chunkCount > 0 {
		jobStatus = "indexed"
		jobStage = "indexed"
		processedFiles = totalFiles
		successfulFiles = totalFiles
	}
	
	if chunkCount <= 0 {
		chunkCount = util.MaxInt(totalFiles, 1)
	}
	if payloadBytes <= 0 {
		payloadBytes = len(payloadJSON)
	}
	
	_, err := m.DB.ExecContext(r.Context(), `INSERT INTO ingestion_jobs (job_id, tenant_id, source_uri, checksum, status, stage, processed_files, total_files, successful_files, failed_files, policy_code, policy_reason, source_mode, payload_json, chunk_count, payload_bytes, submitted_at, updated_at)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,$16,$17,$18)`, jobID, tenantID, sourceURI, checksum, jobStatus, jobStage, processedFiles, totalFiles, successfulFiles, 0, "", "", sourceMode, string(payloadJSON), chunkCount, payloadBytes, now, now)
	if err != nil {
		util.WriteError(w, http.StatusInternalServerError, "job_submit_failed", err.Error())
		return
	}
	util.WriteJSON(w, http.StatusCreated, map[string]any{"jobId": jobID, "sourceUri": sourceURI, "status": jobStatus, "stage": jobStage, "processedFiles": processedFiles, "totalFiles": totalFiles, "successfulFiles": successfulFiles, "failedFiles": 0, "submittedAt": now.Format(time.RFC3339)})
}

func (m *IngestionManager) HandleListJobs(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	rows, err := m.DB.QueryContext(r.Context(), `SELECT job_id, source_uri, status, COALESCE(stage,''), processed_files, total_files, successful_files, failed_files, COALESCE(policy_code,''), COALESCE(policy_reason,''), submitted_at
FROM ingestion_jobs WHERE tenant_id = $1 ORDER BY submitted_at DESC LIMIT 100`, tenantID)
	if err != nil {
		util.WriteError(w, http.StatusInternalServerError, "job_list_failed", err.Error())
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var jobID, sourceURI, status string
		var stage, policyCode, policyReason sql.NullString
		var processed, total, successful, failed int
		var submitted time.Time
		if err := rows.Scan(&jobID, &sourceURI, &status, &stage, &processed, &total, &successful, &failed, &policyCode, &policyReason, &submitted); err != nil {
			util.WriteError(w, http.StatusInternalServerError, "job_scan_failed", err.Error())
			return
		}
		out = append(out, map[string]any{"jobId": jobID, "sourceUri": sourceURI, "status": status, "stage": stage.String, "processedFiles": processed, "totalFiles": total, "successfulFiles": successful, "failedFiles": failed, "policyCode": policyCode.String, "policyReason": policyReason.String, "submittedAt": submitted.UTC().Format(time.RFC3339)})
	}
	util.WriteJSON(w, http.StatusOK, out)
}

func (m *IngestionManager) HandleJobStream(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenantId"))
	if tenantID == "" {
		tenantID = strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	}
	if tenantID == "" {
		util.WriteError(w, http.StatusBadRequest, "tenant_required", "tenantId is required")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		util.WriteError(w, http.StatusInternalServerError, "sse_unsupported", "stream unsupported")
		return
	}
	emit := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}
	rows, err := m.DB.QueryContext(r.Context(), `SELECT job_id, source_uri, status, COALESCE(stage,''), processed_files, total_files, successful_files, failed_files, COALESCE(policy_code,''), COALESCE(policy_reason,''), submitted_at
FROM ingestion_jobs WHERE tenant_id = $1 ORDER BY submitted_at DESC LIMIT 25`, tenantID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var jobID, sourceURI, status string
			var stage, policyCode, policyReason sql.NullString
			var processed, total, successful, failed int
			var submitted time.Time
			if rows.Scan(&jobID, &sourceURI, &status, &stage, &processed, &total, &successful, &failed, &policyCode, &policyReason, &submitted) == nil {
				emit(map[string]any{"type": "job", "job": map[string]any{"jobId": jobID, "sourceUri": sourceURI, "status": status, "stage": stage.String, "processedFiles": processed, "totalFiles": total, "successfulFiles": successful, "failedFiles": failed, "policyCode": policyCode.String, "policyReason": policyReason.String, "submittedAt": submitted.UTC().Format(time.RFC3339)}})
			}
		}
	}
	emit(map[string]any{"type": "done"})
}

func (m *IngestionManager) HandleRetryJob(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	jobID := strings.TrimSpace(r.PathValue("jobId"))
	if jobID == "" {
		util.WriteError(w, http.StatusBadRequest, "job_id_required", "job id is required")
		return
	}
	_, err := m.DB.ExecContext(r.Context(), `UPDATE ingestion_jobs SET status='queued', stage='cleanup', updated_at=NOW() WHERE tenant_id = $1 AND job_id = $2`, tenantID, jobID)
	if err != nil {
		util.WriteError(w, http.StatusInternalServerError, "job_retry_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Internal implementation of indexing logic
func (m *IngestionManager) indexPayloadArtifacts(ctx context.Context, tenantID contracts.TenantID, jobID string, checksum string, fallbackSourceURI string, payload map[string]any) (int, int, error) {
	if m.Index == nil || m.ObjectStore == nil {
		return 0, 0, nil
	}
	objectKeys := m.readStringArray(payload["objectKeys"])
	if len(objectKeys) == 0 {
		return 0, 0, nil
	}
	if m.Embedder == nil {
		return 0, 0, fmt.Errorf("embedding provider unavailable")
	}

	localItemByPath := map[string]map[string]any{}
	if localItems, ok := payload["localItems"].([]any); ok {
		for _, item := range localItems {
			if asMap, mapOK := item.(map[string]any); mapOK {
				relativePath := strings.TrimSpace(m.asString(asMap["relativePath"]))
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
	indexedChunks, payloadBytes := 0, 0
	now := time.Now().UTC()

	for keyIndex, objectKey := range objectKeys {
		key := strings.TrimSpace(objectKey)
		if key == "" {
			continue
		}
		input := &s3.GetObjectInput{Bucket: &m.UploadBucket, Key: &key}
		object, err := m.ObjectStore.GetObject(ctx, input)
		if err != nil {
			return 0, 0, fmt.Errorf("read object %q: %w", key, err)
		}
		payloadData, readErr := io.ReadAll(io.LimitReader(object.Body, m.MaxObjectBytes))
		_ = object.Body.Close()
		if readErr != nil {
			return 0, 0, fmt.Errorf("read object body %q: %w", key, readErr)
		}
		payloadBytes += len(payloadData)

		relativePath := key
		if idx := strings.LastIndex(key, "/"); idx >= 0 && idx+1 < len(key) {
			relativePath = key[idx+1:]
		}
		personHint := m.inferPersonHint(relativePath)

		declaredType := ""
		for rel, item := range localItemByPath {
			if strings.HasSuffix(key, rel) || rel == relativePath {
				declaredType = strings.TrimSpace(m.asString(item["type"]))
				break
			}
		}

		sourceURI := fallbackSourceURI
		if sourceURI == "" {
			sourceURI = "s3://" + m.UploadBucket + "/" + key
		}

		text := ""
		if m.isPDFContent(declaredType, key) {
			text = m.extractPDFSearchableText(payloadData)
		} else if m.isTextLikeContent(declaredType, key) {
			text = m.extractSearchableText(payloadData)
		}
		if text == "" {
			text = "uploaded document " + relativePath + " for tenant " + string(tenantID)
			if declaredType != "" {
				text += " (" + declaredType + ")"
			}
		}
		chunks := util.ChunkText(text, m.ChunkSizeChars, m.ChunkOverlapWords)
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
	vectors, err := m.Embedder.Embed(ctx, tenantID, chunkTexts)
	if err != nil {
		return 0, 0, fmt.Errorf("embed chunks: %w", err)
	}
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
	if err := m.Index.Upsert(ctx, records); err != nil {
		return 0, 0, err
	}
	return indexedChunks, payloadBytes, nil
}

// Helpers
func (m *IngestionManager) asString(v any) string {
	s, _ := v.(string)
	return s
}

func (m *IngestionManager) hashHex(v string) string {
	return util.RandomString(12)
}

func (m *IngestionManager) inferTotalFiles(payload map[string]any) int {
	items, ok := payload["localItems"].([]any)
	if !ok || len(items) == 0 {
		return 1
	}
	return len(items)
}

func (m *IngestionManager) readStringArray(value any) []string {
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

func (m *IngestionManager) extractPDFSearchableText(payload []byte) string {
	return util.ExtractPDFSearchableText(payload)
}

func (m *IngestionManager) inferPersonHint(relativePath string) string {
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
	base = m.splitCamelCase(base)
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

func (m *IngestionManager) splitCamelCase(input string) string {
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

func (m *IngestionManager) isTextLikeContent(contentType string, objectKey string) bool {
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

func (m *IngestionManager) isPDFContent(contentType string, objectKey string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(ct, "pdf") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(objectKey)), ".pdf")
}

func (m *IngestionManager) extractSearchableText(payload []byte) string {
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
	if util.IsLikelyNoisyText(text) {
		return ""
	}
	if len(text) > 12000 {
		return text[:12000]
	}
	return text
}

