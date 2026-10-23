package secrets

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	testPool        *pgxpool.Pool
	testQueries     *db.Queries
	testWorkspaceID pgtype.UUID
)

// TestMain mirrors server/internal/handler's pattern: connect to
// DATABASE_URL (or skip the whole suite if unreachable — these tests must
// never assume a shared/production database is sitting at the default
// address), create one throwaway workspace row, run the suite, then clean
// up everything this package wrote.
func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping secrets package tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping secrets package tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}

	testPool = pool
	testQueries = db.New(pool)

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Secrets Package Tests', 'secrets-pkg-tests', 'Throwaway workspace for server/internal/secrets tests', 'SPT')
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		fmt.Printf("Failed to set up secrets test workspace: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := testWorkspaceID.Scan(workspaceID); err != nil {
		fmt.Printf("Failed to parse test workspace id: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()

	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID); err != nil {
		fmt.Printf("Failed to clean up secrets test workspace: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

// testEncryptor returns a fresh Encryptor backed by a random master key
// set via envVar, so parallel tests (and repeated runs) never share
// key state through the process environment.
func testEncryptor(t *testing.T, envVar string) *Encryptor {
	t.Helper()
	kp := testKeyProvider(t, envVar)
	return NewEncryptor(kp)
}

func testKeyProvider(t *testing.T, envVar string) *EnvKeyProvider {
	t.Helper()
	t.Setenv(envVar, randomBase64Key(t))
	kp, err := NewEnvKeyProvider(envVar)
	if err != nil {
		t.Fatalf("NewEnvKeyProvider(%s): %v", envVar, err)
	}
	return kp
}
