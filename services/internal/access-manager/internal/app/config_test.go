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
	t.Setenv("KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN", "test-token")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.DatabaseDSN == "" {
		t.Fatal("DatabaseDSN is empty")
	}
}

func TestValidateRequiresGRPCAuthTokenWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want gRPC auth token error")
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

func TestOutboxDispatcherConfigIncludesRetryConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()

	outboxCfg := cfg.OutboxDispatcherConfig()
	if outboxCfg.BatchSize != cfg.OutboxBatchSize {
		t.Fatalf("BatchSize = %d, want %d", outboxCfg.BatchSize, cfg.OutboxBatchSize)
	}
	if outboxCfg.RetryInitialDelay != cfg.OutboxRetryInitialDelay {
		t.Fatalf("RetryInitialDelay = %s, want %s", outboxCfg.RetryInitialDelay, cfg.OutboxRetryInitialDelay)
	}
	if outboxCfg.RetryMaxDelay != cfg.OutboxRetryMaxDelay {
		t.Fatalf("RetryMaxDelay = %s, want %s", outboxCfg.RetryMaxDelay, cfg.OutboxRetryMaxDelay)
	}
}

func TestValidateRejectsInvalidOutboxConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.OutboxRetryMaxDelay = cfg.OutboxRetryInitialDelay - time.Nanosecond
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want outbox retry delay error")
	}
}

func TestValidateRejectsUnsafeOutboxLeaseConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.OutboxPublishTimeout = 26 * time.Second
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want outbox lease safety error")
	}
}

func TestValidateRejectsEnabledOutboxWithoutPublisher(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.OutboxDispatchEnabled = true
	cfg.OutboxPublisherKind = outboxPublisherKindDisabled
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want outbox publisher kind error")
	}
}

func TestValidateAllowsExplicitLossyDiagnosticPublisher(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.OutboxDispatchEnabled = true
	cfg.OutboxPublisherKind = outboxPublisherKindDiagnosticLogLossy
	cfg.OutboxAllowLossyPublisher = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}
}

func TestValidateRejectsPostgresEventLogWithoutSource(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.OutboxEventLogSource = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want event log source error")
	}
}

func validConfig() Config {
	return Config{
		HTTPAddr:                  ":8080",
		GRPCAddr:                  ":9090",
		GRPCAuthRequired:          true,
		GRPCAuthToken:             "test-token",
		GRPCMaxInFlight:           128,
		GRPCMaxConcurrentStreams:  128,
		GRPCUnaryTimeout:          30 * time.Second,
		GRPCKeepaliveTime:         2 * time.Minute,
		GRPCKeepaliveTimeout:      20 * time.Second,
		GRPCKeepaliveMinTime:      30 * time.Second,
		GRPCMaxRecvMessageBytes:   4 * 1024 * 1024,
		GRPCMaxSendMessageBytes:   4 * 1024 * 1024,
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
		OutboxDispatchEnabled:     true,
		OutboxPublisherKind:       outboxPublisherKindPostgresEventLog,
		OutboxEventLogSource:      "access-manager",
		OutboxAllowLossyPublisher: false,
		OutboxBatchSize:           100,
		OutboxPollInterval:        time.Second,
		OutboxLockTTL:             30 * time.Second,
		OutboxPublishTimeout:      10 * time.Second,
		OutboxLeaseSafetyMargin:   5 * time.Second,
		OutboxRetryInitialDelay:   time.Second,
		OutboxRetryMaxDelay:       time.Minute,
		OutboxFailureMessageLimit: 512,
	}
}
