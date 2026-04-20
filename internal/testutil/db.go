package testutil

import (
	"context"
	"database/sql"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/platform"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// moduleRoot returns the absolute path of the Go module root.
func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	// GOMODCACHE is not what we want; use `go list` to get the module root.
	out, err = exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// NewTestDB starts a throwaway Postgres container, runs all migrations, seeds
// the hardcoded user, and returns a ready *sql.DB. The container is
// automatically terminated when the test finishes.
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("fishhub_test"),
		tcpostgres.WithUsername("fishhub"),
		tcpostgres.WithPassword("fishhub"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	migrationsPath := filepath.Join(moduleRoot(t), "db", "migrations")
	if err := platform.Migrate(sqlDB, migrationsPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := platform.SeedUser(sqlDB); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return sqlDB
}
