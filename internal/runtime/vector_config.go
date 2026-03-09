package runtime

import (
	"errors"
	"fmt"
	"strings"
)

type VectorConfig struct {
	Provider   string
	Endpoint   string
	Database   string
	Collection string
	Username   string
	Password   string
	TLS        bool
}

func LoadVectorConfigFromEnv(lookupEnv func(string) (string, bool)) VectorConfig {
	provider := "milvus"
	if v, ok := lookupEnv("FINE_RAG_VECTOR_PROVIDER"); ok && strings.TrimSpace(v) != "" {
		provider = strings.ToLower(strings.TrimSpace(v))
	}
	tls := true
	if v, ok := lookupEnv("FINE_RAG_MILVUS_TLS"); ok && strings.EqualFold(strings.TrimSpace(v), "false") {
		tls = false
	}
	return VectorConfig{
		Provider:   provider,
		Endpoint:   strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_MILVUS_ENDPOINT")),
		Database:   strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_MILVUS_DATABASE")),
		Collection: strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_MILVUS_COLLECTION")),
		Username:   strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_MILVUS_USERNAME")),
		Password:   strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_MILVUS_PASSWORD")),
		TLS:        tls,
	}
}

func (c VectorConfig) Validate() error {
	switch c.Provider {
	case "milvus":
		if c.Endpoint == "" {
			return errors.New("FINE_RAG_MILVUS_ENDPOINT is required when FINE_RAG_VECTOR_PROVIDER=milvus")
		}
		if c.Database == "" {
			return errors.New("FINE_RAG_MILVUS_DATABASE is required when FINE_RAG_VECTOR_PROVIDER=milvus")
		}
		if c.Collection == "" {
			return errors.New("FINE_RAG_MILVUS_COLLECTION is required when FINE_RAG_VECTOR_PROVIDER=milvus")
		}
		if !c.TLS {
			return errors.New("FINE_RAG_MILVUS_TLS must be true for milvus provider")
		}
		return nil
	case "stub":
		return nil
	default:
		return fmt.Errorf("unsupported FINE_RAG_VECTOR_PROVIDER %q", c.Provider)
	}
}

func (c VectorConfig) RedactedPassword() string {
	if strings.TrimSpace(c.Password) == "" {
		return ""
	}
	return "REDACTED"
}
