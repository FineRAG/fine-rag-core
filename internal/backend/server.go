package backend

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/repository"
	"enterprise-go-rag/internal/runtime"
	"enterprise-go-rag/internal/services/retrieval"
)

type Config struct {
	Addr            string
	JWTSecret       string
	TokenTTL        time.Duration
	AllowedOrigins  []string
	RateLimitPerMin int
	UploadBaseURL   string
	UploadBucket    string
}

type Server struct {
	cfg       Config
	db        *sql.DB
	auditRepo contracts.AuditEventRepository
	retrieval *retrieval.DeterministicRetrievalService
	origins   map[string]struct{}
	limiter   *windowLimiter
}

type authClaims struct {
	Sub string `json:"sub"`
	UID int64  `json:"uid"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

type contextKey string

const userIDKey contextKey = "uid"

func ConfigFromEnv() Config {
	ttl := 8 * time.Hour
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_TOKEN_TTL")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			ttl = parsed
		}
	}
	limit := 120
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_RATE_LIMIT_PER_MIN")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	origins := splitCSV(os.Getenv("FINE_RAG_ALLOWED_ORIGINS"))
	if len(origins) == 0 {
		origins = []string{"https://finer.shafeeq.dev", "https://dash-finer.shafeeq.dev", "http://localhost:14173", "http://localhost:14174"}
	}
	return Config{
		Addr:            envOr("FINE_RAG_HTTP_ADDR", ":8080"),
		JWTSecret:       strings.TrimSpace(os.Getenv("FINE_RAG_JWT_SECRET")),
		TokenTTL:        ttl,
		AllowedOrigins:  origins,
		RateLimitPerMin: limit,
		UploadBaseURL:   strings.TrimSpace(os.Getenv("FINE_RAG_MINIO_PUBLIC_BASE_URL")),
		UploadBucket:    envOr("FINE_RAG_MINIO_BUCKET", "finerag-ingestion"),
	}
}

func NewServer(cfg Config, db *sql.DB, auditRepo contracts.AuditEventRepository, retrievalSvc *retrieval.DeterministicRetrievalService) (*Server, error) {
	if strings.TrimSpace(cfg.JWTSecret) == "" {
		return nil, errors.New("FINE_RAG_JWT_SECRET is required")
	}
	origins := map[string]struct{}{}
	for _, origin := range cfg.AllowedOrigins {
		if t := strings.TrimSpace(origin); t != "" {
			origins[t] = struct{}{}
		}
	}
	return &Server{
		cfg:       cfg,
		db:        db,
		auditRepo: auditRepo,
		retrieval: retrievalSvc,
		origins:   origins,
		limiter:   newWindowLimiter(cfg.RateLimitPerMin),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.Handle("GET /api/v1/tenants", s.withAuth(http.HandlerFunc(s.handleListTenants)))
	mux.Handle("POST /api/v1/tenants", s.withAuth(http.HandlerFunc(s.handleCreateTenant)))
	mux.Handle("GET /api/v1/knowledge-bases", s.withAuth(s.withTenant(http.HandlerFunc(s.handleKnowledgeBases))))
	mux.Handle("GET /api/v1/tenants/{tenantId}/vector-stats", s.withAuth(s.withTenant(http.HandlerFunc(s.handleVectorStats))))
	mux.Handle("POST /api/v1/uploads/presign", s.withAuth(s.withTenant(http.HandlerFunc(s.handlePresign))))
	mux.Handle("POST /api/v1/ingestion/jobs", s.withAuth(s.withTenant(http.HandlerFunc(s.handleSubmitJob))))
	mux.Handle("GET /api/v1/ingestion/jobs", s.withAuth(s.withTenant(http.HandlerFunc(s.handleListJobs))))
	mux.Handle("GET /api/v1/ingestion/jobs/stream", s.withAuth(s.withTenant(http.HandlerFunc(s.handleJobStream))))
	mux.Handle("POST /api/v1/ingestion/jobs/{jobId}/retry", s.withAuth(s.withTenant(http.HandlerFunc(s.handleRetryJob))))
	mux.Handle("POST /api/v1/search", s.withAuth(s.withTenant(http.HandlerFunc(s.handleSearch))))
	mux.Handle("POST /api/v1/search/stream", s.withAuth(s.withTenant(http.HandlerFunc(s.handleSearchStream))))
	return s.withCORS(mux)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			if _, ok := s.origins[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, X-Tenant-ID")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			writeError(w, http.StatusUnauthorized, "auth_required", "bearer token is required")
			return
		}
		token := strings.TrimSpace(authz[len("Bearer "):])
		claims, err := s.verifyJWT(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "auth_invalid", "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, claims.UID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) withTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "tenant_required", "X-Tenant-ID is required")
			return
		}
		uid, ok := userIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "auth_required", "missing user context")
			return
		}
		allowed, err := s.userHasTenant(r.Context(), uid, tenantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "tenant_check_failed", err.Error())
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "tenant_forbidden", "tenant not assigned")
			return
		}
		if !s.limiter.Allow(tenantID + ":" + r.URL.Path) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func BuildRuntimeDependencies() (*sql.DB, contracts.AuditEventRepository, *retrieval.DeterministicRetrievalService, error) {
	dbCfg := runtime.LoadDatabaseConfigFromEnv(os.LookupEnv)
	dbCfg.Provider = "postgres"
	db, err := runtime.OpenPostgresDB(context.Background(), nil, dbCfg)
	if err != nil {
		return nil, nil, nil, err
	}
	auditRepo := repository.NewPostgresAuditEventRepository(db, repository.PostgresConfig{})
	vectorCfg := runtime.LoadVectorConfigFromEnv(os.LookupEnv)
	_, searcher, _, err := runtime.BuildVectorAdapters(vectorCfg)
	if err != nil {
		return nil, nil, nil, err
	}
	gatewayCfg := runtime.LoadGatewayConfigFromEnv(os.LookupEnv)
	reranker, _, err := runtime.BuildGatewayReranker(gatewayCfg, func() int64 { return time.Now().UTC().UnixMilli() })
	if err != nil {
		return nil, nil, nil, err
	}
	return db, auditRepo, retrieval.NewDeterministicRetrievalService(searcher, reranker, retrieval.Config{}), nil
}

func (s *Server) EnsureBootstrapData(ctx context.Context) error {
	user := envOr("FINE_RAG_BOOTSTRAP_ADMIN_USERNAME", "admin")
	pass := envOr("FINE_RAG_BOOTSTRAP_ADMIN_PASSWORD", "sk-1234")
	apiKey := envOr("FINE_RAG_BOOTSTRAP_ADMIN_API_KEY", "sk-1234")
	tenantID := envOr("FINE_RAG_BOOTSTRAP_TENANT_ID", "tenant-a")
	tenantName := envOr("FINE_RAG_BOOTSTRAP_TENANT_NAME", "Tenant A")
	var userID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM app_users WHERE username = $1`, user).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.db.QueryRowContext(ctx, `INSERT INTO app_users (username, password_hash, api_key_hash, active) VALUES ($1,$2,$3,TRUE) RETURNING id`, user, HashSecret(pass), HashSecret(apiKey)).Scan(&userID)
	}
	if err != nil {
		return err
	}
	repo := repository.NewPostgresTenantRegistryRepository(s.db, repository.PostgresConfig{})
	tctx, err := contracts.WithTenantContext(ctx, contracts.TenantContext{TenantID: contracts.TenantID(tenantID), RequestID: "bootstrap"})
	if err != nil {
		return err
	}
	if err := repo.Upsert(tctx, contracts.TenantRecord{TenantID: contracts.TenantID(tenantID), DisplayName: tenantName, PlanTier: "starter", Active: true, UpdatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_tenants (user_id, tenant_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, tenantID)
	return err
}

func HashSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256$" + hex.EncodeToString(sum[:])
}

func (s *Server) signJWT(claims authClaims) (string, error) {
	h, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	c, _ := json.Marshal(claims)
	left := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	sig := hmacSHA256([]byte(s.cfg.JWTSecret), []byte(left))
	return left + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *Server) verifyJWT(token string) (authClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return authClaims{}, errors.New("invalid token")
	}
	left := parts[0] + "." + parts[1]
	want := hmacSHA256([]byte(s.cfg.JWTSecret), []byte(left))
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return authClaims{}, err
	}
	if !hmac.Equal(want, got) {
		return authClaims{}, errors.New("signature mismatch")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return authClaims{}, err
	}
	var claims authClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return authClaims{}, err
	}
	if claims.Exp <= time.Now().UTC().Unix() {
		return authClaims{}, errors.New("expired")
	}
	return claims, nil
}

func verifySecret(raw string, stored string) bool {
	if strings.TrimSpace(stored) == "" {
		return false
	}
	if strings.HasPrefix(stored, "sha256$") {
		return hmac.Equal([]byte(HashSecret(raw)), []byte(stored))
	}
	return hmac.Equal([]byte(raw), []byte(stored))
}

func hmacSHA256(secret []byte, data []byte) []byte {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func userIDFromContext(ctx context.Context) (int64, bool) {
	uid, ok := ctx.Value(userIDKey).(int64)
	return uid, ok
}

func decodeJSON(body io.ReadCloser, out any) error {
	defer body.Close()
	d := json.NewDecoder(io.LimitReader(body, 2<<20))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode string, message string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": message}})
}

func randomString(n int) string {
	if n <= 0 {
		n = 8
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)[:n]
}

func envOr(key string, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitCSV(raw string) []string {
	out := make([]string, 0)
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

type windowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	state  map[string]windowState
}

type windowState struct {
	start time.Time
	count int
}

func newWindowLimiter(limit int) *windowLimiter {
	if limit <= 0 {
		limit = 120
	}
	return &windowLimiter{limit: limit, window: time.Minute, state: map[string]windowState{}}
}

func (l *windowLimiter) Allow(key string) bool {
	now := time.Now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	cur := l.state[key]
	if cur.start.IsZero() || now.Sub(cur.start) >= l.window {
		l.state[key] = windowState{start: now, count: 1}
		return true
	}
	if cur.count >= l.limit {
		return false
	}
	cur.count++
	l.state[key] = cur
	return true
}
