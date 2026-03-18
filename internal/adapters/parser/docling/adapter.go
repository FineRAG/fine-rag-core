package docling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"enterprise-go-rag/internal/contracts"
)

type Config struct {
	Endpoint string // e.g., "http://localhost:5000"
	Timeout  time.Duration
}

type Adapter struct {
	cfg    Config
	client *http.Client
}

func NewAdapter(cfg Config) *Adapter {
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &Adapter{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (a *Adapter) Parse(ctx context.Context, tenantID contracts.TenantID, contentType string, payload []byte) (string, error) {
	if a.cfg.Endpoint == "" {
		return "", fmt.Errorf("docling endpoint is required")
	}

	// docling-serve usually has a /convert or similar endpoint
	// Based on ds4sd/docling-serve:
	// POST /convert/task
	
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	
	// Docling serve expects a 'file' field
	part, err := writer.CreateFormFile("file", "document")
	if err != nil {
		return "", fmt.Errorf("failed to create multipart part: %w", err)
	}
	if _, err := part.Write(payload); err != nil {
		return "", fmt.Errorf("failed to write payload to multipart: %w", err)
	}
	writer.Close()

	url := fmt.Sprintf("%s/convert", a.cfg.Endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Tenant-ID", string(tenantID))

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("docling service call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("docling service error (status %d): %s", resp.StatusCode, string(body))
	}

	// Docling returns the converted text/markdown
	var result struct {
		Markdown string `json:"markdown"`
		Text     string `json:"text"`
	}
	
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read docling response: %w", err)
	}
	
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		// Fallback: try reading raw body if it's not JSON
		return string(bodyBytes), nil
	}

	if result.Markdown != "" {
		return result.Markdown, nil
	}
	return result.Text, nil
}
