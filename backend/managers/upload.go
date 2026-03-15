package managers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	util "enterprise-go-rag/backend/util/apiutil"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type UploadManager struct {
	Presign        *s3.PresignClient
	UploadBucket   string
	UploadBaseURL  string
	PresignTTL     time.Duration
	MaxObjectBytes int64
}

func (m *UploadManager) HandlePresign(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	var req struct {
		Files []struct {
			Name         string `json:"name"`
			Size         int64  `json:"size"`
			Type         string `json:"type"`
			RelativePath string `json:"relativePath"`
		} `json:"files"`
	}
	if err := util.DecodeJSON(r.Body, &req); err != nil {
		util.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(req.Files) == 0 {
		util.WriteError(w, http.StatusBadRequest, "files_required", "at least one file is required")
		return
	}
	base := strings.TrimRight(m.UploadBaseURL, "/")
	items := make([]map[string]any, 0, len(req.Files))
	for _, file := range req.Files {
		if file.Size <= 0 {
			util.WriteError(w, http.StatusBadRequest, "invalid_file_size", fmt.Sprintf("file %q must include size > 0", file.Name))
			return
		}
		if file.Size > m.MaxObjectBytes {
			util.WriteError(w, http.StatusBadRequest, "object_too_large", fmt.Sprintf("file %q exceeds max size of %d bytes", file.Name, m.MaxObjectBytes))
			return
		}
		rel := m.sanitizeRelativePath(file.RelativePath)
		if rel == "" {
			rel = m.sanitizeRelativePath(file.Name)
		}
		key := fmt.Sprintf("%s/%s/%s", tenantID, time.Now().UTC().Format("20060102"), rel)
		expiresAt := time.Now().UTC().Add(m.PresignTTL)
		presigned, err := m.Presign.PresignPutObject(r.Context(), &s3.PutObjectInput{
			Bucket:      &m.UploadBucket,
			Key:         &key,
			ContentType: m.optionalString(strings.TrimSpace(file.Type)),
		}, func(opts *s3.PresignOptions) {
			opts.Expires = m.PresignTTL
		})
		if err != nil {
			util.WriteError(w, http.StatusInternalServerError, "presign_failed", err.Error())
			return
		}
		uploadURL := presigned.URL
		if strings.HasPrefix(uploadURL, "https://") && strings.HasPrefix(base, "http://") {
			uploadURL = strings.Replace(uploadURL, "https://", "http://", 1)
		}
		headers := map[string]string{}
		if strings.TrimSpace(file.Type) != "" {
			headers["Content-Type"] = file.Type
		}
		items = append(items, map[string]any{
			"relativePath":     rel,
			"objectKey":        key,
			"uploadUrl":        uploadURL,
			"headers":          headers,
			"expiresAt":        expiresAt.Format(time.RFC3339),
			"expiresInSeconds": int(m.PresignTTL.Seconds()),
			"maxObjectBytes":   m.MaxObjectBytes,
		})
	}
	util.WriteJSON(w, http.StatusOK, map[string]any{"uploads": items})
}

func (m *UploadManager) sanitizeRelativePath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "file.bin"
	}
	value = strings.ReplaceAll(value, "\\", "/")
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	value = strings.TrimPrefix(value, "/")
	if strings.HasPrefix(value, "../") || value == ".." || value == "." {
		return "file.bin"
	}
	return value
}

func (m *UploadManager) optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
