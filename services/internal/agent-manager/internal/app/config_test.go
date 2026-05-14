package app

import (
	"testing"
	"time"
)

func TestLoadConfigAllowsMissingConditionalEnvWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_AGENT_MANAGER_DATABASE_DSN", "postgres://agent-manager")
	t.Setenv("KODEX_AGENT_MANAGER_OUTBOX_DISPATCH_ENABLED", "false")
	t.Setenv("KODEX_AGENT_MANAGER_OUTBOX_PUBLISHER_KIND", "disabled")
	t.Setenv("KODEX_AGENT_MANAGER_GRPC_AUTH_REQUIRED", "false")
	t.Setenv("KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN", "")

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

func TestValidateRequiresEventLogDSNWhenPostgresPublisherEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.OutboxDispatchEnabled = true
	cfg.OutboxPublisherKind = "postgres-event-log"
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want event-log database dsn error")
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

func validConfig() Config {
	return Config{
		HTTPAddr:                 ":8080",
		DatabaseDSN:              "postgres://agent-manager",
		DatabaseMaxConns:         8,
		DatabaseMinConns:         1,
		DatabaseMaxConnLifetime:  time.Hour,
		DatabaseMaxConnIdleTime:  15 * time.Minute,
		DatabaseHealthPeriod:     30 * time.Second,
		DatabasePingTimeout:      5 * time.Second,
		DatabaseRetryAttempts:    6,
		DatabaseRetryInitial:     500 * time.Millisecond,
		DatabaseRetryMax:         5 * time.Second,
		DatabaseRetryJitterRatio: 0.2,
		EventLogDatabaseMaxConns: 4,
		GRPCAddr:                 ":9090",
		GRPCAuthRequired:         true,
		GRPCAuthToken:            "test-token",
		GRPCMaxInFlight:          128,
		GRPCMaxConcurrentStreams: 128,
		GRPCUnaryTimeout:         30 * time.Second,
		GRPCKeepaliveTime:        2 * time.Minute,
		GRPCKeepaliveTimeout:     20 * time.Second,
		GRPCKeepaliveMinTime:     30 * time.Second,
		GRPCMaxRecvMessageBytes:  4 * 1024 * 1024,
		GRPCMaxSendMessageBytes:  4 * 1024 * 1024,
		OutboxDispatchEnabled:    false,
		OutboxPublisherKind:      "disabled",
		OutboxBatchSize:          100,
		OutboxPollInterval:       time.Second,
		OutboxLockTTL:            30 * time.Second,
		OutboxPublishTimeout:     10 * time.Second,
		OutboxLeaseSafetyMargin:  5 * time.Second,
		OutboxRetryInitialDelay:  time.Second,
		OutboxRetryMaxDelay:      time.Minute,
		OutboxFailureLimit:       512,
	}
}
