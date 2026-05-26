package app

import (
	"testing"
	"time"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
)

func TestLoadConfigRequiresDatabaseDSN(t *testing.T) {
	t.Setenv("KODEX_PROJECT_CATALOG_DATABASE_DSN", "")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() err = nil, want required database DSN error")
	}
}

func TestLoadConfigAllowsMissingGRPCAuthTokenWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_PROJECT_CATALOG_DATABASE_DSN", "postgres://postgres:5432/kodex_project_catalog?sslmode=disable")
	t.Setenv("KODEX_PROJECT_CATALOG_GRPC_AUTH_REQUIRED", "false")
	t.Setenv("KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN", "")
	t.Setenv("KODEX_PROJECT_CATALOG_ACCESS_CHECK_ENABLED", "false")
	t.Setenv("KODEX_PROJECT_CATALOG_PROVIDER_HUB_BOOTSTRAP_ENABLED", "false")
	t.Setenv("KODEX_PROJECT_CATALOG_OUTBOX_DISPATCH_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.GRPCAuthRequired {
		t.Fatal("GRPCAuthRequired = true, want false")
	}
}

func TestDatabasePoolSettingsIncludesRetryConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()

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

func validConfig() Config {
	return Config{
		HTTPAddr:                    ":8080",
		GRPCAddr:                    ":9090",
		GRPCAuthRequired:            true,
		GRPCAuthToken:               "test-token",
		GRPCMaxInFlight:             128,
		GRPCMaxConcurrentStreams:    128,
		GRPCUnaryTimeout:            30 * time.Second,
		GRPCKeepaliveTime:           2 * time.Minute,
		GRPCKeepaliveTimeout:        20 * time.Second,
		GRPCKeepaliveMinTime:        30 * time.Second,
		GRPCMaxRecvMessageBytes:     4 * 1024 * 1024,
		GRPCMaxSendMessageBytes:     4 * 1024 * 1024,
		DatabaseDSN:                 "postgres://postgres:5432/kodex_project_catalog?sslmode=disable",
		DatabaseMaxConns:            8,
		DatabaseMinConns:            1,
		DatabaseMaxConnLifetime:     time.Hour,
		DatabaseMaxConnIdleTime:     15 * time.Minute,
		DatabaseHealthCheckPeriod:   30 * time.Second,
		DatabasePingTimeout:         5 * time.Second,
		DatabaseRetryMaxAttempts:    6,
		DatabaseRetryInitialDelay:   500 * time.Millisecond,
		DatabaseRetryMaxDelay:       5 * time.Second,
		DatabaseRetryJitterRatio:    0.2,
		AccessCheckEnabled:          true,
		AccessManagerGRPCAddr:       "access-manager:9090",
		AccessManagerGRPCAuthToken:  "test-access-token",
		AccessManagerCheckTimeout:   3 * time.Second,
		ProviderHubBootstrapEnabled: true,
		ProviderHubGRPCAddr:         "provider-hub:9090",
		ProviderHubGRPCAuthToken:    "provider-token",
		ProviderHubRequestTimeout:   5 * time.Second,
		EventLogDatabaseDSN:         "postgres://postgres:5432/kodex_platform_event_log?sslmode=disable",
		EventLogDatabaseMaxConns:    4,
		EventLogDatabaseMinConns:    0,
		OutboxDispatchEnabled:       true,
		OutboxPublisherKind:         outboxlib.PublisherKindPostgresEventLog,
		OutboxEventLogSource:        "project-catalog",
		OutboxBatchSize:             100,
		OutboxPollInterval:          time.Second,
		OutboxLockTTL:               30 * time.Second,
		OutboxPublishTimeout:        10 * time.Second,
		OutboxLeaseSafetyMargin:     5 * time.Second,
		OutboxRetryInitialDelay:     time.Second,
		OutboxRetryMaxDelay:         time.Minute,
		OutboxFailureMessageLimit:   512,
	}
}
