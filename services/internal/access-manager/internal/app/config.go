package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
)

// Config contains process-level access-manager server configuration.
type Config struct {
	HTTPAddr                  string        `env:"KODEX_ACCESS_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                  string        `env:"KODEX_ACCESS_MANAGER_GRPC_ADDR" envDefault:":9090"`
	GRPCAuthRequired          bool          `env:"KODEX_ACCESS_MANAGER_GRPC_AUTH_REQUIRED" envDefault:"true"`
	GRPCAuthToken             string        `env:"KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN"`
	GRPCMaxInFlight           int           `env:"KODEX_ACCESS_MANAGER_GRPC_MAX_IN_FLIGHT" envDefault:"128"`
	GRPCMaxConcurrentStreams  uint32        `env:"KODEX_ACCESS_MANAGER_GRPC_MAX_CONCURRENT_STREAMS" envDefault:"128"`
	GRPCUnaryTimeout          time.Duration `env:"KODEX_ACCESS_MANAGER_GRPC_UNARY_TIMEOUT" envDefault:"30s"`
	GRPCKeepaliveTime         time.Duration `env:"KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_TIME" envDefault:"2m"`
	GRPCKeepaliveTimeout      time.Duration `env:"KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_TIMEOUT" envDefault:"20s"`
	GRPCKeepaliveMinTime      time.Duration `env:"KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_MIN_TIME" envDefault:"30s"`
	GRPCPermitWithoutStream   bool          `env:"KODEX_ACCESS_MANAGER_GRPC_PERMIT_WITHOUT_STREAM" envDefault:"false"`
	GRPCMaxRecvMessageBytes   int           `env:"KODEX_ACCESS_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCMaxSendMessageBytes   int           `env:"KODEX_ACCESS_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
	DatabaseDSN               string        `env:"KODEX_ACCESS_MANAGER_DATABASE_DSN,required,notEmpty"`
	DatabaseMaxConns          int32         `env:"KODEX_ACCESS_MANAGER_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns          int32         `env:"KODEX_ACCESS_MANAGER_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime   time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime   time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthCheckPeriod time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout       time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_PING_TIMEOUT" envDefault:"5s"`
	DatabaseRetryMaxAttempts  int           `env:"KODEX_ACCESS_MANAGER_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS" envDefault:"6"`
	DatabaseRetryInitialDelay time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_CONNECT_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	DatabaseRetryMaxDelay     time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_CONNECT_RETRY_MAX_DELAY" envDefault:"5s"`
	DatabaseRetryJitterRatio  float64       `env:"KODEX_ACCESS_MANAGER_DATABASE_CONNECT_RETRY_JITTER_RATIO" envDefault:"0.2"`
	EventLogDatabaseDSN       string        `env:"KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN"`
	EventLogDatabaseMaxConns  int32         `env:"KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS" envDefault:"4"`
	EventLogDatabaseMinConns  int32         `env:"KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS" envDefault:"0"`
	OutboxDispatchEnabled     bool          `env:"KODEX_ACCESS_MANAGER_OUTBOX_DISPATCH_ENABLED" envDefault:"true"`
	OutboxPublisherKind       string        `env:"KODEX_ACCESS_MANAGER_OUTBOX_PUBLISHER_KIND" envDefault:"postgres-event-log"`
	OutboxEventLogSource      string        `env:"KODEX_ACCESS_MANAGER_OUTBOX_EVENT_LOG_SOURCE" envDefault:"access-manager"`
	OutboxAllowLossyPublisher bool          `env:"KODEX_ACCESS_MANAGER_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER" envDefault:"false"`
	OutboxBatchSize           int           `env:"KODEX_ACCESS_MANAGER_OUTBOX_BATCH_SIZE" envDefault:"100"`
	OutboxPollInterval        time.Duration `env:"KODEX_ACCESS_MANAGER_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	OutboxLockTTL             time.Duration `env:"KODEX_ACCESS_MANAGER_OUTBOX_LOCK_TTL" envDefault:"30s"`
	OutboxPublishTimeout      time.Duration `env:"KODEX_ACCESS_MANAGER_OUTBOX_PUBLISH_TIMEOUT" envDefault:"10s"`
	OutboxLeaseSafetyMargin   time.Duration `env:"KODEX_ACCESS_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN" envDefault:"5s"`
	OutboxRetryInitialDelay   time.Duration `env:"KODEX_ACCESS_MANAGER_OUTBOX_RETRY_INITIAL_DELAY" envDefault:"1s"`
	OutboxRetryMaxDelay       time.Duration `env:"KODEX_ACCESS_MANAGER_OUTBOX_RETRY_MAX_DELAY" envDefault:"1m"`
	OutboxFailureMessageLimit int           `env:"KODEX_ACCESS_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse access-manager config from environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks configuration invariants that protect runtime boundaries.
func (cfg Config) Validate() error {
	if cfg.GRPCAuthRequired && strings.TrimSpace(cfg.GRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if cfg.GRPCMaxInFlight < 1 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_MAX_IN_FLIGHT must be greater than zero")
	}
	if cfg.GRPCMaxConcurrentStreams < 1 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_MAX_CONCURRENT_STREAMS must be greater than zero")
	}
	if cfg.GRPCUnaryTimeout <= 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_UNARY_TIMEOUT must be positive")
	}
	if cfg.GRPCKeepaliveTime <= 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_TIME must be positive")
	}
	if cfg.GRPCKeepaliveTimeout <= 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_TIMEOUT must be positive")
	}
	if cfg.GRPCKeepaliveMinTime <= 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_KEEPALIVE_MIN_TIME must be positive")
	}
	if cfg.GRPCMaxRecvMessageBytes < 1 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES must be greater than zero")
	}
	if cfg.GRPCMaxSendMessageBytes < 1 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES must be greater than zero")
	}
	switch strings.TrimSpace(cfg.OutboxPublisherKind) {
	case outboxPublisherKindDisabled, outboxPublisherKindDiagnosticLogLossy, outboxPublisherKindPostgresEventLog:
	default:
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_PUBLISHER_KIND must be disabled, diagnostic-log-lossy or postgres-event-log")
	}
	if cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxPublisherKindDisabled {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_PUBLISHER_KIND must be configured when outbox dispatch is enabled")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxPublisherKindDiagnosticLogLossy && !cfg.OutboxAllowLossyPublisher {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER must be true for diagnostic-log-lossy publisher")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxPublisherKindPostgresEventLog && strings.TrimSpace(cfg.OutboxEventLogSource) == "" {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_EVENT_LOG_SOURCE must be configured for postgres-event-log publisher")
	}
	if cfg.needsEventLogDatabase() && strings.TrimSpace(cfg.EventLogDatabaseDSN) == "" {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN is required for postgres-event-log publisher")
	}
	if cfg.EventLogDatabaseMaxConns < 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS must not be negative")
	}
	if cfg.EventLogDatabaseMinConns < 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS must not be negative")
	}
	if cfg.needsEventLogDatabase() && cfg.EventLogDatabaseMaxConns < 1 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS must be greater than zero for postgres-event-log publisher")
	}
	if cfg.EventLogDatabaseMaxConns > 0 && cfg.EventLogDatabaseMinConns > cfg.EventLogDatabaseMaxConns {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.OutboxBatchSize < 1 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_BATCH_SIZE must be greater than zero")
	}
	if cfg.OutboxPollInterval <= 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_POLL_INTERVAL must be positive")
	}
	if cfg.OutboxLockTTL <= 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_LOCK_TTL must be positive")
	}
	if cfg.OutboxPublishTimeout <= 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_PUBLISH_TIMEOUT must be positive")
	}
	if cfg.OutboxLeaseSafetyMargin < 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN must not be negative")
	}
	if cfg.OutboxPublishTimeout+cfg.OutboxLeaseSafetyMargin >= cfg.OutboxLockTTL {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_PUBLISH_TIMEOUT plus safety margin must be less than KODEX_ACCESS_MANAGER_OUTBOX_LOCK_TTL")
	}
	if cfg.OutboxRetryInitialDelay <= 0 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_RETRY_INITIAL_DELAY must be positive")
	}
	if cfg.OutboxRetryMaxDelay < cfg.OutboxRetryInitialDelay {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_RETRY_MAX_DELAY must be greater than or equal to initial delay")
	}
	if cfg.OutboxFailureMessageLimit < 1 {
		return fmt.Errorf("KODEX_ACCESS_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT must be greater than zero")
	}
	return nil
}

func (cfg Config) needsEventLogDatabase() bool {
	return cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxPublisherKindPostgresEventLog
}

// DatabasePoolSettings converts service config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettings{
		DSN:                      cfg.DatabaseDSN,
		MaxConns:                 cfg.DatabaseMaxConns,
		MinConns:                 cfg.DatabaseMinConns,
		MaxConnLifetime:          cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime:          cfg.DatabaseMaxConnIdleTime,
		HealthCheckPeriod:        cfg.DatabaseHealthCheckPeriod,
		PingTimeout:              cfg.DatabasePingTimeout,
		ConnectRetryMaxAttempts:  cfg.DatabaseRetryMaxAttempts,
		ConnectRetryInitialDelay: cfg.DatabaseRetryInitialDelay,
		ConnectRetryMaxDelay:     cfg.DatabaseRetryMaxDelay,
		ConnectRetryJitterRatio:  cfg.DatabaseRetryJitterRatio,
	}
}

// EventLogDatabasePoolSettings converts event-log env config to a separate pgxpool contract.
func (cfg Config) EventLogDatabasePoolSettings() postgreslib.PoolSettings {
	settings := cfg.DatabasePoolSettings()
	settings.DSN = cfg.EventLogDatabaseDSN
	settings.MaxConns = cfg.EventLogDatabaseMaxConns
	settings.MinConns = cfg.EventLogDatabaseMinConns
	return settings
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.Config{
		MaxInFlight:          cfg.GRPCMaxInFlight,
		MaxConcurrentStreams: cfg.GRPCMaxConcurrentStreams,
		UnaryTimeout:         cfg.GRPCUnaryTimeout,
		KeepaliveTime:        cfg.GRPCKeepaliveTime,
		KeepaliveTimeout:     cfg.GRPCKeepaliveTimeout,
		KeepaliveMinTime:     cfg.GRPCKeepaliveMinTime,
		PermitWithoutStream:  cfg.GRPCPermitWithoutStream,
		MaxRecvMessageBytes:  cfg.GRPCMaxRecvMessageBytes,
		MaxSendMessageBytes:  cfg.GRPCMaxSendMessageBytes,
		AuthRequired:         cfg.GRPCAuthRequired,
	}
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxDispatcherConfig {
	return outboxDispatcherConfig{
		BatchSize:           cfg.OutboxBatchSize,
		PollInterval:        cfg.OutboxPollInterval,
		LockTTL:             cfg.OutboxLockTTL,
		PublishTimeout:      cfg.OutboxPublishTimeout,
		RetryInitialDelay:   cfg.OutboxRetryInitialDelay,
		RetryMaxDelay:       cfg.OutboxRetryMaxDelay,
		FailureMessageLimit: cfg.OutboxFailureMessageLimit,
	}
}
