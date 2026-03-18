package contracts

import (
	"context"
)

// DocumentParser defines the interface for high-performance cross-format text extraction.
// This is used for complex formats like DOCX, PPTX, and Image OCR.
type DocumentParser interface {
	// Parse extracts clean text from a raw document payload.
	// It should return the extracted text or an error.
	Parse(ctx context.Context, tenantID TenantID, contentType string, payload []byte) (string, error)
}
