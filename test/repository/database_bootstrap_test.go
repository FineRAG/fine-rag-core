package repository_test

import (
	"regexp"
	"testing"
	"testing/fstest"

	"enterprise-go-rag/internal/repository"
	runtimecfg "enterprise-go-rag/internal/runtime"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestDatabaseURLValidationCoversMissingAndMalformedCases(t *testing.T) {
	missing := runtimecfg.DatabaseConfig{Provider: "postgres"}
	if err := missing.Validate(); err == nil {
		t.Fatal("expected missing DB URL validation error")
	}

	invalid := runtimecfg.DatabaseConfig{Provider: "postgres", URL: "http://bad-host"}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected invalid DB URL scheme error")
	}

	valid := runtimecfg.DatabaseConfig{Provider: "postgres", URL: "postgres://user:pass@db.example.com:5432/postgres?sslmode=require"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid DB config, got: %v", err)
	}
	if redacted := valid.RedactedURL(); redacted == "" || regexp.MustCompile(`pass`).MatchString(redacted) {
		t.Fatalf("expected redacted URL, got %q", redacted)
	}
}

func TestMigrationBootstrapAppliesInDeterministicOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	migrationsFS := fstest.MapFS{
		"migrations/0002_second.sql": {Data: []byte("CREATE TABLE IF NOT EXISTS two(id INT);")},
		"migrations/0001_first.sql":  {Data: []byte("CREATE TABLE IF NOT EXISTS one(id INT);")},
	}

	mock.ExpectExec(regexp.QuoteMeta(`CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`)).WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`)).WithArgs("0001_first").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS one(id INT);")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO schema_migrations (version) VALUES ($1)`)).WithArgs("0001_first").WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`)).WithArgs("0002_second").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS two(id INT);")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO schema_migrations (version) VALUES ($1)`)).WithArgs("0002_second").WillReturnResult(sqlmock.NewResult(1, 1))

	runner := repository.MigrationRunner{Filesystem: migrationsFS, Dir: "migrations"}
	applied, err := runner.Apply(t.Context(), db)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if len(applied) != 2 || applied[0] != "0001_first.sql" || applied[1] != "0002_second.sql" {
		t.Fatalf("unexpected applied order: %+v", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestMigrationBootstrapSkipsPreviouslyAppliedVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock new: %v", err)
	}
	defer db.Close()

	migrationsFS := fstest.MapFS{
		"migrations/0001_first.sql":  {Data: []byte("CREATE TABLE IF NOT EXISTS one(id INT);")},
		"migrations/0002_second.sql": {Data: []byte("CREATE TABLE IF NOT EXISTS two(id INT);")},
	}

	mock.ExpectExec(regexp.QuoteMeta(`CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`)).WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`)).WithArgs("0001_first").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`)).WithArgs("0002_second").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(regexp.QuoteMeta("CREATE TABLE IF NOT EXISTS two(id INT);")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO schema_migrations (version) VALUES ($1)`)).WithArgs("0002_second").WillReturnResult(sqlmock.NewResult(1, 1))

	runner := repository.MigrationRunner{Filesystem: migrationsFS, Dir: "migrations"}
	applied, err := runner.Apply(t.Context(), db)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if len(applied) != 1 || applied[0] != "0002_second.sql" {
		t.Fatalf("unexpected applied list: %+v", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
