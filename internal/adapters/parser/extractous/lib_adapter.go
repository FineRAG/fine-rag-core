package extractous

import (
	"context"
	"fmt"

	"enterprise-go-rag/internal/contracts"
	"enterprise-go-rag/internal/logging"
	exgo "github.com/rahulpoonia29/extractous-go"
	"go.uber.org/zap"
)

type LibAdapter struct {
	extractor *exgo.Extractor
}

func NewLibAdapter() *LibAdapter {
	extractor := exgo.New()
	if extractor == nil {
		logging.Logger.Error("extractous.lib_adapter.init.failed: extractor is nil")
	} else {
		logging.Logger.Info("extractous.lib_adapter.init.success")
	}
	return &LibAdapter{
		extractor: extractor,
	}
}

func (a *LibAdapter) Close() {
	if a.extractor != nil {
		a.extractor.Close()
	}
}

func (a *LibAdapter) Parse(ctx context.Context, tenantID contracts.TenantID, contentType string, payload []byte) (string, error) {
	if a.extractor == nil {
		logging.Logger.Error("extractous.lib_adapter.parse.error: extractor not initialized")
		return "", fmt.Errorf("extractor not initialized")
	}

	logging.Logger.Info("extractous.lib_adapter.parse.start", zap.String("contentType", contentType), zap.Int("payloadBytes", len(payload)))
	// ExtractBytesToString returns (content, metadata, error)
	content, _, err := a.extractor.ExtractBytesToString(payload)
	if err != nil {
		logging.Logger.Error("extractous.lib_adapter.parse.failed", zap.Error(err))
		return "", fmt.Errorf("extraction failed: %w", err)
	}

	logging.Logger.Info("extractous.lib_adapter.parse.done", zap.Int("contentLen", len(content)))
	return content, nil
}
