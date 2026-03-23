package managers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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
	Parser            contracts.DocumentParser
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
	
	logging.Logger.Info("queuing ingestion job", zap.String("jobID", jobID), zap.String("tenantID", tenantID), zap.String("sourceURI", sourceURI), zap.Int("totalFiles", totalFiles))

	// Insert initial "indexing" status
	_, err := m.DB.ExecContext(r.Context(), `INSERT INTO ingestion_jobs (job_id, tenant_id, source_uri, checksum, status, stage, processed_files, total_files, successful_files, failed_files, policy_code, policy_reason, source_mode, payload_json, chunk_count, payload_bytes, submitted_at, updated_at)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,$16,$17,$18)`, jobID, tenantID, sourceURI, checksum, "indexing", "parsing", 0, totalFiles, 0, 0, "", "", sourceMode, string(payloadJSON), 0, 0, now, now)
	if err != nil {
		logging.Logger.Error("failed to create ingestion job record", zap.Error(err), zap.String("jobID", jobID))
		util.WriteError(w, http.StatusInternalServerError, "job_submit_failed", err.Error())
		return
	}

	// Launch background processing
	go m.processIngestionBackground(jobID, contracts.TenantID(tenantID), checksum, sourceURI, payload)

	util.WriteJSON(w, http.StatusCreated, map[string]any{
		"jobId":           jobID,
		"sourceUri":       sourceURI,
		"status":          "indexing",
		"stage":           "parsing",
		"totalFiles":      totalFiles,
		"submittedAt":     now.Format(time.RFC3339),
	})
}

func (m *IngestionManager) processIngestionBackground(jobID string, tenantID contracts.TenantID, checksum string, sourceURI string, payload map[string]any) {
	// Use background context with a generous timeout for long-running extractions
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	logging.Logger.Info("starting background ingestion processing", zap.String("jobID", jobID), zap.String("tenantID", string(tenantID)))

	chunkCount, payloadBytes, err := m.indexPayloadArtifacts(ctx, tenantID, jobID, checksum, sourceURI, payload)
	
	now := time.Now().UTC()
	status := "indexed"
	stage := "complete"
	reason := ""
	
	if err != nil {
		logging.Logger.Error("background ingestion failed", zap.Error(err), zap.String("jobID", jobID))
		status = "failed"
		stage = "error"
		reason = err.Error()
	} else {
		logging.Logger.Info("background ingestion succeeded", zap.String("jobID", jobID), zap.Int("chunks", chunkCount))
	}

	totalFiles := m.inferTotalFiles(payload)
	processed := totalFiles
	successful := totalFiles
	failed := 0
	if err != nil {
		successful = 0
		failed = totalFiles
	}

	_, dbErr := m.DB.ExecContext(context.Background(), `UPDATE ingestion_jobs 
		SET status = $1, stage = $2, policy_reason = $3, chunk_count = $4, payload_bytes = $5, 
		    processed_files = $6, successful_files = $7, failed_files = $8, updated_at = $9 
		WHERE job_id = $10`, status, stage, reason, chunkCount, payloadBytes, processed, successful, failed, now, jobID)
	
	if dbErr != nil {
		logging.Logger.Error("failed to update ingestion job status", zap.Error(dbErr), zap.String("jobID", jobID))
	}
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
		_, _ = io.WriteString(w, "data: " + string(b) + "\n\n")
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
		logging.Logger.Error("embedding provider unavailable", zap.String("jobID", jobID))
		return 0, 0, errors.New("embedding provider unavailable")
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
		objectKey  string
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
			logging.Logger.Error("read object failed", zap.String("key", key), zap.Error(err), zap.String("jobID", jobID))
			return 0, 0, errors.New("read object " + key + ": " + err.Error())
		}
		
		contentLength := int64(0)
		if object.ContentLength != nil {
			contentLength = *object.ContentLength
		}
		logging.Logger.Info("S3 object metadata", zap.String("key", key), zap.Int64("contentLength", contentLength), zap.Int64("maxObjectBytes", m.MaxObjectBytes))

		payloadData, readErr := io.ReadAll(io.LimitReader(object.Body, m.MaxObjectBytes))
		_ = object.Body.Close()
		if readErr != nil {
			logging.Logger.Error("read object body failed", zap.String("key", key), zap.Error(readErr), zap.String("jobID", jobID))
			return 0, 0, errors.New("read object body " + key + ": " + readErr.Error())
		}
		logging.Logger.Info("read object body success", zap.String("key", key), zap.Int("readBytes", len(payloadData)))
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
		contentType := m.inferContentType(declaredType, key)
		logging.Logger.Info("processing content", zap.String("key", key), zap.String("contentType", contentType), zap.Bool("hasParser", m.Parser != nil))
		
		if m.Parser != nil && m.isComplexContent(contentType) {
			logging.Logger.Info("attempting complex parsing", zap.String("key", key), zap.String("contentType", contentType))
			var pErr error
			text, pErr = m.Parser.Parse(ctx, tenantID, contentType, payloadData)
			if pErr != nil {
				logging.Logger.Warn("complex parsing failed", zap.Error(pErr), zap.String("key", key))
			} else {
				logging.Logger.Info("complex parsing succeeded", zap.String("key", key), zap.Int("textLen", len(text)))
			}
		}

		if text == "" {
			if m.isPDFContent(declaredType, key) {
				logging.Logger.Info("falling back to basic PDF extraction", zap.String("key", key))
				text = m.extractPDFSearchableText(payloadData)
			} else if m.isTextLikeContent(declaredType, key) {
				logging.Logger.Info("falling back to basic text extraction", zap.String("key", key))
				text = m.extractSearchableText(payloadData)
			}
		}
		if text == "" {
			logging.Logger.Warn("all extraction failed, using placeholder", zap.String("key", key))
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
				recordID:   "vec-" + jobID + "-" + strconv.Itoa(keyIndex) + "-" + strconv.Itoa(chunkIndex),
				tenantID:   tenantID,
				jobID:      jobID,
				chunkText:  chunk,
				metadata:   chunkMetadata,
				indexedAt:  now,
				sourceURI:  sourceURI,
				checksum:   checksum,
				retryCount: 0,
				objectKey:  key,
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
		logging.Logger.Error("embed chunks failed", zap.Error(err), zap.String("jobID", jobID))
		return 0, 0, errors.New("embed chunks: " + err.Error())
	}
	if len(vectors) != len(pending) {
		logging.Logger.Error("embedding count mismatch", zap.Int("got", len(vectors)), zap.Int("want", len(pending)), zap.String("jobID", jobID))
		return 0, 0, errors.New("embedding count mismatch")
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
			ObjectKey:  item.objectKey,
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

func (m *IngestionManager) inferContentType(declared string, key string) string {
	if declared != "" {
		return declared
	}
	ext := strings.ToLower(key)
	switch {
	case strings.HasSuffix(ext, ".pdf"):
		return "application/pdf"
	case strings.HasSuffix(ext, ".docx"):
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case strings.HasSuffix(ext, ".doc"):
		return "application/msword"
	case strings.HasSuffix(ext, ".pptx"):
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case strings.HasSuffix(ext, ".ppt"):
		return "application/vnd.ms-powerpoint"
	case strings.HasSuffix(ext, ".png"):
		return "image/png"
	case strings.HasSuffix(ext, ".jpg"), strings.HasSuffix(ext, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(ext, ".txt"):
		return "text/plain"
	case strings.HasSuffix(ext, ".md"):
		return "text/markdown"
	default:
		return "application/octet-stream"
	}
}

func (m *IngestionManager) isComplexContent(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "word") ||
		strings.Contains(ct, "officedocument") ||
		strings.Contains(ct, "powerpoint") ||
		strings.Contains(ct, "image/") ||
		strings.Contains(ct, "pdf")
}

