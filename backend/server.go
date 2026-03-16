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
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/internal/adapters/gateway/portkey"
	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/repository"
	"enterprise-go-rag/internal/runtime"
	"enterprise-go-rag/internal/services/retrieval"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Addr                   string
	JWTSecret              string
	TokenTTL               time.Duration
	AllowedOrigins         []string
	RateLimitPerMin        int
	UploadBaseURL          string
	S3Endpoint             string
	UploadBucket           string
	S3AccessKey            string
	S3SecretKey            string
	S3Region               string
	S3UsePathStyle         bool
	PresignTTL             time.Duration
	MaxObjectBytes         int64
	GatewayProvider        string
	PortkeyBaseURL         string
	PortkeyAPIKey          string
	OpenRouterKey          string
	OpenRouterModel        string
	EnableQueryRewrite     bool
	QueryRewriteModel      string
	EmbeddingModel         string
	EmbeddingFallbackModel string
	LLMTimeout             time.Duration
	OpenRouterMinInterval  time.Duration
	OpenRouterRetryMax     int
	ChunkSizeChars         int
	ChunkOverlapWords      int
}

type Server struct {
	cfg         Config
	db          *sql.DB
	auditRepo   contracts.AuditEventRepository
	retrieval   *retrieval.DeterministicRetrievalService
	index       contracts.VectorIndex
	origins     map[string]struct{}
	limiter     *windowLimiter
	presign     *s3.PresignClient
	objectStore *s3.Client
	llm         llmAnswerGenerator
	embedder    contracts.EmbeddingProvider
}

type llmAnswerGenerator interface {
	GenerateAnswer(ctx context.Context, query string, contextText string) (string, error)
}

func envOrAny(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func envOrSecretAny(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(envOrSecret(key, "")); value != "" {
			return value
		}
	}
	return fallback
}

func envBoolAny(keys []string, fallback bool) bool {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}

type authClaims struct {
	Sub string `json:"sub"`
	UID int64  `json:"uid"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

type contextKey string

const userIDKey contextKey = "uid"
const requestIDKey contextKey = "request_id"

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
		origins = []string{"https://finer.shafeeq.dev", "https://dash-finer.shafeeq.dev"}
	}
	s3UsePathStyle := envBoolAny([]string{"FINE_RAG_S3_USE_PATH_STYLE"}, false)
	if strings.TrimSpace(envOrSecretAny([]string{"FINE_RAG_S3_ENDPOINT"}, "")) != "" {
		s3UsePathStyle = true
	}
	presignTTL := 5 * time.Minute
	if raw := strings.TrimSpace(envOrAny([]string{"FINE_RAG_S3_PRESIGN_TTL"}, "")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			presignTTL = parsed
		}
	}
	maxObjectBytes := int64(20 * 1024 * 1024)
	if raw := strings.TrimSpace(envOrAny([]string{"FINE_RAG_S3_MAX_OBJECT_BYTES"}, "")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			maxObjectBytes = parsed
		}
	}
	llmTimeout := 12 * time.Second
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_LLM_TIMEOUT")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			llmTimeout = parsed
		}
	}
	openRouterMinInterval := 900 * time.Millisecond
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_OPENROUTER_MIN_INTERVAL")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			openRouterMinInterval = parsed
		}
	}
	openRouterRetryMax := 3
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_OPENROUTER_RETRY_MAX")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			openRouterRetryMax = parsed
		}
	}

	chunkSizeChars := 900
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_CHUNK_SIZE_CHARS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			chunkSizeChars = parsed
		}
	}
	chunkOverlapWords := 30
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_CHUNK_OVERLAP_WORDS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			chunkOverlapWords = parsed
		}
	}
	enableQueryRewrite := false
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_ENABLE_QUERY_REWRITE")); raw != "" {
		enableQueryRewrite = strings.EqualFold(raw, "true") || raw == "1" || strings.EqualFold(raw, "yes")
	}

	return Config{
		Addr:                   envOr("FINE_RAG_HTTP_ADDR", ":8080"),
		JWTSecret:              envOrSecret("FINE_RAG_JWT_SECRET", ""),
		TokenTTL:               ttl,
		AllowedOrigins:         origins,
		RateLimitPerMin:        limit,
		UploadBaseURL:          envOrSecretAny([]string{"FINE_RAG_UPLOAD_PUBLIC_BASE_URL", "FINE_RAG_S3_PUBLIC_BASE_URL"}, ""),
		S3Endpoint:             envOrSecretAny([]string{"FINE_RAG_S3_ENDPOINT"}, ""),
		UploadBucket:           envOrAny([]string{"FINE_RAG_S3_BUCKET"}, "finerag-ingestion"),
		S3AccessKey:            envOrSecretAny([]string{"FINE_RAG_S3_ACCESS_KEY"}, ""),
		S3SecretKey:            envOrSecretAny([]string{"FINE_RAG_S3_SECRET_KEY"}, ""),
		S3Region:               envOrAny([]string{"FINE_RAG_S3_REGION"}, "us-east-1"),
		S3UsePathStyle:         s3UsePathStyle,
		PresignTTL:             presignTTL,
		MaxObjectBytes:         maxObjectBytes,
		GatewayProvider:        strings.ToLower(envOr("FINE_RAG_GATEWAY_PROVIDER", "stub")),
		PortkeyBaseURL:         envOr("FINE_RAG_PORTKEY_BASE_URL", "https://api.portkey.ai"),
		PortkeyAPIKey:          envOrSecret("FINE_RAG_PORTKEY_API_KEY", ""),
		OpenRouterKey:          envOrSecret("FINE_RAG_OPENROUTER_API_KEY", ""),
		OpenRouterModel:        envOr("FINE_RAG_OPENROUTER_MODEL", "@groq-key/openai/gpt-oss-120b"),
		EnableQueryRewrite:     enableQueryRewrite,
		QueryRewriteModel:      envOr("FINE_RAG_QUERY_REWRITE_MODEL", "@groq-key/openai/gpt-oss-20b"),
		EmbeddingModel:         envOr("FINE_RAG_OPENROUTER_EMBEDDING_MODEL", "@openrouter-key/nvidia/llama-nemotron-embed-vl-1b-v2:free"),
		EmbeddingFallbackModel: envOr("FINE_RAG_OPENROUTER_EMBEDDING_FALLBACK_MODEL", ""),
		LLMTimeout:             llmTimeout,
		OpenRouterMinInterval:  openRouterMinInterval,
		OpenRouterRetryMax:     openRouterRetryMax,
		ChunkSizeChars:         chunkSizeChars,
		ChunkOverlapWords:      chunkOverlapWords,
	}
}

func NewServer(cfg Config, db *sql.DB, auditRepo contracts.AuditEventRepository, retrievalSvc *retrieval.DeterministicRetrievalService, vectorIndex contracts.VectorIndex) (*Server, error) {
	if strings.TrimSpace(cfg.JWTSecret) == "" {
		return nil, errors.New("FINE_RAG_JWT_SECRET is required")
	}
	if strings.TrimSpace(cfg.UploadBucket) == "" {
		return nil, errors.New("FINE_RAG_S3_BUCKET is required")
	}
	origins := map[string]struct{}{}
	for _, origin := range cfg.AllowedOrigins {
		if t := strings.TrimSpace(origin); t != "" {
			origins[t] = struct{}{}
		}
	}
	publicEndpoint := strings.TrimRight(cfg.UploadBaseURL, "/")
	internalEndpoint := strings.TrimRight(cfg.S3Endpoint, "/")
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.S3Region)}
	if strings.TrimSpace(cfg.S3AccessKey) != "" || strings.TrimSpace(cfg.S3SecretKey) != "" {
		if strings.TrimSpace(cfg.S3AccessKey) == "" || strings.TrimSpace(cfg.S3SecretKey) == "" {
			return nil, errors.New("FINE_RAG_S3_ACCESS_KEY and FINE_RAG_S3_SECRET_KEY must both be set when using static credentials")
		}
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	presignClient := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if publicEndpoint != "" {
			o.BaseEndpoint = &publicEndpoint
		}
		o.UsePathStyle = cfg.S3UsePathStyle
	})
	objectStoreClient := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if internalEndpoint != "" {
			o.BaseEndpoint = &internalEndpoint
		}
		o.UsePathStyle = cfg.S3UsePathStyle
	})
	var answerGenerator llmAnswerGenerator
	var embeddingProvider contracts.EmbeddingProvider
	if cfg.GatewayProvider == "portkey" && strings.TrimSpace(cfg.PortkeyAPIKey) != "" {
		chatClient, chatErr := portkey.NewChatClient(portkey.ChatConfig{
			BaseURL:       cfg.PortkeyBaseURL,
			PortkeyAPIKey: cfg.PortkeyAPIKey,
			OpenRouterKey: cfg.OpenRouterKey,
			Model:         cfg.OpenRouterModel,
			Timeout:       cfg.LLMTimeout,
		})
		if chatErr == nil {
			answerGenerator = chatClient
		}
		embeddingClient, embeddingErr := portkey.NewEmbeddingClient(portkey.EmbeddingConfig{
			BaseURL:       cfg.PortkeyBaseURL,
			PortkeyAPIKey: cfg.PortkeyAPIKey,
			OpenRouterKey: cfg.OpenRouterKey,
			Model:         cfg.EmbeddingModel,
			FallbackModel: cfg.EmbeddingFallbackModel,
			Timeout:       cfg.LLMTimeout,
			MinInterval:   cfg.OpenRouterMinInterval,
			RetryMax:      cfg.OpenRouterRetryMax,
		})
		if embeddingErr == nil {
			embeddingProvider = embeddingClient
		}
	}
	return &Server{
		cfg:         cfg,
		db:          db,
		auditRepo:   auditRepo,
		retrieval:   retrievalSvc,
		index:       vectorIndex,
		origins:     origins,
		limiter:     newWindowLimiter(cfg.RateLimitPerMin),
		presign:     s3.NewPresignClient(presignClient),
		objectStore: objectStoreClient,
		llm:         answerGenerator,
		embedder:    embeddingProvider,
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
	mux.Handle("POST /api/v1/tenants/{tenantId}/purge", s.withAuth(s.withTenant(http.HandlerFunc(s.handlePurgeTenantData))))
	return s.withAccessLog(s.withCORS(s.withRequestID(mux)))
}

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prefer reverse-proxy supplied request IDs for cross-service tracing.
		requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if requestID == "" {
			requestID = strings.TrimSpace(r.Header.Get("X-Request-ID"))
		}
		if requestID == "" {
			requestID = randomUUIDv4Like()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func randomUUIDv4Like() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-" + randomString(16)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	)
}

type accessLogWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *accessLogWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *accessLogWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *accessLogWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &accessLogWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		requestID := strings.TrimSpace(wrapped.Header().Get("X-Request-ID"))
		if requestID == "" {
			requestID = requestIDFromContext(r.Context())
		}
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		log.Printf("request_id=%s method=%s path=%s status=%d bytes=%d duration_ms=%d tenant=%s remote=%s",
			requestID, r.Method, r.URL.Path, wrapped.status, wrapped.bytes, time.Since(start).Milliseconds(), tenantID, r.RemoteAddr)
	})
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

func BuildRuntimeDependencies() (*sql.DB, contracts.AuditEventRepository, contracts.VectorIndex, *retrieval.DeterministicRetrievalService, error) {
	dbCfg := runtime.LoadDatabaseConfigFromEnv(os.LookupEnv)
	dbCfg.Provider = "postgres"
	db, err := runtime.OpenPostgresDB(context.Background(), nil, dbCfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	auditRepo := repository.NewPostgresAuditEventRepository(db, repository.PostgresConfig{})
	vectorCfg := runtime.LoadVectorConfigFromEnv(os.LookupEnv)
	index, searcher, _, err := runtime.BuildVectorAdapters(vectorCfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	gatewayCfg := runtime.LoadGatewayConfigFromEnv(os.LookupEnv)
	reranker, _, err := runtime.BuildGatewayReranker(gatewayCfg, func() int64 { return time.Now().UTC().UnixMilli() })
	if err != nil {
		return nil, nil, nil, nil, err
	}
	serverCfg := ConfigFromEnv()
	var embedder contracts.EmbeddingProvider
	var queryRewriter contracts.QueryRewriter
	if serverCfg.GatewayProvider == "portkey" && strings.TrimSpace(serverCfg.PortkeyAPIKey) != "" {
		embeddingClient, embeddingErr := portkey.NewEmbeddingClient(portkey.EmbeddingConfig{
			BaseURL:       serverCfg.PortkeyBaseURL,
			PortkeyAPIKey: serverCfg.PortkeyAPIKey,
			OpenRouterKey: serverCfg.OpenRouterKey,
			Model:         serverCfg.EmbeddingModel,
			FallbackModel: serverCfg.EmbeddingFallbackModel,
			Timeout:       serverCfg.LLMTimeout,
			MinInterval:   serverCfg.OpenRouterMinInterval,
			RetryMax:      serverCfg.OpenRouterRetryMax,
		})
		if embeddingErr == nil {
			embedder = embeddingClient
		}
		if serverCfg.EnableQueryRewrite {
			rewriteClient, rewriteErr := portkey.NewChatClient(portkey.ChatConfig{
				BaseURL:       serverCfg.PortkeyBaseURL,
				PortkeyAPIKey: serverCfg.PortkeyAPIKey,
				OpenRouterKey: serverCfg.OpenRouterKey,
				Model:         serverCfg.QueryRewriteModel,
				Timeout:       serverCfg.LLMTimeout,
			})
			if rewriteErr == nil {
				queryRewriter = rewriteClient
			}
		}
	}
	return db, auditRepo, index, retrieval.NewDeterministicRetrievalService(searcher, reranker, retrieval.Config{EmbeddingProvider: embedder, QueryRewriter: queryRewriter}), nil
}

func (s *Server) EnsureBootstrapData(ctx context.Context) error {
	user := envOrSecret("FINE_RAG_BOOTSTRAP_ADMIN_USERNAME", "")
	pass := envOrSecret("FINE_RAG_BOOTSTRAP_ADMIN_PASSWORD", "")
	apiKey := envOrSecret("FINE_RAG_BOOTSTRAP_ADMIN_API_KEY", "")
	if user == "" || pass == "" || apiKey == "" {
		return errors.New("bootstrap admin secrets are required via *_FILE or env")
	}
	tenantID := envOr("FINE_RAG_BOOTSTRAP_TENANT_ID", "tenant-a")
	tenantName := envOr("FINE_RAG_BOOTSTRAP_TENANT_NAME", "Tenant A")
	_, err := s.ensureBootstrapUserAssigned(ctx, user, pass, apiKey, tenantID)
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
	secondaryUser := envOrSecret("FINE_RAG_BOOTSTRAP_SECONDARY_USERNAME", "")
	secondaryPass := envOrSecret("FINE_RAG_BOOTSTRAP_SECONDARY_PASSWORD", "")
	secondaryAPIKey := envOrSecret("FINE_RAG_BOOTSTRAP_SECONDARY_API_KEY", "")
	if secondaryUser != "" || secondaryPass != "" || secondaryAPIKey != "" {
		if secondaryUser == "" || secondaryPass == "" {
			return errors.New("secondary bootstrap user requires username and password when enabled")
		}
		if _, err := s.ensureBootstrapUserAssigned(ctx, secondaryUser, secondaryPass, secondaryAPIKey, tenantID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) ensureBootstrapUserAssigned(ctx context.Context, username string, password string, apiKey string, tenantID string) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM app_users WHERE username = $1`, username).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		if strings.TrimSpace(apiKey) == "" {
			err = s.db.QueryRowContext(ctx, `INSERT INTO app_users (username, password_hash, active) VALUES ($1,$2,TRUE) RETURNING id`, username, HashSecret(password)).Scan(&userID)
		} else {
			err = s.db.QueryRowContext(ctx, `INSERT INTO app_users (username, password_hash, api_key_hash, active) VALUES ($1,$2,$3,TRUE) RETURNING id`, username, HashSecret(password), HashSecret(apiKey)).Scan(&userID)
		}
	}
	if err != nil {
		return 0, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_tenants (user_id, tenant_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, userID, tenantID)
	if err != nil {
		return 0, err
	}
	return userID, nil
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

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return strings.TrimSpace(value)
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

func envOrSecret(key string, fallback string) string {
	if filePath := strings.TrimSpace(os.Getenv(key + "_FILE")); filePath != "" {
		if raw, err := os.ReadFile(filePath); err == nil {
			if value := strings.TrimSpace(string(raw)); value != "" {
				return value
			}
		}
	}
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
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
