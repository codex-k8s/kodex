package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultPingTimeout = 5 * time.Second

// PoolSettings contains bounded pgxpool settings controlled by service config.
type PoolSettings struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	PingTimeout       time.Duration
}

// OpenPool creates a pgxpool and verifies it with Ping.
func OpenPool(ctx context.Context, settings PoolSettings) (*pgxpool.Pool, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	cfg, err := ParsePoolConfig(settings)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	pingTimeout := settings.PingTimeout
	if pingTimeout <= 0 {
		pingTimeout = defaultPingTimeout
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres pool: %w", err)
	}
	return pool, nil
}

// ParsePoolConfig parses DSN and applies explicit pool bounds.
func ParsePoolConfig(settings PoolSettings) (*pgxpool.Config, error) {
	if strings.TrimSpace(settings.DSN) == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}
	cfg, err := pgxpool.ParseConfig(settings.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse postgres pool config: %w", err)
	}
	if settings.MaxConns > 0 {
		cfg.MaxConns = settings.MaxConns
	}
	if settings.MinConns > 0 {
		cfg.MinConns = settings.MinConns
	}
	if cfg.MaxConns > 0 && cfg.MinConns > cfg.MaxConns {
		return nil, fmt.Errorf("postgres min_conns must be <= max_conns")
	}
	if settings.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = settings.MaxConnLifetime
	}
	if settings.MaxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = settings.MaxConnIdleTime
	}
	if settings.HealthCheckPeriod > 0 {
		cfg.HealthCheckPeriod = settings.HealthCheckPeriod
	}
	return cfg, nil
}
