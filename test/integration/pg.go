// Package integration provides a throwaway PostgreSQL via testcontainers.
// Tests skip (not fail) when Docker is unavailable.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func StartPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pgc, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("plinth"),
		postgres.WithUsername("plinth"),
		postgres.WithPassword("plinth"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	t.Cleanup(func() { _ = pgc.Terminate(ctx) })
	csn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, csn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// ApplyFixtureOn executes fixtureSQL on the pool.
func ApplyFixtureOn(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, fixtureSQL); err != nil {
		t.Fatal(err)
	}
}
