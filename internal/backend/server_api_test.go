package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"enterprise-go-rag/internal/adapters/gateway/portkey"
	vectorstub "enterprise-go-rag/internal/adapters/vector/stub"
	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/services/retrieval"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newTestServer(t *testing.T) (*Server, sqlmock.Sqlmock, *vectorstub.Adapter) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	adapter := vectorstub.NewAdapter()
	retrievalSvc := retrieval.NewDeterministicRetrievalService(adapter, portkey.NewStubReranker(), retrieval.Config{})
	srv, err := NewServer(Config{
		Addr:            ":8080",
		JWTSecret:       "test-secret",
		TokenTTL:        time.Hour,
		AllowedOrigins:  []string{"http://localhost:14173"},
		RateLimitPerMin: 1000,
		UploadBaseURL:   "http://minio:9000",
		UploadBucket:    "finerag-ingestion",
	}, db, nil, retrievalSvc)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	return srv, mock, adapter
}

func requestJSON(t *testing.T, handler http.Handler, method string, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body failed: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestLoginWithPasswordReturnsToken(t *testing.T) {
	srv, mock, _ := newTestServer(t)
	h := srv.Handler()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, password_hash, api_key_hash, active FROM app_users WHERE username = $1`)).
		WithArgs("admin").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "password_hash", "api_key_hash", "active"}).
			AddRow(int64(1), "admin", HashSecret("sk-1234"), HashSecret("sk-1234"), true))

	rr := requestJSON(t, h, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "admin",
		"password": "sk-1234",
	}, nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "token") {
		t.Fatalf("expected token in response, got: %s", rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestListTenantsRequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	h := srv.Handler()

	rr := requestJSON(t, h, http.MethodGet, "/api/v1/tenants", nil, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestListTenantsWithAuthReturnsTenants(t *testing.T) {
	srv, mock, _ := newTestServer(t)
	h := srv.Handler()

	token, err := srv.signJWT(authClaims{Sub: "admin", UID: 42, Iat: time.Now().Unix(), Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT t.tenant_id, t.display_name
FROM tenant_registry t
JOIN user_tenants ut ON ut.tenant_id = t.tenant_id
WHERE ut.user_id = $1 AND t.active = TRUE
ORDER BY t.updated_at DESC`)).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "display_name"}).AddRow("tenant-a", "Tenant A"))

	rr := requestJSON(t, h, http.MethodGet, "/api/v1/tenants", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "tenant-a") {
		t.Fatalf("expected tenant response, got: %s", rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestPresignUploadsWithTenantGuard(t *testing.T) {
	srv, mock, _ := newTestServer(t)
	h := srv.Handler()

	token, err := srv.signJWT(authClaims{Sub: "admin", UID: 7, Iat: time.Now().Unix(), Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM user_tenants WHERE user_id = $1 AND tenant_id = $2)`)).
		WithArgs(int64(7), "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	rr := requestJSON(t, h, http.MethodPost, "/api/v1/uploads/presign", map[string]any{
		"files": []map[string]any{{"name": "a.txt", "relativePath": "docs/a.txt", "type": "text/plain"}},
	}, map[string]string{
		"Authorization": "Bearer " + token,
		"X-Tenant-ID":   "tenant-a",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "uploads") || !strings.Contains(rr.Body.String(), "tenant-a") {
		t.Fatalf("expected uploads payload, got: %s", rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestSearchStreamReturnsDoneEvent(t *testing.T) {
	srv, mock, adapter := newTestServer(t)
	h := srv.Handler()

	token, err := srv.signJWT(authClaims{Sub: "admin", UID: 8, Iat: time.Now().Unix(), Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	record := contracts.VectorRecord{
		RecordID:   "r1",
		TenantID:   "tenant-a",
		JobID:      "job-1",
		ChunkText:  "hello world enterprise search",
		Embedding:  []float32{0.1, 0.2},
		Metadata:   map[string]string{"source": "test"},
		IndexedAt:  time.Now().UTC(),
		SourceURI:  "s3://tenant-a/docs/a.txt",
		Checksum:   "abc",
		RetryCount: 0,
	}
	if err := adapter.Upsert(context.Background(), []contracts.VectorRecord{record}); err != nil {
		t.Fatalf("seed vector record failed: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM user_tenants WHERE user_id = $1 AND tenant_id = $2)`)).
		WithArgs(int64(8), "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	rr := requestJSON(t, h, http.MethodPost, "/api/v1/search/stream", map[string]any{
		"queryText": "hello",
	}, map[string]string{
		"Authorization": "Bearer " + token,
		"X-Tenant-ID":   "tenant-a",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"type":"done"`) {
		t.Fatalf("expected done event in stream, got: %s", rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}
