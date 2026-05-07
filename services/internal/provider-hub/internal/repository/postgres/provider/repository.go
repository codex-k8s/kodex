package provider

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository stores provider-hub state in PostgreSQL.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a PostgreSQL repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		panic("provider-hub postgres pool is required")
	}
	return &Repository{pool: pool}
}

// Ping verifies connectivity to provider-hub storage.
func (r *Repository) Ping(ctx context.Context) error {
	if err := r.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping provider-hub postgres: %w", err)
	}
	return nil
}
