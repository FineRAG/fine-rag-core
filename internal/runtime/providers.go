package runtime

import (
	"strings"

	"enterprise-go-rag/internal/adapters/gateway/cohere"
	"enterprise-go-rag/internal/adapters/gateway/portkey"
	"enterprise-go-rag/internal/adapters/vector/milvus"
	"enterprise-go-rag/internal/adapters/vector/stub"
	"enterprise-go-rag/internal/contracts"
)

func BuildVectorAdapters(cfg VectorConfig) (contracts.VectorIndex, contracts.VectorSearcher, string, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, "", err
	}
	switch cfg.Provider {
	case "stub":
		adapter := stub.NewAdapter()
		return adapter, adapter, "stub", nil
	default:
		adapter, err := milvus.NewAdapter(milvus.Config{
			Endpoint:   cfg.Endpoint,
			Database:   cfg.Database,
			Collection: cfg.Collection,
			Username:   cfg.Username,
			Password:   cfg.Password,
			Token:      cfg.Token,
			TLS:        cfg.TLS,
		})
		if err != nil {
			return nil, nil, "", err
		}
		return adapter, adapter, "milvus", nil
	}
}

func BuildGatewayReranker(cfg GatewayConfig, nowFn func() int64) (contracts.Reranker, string, error) {
	if err := cfg.Validate(); err != nil {
		return nil, "", err
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.RerankerProvider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	}

	switch provider {
	case "portkey":
		adapter, err := portkey.NewRerankerAdapter(portkey.Config{
			BaseURL:                 cfg.PortkeyBaseURL,
			APIKey:                  cfg.PortkeyAPIKey,
			Timeout:                 cfg.Timeout,
			RetryMax:                cfg.RetryMax,
			CircuitFailureThreshold: cfg.CircuitFailureThreshold,
			FallbackMode:            cfg.FallbackMode,
			DirectAllowlist:         cfg.DirectAllowlist,
			Model:                   cfg.RerankModel,
			ProviderKey:             cfg.PortkeyProviderKey,
			NowMillis:               nowFn,
		})
		if err != nil {
			return nil, "", err
		}
		return adapter, "portkey", nil
	case "cohere":
		adapter, err := cohere.NewCohereReranker(cohere.Config{
			APIKey:                  cfg.PortkeyProviderKey, // Reusing the same key variable
			Model:                   cfg.RerankModel,
			Timeout:                 cfg.Timeout,
			RetryMax:                cfg.RetryMax,
			CircuitFailureThreshold: cfg.CircuitFailureThreshold,
		})
		if err != nil {
			return nil, "", err
		}
		return adapter, "cohere", nil
	default:
		return portkey.NewStubReranker(), "stub", nil
	}
}
