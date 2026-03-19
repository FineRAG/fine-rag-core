package apiutil

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type AuthClaims struct {
	Sub string `json:"sub"`
	UID int64  `json:"uid"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

type ContextKey string

const UserIDKey ContextKey = "uid"
const RequestIDKey ContextKey = "request_id"

func EnvOr(key string, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func EnvOrSecret(key string, fallback string) string {
	if filePath := strings.TrimSpace(os.Getenv(key + "_FILE")); filePath != "" {
		if raw, err := os.ReadFile(filePath); err == nil {
			if value := strings.TrimSpace(string(raw)); value != "" {
				return value
			}
		}
	}
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func EnvOrAny(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func EnvOrSecretAny(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(EnvOrSecret(key, "")); value != "" {
			return value
		}
	}
	return fallback
}

func EnvBoolAny(keys []string, fallback bool) bool {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}

func SplitCSV(raw string) []string {
	out := make([]string, 0)
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func HashSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256$" + hex.EncodeToString(sum[:])
}

func RandomString(n int) string {
	if n <= 0 {
		n = 8
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)[:n]
}

func SignJWT(claims AuthClaims, secret string) (string, error) {
	h, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	c, _ := json.Marshal(claims)
	left := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	sig := hmacSHA256([]byte(secret), []byte(left))
	return left + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func VerifyJWT(token string, secret string) (AuthClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AuthClaims{}, fmt.Errorf("invalid token")
	}
	left := parts[0] + "." + parts[1]
	want := hmacSHA256([]byte(secret), []byte(left))
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return AuthClaims{}, err
	}
	if !hmac.Equal(want, got) {
		return AuthClaims{}, fmt.Errorf("signature mismatch")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AuthClaims{}, err
	}
	var claims AuthClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return AuthClaims{}, err
	}
	if claims.Exp <= time.Now().UTC().Unix() {
		return AuthClaims{}, fmt.Errorf("expired")
	}
	return claims, nil
}

func VerifySecret(raw string, stored string) bool {
	if strings.TrimSpace(stored) == "" {
		return false
	}
	if strings.HasPrefix(stored, "sha256$") {
		return hmac.Equal([]byte(HashSecret(raw)), []byte(stored))
	}
	return hmac.Equal([]byte(raw), []byte(stored))
}

func hmacSHA256(secret []byte, data []byte) []byte {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func DecodeJSON(body io.ReadCloser, out any) error {
	defer body.Close()
	d := json.NewDecoder(io.LimitReader(body, 2<<20))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, code int, errCode string, message string) {
	WriteJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": message}})
}

func RandomUUIDv4Like() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-" + RandomString(16)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16],
	)
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	uid, ok := ctx.Value(UserIDKey).(int64)
	return uid, ok
}

func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(RequestIDKey).(string)
	return strings.TrimSpace(value)
}

func TruncateForDebugLog(input string, maxLen int) string {
	value := strings.TrimSpace(input)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}
