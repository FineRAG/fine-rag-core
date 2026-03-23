package util

import (
	"os"
	"strconv"
	"strings"
	"time"

	"enterprise-go-rag/backend/util/apiutil"
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
	ParserType             string // "extractous" or "docling"
	DoclingEndpoint        string
}

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
	origins := apiutil.SplitCSV(os.Getenv("FINE_RAG_ALLOWED_ORIGINS"))
	if len(origins) == 0 {
		origins = []string{"https://finer.shafeeq.dev", "https://dash-finer.shafeeq.dev"}
	}
	s3UsePathStyle := apiutil.EnvBoolAny([]string{"FINE_RAG_S3_USE_PATH_STYLE"}, false)
	if strings.TrimSpace(apiutil.EnvOrSecretAny([]string{"FINE_RAG_S3_ENDPOINT"}, "")) != "" {
		s3UsePathStyle = true
	}
	presignTTL := 5 * time.Minute
	if raw := strings.TrimSpace(apiutil.EnvOrAny([]string{"FINE_RAG_S3_PRESIGN_TTL"}, "")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			presignTTL = parsed
		}
	}
	maxObjectBytes := int64(20 * 1024 * 1024)
	if raw := strings.TrimSpace(apiutil.EnvOrAny([]string{"FINE_RAG_S3_MAX_OBJECT_BYTES"}, "")); raw != "" {
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
	enableQueryRewrite := true
	if raw := strings.TrimSpace(os.Getenv("FINE_RAG_ENABLE_QUERY_REWRITE")); raw != "" {
		enableQueryRewrite = strings.EqualFold(raw, "true") || raw == "1" || strings.EqualFold(raw, "yes")
	}

	return Config{
		Addr:                   apiutil.EnvOr("FINE_RAG_HTTP_ADDR", ":8080"),
		JWTSecret:              apiutil.EnvOrSecret("FINE_RAG_JWT_SECRET", ""),
		TokenTTL:               ttl,
		AllowedOrigins:         origins,
		RateLimitPerMin:        limit,
		UploadBaseURL:          apiutil.EnvOrSecretAny([]string{"FINE_RAG_UPLOAD_PUBLIC_BASE_URL", "FINE_RAG_S3_PUBLIC_BASE_URL"}, ""),
		S3Endpoint:             apiutil.EnvOrSecretAny([]string{"FINE_RAG_S3_ENDPOINT"}, ""),
		UploadBucket:           apiutil.EnvOrAny([]string{"FINE_RAG_S3_BUCKET"}, "finerag-ingestion"),
		S3AccessKey:            apiutil.EnvOrSecretAny([]string{"FINE_RAG_S3_ACCESS_KEY"}, ""),
		S3SecretKey:            apiutil.EnvOrSecretAny([]string{"FINE_RAG_S3_SECRET_KEY"}, ""),
		S3Region:               apiutil.EnvOrAny([]string{"FINE_RAG_S3_REGION"}, "us-east-1"),
		S3UsePathStyle:         s3UsePathStyle,
		PresignTTL:             presignTTL,
		MaxObjectBytes:         maxObjectBytes,
		GatewayProvider:        strings.ToLower(apiutil.EnvOr("FINE_RAG_GATEWAY_PROVIDER", "stub")),
		PortkeyBaseURL:         apiutil.EnvOr("FINE_RAG_PORTKEY_BASE_URL", "https://api.portkey.ai"),
		PortkeyAPIKey:          apiutil.EnvOrSecret("FINE_RAG_PORTKEY_API_KEY", ""),
		OpenRouterKey:          apiutil.EnvOrSecret("FINE_RAG_OPENROUTER_API_KEY", ""),
		OpenRouterModel:        apiutil.EnvOr("FINE_RAG_OPENROUTER_MODEL", "@groq-key/openai/gpt-oss-120b"),
		EnableQueryRewrite:     enableQueryRewrite,
		QueryRewriteModel:      apiutil.EnvOr("FINE_RAG_QUERY_REWRITE_MODEL", "@groq-key/openai/gpt-oss-20b"),
		EmbeddingModel:         apiutil.EnvOr("FINE_RAG_OPENROUTER_EMBEDDING_MODEL", "@openrouter-key/nvidia/llama-nemotron-embed-vl-1b-v2:free"),
		EmbeddingFallbackModel: apiutil.EnvOr("FINE_RAG_OPENROUTER_EMBEDDING_FALLBACK_MODEL", ""),
		LLMTimeout:             llmTimeout,
		OpenRouterMinInterval:  openRouterMinInterval,
		OpenRouterRetryMax:     openRouterRetryMax,
		ChunkSizeChars:         chunkSizeChars,
		ChunkOverlapWords:      chunkOverlapWords,
		ParserType:             strings.ToLower(apiutil.EnvOr("FINE_RAG_PARSER_TYPE", "extractous")),
		DoclingEndpoint:        apiutil.EnvOr("FINE_RAG_DOCLING_ENDPOINT", "http://localhost:5000"),
	}
}
