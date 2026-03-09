package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"enterprise-go-rag/internal/contracts"
)

var (
	ErrAPIKeyMissing = errors.New("api key is required")
	ErrAPIKeyInvalid = errors.New("api key is invalid")
	ErrAPIKeyRevoked = errors.New("api key is revoked")
)

type APIKeyRecord struct {
	APIKeyID      string
	TenantID      contracts.TenantID
	Subject       string
	RawKeySHA256  string
	Revoked       bool
	RevokedAt     time.Time
	IssuedAt      time.Time
	AllowedScopes []string
}

type APIKeyStore interface {
	GetByHash(ctx context.Context, keySHA256 string) (APIKeyRecord, bool, error)
}

type InMemoryAPIKeyStore struct {
	mu      sync.RWMutex
	records map[string]APIKeyRecord
}

func NewInMemoryAPIKeyStore() *InMemoryAPIKeyStore {
	return &InMemoryAPIKeyStore{records: make(map[string]APIKeyRecord)}
}

func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *InMemoryAPIKeyStore) Put(record APIKeyRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.RawKeySHA256] = record
}

func (s *InMemoryAPIKeyStore) GetByHash(_ context.Context, keySHA256 string) (APIKeyRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[keySHA256]
	return record, ok, nil
}

type APIKeyAuthenticator struct {
	Store     APIKeyStore
	AuditSink contracts.AuditSink
}

func (a APIKeyAuthenticator) Authenticate(ctx context.Context, rawAPIKey string, metadata contracts.RequestMetadata) (contracts.AuthClaims, error) {
	if a.Store == nil {
		return contracts.AuthClaims{}, errors.New("api key store is required")
	}
	if err := metadata.Validate(); err != nil {
		return contracts.AuthClaims{}, contracts.WrapValidationErr("request_metadata", err)
	}
	if rawAPIKey == "" {
		a.writeAudit(ctx, metadata, "api_key.auth", contracts.AuditOutcomeDenied, map[string]string{"reason": "missing_key"})
		return contracts.AuthClaims{}, ErrAPIKeyMissing
	}

	hash := HashAPIKey(rawAPIKey)
	record, ok, err := a.Store.GetByHash(ctx, hash)
	if err != nil {
		a.writeAudit(ctx, metadata, "api_key.auth", contracts.AuditOutcomeError, map[string]string{"reason": "store_error"})
		return contracts.AuthClaims{}, err
	}
	if !ok {
		a.writeAudit(ctx, metadata, "api_key.auth", contracts.AuditOutcomeDenied, map[string]string{"reason": "invalid_key"})
		return contracts.AuthClaims{}, ErrAPIKeyInvalid
	}
	if record.Revoked {
		a.writeAudit(ctx, metadata, "api_key.auth", contracts.AuditOutcomeDenied, map[string]string{"reason": "revoked_key", "api_key_id": record.APIKeyID})
		return contracts.AuthClaims{}, ErrAPIKeyRevoked
	}

	claims := contracts.AuthClaims{
		TenantID: record.TenantID,
		Subject:  record.Subject,
		APIKeyID: record.APIKeyID,
		Scopes:   append([]string(nil), record.AllowedScopes...),
		IssuedAt: record.IssuedAt,
	}
	if err := claims.Validate(); err != nil {
		a.writeAudit(ctx, metadata, "api_key.auth", contracts.AuditOutcomeError, map[string]string{"reason": "invalid_claims", "api_key_id": record.APIKeyID})
		return contracts.AuthClaims{}, fmt.Errorf("auth claims validation failed: %w", err)
	}

	a.writeAudit(ctx, metadata, "api_key.auth", contracts.AuditOutcomeAllowed, map[string]string{"api_key_id": record.APIKeyID})
	return claims, nil
}

func (a APIKeyAuthenticator) writeAudit(ctx context.Context, metadata contracts.RequestMetadata, eventType string, outcome contracts.AuditOutcome, attrs map[string]string) {
	if a.AuditSink == nil {
		return
	}
	_ = a.AuditSink.Write(ctx, contracts.AuditEvent{
		EventID:    fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		TenantID:   metadata.TenantID,
		EventType:  eventType,
		Resource:   "api_key",
		Actor:      metadata.RequestID,
		Outcome:    outcome,
		OccurredAt: time.Now().UTC(),
		Attributes: attrs,
	})
}
