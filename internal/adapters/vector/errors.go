package vector

import (
	"context"
	"errors"
	"strings"

	"enterprise-go-rag/internal/contracts"
)

func NormalizeProviderError(provider string, op string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	category := contracts.ProviderErrInternal
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "timeout"):
		category = contracts.ProviderErrTimeout
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "auth"):
		category = contracts.ProviderErrUnauthorized
	case strings.Contains(msg, "invalid"), strings.Contains(msg, "malformed"), strings.Contains(msg, "required"):
		category = contracts.ProviderErrValidation
	case strings.Contains(msg, "unavailable"), strings.Contains(msg, "connection refused"), strings.Contains(msg, "temporarily"):
		category = contracts.ProviderErrUnavailable
	}
	return contracts.ProviderError{
		Category: category,
		Provider: provider,
		Op:       op,
		Err:      err,
	}
}
