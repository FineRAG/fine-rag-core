package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type DatabaseConfig struct {
	Provider string
	URL      string

	ConnectTimeout  time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
	MaxConnLifetime time.Duration
}

func LoadDatabaseConfigFromEnv(lookupEnv func(string) (string, bool)) DatabaseConfig {
	provider := "memory"
	if v, ok := lookupEnv("FINE_RAG_DB_PROVIDER"); ok && strings.TrimSpace(v) != "" {
		provider = strings.ToLower(strings.TrimSpace(v))
	}

	cfg := DatabaseConfig{
		Provider: provider,
		URL:      strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_DATABASE_URL")),
	}

	if v := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_DB_CONNECT_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.ConnectTimeout = d
		}
	}
	if v := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_DB_MAX_OPEN_CONNS")); v != "" {
		fmt.Sscanf(v, "%d", &cfg.MaxOpenConns)
	}
	if v := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_DB_MAX_IDLE_CONNS")); v != "" {
		fmt.Sscanf(v, "%d", &cfg.MaxIdleConns)
	}
	if v := strings.TrimSpace(getEnv(lookupEnv, "FINE_RAG_DB_MAX_CONN_LIFETIME")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.MaxConnLifetime = d
		}
	}

	return cfg.withDefaults()
}

func (c DatabaseConfig) withDefaults() DatabaseConfig {
	if c.Provider == "" {
		c.Provider = "memory"
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.MaxOpenConns <= 0 {
		c.MaxOpenConns = 10
	}
	if c.MaxIdleConns < 0 {
		c.MaxIdleConns = 0
	}
	if c.MaxConnLifetime <= 0 {
		c.MaxConnLifetime = 30 * time.Minute
	}
	return c
}

func (c DatabaseConfig) Validate() error {
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	switch provider {
	case "memory":
		return nil
	case "postgres":
		if strings.TrimSpace(c.URL) == "" {
			return errors.New("FINE_RAG_DATABASE_URL is required when FINE_RAG_DB_PROVIDER=postgres")
		}
		u, err := url.Parse(c.URL)
		if err != nil {
			return fmt.Errorf("invalid FINE_RAG_DATABASE_URL: %w", err)
		}
		if u.Scheme != "postgres" && u.Scheme != "postgresql" {
			return fmt.Errorf("invalid DB URL scheme %q", u.Scheme)
		}
		if u.Host == "" {
			return errors.New("database host is required in FINE_RAG_DATABASE_URL")
		}
		return nil
	default:
		return fmt.Errorf("unsupported FINE_RAG_DB_PROVIDER %q", c.Provider)
	}
}

func (c DatabaseConfig) RedactedURL() string {
	u, err := url.Parse(c.URL)
	if err != nil {
		return ""
	}
	if u.User != nil {
		username := u.User.Username()
		u.User = url.UserPassword(username, "REDACTED")
	}
	return u.String()
}

func OpenPostgresDB(ctx context.Context, openFn func(driverName, dataSourceName string) (*sql.DB, error), cfg DatabaseConfig) (*sql.DB, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if strings.ToLower(cfg.Provider) != "postgres" {
		return nil, errors.New("database provider is not postgres")
	}
	if openFn == nil {
		openFn = sql.Open
	}

	db, err := openFn("postgres", cfg.URL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.MaxConnLifetime)

	timedCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(timedCtx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func getEnv(lookupEnv func(string) (string, bool), key string) string {
	if lookupEnv == nil {
		return ""
	}
	v, _ := lookupEnv(key)
	return v
}
