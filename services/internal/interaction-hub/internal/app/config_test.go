package app

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaultsWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_INTERACTION_HUB_GRPC_AUTH_REQUIRED", "false")
	t.Setenv("KODEX_INTERACTION_HUB_DATABASE_DSN", "postgres://interaction-hub")
	t.Setenv("KODEX_INTERACTION_HUB_OUTBOX_PUBLISHER_KIND", "disabled")
	t.Setenv("KODEX_INTERACTION_HUB_OUTBOX_DISPATCH_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.GRPCAddr != ":9090" {
		t.Fatalf("unexpected listen addresses: http=%q grpc=%q", cfg.HTTPAddr, cfg.GRPCAddr)
	}
	if cfg.GRPC.MaxInFlight != 128 || cfg.GRPC.UnaryTimeout != 30*time.Second {
		t.Fatalf("unexpected grpc defaults: %+v", cfg.GRPC)
	}
}

func TestConfigRequiresAuthTokenWhenAuthEnabled(t *testing.T) {
	cfg := Config{
		HTTPAddr:                ":8080",
		GRPCAddr:                ":9090",
		DatabaseDSN:             "postgres://interaction-hub",
		DatabaseMaxConns:        8,
		DatabaseMaxConnLifetime: time.Hour,
		DatabaseMaxConnIdleTime: 15 * time.Minute,
		DatabaseHealthPeriod:    30 * time.Second,
		DatabasePingTimeout:     5 * time.Second,
		DatabaseRetryAttempts:   6,
		DatabaseRetryMax:        time.Second,
		OutboxDispatchEnabled:   false,
		OutboxPublisherKind:     "disabled",
		OutboxBatchSize:         100,
		OutboxPollInterval:      time.Second,
		OutboxLockTTL:           30 * time.Second,
		OutboxPublishTimeout:    10 * time.Second,
		OutboxLeaseSafetyMargin: 5 * time.Second,
		OutboxRetryInitialDelay: time.Second,
		OutboxRetryMaxDelay:     time.Minute,
		OutboxFailureLimit:      512,
		GRPC:                    validGRPCConfig(true),
	}
	cfg.GRPC.AuthToken = ""

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN") {
		t.Fatalf("Validate() err = %v, want auth token error", err)
	}
}

func TestConfigRejectsInvalidGRPCBounds(t *testing.T) {
	cfg := Config{
		HTTPAddr:                ":8080",
		GRPCAddr:                ":9090",
		DatabaseDSN:             "postgres://interaction-hub",
		DatabaseMaxConns:        8,
		DatabaseMaxConnLifetime: time.Hour,
		DatabaseMaxConnIdleTime: 15 * time.Minute,
		DatabaseHealthPeriod:    30 * time.Second,
		DatabasePingTimeout:     5 * time.Second,
		DatabaseRetryAttempts:   6,
		DatabaseRetryMax:        time.Second,
		OutboxDispatchEnabled:   false,
		OutboxPublisherKind:     "disabled",
		OutboxBatchSize:         100,
		OutboxPollInterval:      time.Second,
		OutboxLockTTL:           30 * time.Second,
		OutboxPublishTimeout:    10 * time.Second,
		OutboxLeaseSafetyMargin: 5 * time.Second,
		OutboxRetryInitialDelay: time.Second,
		OutboxRetryMaxDelay:     time.Minute,
		OutboxFailureLimit:      512,
		GRPC:                    validGRPCConfig(false),
	}
	cfg.GRPC.MaxInFlight = 0

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "KODEX_INTERACTION_HUB_GRPC_MAX_IN_FLIGHT") {
		t.Fatalf("Validate() err = %v, want max in flight error", err)
	}
}

func TestValidateRequiresEventLogDSNForPostgresPublisher(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_DSN") {
		t.Fatalf("Validate() err = %v, want event-log dsn error", err)
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
		HTTPAddr:                 ":8080",
		GRPCAddr:                 ":9090",
		DatabaseDSN:              "postgres://interaction-hub",
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
		EventLogDatabaseDSN:      "postgres://platform-event-log",
		EventLogDatabaseMaxConns: 4,
		EventLogDatabaseMinConns: 0,
		OutboxDispatchEnabled:    true,
		OutboxPublisherKind:      "postgres-event-log",
		OutboxEventLogSource:     "interaction-hub",
		OutboxBatchSize:          100,
		OutboxPollInterval:       time.Second,
		OutboxLockTTL:            30 * time.Second,
		OutboxPublishTimeout:     10 * time.Second,
		OutboxLeaseSafetyMargin:  5 * time.Second,
		OutboxRetryInitialDelay:  time.Second,
		OutboxRetryMaxDelay:      time.Minute,
		OutboxFailureLimit:       512,
		GRPC:                     validGRPCConfig(true),
	}
}

func validGRPCConfig(authRequired bool) InteractionGRPCConfig {
	cfg := InteractionGRPCConfig{
		AuthRequired:         authRequired,
		MaxInFlight:          128,
		MaxConcurrentStreams: 128,
		UnaryTimeout:         30 * time.Second,
		KeepaliveTime:        2 * time.Minute,
		KeepaliveTimeout:     20 * time.Second,
		KeepaliveMinTime:     30 * time.Second,
		MaxRecvMessageBytes:  4 * 1024 * 1024,
		MaxSendMessageBytes:  4 * 1024 * 1024,
	}
	if authRequired {
		cfg.AuthToken = "test-token"
	}
	return cfg
}
