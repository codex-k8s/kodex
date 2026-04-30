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

func TestNormalizeConnectRetryAppliesDefaults(t *testing.T) {
	t.Parallel()

	retry, err := normalizeConnectRetry(PoolSettings{})
	if err != nil {
		t.Fatalf("normalize connect retry: %v", err)
	}
	if retry.maxAttempts != defaultConnectRetryMaxAttempts {
		t.Fatalf("unexpected max attempts: %d", retry.maxAttempts)
	}
	if retry.initialDelay != defaultConnectRetryInitialDelay {
		t.Fatalf("unexpected initial delay: %s", retry.initialDelay)
	}
	if retry.maxDelay != defaultConnectRetryMaxDelay {
		t.Fatalf("unexpected max delay: %s", retry.maxDelay)
	}
	if retry.jitterRatio != defaultConnectRetryJitterRatio {
		t.Fatalf("unexpected jitter ratio: %f", retry.jitterRatio)
	}
	if retry.pingTimeout != defaultPingTimeout {
		t.Fatalf("unexpected ping timeout: %s", retry.pingTimeout)
	}
}

func TestNormalizeConnectRetryRejectsInvalidSettings(t *testing.T) {
	t.Parallel()

	cases := map[string]PoolSettings{
		"attempts": {
			ConnectRetryMaxAttempts: -1,
		},
		"initial delay": {
			ConnectRetryInitialDelay: -time.Second,
		},
		"max delay": {
			ConnectRetryMaxDelay: -time.Second,
		},
		"delay order": {
			ConnectRetryInitialDelay: 2 * time.Second,
			ConnectRetryMaxDelay:     time.Second,
		},
		"jitter": {
			ConnectRetryJitterRatio: 1.1,
		},
		"ping timeout": {
			PingTimeout: -time.Second,
		},
	}
	for name, settings := range cases {
		name := name
		settings := settings
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := normalizeConnectRetry(settings); err == nil {
				t.Fatal("expected invalid retry settings error")
			}
		})
	}
}

func TestConnectRetryDelayUsesBackoffCapAndJitter(t *testing.T) {
	t.Parallel()

	settings := connectRetrySettings{
		initialDelay: time.Second,
		maxDelay:     3 * time.Second,
		jitterRatio:  0.25,
	}

	if delay := connectRetryDelay(settings, 1, 0); delay != time.Second {
		t.Fatalf("attempt 1 delay = %s, want 1s", delay)
	}
	if delay := connectRetryDelay(settings, 2, 0); delay != 2*time.Second {
		t.Fatalf("attempt 2 delay = %s, want 2s", delay)
	}
	if delay := connectRetryDelay(settings, 3, 0); delay != 3*time.Second {
		t.Fatalf("attempt 3 delay = %s, want capped 3s", delay)
	}
	if delay := connectRetryDelay(settings, 3, 1); delay != 3750*time.Millisecond {
		t.Fatalf("attempt 3 delay with jitter = %s, want 3.75s", delay)
	}
}
