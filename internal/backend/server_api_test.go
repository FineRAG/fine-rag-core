package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

type testLLMAnswerGenerator struct{}

func (testLLMAnswerGenerator) GenerateAnswer(_ context.Context, query string, contextText string) (string, error) {
	trimmedContext := strings.TrimSpace(contextText)
	if trimmedContext == "" {
		return "", fmt.Errorf("empty context")
	}
	return "answer for: " + strings.TrimSpace(query), nil
}

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
		MinIOEndpoint:   "http://minio:9000",
		UploadBucket:    "finerag-ingestion",
		MinIOAccessKey:  "minioadmin",
		MinIOSecretKey:  "minioadmin123",
		MinIORegion:     "us-east-1",
		PresignTTL:      5 * time.Minute,
		MaxObjectBytes:  20 * 1024 * 1024,
	}, db, nil, retrievalSvc, adapter)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	srv.llm = testLLMAnswerGenerator{}
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

func TestExtractPDFFlateStreamsNoPanicOnUnicodePayload(t *testing.T) {
	payload := append([]byte("\xC4\xB0"), []byte("stream\nnot-zlib\nendstream")...)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("extractPDFFlateStreams panicked: %v", r)
		}
	}()

	streams := extractPDFFlateStreams(payload)
	if streams == nil {
		t.Fatalf("expected non-nil streams slice")
	}
}

func TestToASCIILowerPreservesLength(t *testing.T) {
	in := append([]byte{0xC4, 0xB0}, []byte("STREAM\nxx\nENDSTREAM")...)
	out := toASCIILower(in)
	if len(out) != len(in) {
		t.Fatalf("expected same length, got in=%d out=%d", len(in), len(out))
	}
	if string(out[2:8]) != "stream" {
		t.Fatalf("expected ASCII lower conversion to apply, got %q", string(out[2:8]))
	}
}

func TestExtractPDFSearchableText_FromAttachedResume(t *testing.T) {
	payload, err := os.ReadFile("../../RounakPoddar-Resume.pdf")
	if err != nil {
		t.Fatalf("read resume fixture: %v", err)
	}
	text := strings.ToLower(extractPDFSearchableText(payload))
	if text == "" {
		t.Fatalf("expected extracted text, got empty")
	}
	if !strings.Contains(text, "rounak") && !strings.Contains(text, "poddar") && !strings.Contains(text, "gmail") {
		t.Fatalf("expected resume-identifying text in extraction, got: %.200q", text)
	}
}

func TestExtractPDFSearchableText_FromShafeeqResume(t *testing.T) {
	payload, err := os.ReadFile("../../Shafeeq-Resume-Mar-2026.pdf")
	if err != nil {
		t.Fatalf("read shafeeq resume fixture: %v", err)
	}
	text := strings.ToLower(extractPDFSearchableText(payload))
	if text == "" {
		t.Fatalf("expected extracted text, got empty")
	}
	if !strings.Contains(text, "shafeeq") && !strings.Contains(text, "@") && !strings.Contains(text, "gmail") {
		t.Fatalf("expected shafeeq resume-identifying text in extraction, got: %.200q", text)
	}
}

func TestNormalizeExtractedTextCompactsSpacedGlyphRuns(t *testing.T) {
	raw := "S H A F E E Q U L I S L A M i a m s h a f e e q u l @ g m a i l . c o m"
	normalized := normalizeExtractedText(raw)
	lower := strings.ToLower(normalized)
	if !strings.Contains(lower, "shafeequlislam") {
		t.Fatalf("expected compacted person name, got: %q", normalized)
	}
	if !strings.Contains(lower, "iamshafeequl@gmail.com") {
		t.Fatalf("expected compacted email, got: %q", normalized)
	}
}

func TestBuildContextPartFallbackKeepsReadableChunk(t *testing.T) {
	raw := "S H A F E E Q U L I S L A M + 9 1 - 9 0 4 5 6 2 7 6 0 2 # i a m s h a f e e q u l @ g m a i l . c o m"
	got := buildContextPart("what is shafeeq email", raw)
	if strings.TrimSpace(got) == "" {
		t.Fatalf("expected non-empty context part for readable chunk")
	}
	lower := strings.ToLower(got)
	if !(strings.Contains(lower, "shaf") || strings.Contains(lower, "s h a f")) {
		t.Fatalf("expected preserved contact signal, got: %q", got)
	}
	if !strings.Contains(lower, "@") {
		t.Fatalf("expected preserved email marker, got: %q", got)
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
		"files": []map[string]any{{"name": "a.txt", "size": 12, "relativePath": "docs/a.txt", "type": "text/plain"}},
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
	if !strings.Contains(rr.Body.String(), "expiresInSeconds") || !strings.Contains(rr.Body.String(), "300") {
		t.Fatalf("expected 5 minute presign ttl in response, got: %s", rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestPresignRejectsObjectLargerThan20MB(t *testing.T) {
	srv, mock, _ := newTestServer(t)
	h := srv.Handler()

	token, err := srv.signJWT(authClaims{Sub: "admin", UID: 10, Iat: time.Now().Unix(), Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM user_tenants WHERE user_id = $1 AND tenant_id = $2)`)).
		WithArgs(int64(10), "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	rr := requestJSON(t, h, http.MethodPost, "/api/v1/uploads/presign", map[string]any{
		"files": []map[string]any{{"name": "big.pdf", "size": 20*1024*1024 + 1, "relativePath": "big.pdf", "type": "application/pdf"}},
	}, map[string]string{
		"Authorization": "Bearer " + token,
		"X-Tenant-ID":   "tenant-a",
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "object_too_large") {
		t.Fatalf("expected object_too_large error, got: %s", rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}

func TestSubmitJobAllowsMissingChecksum(t *testing.T) {
	srv, mock, _ := newTestServer(t)
	h := srv.Handler()

	token, err := srv.signJWT(authClaims{Sub: "admin", UID: 9, Iat: time.Now().Unix(), Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM user_tenants WHERE user_id = $1 AND tenant_id = $2)`)).
		WithArgs(int64(9), "tenant-a").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO ingestion_jobs (job_id, tenant_id, source_uri, checksum, status, stage, processed_files, total_files, successful_files, failed_files, policy_code, policy_reason, source_mode, payload_json, chunk_count, payload_bytes, submitted_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,$16,$17,$18)`)).
		WithArgs(
			sqlmock.AnyArg(),
			"tenant-a",
			"s3://tenant-a-ap-south-1/docs/new.txt",
			sqlmock.AnyArg(),
			"queued",
			"cleanup",
			0,
			1,
			0,
			0,
			"",
			"",
			"uri",
			sqlmock.AnyArg(),
			1,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rr := requestJSON(t, h, http.MethodPost, "/api/v1/ingestion/jobs", map[string]any{
		"sourceMode": "uri",
		"sourceUri":  "s3://tenant-a-ap-south-1/docs/new.txt",
	}, map[string]string{
		"Authorization": "Bearer " + token,
		"X-Tenant-ID":   "tenant-a",
	})

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "jobId") {
		t.Fatalf("expected job response, got: %s", rr.Body.String())
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
	if !strings.Contains(rr.Body.String(), `"topVectors"`) || !strings.Contains(rr.Body.String(), `"type":"top_vectors"`) {
		t.Fatalf("expected top vectors event/payload in stream, got: %s", rr.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations failed: %v", err)
	}
}
