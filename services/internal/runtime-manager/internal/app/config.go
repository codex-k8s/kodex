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

// Config contains process-level runtime-manager server configuration.
type Config struct {
	HTTPAddr         string                  `env:"KODEX_RUNTIME_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr         string                  `env:"KODEX_RUNTIME_MANAGER_GRPC_ADDR" envDefault:":9090"`
	GRPC             RuntimeGRPCConfig       `envPrefix:"KODEX_RUNTIME_MANAGER_GRPC_"`
	Database         RuntimeDatabaseConfig   `envPrefix:"KODEX_RUNTIME_MANAGER_DATABASE_"`
	EventLogDatabase RuntimeEventLogDBConfig `envPrefix:"KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_"`
	Outbox           RuntimeOutboxConfig     `envPrefix:"KODEX_RUNTIME_MANAGER_OUTBOX_"`
}

// RuntimeGRPCConfig contains gRPC boundary limits.
type RuntimeGRPCConfig struct {
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

// RuntimeDatabaseConfig contains owned runtime-manager database settings.
type RuntimeDatabaseConfig struct {
	DSN   string                     `env:"DSN,required,notEmpty"`
	Pool  RuntimeDatabasePoolConfig  `envPrefix:""`
	Retry RuntimeDatabaseRetryConfig `envPrefix:"CONNECT_RETRY_"`
}

// RuntimeDatabasePoolConfig contains bounded connection pool settings.
type RuntimeDatabasePoolConfig struct {
	MaxConns          int32         `env:"MAX_CONNS" envDefault:"8"`
	MinConns          int32         `env:"MIN_CONNS" envDefault:"1"`
	MaxConnLifetime   time.Duration `env:"MAX_CONN_LIFETIME" envDefault:"1h"`
	MaxConnIdleTime   time.Duration `env:"MAX_CONN_IDLE_TIME" envDefault:"15m"`
	HealthCheckPeriod time.Duration `env:"HEALTH_CHECK_PERIOD" envDefault:"30s"`
	PingTimeout       time.Duration `env:"PING_TIMEOUT" envDefault:"5s"`
}

// RuntimeDatabaseRetryConfig contains startup database connection retry settings.
type RuntimeDatabaseRetryConfig struct {
	MaxAttempts int           `env:"MAX_ATTEMPTS" envDefault:"6"`
	Initial     time.Duration `env:"INITIAL_DELAY" envDefault:"500ms"`
	Max         time.Duration `env:"MAX_DELAY" envDefault:"5s"`
	JitterRatio float64       `env:"JITTER_RATIO" envDefault:"0.2"`
}

// RuntimeEventLogDBConfig contains shared event-log database settings.
type RuntimeEventLogDBConfig struct {
	DSN      string `env:"DSN"`
	MaxConns int32  `env:"MAX_CONNS" envDefault:"4"`
	MinConns int32  `env:"MIN_CONNS" envDefault:"0"`
}

// RuntimeOutboxConfig contains local outbox dispatcher settings.
type RuntimeOutboxConfig struct {
	DispatchEnabled     bool          `env:"DISPATCH_ENABLED" envDefault:"true"`
	PublisherKind       string        `env:"PUBLISHER_KIND" envDefault:"postgres-event-log"`
	EventLogSource      string        `env:"EVENT_LOG_SOURCE" envDefault:"runtime-manager"`
	AllowLossyPublisher bool          `env:"ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER" envDefault:"false"`
	BatchSize           int           `env:"BATCH_SIZE" envDefault:"100"`
	PollInterval        time.Duration `env:"POLL_INTERVAL" envDefault:"1s"`
	LockTTL             time.Duration `env:"LOCK_TTL" envDefault:"30s"`
	PublishTimeout      time.Duration `env:"PUBLISH_TIMEOUT" envDefault:"10s"`
	LeaseSafetyMargin   time.Duration `env:"LEASE_SAFETY_MARGIN" envDefault:"5s"`
	RetryInitialDelay   time.Duration `env:"RETRY_INITIAL_DELAY" envDefault:"1s"`
	RetryMaxDelay       time.Duration `env:"RETRY_MAX_DELAY" envDefault:"1m"`
	FailureMessageLimit int           `env:"FAILURE_MESSAGE_LIMIT" envDefault:"512"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load runtime-manager config: %w", err)
	}
	return cfg, nil
}

// Validate checks configuration invariants that protect runtime boundaries.
func (cfg Config) Validate() error {
	if cfg.GRPC.AuthRequired && strings.TrimSpace(cfg.GRPC.AuthToken) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if err := cfg.validateGRPCSettings(); err != nil {
		return err
	}
	if err := cfg.validateDatabaseSettings(); err != nil {
		return err
	}
	return cfg.validateOutboxSettings()
}

func (cfg Config) validateGRPCSettings() error {
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_RUNTIME_MANAGER_GRPC_MAX_IN_FLIGHT", valid: cfg.GRPC.MaxInFlight > 0},
		{name: "KODEX_RUNTIME_MANAGER_GRPC_MAX_CONCURRENT_STREAMS", valid: cfg.GRPC.MaxConcurrentStreams > 0},
		{name: "KODEX_RUNTIME_MANAGER_GRPC_UNARY_TIMEOUT", valid: cfg.GRPC.UnaryTimeout > 0},
		{name: "KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_TIME", valid: cfg.GRPC.KeepaliveTime > 0},
		{name: "KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_TIMEOUT", valid: cfg.GRPC.KeepaliveTimeout > 0},
		{name: "KODEX_RUNTIME_MANAGER_GRPC_KEEPALIVE_MIN_TIME", valid: cfg.GRPC.KeepaliveMinTime > 0},
		{name: "KODEX_RUNTIME_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES", valid: cfg.GRPC.MaxRecvMessageBytes > 0},
		{name: "KODEX_RUNTIME_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES", valid: cfg.GRPC.MaxSendMessageBytes > 0},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	return nil
}

func (cfg Config) validateDatabaseSettings() error {
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_MAX_CONNS", valid: cfg.Database.Pool.MaxConns > 0},
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_MIN_CONNS", valid: cfg.Database.Pool.MinConns >= 0},
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_MAX_CONN_LIFETIME", valid: cfg.Database.Pool.MaxConnLifetime > 0},
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_MAX_CONN_IDLE_TIME", valid: cfg.Database.Pool.MaxConnIdleTime > 0},
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_HEALTH_CHECK_PERIOD", valid: cfg.Database.Pool.HealthCheckPeriod > 0},
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_PING_TIMEOUT", valid: cfg.Database.Pool.PingTimeout > 0},
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS", valid: cfg.Database.Retry.MaxAttempts > 0},
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_CONNECT_RETRY_INITIAL_DELAY", valid: cfg.Database.Retry.Initial >= 0},
		{name: "KODEX_RUNTIME_MANAGER_DATABASE_CONNECT_RETRY_MAX_DELAY", valid: cfg.Database.Retry.Max >= cfg.Database.Retry.Initial},
		{name: "KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS", valid: cfg.EventLogDatabase.MaxConns >= 0},
		{name: "KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS", valid: cfg.EventLogDatabase.MinConns >= 0},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	if cfg.Database.Pool.MinConns > cfg.Database.Pool.MaxConns {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.Database.Retry.JitterRatio < 0 || cfg.Database.Retry.JitterRatio > 1 {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_DATABASE_CONNECT_RETRY_JITTER_RATIO must be between 0 and 1")
	}
	if cfg.EventLogDatabase.MaxConns > 0 && cfg.EventLogDatabase.MinConns > cfg.EventLogDatabase.MaxConns {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	return nil
}

func (cfg Config) validateOutboxSettings() error {
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_RUNTIME_MANAGER_OUTBOX_BATCH_SIZE", valid: cfg.Outbox.BatchSize > 0},
		{name: "KODEX_RUNTIME_MANAGER_OUTBOX_POLL_INTERVAL", valid: cfg.Outbox.PollInterval > 0},
		{name: "KODEX_RUNTIME_MANAGER_OUTBOX_LOCK_TTL", valid: cfg.Outbox.LockTTL > 0},
		{name: "KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISH_TIMEOUT", valid: cfg.Outbox.PublishTimeout > 0},
		{name: "KODEX_RUNTIME_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN", valid: cfg.Outbox.LeaseSafetyMargin >= 0},
		{name: "KODEX_RUNTIME_MANAGER_OUTBOX_RETRY_INITIAL_DELAY", valid: cfg.Outbox.RetryInitialDelay > 0},
		{name: "KODEX_RUNTIME_MANAGER_OUTBOX_RETRY_MAX_DELAY", valid: cfg.Outbox.RetryMaxDelay >= cfg.Outbox.RetryInitialDelay},
		{name: "KODEX_RUNTIME_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT", valid: cfg.Outbox.FailureMessageLimit > 0},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	switch strings.TrimSpace(cfg.Outbox.PublisherKind) {
	case outboxlib.PublisherKindDisabled, outboxlib.PublisherKindDiagnosticLogLossy, outboxlib.PublisherKindPostgresEventLog:
	default:
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISHER_KIND must be disabled, diagnostic-log-lossy or postgres-event-log")
	}
	if cfg.Outbox.DispatchEnabled && strings.TrimSpace(cfg.Outbox.PublisherKind) == outboxlib.PublisherKindDisabled {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISHER_KIND must be configured when outbox dispatch is enabled")
	}
	if strings.TrimSpace(cfg.Outbox.PublisherKind) == outboxlib.PublisherKindDiagnosticLogLossy && !cfg.Outbox.AllowLossyPublisher {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER must be true for diagnostic-log-lossy publisher")
	}
	if strings.TrimSpace(cfg.Outbox.PublisherKind) == outboxlib.PublisherKindPostgresEventLog && strings.TrimSpace(cfg.Outbox.EventLogSource) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_OUTBOX_EVENT_LOG_SOURCE must be configured for postgres-event-log publisher")
	}
	if cfg.needsEventLogDatabase() && strings.TrimSpace(cfg.EventLogDatabase.DSN) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN is required for postgres-event-log publisher")
	}
	if cfg.needsEventLogDatabase() && cfg.EventLogDatabase.MaxConns < 1 {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS must be greater than zero for postgres-event-log publisher")
	}
	if cfg.Outbox.PublishTimeout+cfg.Outbox.LeaseSafetyMargin >= cfg.Outbox.LockTTL {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_OUTBOX_PUBLISH_TIMEOUT plus safety margin must be less than KODEX_RUNTIME_MANAGER_OUTBOX_LOCK_TTL")
	}
	return nil
}

func (cfg Config) needsEventLogDatabase() bool {
	return cfg.Outbox.DispatchEnabled && strings.TrimSpace(cfg.Outbox.PublisherKind) == outboxlib.PublisherKindPostgresEventLog
}

// DatabasePoolSettings converts service config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.poolRuntimeSettings(cfg.Database.DSN, cfg.Database.Pool.MaxConns, cfg.Database.Pool.MinConns))
}

// EventLogDatabasePoolSettings converts event-log env config to a separate pgxpool contract.
func (cfg Config) EventLogDatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.poolRuntimeSettings(cfg.EventLogDatabase.DSN, cfg.EventLogDatabase.MaxConns, cfg.EventLogDatabase.MinConns))
}

func (cfg Config) poolRuntimeSettings(dsn string, maxConns int32, minConns int32) postgreslib.PoolRuntimeSettings {
	return postgreslib.PoolRuntimeSettings{
		DSN:                      dsn,
		MaxConns:                 maxConns,
		MinConns:                 minConns,
		MaxConnLifetime:          cfg.Database.Pool.MaxConnLifetime,
		MaxConnIdleTime:          cfg.Database.Pool.MaxConnIdleTime,
		HealthCheckPeriod:        cfg.Database.Pool.HealthCheckPeriod,
		PingTimeout:              cfg.Database.Pool.PingTimeout,
		ConnectRetryMaxAttempts:  cfg.Database.Retry.MaxAttempts,
		ConnectRetryInitialDelay: cfg.Database.Retry.Initial,
		ConnectRetryMaxDelay:     cfg.Database.Retry.Max,
		ConnectRetryJitterRatio:  cfg.Database.Retry.JitterRatio,
	}
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.ConfigFromRuntimeValues(cfg.GRPC.MaxInFlight, cfg.GRPC.MaxConcurrentStreams, cfg.GRPC.UnaryTimeout, cfg.GRPC.KeepaliveTime, cfg.GRPC.KeepaliveTimeout, cfg.GRPC.KeepaliveMinTime, cfg.GRPC.PermitWithoutStream, cfg.GRPC.MaxRecvMessageBytes, cfg.GRPC.MaxSendMessageBytes, cfg.GRPC.AuthRequired)
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxlib.Config {
	return outboxlib.ConfigFromRuntimeValues(cfg.Outbox.BatchSize, cfg.Outbox.PollInterval, cfg.Outbox.LockTTL, cfg.Outbox.PublishTimeout, cfg.Outbox.RetryInitialDelay, cfg.Outbox.RetryMaxDelay, cfg.Outbox.FailureMessageLimit)
}
