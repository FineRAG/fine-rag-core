package backend

import (
	"net/http"

	"enterprise-go-rag/backend/util"
	"enterprise-go-rag/backend/util/apiutil"
)

func MainAPIHandler(s *util.Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Domain-driven route registrations (One-liners)
	mux.HandleFunc("POST /api/v1/auth/login", s.Auth.HandleLogin)
	mux.Handle("GET /api/v1/tenants", s.WithAuth(http.HandlerFunc(s.Tenants.HandleListTenants)))
	mux.Handle("POST /api/v1/tenants", s.WithAuth(http.HandlerFunc(s.Tenants.HandleCreateTenant)))
	mux.Handle("GET /api/v1/knowledge-bases", s.WithAuth(s.WithTenant(http.HandlerFunc(s.KB.HandleKnowledgeBases))))
	mux.Handle("GET /api/v1/tenants/{tenantId}/vector-stats", s.WithAuth(s.WithTenant(http.HandlerFunc(s.KB.HandleVectorStats))))
	mux.Handle("POST /api/v1/uploads/presign", s.WithAuth(s.WithTenant(http.HandlerFunc(s.Uploads.HandlePresign))))
	mux.Handle("POST /api/v1/ingestion/jobs", s.WithAuth(s.WithTenant(http.HandlerFunc(s.Ingestion.HandleSubmitJob))))
	mux.Handle("GET /api/v1/ingestion/jobs", s.WithAuth(s.WithTenant(http.HandlerFunc(s.Ingestion.HandleListJobs))))
	mux.Handle("GET /api/v1/ingestion/jobs/stream", s.WithAuth(s.WithTenant(http.HandlerFunc(s.Ingestion.HandleJobStream))))
	mux.Handle("POST /api/v1/ingestion/jobs/{jobId}/retry", s.WithAuth(s.WithTenant(http.HandlerFunc(s.Ingestion.HandleRetryJob))))
	mux.Handle("POST /api/v1/search", s.WithAuth(s.WithTenant(http.HandlerFunc(s.Search.HandleSearch))))
	mux.Handle("POST /api/v1/search/stream", s.WithAuth(s.WithTenant(http.HandlerFunc(s.Search.HandleSearchStream))))
	mux.Handle("POST /api/v1/tenants/{tenantId}/purge", s.WithAuth(s.WithTenant(http.HandlerFunc(s.Tenants.HandlePurgeTenantData))))

	return s.WithAccessLog(s.WithCORS(s.WithRequestID(mux)))
}
