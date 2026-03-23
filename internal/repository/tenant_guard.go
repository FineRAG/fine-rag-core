package repository

import (
	"context"
	"errors"
	"fmt"

	"enterprise-go-rag/internal/contracts"
)

var ErrUnscopedRepositoryAccess = errors.New("repository access requires tenant scope")

func GuardReadScope(ctx context.Context, targetTenant contracts.TenantID) error {
	if err := contracts.EnsureTenantMatch(ctx, targetTenant); err != nil {
		return fmt.Errorf("%w: %v", ErrUnscopedRepositoryAccess, err)
	}
	return nil
}

func GuardWriteScope(ctx context.Context, targetTenant contracts.TenantID) error {
	if err := contracts.EnsureTenantMatch(ctx, targetTenant); err != nil {
		return fmt.Errorf("%w: %v", ErrUnscopedRepositoryAccess, err)
	}
	return nil
}
