package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	"enterprise-go-rag/internal/backend"
	"enterprise-go-rag/internal/repository"
)

func main() {
	cfg := backend.ConfigFromEnv()

	db, auditRepo, retrievalSvc, err := backend.BuildRuntimeDependencies()
	if err != nil {
		log.Fatalf("failed to initialize runtime dependencies: %v", err)
	}
	defer db.Close()

	if err := applyMigrations(context.Background(), db); err != nil {
		log.Fatalf("failed to apply migrations: %v", err)
	}

	srv, err := backend.NewServer(cfg, db, auditRepo, retrievalSvc)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	if err := srv.EnsureBootstrapData(context.Background()); err != nil {
		log.Fatalf("failed to ensure bootstrap data: %v", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("finerag backend listening on %s", cfg.Addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	runner := repository.MigrationRunner{Filesystem: os.DirFS("migrations"), Dir: "."}
	_, err := runner.Apply(ctx, db)
	return err
}
