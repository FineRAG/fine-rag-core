package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type MigrationRunner struct {
	Filesystem fs.FS
	Dir        string
}

func (r MigrationRunner) Apply(ctx context.Context, db *sql.DB) ([]string, error) {
	if db == nil {
		return nil, errors.New("postgres db is required")
	}
	fsys := r.Filesystem
	if fsys == nil {
		return nil, errors.New("migration filesystem is required")
	}
	dir := strings.TrimSpace(r.Dir)
	if dir == "" {
		dir = "."
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return nil, fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.ToLower(filepath.Ext(entry.Name())) != ".sql" {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	applied := make([]string, 0, len(files))
	for _, fileName := range files {
		version := strings.TrimSuffix(fileName, ".sql")
		var exists bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists)
		if err != nil {
			return nil, fmt.Errorf("lookup migration version %s: %w", version, err)
		}
		if exists {
			continue
		}

		raw, err := fs.ReadFile(fsys, filepath.Join(dir, fileName))
		if err != nil {
			return nil, fmt.Errorf("read migration file %s: %w", fileName, err)
		}
		sqlText := strings.TrimSpace(string(raw))
		if sqlText == "" {
			continue
		}

		if _, err := db.ExecContext(ctx, sqlText); err != nil {
			return nil, fmt.Errorf("apply migration %s: %w", fileName, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			return nil, fmt.Errorf("record migration %s: %w", fileName, err)
		}
		applied = append(applied, fileName)
	}

	return applied, nil
}
