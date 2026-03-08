package retrieval

import (
	"context"

	"enterprise-go-rag/internal/contracts"
)

// Service defines retrieval boundaries for query execution and reranking.
type Service interface {
	Search(ctx context.Context, metadata contracts.RequestMetadata, query contracts.RetrievalQuery) (contracts.RetrievalResult, error)
	Rerank(ctx context.Context, metadata contracts.RequestMetadata, req contracts.RerankRequest) ([]contracts.RerankCandidate, error)
}
