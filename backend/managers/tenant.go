package managers

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	util "enterprise-go-rag/backend/util/apiutil"
	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/repository"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type TenantManager struct {
	DB          *sql.DB
	ObjectStore *s3.Client
	Index       contracts.VectorIndex
	Bucket      string
}

type vectorTenantPurger interface {
	PurgeTenant(ctx context.Context, tenantID contracts.TenantID) error
}

func (m *TenantManager) HandleListTenants(w http.ResponseWriter, r *http.Request) {
	uid, ok := util.UserIDFromContext(r.Context())
	if !ok {
		util.WriteError(w, http.StatusUnauthorized, "auth_required", "missing user context")
		return
	}
	rows, err := m.DB.QueryContext(r.Context(), `SELECT t.tenant_id, t.display_name
FROM tenant_registry t
JOIN user_tenants ut ON ut.tenant_id = t.tenant_id
WHERE ut.user_id = $1 AND t.active = TRUE
ORDER BY t.updated_at DESC`, uid)
	if err != nil {
		util.WriteError(w, http.StatusInternalServerError, "tenant_list_failed", err.Error())
		return
	}
	defer rows.Close()
	out := make([]map[string]string, 0)
	for rows.Next() {
		var tenantID, displayName string
		if err := rows.Scan(&tenantID, &displayName); err != nil {
			util.WriteError(w, http.StatusInternalServerError, "tenant_scan_failed", err.Error())
			return
		}
		out = append(out, map[string]string{"tenantId": tenantID, "displayName": displayName})
	}
	util.WriteJSON(w, http.StatusOK, out)
}

func (m *TenantManager) HandleCreateTenant(w http.ResponseWriter, r *http.Request) {
	uid, ok := util.UserIDFromContext(r.Context())
	if !ok {
		util.WriteError(w, http.StatusUnauthorized, "auth_required", "missing user context")
		return
	}
	var req struct {
		TenantID    string `json:"tenantId"`
		DisplayName string `json:"displayName"`
		PlanTier    string `json:"planTier"`
	}
	if err := util.DecodeJSON(r.Body, &req); err != nil {
		util.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.TenantID == "" || req.DisplayName == "" {
		util.WriteError(w, http.StatusBadRequest, "invalid_tenant", "tenantId and displayName are required")
		return
	}
	if strings.TrimSpace(req.PlanTier) == "" {
		req.PlanTier = "starter"
	}
	repo := repository.NewPostgresTenantRegistryRepository(m.DB, repository.PostgresConfig{})
	ctx, err := contracts.WithTenantContext(r.Context(), contracts.TenantContext{TenantID: contracts.TenantID(req.TenantID), RequestID: "tenant-create"})
	if err != nil {
		util.WriteError(w, http.StatusBadRequest, "tenant_context_invalid", err.Error())
		return
	}
	if err := repo.Upsert(ctx, contracts.TenantRecord{TenantID: contracts.TenantID(req.TenantID), DisplayName: req.DisplayName, PlanTier: req.PlanTier, Active: true, UpdatedAt: time.Now().UTC()}); err != nil {
		util.WriteError(w, http.StatusInternalServerError, "tenant_create_failed", err.Error())
		return
	}
	if _, err := m.DB.ExecContext(r.Context(), `INSERT INTO user_tenants (user_id, tenant_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, req.TenantID); err != nil {
		util.WriteError(w, http.StatusInternalServerError, "tenant_assign_failed", err.Error())
		return
	}
	util.WriteJSON(w, http.StatusCreated, map[string]string{"tenantId": req.TenantID, "displayName": req.DisplayName})
}

func (m *TenantManager) HandlePurgeTenantData(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	pathTenant := strings.TrimSpace(r.PathValue("tenantId"))
	if tenantID == "" || pathTenant == "" || tenantID != pathTenant {
		util.WriteError(w, http.StatusBadRequest, "tenant_mismatch", "tenant header and path must match")
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := util.DecodeJSON(r.Body, &req); err != nil {
		util.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.ToUpper(strings.TrimSpace(req.Confirm)) != "PURGE" {
		util.WriteError(w, http.StatusBadRequest, "confirmation_required", "set confirm to PURGE")
		return
	}

	deletedObjects := 0
	prefix := tenantID + "/"
	p := s3.NewListObjectsV2Paginator(m.ObjectStore, &s3.ListObjectsV2Input{
		Bucket: &m.Bucket,
		Prefix: &prefix,
	})
	for p.HasMorePages() {
		page, err := p.NextPage(r.Context())
		if err != nil {
			util.WriteError(w, http.StatusInternalServerError, "object_store_list_failed", err.Error())
			return
		}
		for _, item := range page.Contents {
			if item.Key == nil || strings.TrimSpace(*item.Key) == "" {
				continue
			}
			_, delErr := m.ObjectStore.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: &m.Bucket, Key: item.Key})
			if delErr != nil {
				util.WriteError(w, http.StatusInternalServerError, "object_store_delete_failed", delErr.Error())
				return
			}
			deletedObjects++
		}
	}

	if purger, ok := m.Index.(vectorTenantPurger); ok {
		if err := purger.PurgeTenant(r.Context(), contracts.TenantID(tenantID)); err != nil {
			util.WriteError(w, http.StatusInternalServerError, "vector_purge_failed", err.Error())
			return
		}
	}

	if _, err := m.DB.ExecContext(r.Context(), `DELETE FROM ingestion_jobs WHERE tenant_id = $1`, tenantID); err != nil {
		util.WriteError(w, http.StatusInternalServerError, "job_purge_failed", err.Error())
		return
	}

	util.WriteJSON(w, http.StatusOK, map[string]any{
		"status":         "purged",
		"tenantId":       tenantID,
		"deletedObjects": deletedObjects,
	})
}
