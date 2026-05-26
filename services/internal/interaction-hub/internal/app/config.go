package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
)

// Config contains process-level interaction-hub server configuration.
type Config struct {
	DatabaseDSN              string                `env:"KODEX_INTERACTION_HUB_DATABASE_DSN,required,notEmpty"`
	DatabaseMaxConns         int32                 `env:"KODEX_INTERACTION_HUB_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns         int32                 `env:"KODEX_INTERACTION_HUB_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime  time.Duration         `env:"KODEX_INTERACTION_HUB_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime  time.Duration         `env:"KODEX_INTERACTION_HUB_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthPeriod     time.Duration         `env:"KODEX_INTERACTION_HUB_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout      time.Duration         `env:"KODEX_INTERACTION_HUB_DATABASE_PING_TIMEOUT" envDefault:"5s"`
	DatabaseRetryAttempts    int                   `env:"KODEX_INTERACTION_HUB_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS" envDefault:"6"`
	DatabaseRetryInitial     time.Duration         `env:"KODEX_INTERACTION_HUB_DATABASE_CONNECT_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	DatabaseRetryMax         time.Duration         `env:"KODEX_INTERACTION_HUB_DATABASE_CONNECT_RETRY_MAX_DELAY" envDefault:"5s"`
	DatabaseRetryJitterRatio float64               `env:"KODEX_INTERACTION_HUB_DATABASE_CONNECT_RETRY_JITTER_RATIO" envDefault:"0.2"`
	EventLogDatabaseDSN      string                `env:"KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_DSN"`
	EventLogDatabaseMaxConns int32                 `env:"KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_MAX_CONNS" envDefault:"4"`
	EventLogDatabaseMinConns int32                 `env:"KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_MIN_CONNS" envDefault:"0"`
	HTTPAddr                 string                `env:"KODEX_INTERACTION_HUB_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                 string                `env:"KODEX_INTERACTION_HUB_GRPC_ADDR" envDefault:":9090"`
	GRPC                     InteractionGRPCConfig `envPrefix:"KODEX_INTERACTION_HUB_GRPC_"`
	OutboxDispatchEnabled    bool                  `env:"KODEX_INTERACTION_HUB_OUTBOX_DISPATCH_ENABLED" envDefault:"true"`
	OutboxPublisherKind      string                `env:"KODEX_INTERACTION_HUB_OUTBOX_PUBLISHER_KIND" envDefault:"postgres-event-log"`
	OutboxEventLogSource     string                `env:"KODEX_INTERACTION_HUB_OUTBOX_EVENT_LOG_SOURCE" envDefault:"interaction-hub"`
	OutboxAllowLossy         bool                  `env:"KODEX_INTERACTION_HUB_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER" envDefault:"false"`
	OutboxBatchSize          int                   `env:"KODEX_INTERACTION_HUB_OUTBOX_BATCH_SIZE" envDefault:"100"`
	OutboxPollInterval       time.Duration         `env:"KODEX_INTERACTION_HUB_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	OutboxLockTTL            time.Duration         `env:"KODEX_INTERACTION_HUB_OUTBOX_LOCK_TTL" envDefault:"30s"`
	OutboxPublishTimeout     time.Duration         `env:"KODEX_INTERACTION_HUB_OUTBOX_PUBLISH_TIMEOUT" envDefault:"10s"`
	OutboxLeaseSafetyMargin  time.Duration         `env:"KODEX_INTERACTION_HUB_OUTBOX_LEASE_SAFETY_MARGIN" envDefault:"5s"`
	OutboxRetryInitialDelay  time.Duration         `env:"KODEX_INTERACTION_HUB_OUTBOX_RETRY_INITIAL_DELAY" envDefault:"1s"`
	OutboxRetryMaxDelay      time.Duration         `env:"KODEX_INTERACTION_HUB_OUTBOX_RETRY_MAX_DELAY" envDefault:"1m"`
	OutboxFailureLimit       int                   `env:"KODEX_INTERACTION_HUB_OUTBOX_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
}

// InteractionGRPCConfig contains gRPC boundary limits.
type InteractionGRPCConfig struct {
	AuthRequired         bool          `env:"AUTH_REQUIRED" envDefault:"true"`
	AuthToken            string        `env:"AUTH_TOKEN"`
	MaxInFlight          int           `env:"MAX_IN_FLIGHT" envDefault:"128"`
	MaxConcurrentStreams uint32        `env:"MAX_CONCURRENT_STREAMS" envDefault:"128"`
	UnaryTimeout         time.Duration `env:"UNARY_TIMEOUT" envDefault:"30s"`
	KeepaliveTime        time.Duration `env:"KEEPALIVE_TIME" envDefault:"2m"`
	KeepaliveTimeout     time.Duration `env:"KEEPALIVE_TIMEOUT" envDefault:"20s"`
	KeepaliveMinTime     time.Duration `env:"KEEPALIVE_MIN_TIME" envDefault:"30s"`
	PermitWithoutStream  bool          `env:"PERMIT_WITHOUT_STREAM" envDefault:"false"`
	MaxRecvMessageBytes  int           `env:"MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	MaxSendMessageBytes  int           `env:"MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load interaction-hub config: %w", err)
	}
	return cfg, nil
}

// Validate checks process settings before runtime construction.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_INTERACTION_HUB_HTTP_ADDR is required")
	}
	if strings.TrimSpace(cfg.GRPCAddr) == "" {
		return fmt.Errorf("KODEX_INTERACTION_HUB_GRPC_ADDR is required")
	}
	if cfg.GRPC.AuthRequired && strings.TrimSpace(cfg.GRPC.AuthToken) == "" {
		return fmt.Errorf("KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if err := cfg.validateGRPC(); err != nil {
		return err
	}
	if err := cfg.validateDatabase(); err != nil {
		return err
	}
	if err := cfg.validateOutbox(); err != nil {
		return err
	}
	return cfg.GRPCServerConfig().Validate()
}

func (cfg Config) validateDatabase() error {
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_INTERACTION_HUB_DATABASE_MAX_CONNS", valid: cfg.DatabaseMaxConns > 0},
		{name: "KODEX_INTERACTION_HUB_DATABASE_MIN_CONNS", valid: cfg.DatabaseMinConns >= 0},
		{name: "KODEX_INTERACTION_HUB_DATABASE_MAX_CONN_LIFETIME", valid: cfg.DatabaseMaxConnLifetime > 0},
		{name: "KODEX_INTERACTION_HUB_DATABASE_MAX_CONN_IDLE_TIME", valid: cfg.DatabaseMaxConnIdleTime > 0},
		{name: "KODEX_INTERACTION_HUB_DATABASE_HEALTH_CHECK_PERIOD", valid: cfg.DatabaseHealthPeriod > 0},
		{name: "KODEX_INTERACTION_HUB_DATABASE_PING_TIMEOUT", valid: cfg.DatabasePingTimeout > 0},
		{name: "KODEX_INTERACTION_HUB_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS", valid: cfg.DatabaseRetryAttempts > 0},
		{name: "KODEX_INTERACTION_HUB_DATABASE_CONNECT_RETRY_INITIAL_DELAY", valid: cfg.DatabaseRetryInitial >= 0},
		{name: "KODEX_INTERACTION_HUB_DATABASE_CONNECT_RETRY_MAX_DELAY", valid: cfg.DatabaseRetryMax >= cfg.DatabaseRetryInitial},
		{name: "KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_MAX_CONNS", valid: cfg.EventLogDatabaseMaxConns >= 0},
		{name: "KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_MIN_CONNS", valid: cfg.EventLogDatabaseMinConns >= 0},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return fmt.Errorf("KODEX_INTERACTION_HUB_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.DatabaseRetryJitterRatio < 0 || cfg.DatabaseRetryJitterRatio > 1 {
		return fmt.Errorf("KODEX_INTERACTION_HUB_DATABASE_CONNECT_RETRY_JITTER_RATIO must be between 0 and 1")
	}
	if cfg.EventLogDatabaseMaxConns > 0 && cfg.EventLogDatabaseMinConns > cfg.EventLogDatabaseMaxConns {
		return fmt.Errorf("KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	return nil
}

func (cfg Config) validateOutbox() error {
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_INTERACTION_HUB_OUTBOX_BATCH_SIZE", valid: cfg.OutboxBatchSize > 0},
		{name: "KODEX_INTERACTION_HUB_OUTBOX_POLL_INTERVAL", valid: cfg.OutboxPollInterval > 0},
		{name: "KODEX_INTERACTION_HUB_OUTBOX_LOCK_TTL", valid: cfg.OutboxLockTTL > 0},
		{name: "KODEX_INTERACTION_HUB_OUTBOX_PUBLISH_TIMEOUT", valid: cfg.OutboxPublishTimeout > 0},
		{name: "KODEX_INTERACTION_HUB_OUTBOX_LEASE_SAFETY_MARGIN", valid: cfg.OutboxLeaseSafetyMargin >= 0},
		{name: "KODEX_INTERACTION_HUB_OUTBOX_RETRY_INITIAL_DELAY", valid: cfg.OutboxRetryInitialDelay > 0},
		{name: "KODEX_INTERACTION_HUB_OUTBOX_RETRY_MAX_DELAY", valid: cfg.OutboxRetryMaxDelay >= cfg.OutboxRetryInitialDelay},
		{name: "KODEX_INTERACTION_HUB_OUTBOX_FAILURE_MESSAGE_LIMIT", valid: cfg.OutboxFailureLimit > 0},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	switch strings.TrimSpace(cfg.OutboxPublisherKind) {
	case outboxlib.PublisherKindDisabled, outboxlib.PublisherKindDiagnosticLogLossy, outboxlib.PublisherKindPostgresEventLog:
	default:
		return fmt.Errorf("KODEX_INTERACTION_HUB_OUTBOX_PUBLISHER_KIND must be disabled, diagnostic-log-lossy or postgres-event-log")
	}
	if cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDisabled {
		return fmt.Errorf("KODEX_INTERACTION_HUB_OUTBOX_PUBLISHER_KIND must be configured when outbox dispatch is enabled")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDiagnosticLogLossy && !cfg.OutboxAllowLossy {
		return fmt.Errorf("KODEX_INTERACTION_HUB_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER must be true for diagnostic-log-lossy publisher")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog && strings.TrimSpace(cfg.OutboxEventLogSource) == "" {
		return fmt.Errorf("KODEX_INTERACTION_HUB_OUTBOX_EVENT_LOG_SOURCE must be configured for postgres-event-log publisher")
	}
	if cfg.needsEventLogDatabase() && strings.TrimSpace(cfg.EventLogDatabaseDSN) == "" {
		return fmt.Errorf("KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_DSN is required for postgres-event-log publisher")
	}
	if cfg.needsEventLogDatabase() && cfg.EventLogDatabaseMaxConns < 1 {
		return fmt.Errorf("KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_MAX_CONNS must be greater than zero for postgres-event-log publisher")
	}
	if cfg.OutboxPublishTimeout+cfg.OutboxLeaseSafetyMargin >= cfg.OutboxLockTTL {
		return fmt.Errorf("KODEX_INTERACTION_HUB_OUTBOX_PUBLISH_TIMEOUT plus safety margin must be less than KODEX_INTERACTION_HUB_OUTBOX_LOCK_TTL")
	}
	return nil
}

func (cfg Config) needsEventLogDatabase() bool {
	return cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog
}

// DatabasePoolSettings converts service env config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.databaseRuntimeSettings(cfg.DatabaseDSN, cfg.DatabaseMaxConns, cfg.DatabaseMinConns))
}

// EventLogDatabasePoolSettings converts event-log env config to a separate pgxpool contract.
func (cfg Config) EventLogDatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.databaseRuntimeSettings(cfg.EventLogDatabaseDSN, cfg.EventLogDatabaseMaxConns, cfg.EventLogDatabaseMinConns))
}

func (cfg Config) databaseRuntimeSettings(dsn string, maxConns int32, minConns int32) postgreslib.PoolRuntimeSettings {
	settings := postgreslib.PoolRuntimeSettings{DSN: dsn}
	settings.MaxConns = maxConns
	settings.MinConns = minConns
	settings.MaxConnLifetime = cfg.DatabaseMaxConnLifetime
	settings.MaxConnIdleTime = cfg.DatabaseMaxConnIdleTime
	settings.HealthCheckPeriod = cfg.DatabaseHealthPeriod
	settings.PingTimeout = cfg.DatabasePingTimeout
	settings.ConnectRetryMaxAttempts = cfg.DatabaseRetryAttempts
	settings.ConnectRetryInitialDelay = cfg.DatabaseRetryInitial
	settings.ConnectRetryMaxDelay = cfg.DatabaseRetryMax
	settings.ConnectRetryJitterRatio = cfg.DatabaseRetryJitterRatio
	return settings
}

func (cfg Config) validateGRPC() error {
	if err := validatePositiveInt("KODEX_INTERACTION_HUB_GRPC_MAX_IN_FLIGHT", cfg.GRPC.MaxInFlight); err != nil {
		return err
	}
	if cfg.GRPC.MaxConcurrentStreams == 0 {
		return fmt.Errorf("KODEX_INTERACTION_HUB_GRPC_MAX_CONCURRENT_STREAMS is invalid")
	}
	if err := validatePositiveInt("KODEX_INTERACTION_HUB_GRPC_MAX_RECV_MESSAGE_BYTES", cfg.GRPC.MaxRecvMessageBytes); err != nil {
		return err
	}
	if err := validatePositiveInt("KODEX_INTERACTION_HUB_GRPC_MAX_SEND_MESSAGE_BYTES", cfg.GRPC.MaxSendMessageBytes); err != nil {
		return err
	}
	return validateDurationChecks([]durationCheck{
		{name: "KODEX_INTERACTION_HUB_GRPC_UNARY_TIMEOUT", value: cfg.GRPC.UnaryTimeout},
		{name: "KODEX_INTERACTION_HUB_GRPC_KEEPALIVE_TIME", value: cfg.GRPC.KeepaliveTime},
		{name: "KODEX_INTERACTION_HUB_GRPC_KEEPALIVE_TIMEOUT", value: cfg.GRPC.KeepaliveTimeout},
		{name: "KODEX_INTERACTION_HUB_GRPC_KEEPALIVE_MIN_TIME", value: cfg.GRPC.KeepaliveMinTime},
	})
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	grpcCfg := cfg.GRPC
	return grpcserver.ConfigFromRuntimeValues(
		grpcCfg.MaxInFlight,
		grpcCfg.MaxConcurrentStreams,
		grpcCfg.UnaryTimeout,
		grpcCfg.KeepaliveTime,
		grpcCfg.KeepaliveTimeout,
		grpcCfg.KeepaliveMinTime,
		grpcCfg.PermitWithoutStream,
		grpcCfg.MaxRecvMessageBytes,
		grpcCfg.MaxSendMessageBytes,
		grpcCfg.AuthRequired,
	)
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxlib.Config {
	return outboxlib.ConfigFromRuntimeValues(cfg.OutboxBatchSize, cfg.OutboxPollInterval, cfg.OutboxLockTTL, cfg.OutboxPublishTimeout, cfg.OutboxRetryInitialDelay, cfg.OutboxRetryMaxDelay, cfg.OutboxFailureLimit)
}

type durationCheck struct {
	name  string
	value time.Duration
}

func validatePositiveInt(envName string, value int) error {
	if value <= 0 {
		return fmt.Errorf("%s is invalid", envName)
	}
	return nil
}

func validateDurationChecks(checks []durationCheck) error {
	for _, check := range checks {
		if check.value <= 0 {
			return fmt.Errorf("%s is invalid", check.name)
		}
	}
	return nil
}
