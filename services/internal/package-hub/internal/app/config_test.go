package app

import (
	"testing"
	"time"
)

func TestLoadConfigAllowsMissingConditionalEnvWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_PACKAGE_HUB_GRPC_AUTH_REQUIRED", "false")
	t.Setenv("KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN", "")
	t.Setenv("KODEX_PACKAGE_HUB_DATABASE_DSN", "postgres://package-hub")
	t.Setenv("KODEX_PACKAGE_HUB_OUTBOX_PUBLISHER_KIND", "disabled")
	t.Setenv("KODEX_PACKAGE_HUB_OUTBOX_DISPATCH_ENABLED", "false")
	t.Setenv("KODEX_PACKAGE_HUB_ACCESS_CHECK_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.GRPCAuthRequired {
		t.Fatal("GRPCAuthRequired = true, want false")
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

func TestValidateRejectsInvalidGRPCRuntimeLimits(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCMaxInFlight = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want max in-flight error")
	}
}

func TestValidateRequiresEventLogDSNForPostgresPublisher(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want event-log database dsn error")
	}
}

func TestValidateRequiresAccessTokenWhenAccessChecksEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.AccessManagerGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want access-manager auth token error")
	}
}

func TestGRPCServerConfigMapsRuntimeLimits(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	runtime := cfg.GRPCServerConfig()
	if runtime.MaxInFlight != cfg.GRPCMaxInFlight {
		t.Fatalf("MaxInFlight = %d, want %d", runtime.MaxInFlight, cfg.GRPCMaxInFlight)
	}
	if runtime.MaxConcurrentStreams != cfg.GRPCMaxConcurrentStreams {
		t.Fatalf("MaxConcurrentStreams = %d, want %d", runtime.MaxConcurrentStreams, cfg.GRPCMaxConcurrentStreams)
	}
	if runtime.AuthRequired != cfg.GRPCAuthRequired {
		t.Fatalf("AuthRequired = %v, want %v", runtime.AuthRequired, cfg.GRPCAuthRequired)
	}
}

func TestOutboxDispatcherConfigMapsRuntimeLimits(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	runtime := cfg.OutboxDispatcherConfig()
	if runtime.BatchSize != cfg.OutboxBatchSize {
		t.Fatalf("BatchSize = %d, want %d", runtime.BatchSize, cfg.OutboxBatchSize)
	}
}

func validConfig() Config {
	return Config{
		HTTPAddr:                   ":8080",
		GRPCAddr:                   ":9090",
		GRPCAuthRequired:           true,
		GRPCAuthToken:              "test-token",
		GRPCMaxInFlight:            128,
		GRPCMaxConcurrentStreams:   128,
		GRPCUnaryTimeout:           30 * time.Second,
		GRPCKeepaliveTime:          2 * time.Minute,
		GRPCKeepaliveTimeout:       20 * time.Second,
		GRPCKeepaliveMinTime:       30 * time.Second,
		GRPCMaxRecvMessageBytes:    4 * 1024 * 1024,
		GRPCMaxSendMessageBytes:    4 * 1024 * 1024,
		AccessCheckEnabled:         true,
		AccessManagerGRPCAddr:      "access-manager:9090",
		AccessManagerGRPCAuthToken: "access-token",
		AccessManagerCheckTimeout:  3 * time.Second,
		DatabaseDSN:                "postgres://package-hub",
		DatabaseMaxConns:           8,
		DatabaseMinConns:           1,
		DatabaseMaxConnLifetime:    time.Hour,
		DatabaseMaxConnIdleTime:    15 * time.Minute,
		DatabaseHealthPeriod:       30 * time.Second,
		DatabasePingTimeout:        5 * time.Second,
		DatabaseRetryAttempts:      6,
		DatabaseRetryInitial:       500 * time.Millisecond,
		DatabaseRetryMax:           5 * time.Second,
		DatabaseRetryJitterRatio:   0.2,
		EventLogDatabaseDSN:        "postgres://platform-event-log",
		EventLogDatabaseMaxConns:   4,
		EventLogDatabaseMinConns:   0,
		OutboxDispatchEnabled:      true,
		OutboxPublisherKind:        "postgres-event-log",
		OutboxEventLogSource:       "package-hub",
		OutboxAllowLossy:           false,
		OutboxBatchSize:            100,
		OutboxPollInterval:         time.Second,
		OutboxLockTTL:              30 * time.Second,
		OutboxPublishTimeout:       10 * time.Second,
		OutboxLeaseSafetyMargin:    5 * time.Second,
		OutboxRetryInitialDelay:    time.Second,
		OutboxRetryMaxDelay:        time.Minute,
		OutboxFailureLimit:         512,
	}
}
