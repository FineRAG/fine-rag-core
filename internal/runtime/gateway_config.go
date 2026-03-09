package runtime

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type GatewayConfig struct {
	Provider                string
	PortkeyBaseURL          string
	PortkeyAPIKey           string
	Timeout                 time.Duration
	RetryMax                int
	CircuitFailureThreshold int
	FallbackMode            string
	DirectAllowlist         []string
}

func LoadGatewayConfigFromEnv(lookupEnv func(string) (string, bool)) GatewayConfig {
	provider := "stub"
	if v, ok := lookupEnv("FINE_RAG_GATEWAY_PROVIDER"); ok && strings.TrimSpace(v) != "" {
		provider = strings.ToLower(strings.TrimSpace(v))
	}
	timeout := 250 * time.Millisecond
	if v := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_GATEWAY_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		}
	}
	retryMax := 1
	if v := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_GATEWAY_RETRY_MAX")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			retryMax = n
		}
	}
	threshold := 3
	if v := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_GATEWAY_CIRCUIT_FAILURE_THRESHOLD")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			threshold = n
		}
	}
	fallback := strings.ToLower(strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_GATEWAY_FALLBACK_MODE")))
	if fallback == "" {
		fallback = "retrieval_only"
	}

	allowlistRaw := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_GATEWAY_DIRECT_ALLOWLIST"))
	allowlist := make([]string, 0)
	if allowlistRaw != "" {
		for _, item := range strings.Split(allowlistRaw, ",") {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				allowlist = append(allowlist, trimmed)
			}
		}
	}

	return GatewayConfig{
		Provider:                provider,
		PortkeyBaseURL:          strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_PORTKEY_BASE_URL")),
		PortkeyAPIKey:           strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_PORTKEY_API_KEY")),
		Timeout:                 timeout,
		RetryMax:                retryMax,
		CircuitFailureThreshold: threshold,
		FallbackMode:            fallback,
		DirectAllowlist:         allowlist,
	}
}

func (c GatewayConfig) Validate() error {
	switch c.Provider {
	case "stub":
		return validateFallbackMode(c.FallbackMode)
	case "portkey":
		if strings.TrimSpace(c.PortkeyBaseURL) == "" {
			return errors.New("FINE_RAG_PORTKEY_BASE_URL is required when FINE_RAG_GATEWAY_PROVIDER=portkey")
		}
		if strings.TrimSpace(c.PortkeyAPIKey) == "" {
			return errors.New("FINE_RAG_PORTKEY_API_KEY is required when FINE_RAG_GATEWAY_PROVIDER=portkey")
		}
		if c.Timeout <= 0 {
			return errors.New("FINE_RAG_GATEWAY_TIMEOUT must be > 0")
		}
		if c.RetryMax < 0 {
			return errors.New("FINE_RAG_GATEWAY_RETRY_MAX must be >= 0")
		}
		if c.CircuitFailureThreshold <= 0 {
			return errors.New("FINE_RAG_GATEWAY_CIRCUIT_FAILURE_THRESHOLD must be > 0")
		}
		return validateFallbackMode(c.FallbackMode)
	default:
		return fmt.Errorf("unsupported FINE_RAG_GATEWAY_PROVIDER %q", c.Provider)
	}
}

func (c GatewayConfig) RedactedAPIKey() string {
	if strings.TrimSpace(c.PortkeyAPIKey) == "" {
		return ""
	}
	return "REDACTED"
}

func validateFallbackMode(mode string) error {
	switch mode {
	case "fail_closed", "direct_allowlist", "retrieval_only":
		return nil
	default:
		return fmt.Errorf("unsupported FINE_RAG_GATEWAY_FALLBACK_MODE %q", mode)
	}
}
