package milvus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"enterprise-go-rag/internal/adapters/vector"
	"enterprise-go-rag/internal/contracts"

	mclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	fieldRecordID    = "record_id"
	fieldTenantID    = "tenant_id"
	fieldJobID       = "job_id"
	fieldChunkText   = "chunk_text"
	fieldSourceURI   = "source_uri"
	fieldObjectKey   = "object_key"
	fieldChecksum    = "checksum"
	fieldRetryCount  = "retry_count"
	fieldIndexedAtMs = "indexed_at_ms"
	fieldMetadata    = "metadata_json"
	fieldEmbedding   = "embedding"
)

type Config struct {
	Endpoint   string
	Database   string
	Collection string
	Username   string
	Password   string
	Token      string
	TLS        bool
}

type Adapter struct {
	cfg       Config
	cli       mclient.Client
	lastTrace contracts.VectorCallTrace
	mu        sync.Mutex
	dim       int
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
	address, err := normalizeMilvusAddress(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	// Cloud Milvus endpoints can take longer than local instances during TLS+auth handshake.
	connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cliCfg := mclient.Config{
		Address:       address,
		DBName:        strings.TrimSpace(cfg.Database),
		EnableTLSAuth: cfg.TLS,
	}
	// Prefer APIKey auth (Zilliz Cloud); fall back to username/password only when APIKey is absent.
	if tok := strings.TrimSpace(cfg.Token); tok != "" {
		cliCfg.APIKey = tok
	} else {
		cliCfg.Username = strings.TrimSpace(cfg.Username)
		cliCfg.Password = strings.TrimSpace(cfg.Password)
	}
	cli, err := mclient.NewClient(connectCtx, cliCfg)
	if err != nil {
		return nil, vector.NormalizeProviderError("milvus", "connect", err)
	}
	adapter := &Adapter{cfg: cfg, cli: cli}
	adapter.markTrace("ok", time.Now())
	return adapter, nil
}

func (a *Adapter) Upsert(ctx context.Context, records []contracts.VectorRecord) error {
	started := time.Now()
	if len(records) == 0 {
		a.markTrace("validation_error", started)
		return vector.NormalizeProviderError("milvus", "upsert", errors.New("at least one record is required"))
	}
	dim := len(records[0].Embedding)
	if dim <= 0 {
		a.markTrace("validation_error", started)
		return vector.NormalizeProviderError("milvus", "upsert", errors.New("embedding is required"))
	}
	for _, record := range records {
		if err := record.Validate(); err != nil {
			a.markTrace("validation_error", started)
			return vector.NormalizeProviderError("milvus", "upsert", err)
		}
		if len(record.Embedding) != dim {
			a.markTrace("validation_error", started)
			return vector.NormalizeProviderError("milvus", "upsert", fmt.Errorf("embedding dimension mismatch: got=%d want=%d", len(record.Embedding), dim))
		}
	}
	if err := a.ensureCollection(ctx, dim); err != nil {
		a.markTrace("collection_error", started)
		return vector.NormalizeProviderError("milvus", "upsert", err)
	}

	recordIDs := make([]string, 0, len(records))
	tenantIDs := make([]string, 0, len(records))
	jobIDs := make([]string, 0, len(records))
	chunkTexts := make([]string, 0, len(records))
	sourceURIs := make([]string, 0, len(records))
	checksums := make([]string, 0, len(records))
	retryCounts := make([]int64, 0, len(records))
	indexedAt := make([]int64, 0, len(records))
	metadataJSON := make([]string, 0, len(records))
	objectKeys := make([]string, 0, len(records))
	embeddings := make([][]float32, 0, len(records))
	for _, record := range records {
		recordIDs = append(recordIDs, record.RecordID)
		tenantIDs = append(tenantIDs, string(record.TenantID))
		jobIDs = append(jobIDs, record.JobID)
		chunkTexts = append(chunkTexts, record.ChunkText)
		sourceURIs = append(sourceURIs, record.SourceURI)
		objectKeys = append(objectKeys, record.ObjectKey)
		checksums = append(checksums, record.Checksum)
		retryCounts = append(retryCounts, int64(record.RetryCount))
		indexedAt = append(indexedAt, record.IndexedAt.UTC().UnixMilli())
		metadataJSON = append(metadataJSON, marshalMetadata(record.Metadata))
		embeddings = append(embeddings, record.Embedding)
	}

	_, err := a.cli.Upsert(
		ctx,
		a.cfg.Collection,
		"",
		entity.NewColumnVarChar(fieldRecordID, recordIDs),
		entity.NewColumnVarChar(fieldTenantID, tenantIDs),
		entity.NewColumnVarChar(fieldJobID, jobIDs),
		entity.NewColumnVarChar(fieldChunkText, chunkTexts),
		entity.NewColumnVarChar(fieldSourceURI, sourceURIs),
		entity.NewColumnVarChar(fieldObjectKey, objectKeys),
		entity.NewColumnVarChar(fieldChecksum, checksums),
		entity.NewColumnInt64(fieldRetryCount, retryCounts),
		entity.NewColumnInt64(fieldIndexedAtMs, indexedAt),
		entity.NewColumnVarChar(fieldMetadata, metadataJSON),
		entity.NewColumnFloatVector(fieldEmbedding, dim, embeddings),
	)
	if err != nil {
		a.markTrace("error", started)
		return vector.NormalizeProviderError("milvus", "upsert", err)
	}
	if err := a.cli.Flush(ctx, a.cfg.Collection, false); err != nil {
		a.markTrace("error", started)
		return vector.NormalizeProviderError("milvus", "upsert", err)
	}
	a.markTrace("ok", started)
	return nil
}

func (a *Adapter) Search(ctx context.Context, tenantID contracts.TenantID, queryText string, topK int) ([]contracts.RetrievalDocument, error) {
	started := time.Now()
	if err := tenantID.Validate(); err != nil {
		a.markTrace("validation_error", started)
		return nil, vector.NormalizeProviderError("milvus", "search", err)
	}
	if strings.TrimSpace(queryText) == "" {
		a.markTrace("validation_error", started)
		return nil, vector.NormalizeProviderError("milvus", "search", errors.New("query text is required"))
	}
	if topK <= 0 {
		a.markTrace("validation_error", started)
		return nil, vector.NormalizeProviderError("milvus", "search", errors.New("top_k must be > 0"))
	}
	if err := a.ensureCollection(ctx, 0); err != nil {
		a.markTrace("error", started)
		return nil, vector.NormalizeProviderError("milvus", "search", err)
	}

	expr := fmt.Sprintf("%s == %q", fieldTenantID, escapeExprString(string(tenantID)))
	limit := int64(topK * 24)
	if limit < int64(topK) {
		limit = int64(topK)
	}
	if limit > 512 {
		limit = 512
	}
	resultSet, err := a.cli.Query(
		ctx,
		a.cfg.Collection,
		nil,
		expr,
		[]string{fieldRecordID, fieldChunkText, fieldSourceURI, fieldMetadata},
		mclient.WithLimit(limit),
	)
	if err != nil {
		a.markTrace("error", started)
		return nil, vector.NormalizeProviderError("milvus", "search", err)
	}
	if len(resultSet) == 0 {
		a.markTrace("ok", started)
		return nil, nil
	}

	docs := rowsFromResultSet(resultSet, tenantID)
	query := strings.TrimSpace(strings.ToLower(queryText))
	for i := range docs {
		docs[i].Score = lexicalScore(query, docs[i].Content)
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Score > docs[j].Score })
	if topK < len(docs) {
		docs = docs[:topK]
	}
	a.markTrace("ok", started)
	return docs, nil
}

func (a *Adapter) SearchByEmbedding(ctx context.Context, tenantID contracts.TenantID, queryEmbedding []float32, topK int) ([]contracts.RetrievalDocument, error) {
	started := time.Now()
	if err := tenantID.Validate(); err != nil {
		a.markTrace("validation_error", started)
		return nil, vector.NormalizeProviderError("milvus", "search_by_embedding", err)
	}
	if len(queryEmbedding) == 0 {
		a.markTrace("validation_error", started)
		return nil, vector.NormalizeProviderError("milvus", "search_by_embedding", errors.New("query embedding is required"))
	}
	if topK <= 0 {
		a.markTrace("validation_error", started)
		return nil, vector.NormalizeProviderError("milvus", "search_by_embedding", errors.New("top_k must be > 0"))
	}
	if err := a.ensureCollection(ctx, len(queryEmbedding)); err != nil {
		a.markTrace("error", started)
		return nil, vector.NormalizeProviderError("milvus", "search_by_embedding", err)
	}
	if err := a.cli.LoadCollection(ctx, a.cfg.Collection, false); err != nil {
		a.markTrace("error", started)
		return nil, vector.NormalizeProviderError("milvus", "search_by_embedding", err)
	}

	searchParam, err := entity.NewIndexAUTOINDEXSearchParam(2)
	if err != nil {
		a.markTrace("error", started)
		return nil, vector.NormalizeProviderError("milvus", "search_by_embedding", err)
	}
	expr := fmt.Sprintf("%s == %q", fieldTenantID, escapeExprString(string(tenantID)))
	results, err := a.cli.Search(
		ctx,
		a.cfg.Collection,
		nil,
		expr,
		[]string{fieldChunkText, fieldSourceURI, fieldMetadata},
		[]entity.Vector{entity.FloatVector(queryEmbedding)},
		fieldEmbedding,
		entity.COSINE,
		topK,
		searchParam,
	)
	if err != nil {
		a.markTrace("error", started)
		return nil, vector.NormalizeProviderError("milvus", "search_by_embedding", err)
	}
	if len(results) == 0 {
		a.markTrace("ok", started)
		return nil, nil
	}
	result := results[0]
	docs := make([]contracts.RetrievalDocument, 0, result.ResultCount)
	for i := 0; i < result.ResultCount; i++ {
		id := readColumnString(result.IDs, i)
		content := readSearchField(result.Fields, fieldChunkText, i)
		source := readSearchField(result.Fields, fieldSourceURI, i)
		metadata := unmarshalMetadata(readSearchField(result.Fields, fieldMetadata, i))
		score := 0.0
		if i < len(result.Scores) {
			score = float64(result.Scores[i])
		}
		docs = append(docs, contracts.RetrievalDocument{
			DocumentID: id,
			TenantID:   tenantID,
			Content:    content,
			Score:      score,
			SourceURI:  source,
			Metadata:   metadata,
		})
	}
	a.markTrace("ok", started)
	return docs, nil
}

func (a *Adapter) PurgeTenant(ctx context.Context, tenantID contracts.TenantID) error {
	started := time.Now()
	if err := tenantID.Validate(); err != nil {
		a.markTrace("validation_error", started)
		return vector.NormalizeProviderError("milvus", "purge", err)
	}
	if err := a.ensureCollection(ctx, 0); err != nil {
		a.markTrace("error", started)
		return vector.NormalizeProviderError("milvus", "purge", err)
	}
	expr := fmt.Sprintf("%s == %q", fieldTenantID, escapeExprString(string(tenantID)))
	if err := a.cli.Delete(ctx, a.cfg.Collection, "", expr); err != nil {
		a.markTrace("error", started)
		return vector.NormalizeProviderError("milvus", "purge", err)
	}
	if err := a.cli.Flush(ctx, a.cfg.Collection, false); err != nil {
		a.markTrace("error", started)
		return vector.NormalizeProviderError("milvus", "purge", err)
	}
	a.markTrace("ok", started)
	return nil
}

func (a *Adapter) LastVectorTrace() contracts.VectorCallTrace {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastTrace
}

func (a *Adapter) DebugString() string {
	return fmt.Sprintf("milvus(%s/%s)", a.cfg.Database, a.cfg.Collection)
}

func (a *Adapter) ensureCollection(ctx context.Context, dim int) error {
	a.mu.Lock()
	knownDim := a.dim
	a.mu.Unlock()
	if dim <= 0 {
		dim = knownDim
	}
	exists, err := a.cli.HasCollection(ctx, a.cfg.Collection)
	if err != nil {
		return err
	}
	if exists {
		coll, derr := a.cli.DescribeCollection(ctx, a.cfg.Collection)
		if derr == nil {
			foundObjectKey := false
			for _, f := range coll.Schema.Fields {
				if f.Name == fieldObjectKey {
					foundObjectKey = true
				}
				if f.Name == fieldEmbedding {
					if d, ok := f.TypeParams[entity.TypeParamDim]; ok {
						if parsed, perr := strconv.Atoi(d); perr == nil && parsed > 0 {
							a.mu.Lock()
							a.dim = parsed
							a.mu.Unlock()
						}
					}
				}
			}
			if !foundObjectKey {
				return errors.New("milvus collection schema mismatch: " + fieldObjectKey + " field is missing in " + a.cfg.Collection + ". Please drop the collection to allow the backend to recreate it with the correct schema (required for efficient purging).")
			}
		}
		if err := a.ensureIndexAndLoad(ctx); err != nil {
			return err
		}
		return nil
	}
	if dim <= 0 {
		return errors.New("embedding dimension is required to initialize collection")
	}
	schema := entity.NewSchema().
		WithName(a.cfg.Collection).
		WithDescription("FineR tenant-scoped vector collection").
		WithAutoID(false).
		WithDynamicFieldEnabled(false).
		WithField(entity.NewField().WithName(fieldRecordID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(256).WithIsPrimaryKey(true).WithIsAutoID(false)).
		WithField(entity.NewField().WithName(fieldTenantID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(128)).
		WithField(entity.NewField().WithName(fieldJobID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(128)).
		WithField(entity.NewField().WithName(fieldChunkText).WithDataType(entity.FieldTypeVarChar).WithMaxLength(65535)).
		WithField(entity.NewField().WithName(fieldSourceURI).WithDataType(entity.FieldTypeVarChar).WithMaxLength(2048)).
		WithField(entity.NewField().WithName(fieldObjectKey).WithDataType(entity.FieldTypeVarChar).WithMaxLength(1024)).
		WithField(entity.NewField().WithName(fieldChecksum).WithDataType(entity.FieldTypeVarChar).WithMaxLength(128)).
		WithField(entity.NewField().WithName(fieldRetryCount).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldIndexedAtMs).WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(fieldMetadata).WithDataType(entity.FieldTypeVarChar).WithMaxLength(65535)).
		WithField(entity.NewField().WithName(fieldEmbedding).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(dim)))
	if err := a.cli.CreateCollection(ctx, schema, 2); err != nil {
		return err
	}
	a.mu.Lock()
	a.dim = dim
	a.mu.Unlock()
	if err := a.ensureIndexAndLoad(ctx); err != nil {
		return err
	}
	return nil
}

func (a *Adapter) ensureIndexAndLoad(ctx context.Context) error {
	idx, err := entity.NewIndexAUTOINDEX(entity.COSINE)
	if err != nil {
		return err
	}
	if err := a.cli.CreateIndex(ctx, a.cfg.Collection, fieldEmbedding, idx, false); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "already") && !strings.Contains(msg, "exist") {
			return err
		}
	}
	// Note: Scalar indices for tenant_id and object_key are automatically managed by Milvus AUTOINDEX on Zilliz Cloud
	if err := a.cli.LoadCollection(ctx, a.cfg.Collection, false); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "already") && !strings.Contains(msg, "loaded") {
			return err
		}
	}
	return nil
}

func rowsFromResultSet(resultSet mclient.ResultSet, tenantID contracts.TenantID) []contracts.RetrievalDocument {
	if len(resultSet) == 0 {
		return nil
	}
	rowCount := resultSet[0].Len()
	docs := make([]contracts.RetrievalDocument, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		id := readResultField(resultSet, fieldRecordID, i)
		content := readResultField(resultSet, fieldChunkText, i)
		source := readResultField(resultSet, fieldSourceURI, i)
		metadata := unmarshalMetadata(readResultField(resultSet, fieldMetadata, i))
		docs = append(docs, contracts.RetrievalDocument{
			DocumentID: id,
			TenantID:   tenantID,
			Content:    content,
			SourceURI:  source,
			Metadata:   metadata,
		})
	}
	return docs
}

func readResultField(resultSet mclient.ResultSet, field string, idx int) string {
	for _, col := range resultSet {
		if col.Name() != field {
			continue
		}
		if v, err := col.GetAsString(idx); err == nil {
			return strings.TrimSpace(v)
		}
		if v, err := col.Get(idx); err == nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
		return ""
	}
	return ""
}

func readSearchField(cols []entity.Column, field string, idx int) string {
	for _, col := range cols {
		if col.Name() != field {
			continue
		}
		if v, err := col.GetAsString(idx); err == nil {
			return strings.TrimSpace(v)
		}
		if v, err := col.Get(idx); err == nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
		return ""
	}
	return ""
}

func readColumnString(col entity.Column, idx int) string {
	if col == nil {
		return ""
	}
	if v, err := col.GetAsString(idx); err == nil {
		return strings.TrimSpace(v)
	}
	if v, err := col.Get(idx); err == nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func unmarshalMetadata(raw string) map[string]string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return map[string]string{}
	}
	decoded := map[string]string{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return map[string]string{}
	}
	return decoded
}

func marshalMetadata(metadata map[string]string) string {
	if len(metadata) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func escapeExprString(input string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"")
	return replacer.Replace(input)
}

func lexicalScore(query string, content string) float64 {
	queryTokens := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
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

func normalizeMilvusAddress(endpoint string) (string, error) {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return "", errors.New("milvus endpoint is empty")
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("invalid milvus endpoint: %w", err)
		}
		host := strings.TrimSpace(u.Host)
		if host == "" {
			return "", errors.New("invalid milvus endpoint host")
		}
		if strings.Contains(host, ":") {
			return host, nil
		}
		if strings.HasPrefix(raw, "https://") {
			return host + ":443", nil
		}
		return host + ":80", nil
	}
	if strings.Contains(raw, ":") {
		return raw, nil
	}
	return raw + ":19530", nil
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
