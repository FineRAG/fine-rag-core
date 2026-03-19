package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"enterprise-go-rag/backend"
	"enterprise-go-rag/backend/util"
	"enterprise-go-rag/internal/logging"
	"enterprise-go-rag/internal/telemetry"

	"go.uber.org/zap"
)

func main() {
	if err := logging.Init(); err != nil {
		panic("failed to initialize zap logger: " + err.Error())
	}
	defer logging.Sync()

	shutdownTelemetry, err := telemetry.Init(context.Background())
	if err != nil {
		logging.Logger.Fatal("failed to initialize telemetry", zap.Error(err))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			logging.Logger.Warn("failed to shutdown telemetry cleanly", zap.Error(err))
		}
	}()

	cfg := util.ConfigFromEnv()

	db, auditRepo, vectorIndex, retrievalSvc, err := util.BuildRuntimeDependencies()
	if err != nil {
		logging.Logger.Fatal("failed to initialize runtime dependencies", zap.Error(err))
	}
	defer db.Close()

	if err := applyMigrations(context.Background(), db); err != nil {
		logging.Logger.Fatal("failed to apply migrations", zap.Error(err))
	}

	srv, err := util.NewServer(cfg, db, auditRepo, retrievalSvc, vectorIndex)
	if err != nil {
		logging.Logger.Fatal("failed to create server", zap.Error(err))
	}
	if err := srv.EnsureBootstrapData(context.Background()); err != nil {
		logging.Logger.Fatal("failed to ensure bootstrap data", zap.Error(err))
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           telemetry.WrapHandler("http.server", backend.MainAPIHandler(srv)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logging.Logger.Info("finerag backend listening", zap.String("addr", cfg.Addr))
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logging.Logger.Fatal("server failed", zap.Error(err))
	}
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	patterns := []string{"migrations/*.sql", "/app/migrations/*.sql"}
	files := make([]string, 0, 8)
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			files = append(files, m)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("migration files not found in expected patterns: %v", patterns)
	}
	sort.Strings(files)

	for _, path := range files {
		sqlBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "already exists") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				continue
			}
			return fmt.Errorf("apply migration %s: %w", path, err)
		}
	}
	return nil
}
