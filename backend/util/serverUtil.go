package util

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/backend/managers"
	"enterprise-go-rag/backend/util/apiutil"
	"enterprise-go-rag/internal/adapters/gateway/portkey"
	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/logging"
	"enterprise-go-rag/internal/repository"
	"enterprise-go-rag/internal/runtime"
	"enterprise-go-rag/internal/services/retrieval"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.uber.org/zap"
)

type Server struct {
	cfg       Config
	db        *sql.DB
	retrieval *retrieval.DeterministicRetrievalService
	index     contracts.VectorIndex
	origins   map[string]struct{}
	limiter   *WindowLimiter

	// Managers
	Auth      *managers.AuthManager
	Tenants   *managers.TenantManager
	KB        *managers.KnowledgeBaseManager
	Uploads   *managers.UploadManager
	Ingestion *managers.IngestionManager
	Search    *managers.SearchManager
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

	var answerGenerator managers.LLMAnswerGenerator
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

	s := &Server{
		cfg:       cfg,
		db:        db,
		retrieval: retrievalSvc,
		index:     vectorIndex,
		origins:   origins,
		limiter:   NewWindowLimiter(cfg.RateLimitPerMin),
	}

	// Initialize Managers
	s.Auth = &managers.AuthManager{DB: db, Secret: cfg.JWTSecret, TokenTTL: cfg.TokenTTL}
	s.Tenants = &managers.TenantManager{DB: db, ObjectStore: objectStoreClient, Index: vectorIndex, Bucket: cfg.UploadBucket}
	s.KB = &managers.KnowledgeBaseManager{DB: db}
	s.Uploads = &managers.UploadManager{Presign: s3.NewPresignClient(presignClient), UploadBucket: cfg.UploadBucket, UploadBaseURL: cfg.UploadBaseURL, PresignTTL: cfg.PresignTTL, MaxObjectBytes: cfg.MaxObjectBytes}
	s.Ingestion = &managers.IngestionManager{DB: db, Index: vectorIndex, ObjectStore: objectStoreClient, Embedder: embeddingProvider, UploadBucket: cfg.UploadBucket, MaxObjectBytes: cfg.MaxObjectBytes, ChunkSizeChars: cfg.ChunkSizeChars, ChunkOverlapWords: cfg.ChunkOverlapWords}
	s.Search = &managers.SearchManager{Retrieval: retrievalSvc, LLM: answerGenerator, EmbeddingModel: cfg.EmbeddingModel, OpenRouterModel: cfg.OpenRouterModel}

	return s, nil
}

func (s *Server) WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if requestID == "" {
			requestID = strings.TrimSpace(r.Header.Get("X-Request-ID"))
		}
		if requestID == "" {
			requestID = apiutil.RandomUUIDv4Like()
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), apiutil.RequestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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

func (s *Server) WithAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &accessLogWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		requestID := strings.TrimSpace(wrapped.Header().Get("X-Request-ID"))
		if requestID == "" {
			requestID = apiutil.RequestIDFromContext(r.Context())
		}
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		log.Printf("request_id=%s method=%s path=%s status=%d bytes=%d duration_ms=%d tenant=%s remote=%s",
			requestID, r.Method, r.URL.Path, wrapped.status, wrapped.bytes, time.Since(start).Milliseconds(), tenantID, r.RemoteAddr)
	})
}

func (s *Server) WithCORS(next http.Handler) http.Handler {
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

func (s *Server) WithAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
			apiutil.WriteError(w, http.StatusUnauthorized, "auth_required", "bearer token is required")
			return
		}
		token := strings.TrimSpace(authz[len("Bearer "):])
		claims, err := apiutil.VerifyJWT(token, s.cfg.JWTSecret)
		if err != nil {
			apiutil.WriteError(w, http.StatusUnauthorized, "auth_invalid", "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), apiutil.UserIDKey, claims.UID)
		ctx = contracts.WithActorID(ctx, claims.Sub)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) WithTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
		if tenantID == "" {
			apiutil.WriteError(w, http.StatusBadRequest, "tenant_required", "X-Tenant-ID is required")
			return
		}
		uid, ok := apiutil.UserIDFromContext(r.Context())
		if !ok {
			apiutil.WriteError(w, http.StatusUnauthorized, "auth_required", "missing user context")
			return
		}
		allowed, err := s.userHasTenant(r.Context(), uid, tenantID)
		if err != nil {
			logging.Logger.Error("tenant check failed", zap.Error(err), zap.String("tenantID", tenantID), zap.Int64("userID", uid))
			apiutil.WriteError(w, http.StatusInternalServerError, "tenant_check_failed", err.Error())
			return
		}
		if !allowed {
			apiutil.WriteError(w, http.StatusForbidden, "tenant_forbidden", "tenant not assigned")
			return
		}
		if !s.limiter.Allow(tenantID + ":" + r.URL.Path) {
			apiutil.WriteError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
			return
		}
		requestID := apiutil.RequestIDFromContext(r.Context())
		tenantCtx, err := contracts.WithTenantContext(r.Context(), contracts.TenantContext{TenantID: contracts.TenantID(tenantID), RequestID: requestID})
		if err != nil {
			apiutil.WriteError(w, http.StatusInternalServerError, "tenant_context_failed", err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(tenantCtx))
	})
}

func (s *Server) userHasTenant(ctx context.Context, userID int64, tenantID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_tenants WHERE user_id = $1 AND tenant_id = $2`, userID, tenantID).Scan(&count)
	return count > 0, err
}

func BuildRuntimeDependencies() (*sql.DB, contracts.AuditEventRepository, contracts.VectorIndex, *retrieval.DeterministicRetrievalService, error) {
	dbCfg := runtime.LoadDatabaseConfigFromEnv(os.LookupEnv)
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
	reranker, rerankerProvider, err := runtime.BuildGatewayReranker(gatewayCfg, func() int64 { return time.Now().UTC().UnixMilli() })
	if err != nil {
		return nil, nil, nil, nil, err
	}
	logging.Logger.Info("runtime.dependencies.reranker.initialized", zap.String("provider", rerankerProvider))
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
	return db, auditRepo, index, retrieval.NewDeterministicRetrievalService(searcher, reranker, retrieval.Config{
		EmbeddingProvider: embedder,
		QueryRewriter:     queryRewriter,
		RerankTimeout:     gatewayCfg.Timeout,
	}), nil
}

func (s *Server) EnsureBootstrapData(ctx context.Context) error {
	adminUser := apiutil.EnvOrSecret("FINE_RAG_BOOTSTRAP_ADMIN_USERNAME", "")
	adminPass := apiutil.EnvOrSecret("FINE_RAG_BOOTSTRAP_ADMIN_PASSWORD", "")
	adminAPIKey := apiutil.EnvOrSecret("FINE_RAG_BOOTSTRAP_ADMIN_API_KEY", "")
	if adminUser == "" || adminPass == "" || adminAPIKey == "" {
		return errors.New("bootstrap admin secrets are required via *_FILE or env")
	}

	repo := repository.NewPostgresTenantRegistryRepository(s.db, repository.PostgresConfig{})

	if _, err := s.db.ExecContext(ctx, `DELETE FROM user_tenants WHERE tenant_id LIKE '% %'`); err != nil {
		logging.Logger.Warn("failed to cleanup stale user_tenants", zap.Error(err))
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM tenant_registry WHERE tenant_id LIKE '% %'`); err != nil {
		logging.Logger.Warn("failed to cleanup stale tenant_registry", zap.Error(err))
	}

	adminUID, err := s.ensureBootstrapUser(ctx, adminUser, adminPass, adminAPIKey)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM user_tenants WHERE user_id = $1`, adminUID); err != nil {
		return err
	}

	adminTenants := []struct{ ID, Name string }{
		{"tenant-a", "Tenant A"},
		{"tenant-secondary", "Secondary Tenant"},
	}
	for _, t := range adminTenants {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO user_tenants (user_id, tenant_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, adminUID, t.ID); err != nil {
			return err
		}
		tctx, err := contracts.WithTenantContext(ctx, contracts.TenantContext{TenantID: contracts.TenantID(t.ID), RequestID: "bootstrap-admin"})
		if err != nil {
			return err
		}
		if err := repo.Upsert(tctx, contracts.TenantRecord{TenantID: contracts.TenantID(t.ID), DisplayName: t.Name, PlanTier: "starter", Active: true, UpdatedAt: time.Now().UTC()}); err != nil {
			return err
		}
	}

	secondaryUsername := apiutil.EnvOrSecret("FINE_RAG_BOOTSTRAP_SECONDARY_USERNAME", "")
	secondaryPass := apiutil.EnvOrSecret("FINE_RAG_BOOTSTRAP_SECONDARY_PASSWORD", "")
	secondaryAPIKey := apiutil.EnvOrSecret("FINE_RAG_BOOTSTRAP_SECONDARY_API_KEY", "")
	if secondaryUsername != "" || secondaryPass != "" {
		secondaryUID, err := s.ensureBootstrapUser(ctx, secondaryUsername, secondaryPass, secondaryAPIKey)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM user_tenants WHERE user_id = $1`, secondaryUID); err != nil {
			return err
		}

		jeetuTenants := []struct{ ID, Name string }{
			{"tenant-lalita", "Lalita Tenant"},
			{"tenant-jeetu", "Jeetu Tenant"},
		}
		for _, t := range jeetuTenants {
			if _, err := s.db.ExecContext(ctx, `INSERT INTO user_tenants (user_id, tenant_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, secondaryUID, t.ID); err != nil {
				return err
			}
			tctx, err := contracts.WithTenantContext(ctx, contracts.TenantContext{TenantID: contracts.TenantID(t.ID), RequestID: "bootstrap-jeetu"})
			if err != nil {
				return err
			}
			if err := repo.Upsert(tctx, contracts.TenantRecord{TenantID: contracts.TenantID(t.ID), DisplayName: t.Name, PlanTier: "starter", Active: true, UpdatedAt: time.Now().UTC()}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) ensureBootstrapUser(ctx context.Context, username string, password string, apiKey string) (int64, error) {
	var userID int64
	var apiKeyHash any
	if strings.TrimSpace(apiKey) != "" {
		apiKeyHash = apiutil.HashSecret(apiKey)
	}
	err := s.db.QueryRowContext(ctx, `INSERT INTO app_users (username, password_hash, api_key_hash, active)
VALUES ($1,$2,$3,TRUE)
ON CONFLICT (username) DO UPDATE
SET password_hash = EXCLUDED.password_hash,
    api_key_hash = COALESCE(EXCLUDED.api_key_hash, app_users.api_key_hash),
    active = TRUE
RETURNING id`, username, apiutil.HashSecret(password), apiKeyHash).Scan(&userID)
	return userID, err
}

type WindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	state  map[string]windowState
}

type windowState struct {
	start time.Time
	count int
}

func NewWindowLimiter(limit int) *WindowLimiter {
	if limit <= 0 {
		limit = 120
	}
	return &WindowLimiter{limit: limit, window: time.Minute, state: map[string]windowState{}}
}

func (l *WindowLimiter) Allow(key string) bool {
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
