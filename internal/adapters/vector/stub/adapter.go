package stub

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/internal/contracts"
)

type Adapter struct {
	mu        sync.Mutex
	store     map[contracts.TenantID][]contracts.VectorRecord
	lastTrace contracts.VectorCallTrace
}

func NewAdapter() *Adapter {
	return &Adapter{store: map[contracts.TenantID][]contracts.VectorRecord{}}
}

func (a *Adapter) Upsert(_ context.Context, records []contracts.VectorRecord) error {
	start := time.Now()
	a.markTrace("ok", start)
	if len(records) == 0 {
		return errors.New("at least one record is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return err
		}
		a.store[record.TenantID] = append(a.store[record.TenantID], record)
	}
	return nil
}

func (a *Adapter) Search(_ context.Context, tenantID contracts.TenantID, queryText string, topK int) ([]contracts.RetrievalDocument, error) {
	start := time.Now()
	a.markTrace("ok", start)
	if err := tenantID.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(queryText) == "" {
		return nil, errors.New("query text is required")
	}
	if topK <= 0 {
		return nil, errors.New("top_k must be > 0")
	}
	a.mu.Lock()
	records := make([]contracts.VectorRecord, len(a.store[tenantID]))
	copy(records, a.store[tenantID])
	a.mu.Unlock()

	docs := make([]contracts.RetrievalDocument, 0, len(records))
	for _, record := range records {
		score := 0.1
		if strings.Contains(strings.ToLower(record.ChunkText), strings.ToLower(strings.TrimSpace(queryText))) {
			score = 1
		}
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

func (a *Adapter) SearchByEmbedding(_ context.Context, tenantID contracts.TenantID, queryEmbedding []float32, topK int) ([]contracts.RetrievalDocument, error) {
	start := time.Now()
	a.markTrace("ok", start)
	if err := tenantID.Validate(); err != nil {
		return nil, err
	}
	if len(queryEmbedding) == 0 {
		return nil, errors.New("query embedding is required")
	}
	if topK <= 0 {
		return nil, errors.New("top_k must be > 0")
	}
	a.mu.Lock()
	records := make([]contracts.VectorRecord, len(a.store[tenantID]))
	copy(records, a.store[tenantID])
	a.mu.Unlock()

	docs := make([]contracts.RetrievalDocument, 0, len(records))
	for _, record := range records {
		score := cosineSimilarity(queryEmbedding, record.Embedding)
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

func cosineSimilarity(left []float32, right []float32) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	n := len(left)
	if len(right) < n {
		n = len(right)
	}
	var dot float64
	var leftNorm float64
	var rightNorm float64
	for i := 0; i < n; i++ {
		l := float64(left[i])
		r := float64(right[i])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func (a *Adapter) PurgeTenant(_ context.Context, tenantID contracts.TenantID) error {
	if err := tenantID.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.store, tenantID)
	return nil
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
		Provider:      "stub",
		Status:        status,
		LatencyMillis: time.Since(start).Milliseconds(),
	}
}
