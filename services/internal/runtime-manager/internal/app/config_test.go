package app

import (
	"testing"
	"time"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
)

func TestLoadConfigRequiresDatabaseDSN(t *testing.T) {
	t.Setenv("KODEX_RUNTIME_MANAGER_DATABASE_DSN", "")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() err = nil, want required database DSN error")
	}
}

func TestLoadConfigAllowsMissingGRPCAuthTokenWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_RUNTIME_MANAGER_DATABASE_DSN", "postgres://postgres:5432/kodex_runtime_manager?sslmode=disable")
	t.Setenv("KODEX_RUNTIME_MANAGER_GRPC_AUTH_REQUIRED", "false")
	t.Setenv("KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN", "")
	t.Setenv("KODEX_RUNTIME_MANAGER_OUTBOX_DISPATCH_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.GRPC.AuthRequired {
		t.Fatal("GRPC.AuthRequired = true, want false")
	}
}

func TestDatabasePoolSettingsIncludesRetryConfig(t *testing.T) {
	t.Parallel()

	cfg := validConfig()

	settings := cfg.DatabasePoolSettings()
	if settings.ConnectRetryMaxAttempts != cfg.Database.Retry.MaxAttempts {
		t.Fatalf("ConnectRetryMaxAttempts = %d, want %d", settings.ConnectRetryMaxAttempts, cfg.Database.Retry.MaxAttempts)
	}
	if settings.ConnectRetryInitialDelay != cfg.Database.Retry.Initial {
		t.Fatalf("ConnectRetryInitialDelay = %s, want %s", settings.ConnectRetryInitialDelay, cfg.Database.Retry.Initial)
	}
	if settings.ConnectRetryMaxDelay != cfg.Database.Retry.Max {
		t.Fatalf("ConnectRetryMaxDelay = %s, want %s", settings.ConnectRetryMaxDelay, cfg.Database.Retry.Max)
	}
	if settings.ConnectRetryJitterRatio != cfg.Database.Retry.JitterRatio {
		t.Fatalf("ConnectRetryJitterRatio = %f, want %f", settings.ConnectRetryJitterRatio, cfg.Database.Retry.JitterRatio)
	}
}

func TestValidateRequiresEventLogDSNForPostgresPublisher(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.EventLogDatabase.DSN = ""

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want missing event-log DSN error")
	}
}

func validConfig() Config {
	return Config{
		HTTPAddr: ":8080",
		GRPCAddr: ":9090",
		GRPC: RuntimeGRPCConfig{
			AuthRequired:         true,
			AuthToken:            "test-token",
			MaxInFlight:          128,
			MaxConcurrentStreams: 128,
			UnaryTimeout:         30 * time.Second,
			KeepaliveTime:        2 * time.Minute,
			KeepaliveTimeout:     20 * time.Second,
			KeepaliveMinTime:     30 * time.Second,
			MaxRecvMessageBytes:  4 * 1024 * 1024,
			MaxSendMessageBytes:  4 * 1024 * 1024,
		},
		Database: RuntimeDatabaseConfig{
			DSN: "postgres://postgres:5432/kodex_runtime_manager?sslmode=disable",
			Pool: RuntimeDatabasePoolConfig{
				MaxConns:          8,
				MinConns:          1,
				MaxConnLifetime:   time.Hour,
				MaxConnIdleTime:   15 * time.Minute,
				HealthCheckPeriod: 30 * time.Second,
				PingTimeout:       5 * time.Second,
			},
			Retry: RuntimeDatabaseRetryConfig{
				MaxAttempts: 6,
				Initial:     500 * time.Millisecond,
				Max:         5 * time.Second,
				JitterRatio: 0.2,
			},
		},
		EventLogDatabase: RuntimeEventLogDBConfig{
			DSN:      "postgres://postgres:5432/kodex_platform_event_log?sslmode=disable",
			MaxConns: 4,
			MinConns: 0,
		},
		Outbox: RuntimeOutboxConfig{
			DispatchEnabled:     true,
			PublisherKind:       outboxlib.PublisherKindPostgresEventLog,
			EventLogSource:      "runtime-manager",
			BatchSize:           100,
			PollInterval:        time.Second,
			LockTTL:             30 * time.Second,
			PublishTimeout:      10 * time.Second,
			LeaseSafetyMargin:   5 * time.Second,
			RetryInitialDelay:   time.Second,
			RetryMaxDelay:       time.Minute,
			FailureMessageLimit: 512,
		},
	}
}
