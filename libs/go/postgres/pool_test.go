package postgres

import (
	"testing"
	"time"
)

func TestParsePoolConfigAppliesBounds(t *testing.T) {
	t.Parallel()

	cfg, err := ParsePoolConfig(PoolSettings{
		DSN:               "postgres://user:pass@localhost:5432/kodex?sslmode=disable",
		MaxConns:          12,
		MinConns:          3,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   10 * time.Minute,
		HealthCheckPeriod: time.Minute,
	})
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	if cfg.MaxConns != 12 || cfg.MinConns != 3 {
		t.Fatalf("unexpected pool bounds: max=%d min=%d", cfg.MaxConns, cfg.MinConns)
	}
	if cfg.MaxConnLifetime != time.Hour {
		t.Fatalf("unexpected max conn lifetime: %s", cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != 10*time.Minute {
		t.Fatalf("unexpected max conn idle time: %s", cfg.MaxConnIdleTime)
	}
	if cfg.HealthCheckPeriod != time.Minute {
		t.Fatalf("unexpected health check period: %s", cfg.HealthCheckPeriod)
	}
}

func TestParsePoolConfigRequiresDSN(t *testing.T) {
	t.Parallel()

	if _, err := ParsePoolConfig(PoolSettings{}); err == nil {
		t.Fatal("expected error for empty dsn")
	}
}

func TestParsePoolConfigRejectsMinGreaterThanMax(t *testing.T) {
	t.Parallel()

	_, err := ParsePoolConfig(PoolSettings{
		DSN:      "postgres://user:pass@localhost:5432/kodex?sslmode=disable",
		MaxConns: 2,
		MinConns: 3,
	})
	if err == nil {
		t.Fatal("expected error for invalid pool bounds")
	}
}
