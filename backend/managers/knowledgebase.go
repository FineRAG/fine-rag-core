package managers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	util "enterprise-go-rag/backend/util/apiutil"
	"enterprise-go-rag/internal/logging"

	"go.uber.org/zap"
)

type KnowledgeBaseManager struct {
	DB *sql.DB
}

func (m *KnowledgeBaseManager) HandleKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	rows, err := m.DB.QueryContext(r.Context(), `SELECT source_uri, COALESCE(MAX(status), 'queued'), CAST(COUNT(*) AS bigint), COALESCE(CAST(SUM(chunk_count) AS bigint), 0), MAX(submitted_at)
FROM ingestion_jobs
WHERE tenant_id = $1
GROUP BY source_uri
ORDER BY MAX(submitted_at) DESC`, tenantID)
	if err != nil {
		logging.Logger.Error("knowledge_bases_failed", zap.Error(err), zap.String("tenantID", tenantID))
		util.WriteError(w, http.StatusInternalServerError, "knowledge_bases_failed", err.Error())
		return
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	for rows.Next() {
		var sourceURI, status string
		var documentCount, chunkCount int64
		var last sql.NullTime
		if err := rows.Scan(&sourceURI, &status, &documentCount, &chunkCount, &last); err != nil {
			logging.Logger.Error("knowledge_bases_scan_failed", zap.Error(err), zap.String("tenantID", tenantID))
			util.WriteError(w, http.StatusInternalServerError, "knowledge_bases_scan_failed", err.Error())
			return
		}
		item := map[string]any{
			"knowledgeBaseId": m.hashHex(sourceURI),
			"name":            sourceURI,
			"status":          m.mapJobStatus(status),
			"documentCount":   documentCount,
			"chunkCount":      chunkCount,
		}
		if last.Valid {
			item["lastIngestedAt"] = last.Time.UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	util.WriteJSON(w, http.StatusOK, out)
}

func (m *KnowledgeBaseManager) HandleVectorStats(w http.ResponseWriter, r *http.Request) {
	tenantPath := strings.TrimSpace(r.PathValue("tenantId"))
	tenantHeader := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantPath != tenantHeader {
		util.WriteError(w, http.StatusForbidden, "tenant_mismatch", "path tenant must match header")
		return
	}
	var count int64
	var vCount, sBytes sql.NullInt64
	err := m.DB.QueryRowContext(r.Context(), `SELECT COUNT(*), SUM(chunk_count), SUM(payload_bytes) FROM ingestion_jobs WHERE tenant_id = $1`, tenantPath).Scan(&count, &vCount, &sBytes)
	if err != nil {
		logging.Logger.Error("vector_stats_failed", zap.Error(err), zap.String("tenantID", tenantPath))
		util.WriteError(w, http.StatusInternalServerError, "vector_stats_failed", err.Error())
		return
	}
	util.WriteJSON(w, http.StatusOK, map[string]any{
		"vectorCount":  vCount.Int64,
		"storageBytes": sBytes.Int64,
		"updatedAt":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (m *KnowledgeBaseManager) hashHex(v string) string {
	return util.RandomString(8) // Simplified or reuse hashHex logic
}

func (m *KnowledgeBaseManager) mapJobStatus(status string) string {
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

func (m *KnowledgeBaseManager) parseInt64Field(raw string) (int64, error) {
	// Re-implement or move to util
	return 0, nil // Placeholder
}
