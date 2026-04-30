package postgres

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultPingTimeout              = 5 * time.Second
	defaultConnectRetryMaxAttempts  = 6
	defaultConnectRetryInitialDelay = 500 * time.Millisecond
	defaultConnectRetryMaxDelay     = 5 * time.Second
	defaultConnectRetryJitterRatio  = 0.2
)

// PoolSettings contains bounded pgxpool settings controlled by service config.
type PoolSettings struct {
	DSN                      string
	MaxConns                 int32
	MinConns                 int32
	MaxConnLifetime          time.Duration
	MaxConnIdleTime          time.Duration
	HealthCheckPeriod        time.Duration
	PingTimeout              time.Duration
	ConnectRetryMaxAttempts  int
	ConnectRetryInitialDelay time.Duration
	ConnectRetryMaxDelay     time.Duration
	ConnectRetryJitterRatio  float64
}

// OpenPool creates a pgxpool and verifies it with Ping using bounded retry.
func OpenPool(ctx context.Context, settings PoolSettings) (*pgxpool.Pool, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	cfg, err := ParsePoolConfig(settings)
	if err != nil {
		return nil, err
	}
	retry, err := normalizeConnectRetry(settings)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; attempt <= retry.maxAttempts; attempt++ {
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if pingErr := pingPool(ctx, pool, retry.pingTimeout); pingErr == nil {
				return pool, nil
			} else {
				err = pingErr
			}
			pool.Close()
		}
		lastErr = err
		if attempt == retry.maxAttempts {
			break
		}
		delay := connectRetryDelay(retry, attempt, rand.Float64())
		if err := sleepContext(ctx, delay); err != nil {
			return nil, fmt.Errorf("open postgres pool canceled after attempt %d: %w", attempt, err)
		}
	}
	return nil, fmt.Errorf("open postgres pool after %d attempts: %w", retry.maxAttempts, lastErr)
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

type connectRetrySettings struct {
	maxAttempts  int
	initialDelay time.Duration
	maxDelay     time.Duration
	jitterRatio  float64
	pingTimeout  time.Duration
}

func normalizeConnectRetry(settings PoolSettings) (connectRetrySettings, error) {
	retry := connectRetrySettings{
		maxAttempts:  settings.ConnectRetryMaxAttempts,
		initialDelay: settings.ConnectRetryInitialDelay,
		maxDelay:     settings.ConnectRetryMaxDelay,
		jitterRatio:  settings.ConnectRetryJitterRatio,
		pingTimeout:  settings.PingTimeout,
	}
	if retry.maxAttempts == 0 {
		retry.maxAttempts = defaultConnectRetryMaxAttempts
	}
	if retry.initialDelay == 0 {
		retry.initialDelay = defaultConnectRetryInitialDelay
	}
	if retry.maxDelay == 0 {
		retry.maxDelay = defaultConnectRetryMaxDelay
	}
	if retry.jitterRatio == 0 {
		retry.jitterRatio = defaultConnectRetryJitterRatio
	}
	if retry.pingTimeout == 0 {
		retry.pingTimeout = defaultPingTimeout
	}
	if retry.maxAttempts < 1 {
		return connectRetrySettings{}, fmt.Errorf("postgres connect retry max attempts must be >= 1")
	}
	if retry.initialDelay < 0 {
		return connectRetrySettings{}, fmt.Errorf("postgres connect retry initial delay must be >= 0")
	}
	if retry.maxDelay < 0 {
		return connectRetrySettings{}, fmt.Errorf("postgres connect retry max delay must be >= 0")
	}
	if retry.maxDelay > 0 && retry.initialDelay > retry.maxDelay {
		return connectRetrySettings{}, fmt.Errorf("postgres connect retry initial delay must be <= max delay")
	}
	if retry.jitterRatio < 0 || retry.jitterRatio > 1 {
		return connectRetrySettings{}, fmt.Errorf("postgres connect retry jitter ratio must be between 0 and 1")
	}
	if retry.pingTimeout < 0 {
		return connectRetrySettings{}, fmt.Errorf("postgres ping timeout must be >= 0")
	}
	return retry, nil
}

func pingPool(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("ping postgres pool: %w", err)
	}
	return nil
}

func connectRetryDelay(settings connectRetrySettings, attempt int, random float64) time.Duration {
	delay := settings.initialDelay
	for step := 1; step < attempt; step++ {
		if delay >= settings.maxDelay/2 {
			delay = settings.maxDelay
			break
		}
		delay *= 2
	}
	jitter := time.Duration(float64(delay) * settings.jitterRatio * random)
	return delay + jitter
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
