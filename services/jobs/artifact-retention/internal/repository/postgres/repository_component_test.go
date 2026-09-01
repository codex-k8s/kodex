package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestClaimExecutesWithRuntimeRole(t *testing.T) {
	dsn := os.Getenv("KODEX_ARTIFACT_RETENTION_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_ARTIFACT_RETENTION_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	defer pool.Close()
	claims, err := New(pool).Claim(ctx, "component-test", 1, 30)
	if err != nil {
		t.Fatalf("claim due artifacts with runtime role: %v", err)
	}
	if len(claims) != 0 {
		t.Fatalf("empty migrated database returned retention claims: %#v", claims)
	}
}
