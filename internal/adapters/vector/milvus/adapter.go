package milvus

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/internal/adapters/vector"
	"enterprise-go-rag/internal/contracts"
)

type Config struct {
	Endpoint   string
	Database   string
	Collection string
	TLS        bool
}

type Adapter struct {
	cfg       Config
	mu        sync.Mutex
	store     map[contracts.TenantID][]contracts.VectorRecord
	lastTrace contracts.VectorCallTrace
}

func NewAdapter(cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("FINE_RAG_MILVUS_ENDPOINT is required when FINE_RAG_VECTOR_PROVIDER=milvus")
	}
	if strings.TrimSpace(cfg.Database) == "" {
		return nil, errors.New("FINE_RAG_MILVUS_DATABASE is required when FINE_RAG_VECTOR_PROVIDER=milvus")
	}
	if strings.TrimSpace(cfg.Collection) == "" {
		return nil, errors.New("FINE_RAG_MILVUS_COLLECTION is required when FINE_RAG_VECTOR_PROVIDER=milvus")
	}
	if !cfg.TLS {
		return nil, errors.New("FINE_RAG_MILVUS_TLS must be true for milvus provider")
	}
	return &Adapter{cfg: cfg, store: map[contracts.TenantID][]contracts.VectorRecord{}}, nil
}

func (a *Adapter) Upsert(ctx context.Context, records []contracts.VectorRecord) error {
	start := time.Now()
	a.markTrace("ok", start)
	if len(records) == 0 {
		return vector.NormalizeProviderError("milvus", "upsert", errors.New("at least one record is required"))
	}
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return vector.NormalizeProviderError("milvus", "upsert", err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, record := range records {
		byTenant := a.store[record.TenantID]
		replaced := false
		for i := range byTenant {
			if byTenant[i].RecordID == record.RecordID {
				byTenant[i] = record
				replaced = true
				break
			}
		}
		if !replaced {
			byTenant = append(byTenant, record)
		}
		a.store[record.TenantID] = byTenant
	}
	return nil
}

func (a *Adapter) Search(ctx context.Context, tenantID contracts.TenantID, queryText string, topK int) ([]contracts.RetrievalDocument, error) {
	start := time.Now()
	a.markTrace("ok", start)
	if err := tenantID.Validate(); err != nil {
		return nil, vector.NormalizeProviderError("milvus", "search", err)
	}
	if strings.TrimSpace(queryText) == "" {
		return nil, vector.NormalizeProviderError("milvus", "search", errors.New("query text is required"))
	}
	if topK <= 0 {
		return nil, vector.NormalizeProviderError("milvus", "search", errors.New("top_k must be > 0"))
	}

	a.mu.Lock()
	records := make([]contracts.VectorRecord, len(a.store[tenantID]))
	copy(records, a.store[tenantID])
	a.mu.Unlock()

	docs := make([]contracts.RetrievalDocument, 0, len(records))
	for _, record := range records {
		score := lexicalScore(queryText, record.ChunkText)
		docs = append(docs, contracts.RetrievalDocument{
			DocumentID: record.RecordID,
			TenantID:   record.TenantID,
			Content:    record.ChunkText,
			Score:      score,
			SourceURI:  record.SourceURI,
		})
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Score > docs[j].Score })
	if topK < len(docs) {
		docs = docs[:topK]
	}
	return docs, nil
}

func (a *Adapter) LastVectorTrace() contracts.VectorCallTrace {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastTrace
}

func (a *Adapter) markTrace(status string, start time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastTrace = contracts.VectorCallTrace{
		Provider:      "milvus",
		Status:        status,
		LatencyMillis: time.Since(start).Milliseconds(),
	}
}

func lexicalScore(query string, content string) float64 {
	queryTokens := strings.Fields(strings.ToLower(query))
	contentLC := strings.ToLower(content)
	if len(queryTokens) == 0 {
		return 0
	}
	hits := 0
	for _, token := range queryTokens {
		if strings.Contains(contentLC, token) {
			hits++
		}
	}
	if hits == 0 {
		return float64(len(content)%13) / 100.0
	}
	return float64(hits) + float64(len(content)%10)/100.0
}

func (a *Adapter) DebugString() string {
	return fmt.Sprintf("milvus(%s/%s)", a.cfg.Database, a.cfg.Collection)
}
