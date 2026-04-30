package app

import (
	"testing"
	"time"
)

func TestLoadConfigRequiresDatabaseDSN(t *testing.T) {
	t.Setenv("KODEX_ACCESS_MANAGER_DATABASE_DSN", "")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() err = nil, want required database DSN error")
	}
}

func TestLoadConfigAcceptsDatabaseDSNFromEnvironment(t *testing.T) {
	t.Setenv("KODEX_ACCESS_MANAGER_DATABASE_DSN", "postgres://postgres:5432/kodex_access_manager?sslmode=disable")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.DatabaseDSN == "" {
		t.Fatal("DatabaseDSN is empty")
	}
}

func TestDatabasePoolSettingsIncludesRetryConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{
		DatabaseDSN:               "postgres://postgres:5432/kodex_access_manager?sslmode=disable",
		DatabaseMaxConns:          8,
		DatabaseMinConns:          1,
		DatabaseMaxConnLifetime:   time.Hour,
		DatabaseMaxConnIdleTime:   15 * time.Minute,
		DatabaseHealthCheckPeriod: 30 * time.Second,
		DatabasePingTimeout:       5 * time.Second,
		DatabaseRetryMaxAttempts:  6,
		DatabaseRetryInitialDelay: 500 * time.Millisecond,
		DatabaseRetryMaxDelay:     5 * time.Second,
		DatabaseRetryJitterRatio:  0.2,
	}

	settings := cfg.DatabasePoolSettings()
	if settings.ConnectRetryMaxAttempts != cfg.DatabaseRetryMaxAttempts {
		t.Fatalf("ConnectRetryMaxAttempts = %d, want %d", settings.ConnectRetryMaxAttempts, cfg.DatabaseRetryMaxAttempts)
	}
	if settings.ConnectRetryInitialDelay != cfg.DatabaseRetryInitialDelay {
		t.Fatalf("ConnectRetryInitialDelay = %s, want %s", settings.ConnectRetryInitialDelay, cfg.DatabaseRetryInitialDelay)
	}
	if settings.ConnectRetryMaxDelay != cfg.DatabaseRetryMaxDelay {
		t.Fatalf("ConnectRetryMaxDelay = %s, want %s", settings.ConnectRetryMaxDelay, cfg.DatabaseRetryMaxDelay)
	}
	if settings.ConnectRetryJitterRatio != cfg.DatabaseRetryJitterRatio {
		t.Fatalf("ConnectRetryJitterRatio = %f, want %f", settings.ConnectRetryJitterRatio, cfg.DatabaseRetryJitterRatio)
	}
}
