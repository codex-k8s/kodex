package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
)

// Config contains process-level project-catalog server configuration.
type Config struct {
	HTTPAddr                                          string        `env:"KODEX_PROJECT_CATALOG_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                                          string        `env:"KODEX_PROJECT_CATALOG_GRPC_ADDR" envDefault:":9090"`
	GRPCAuthRequired                                  bool          `env:"KODEX_PROJECT_CATALOG_GRPC_AUTH_REQUIRED" envDefault:"true"`
	GRPCAuthToken                                     string        `env:"KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN"`
	GRPCMaxInFlight                                   int           `env:"KODEX_PROJECT_CATALOG_GRPC_MAX_IN_FLIGHT" envDefault:"128"`
	GRPCMaxConcurrentStreams                          uint32        `env:"KODEX_PROJECT_CATALOG_GRPC_MAX_CONCURRENT_STREAMS" envDefault:"128"`
	GRPCUnaryTimeout                                  time.Duration `env:"KODEX_PROJECT_CATALOG_GRPC_UNARY_TIMEOUT" envDefault:"30s"`
	GRPCKeepaliveTime                                 time.Duration `env:"KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_TIME" envDefault:"2m"`
	GRPCKeepaliveTimeout                              time.Duration `env:"KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_TIMEOUT" envDefault:"20s"`
	GRPCKeepaliveMinTime                              time.Duration `env:"KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_MIN_TIME" envDefault:"30s"`
	GRPCPermitWithoutStream                           bool          `env:"KODEX_PROJECT_CATALOG_GRPC_PERMIT_WITHOUT_STREAM" envDefault:"false"`
	GRPCMaxRecvMessageBytes                           int           `env:"KODEX_PROJECT_CATALOG_GRPC_MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCMaxSendMessageBytes                           int           `env:"KODEX_PROJECT_CATALOG_GRPC_MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
	DatabaseDSN                                       string        `env:"KODEX_PROJECT_CATALOG_DATABASE_DSN,required,notEmpty"`
	DatabaseMaxConns                                  int32         `env:"KODEX_PROJECT_CATALOG_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns                                  int32         `env:"KODEX_PROJECT_CATALOG_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime                           time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime                           time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthCheckPeriod                         time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout                               time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_PING_TIMEOUT" envDefault:"5s"`
	DatabaseRetryMaxAttempts                          int           `env:"KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS" envDefault:"6"`
	DatabaseRetryInitialDelay                         time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	DatabaseRetryMaxDelay                             time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_MAX_DELAY" envDefault:"5s"`
	DatabaseRetryJitterRatio                          float64       `env:"KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_JITTER_RATIO" envDefault:"0.2"`
	AccessCheckEnabled                                bool          `env:"KODEX_PROJECT_CATALOG_ACCESS_CHECK_ENABLED" envDefault:"true"`
	AccessManagerGRPCAddr                             string        `env:"KODEX_PROJECT_CATALOG_ACCESS_MANAGER_GRPC_ADDR" envDefault:"access-manager:9090"`
	AccessManagerGRPCAuthToken                        string        `env:"KODEX_PROJECT_CATALOG_ACCESS_MANAGER_GRPC_AUTH_TOKEN"`
	AccessManagerCheckTimeout                         time.Duration `env:"KODEX_PROJECT_CATALOG_ACCESS_MANAGER_CHECK_TIMEOUT" envDefault:"3s"`
	ProviderHubBootstrapEnabled                       bool          `env:"KODEX_PROJECT_CATALOG_PROVIDER_HUB_BOOTSTRAP_ENABLED" envDefault:"true"`
	ProviderHubGRPCAddr                               string        `env:"KODEX_PROJECT_CATALOG_PROVIDER_HUB_GRPC_ADDR" envDefault:"provider-hub:9090"`
	ProviderHubGRPCAuthToken                          string        `env:"KODEX_PROJECT_CATALOG_PROVIDER_HUB_GRPC_AUTH_TOKEN"`
	ProviderHubRequestTimeout                         time.Duration `env:"KODEX_PROJECT_CATALOG_PROVIDER_HUB_REQUEST_TIMEOUT" envDefault:"5s"`
	EventLogDatabaseDSN                               string        `env:"KODEX_PROJECT_CATALOG_EVENT_LOG_DATABASE_DSN"`
	EventLogDatabaseMaxConns                          int32         `env:"KODEX_PROJECT_CATALOG_EVENT_LOG_DATABASE_MAX_CONNS" envDefault:"4"`
	EventLogDatabaseMinConns                          int32         `env:"KODEX_PROJECT_CATALOG_EVENT_LOG_DATABASE_MIN_CONNS" envDefault:"0"`
	OutboxDispatchEnabled                             bool          `env:"KODEX_PROJECT_CATALOG_OUTBOX_DISPATCH_ENABLED" envDefault:"true"`
	OutboxPublisherKind                               string        `env:"KODEX_PROJECT_CATALOG_OUTBOX_PUBLISHER_KIND" envDefault:"postgres-event-log"`
	OutboxEventLogSource                              string        `env:"KODEX_PROJECT_CATALOG_OUTBOX_EVENT_LOG_SOURCE" envDefault:"project-catalog"`
	OutboxAllowLossyPublisher                         bool          `env:"KODEX_PROJECT_CATALOG_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER" envDefault:"false"`
	OutboxBatchSize                                   int           `env:"KODEX_PROJECT_CATALOG_OUTBOX_BATCH_SIZE" envDefault:"100"`
	OutboxPollInterval                                time.Duration `env:"KODEX_PROJECT_CATALOG_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	OutboxLockTTL                                     time.Duration `env:"KODEX_PROJECT_CATALOG_OUTBOX_LOCK_TTL" envDefault:"30s"`
	OutboxPublishTimeout                              time.Duration `env:"KODEX_PROJECT_CATALOG_OUTBOX_PUBLISH_TIMEOUT" envDefault:"10s"`
	OutboxLeaseSafetyMargin                           time.Duration `env:"KODEX_PROJECT_CATALOG_OUTBOX_LEASE_SAFETY_MARGIN" envDefault:"5s"`
	OutboxRetryInitialDelay                           time.Duration `env:"KODEX_PROJECT_CATALOG_OUTBOX_RETRY_INITIAL_DELAY" envDefault:"1s"`
	OutboxRetryMaxDelay                               time.Duration `env:"KODEX_PROJECT_CATALOG_OUTBOX_RETRY_MAX_DELAY" envDefault:"1m"`
	OutboxFailureMessageLimit                         int           `env:"KODEX_PROJECT_CATALOG_OUTBOX_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	ProviderBootstrapMergeConsumerEnabled             bool          `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_ENABLED" envDefault:"true"`
	ProviderBootstrapMergeConsumerName                string        `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_NAME" envDefault:"project-catalog.provider-bootstrap-merge"`
	ProviderBootstrapMergeConsumerLeaseOwner          string        `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_LEASE_OWNER"`
	ProviderBootstrapMergeConsumerBatchSize           int           `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_BATCH_SIZE" envDefault:"50"`
	ProviderBootstrapMergeConsumerPollInterval        time.Duration `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_POLL_INTERVAL" envDefault:"1s"`
	ProviderBootstrapMergeConsumerLeaseTTL            time.Duration `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_LEASE_TTL" envDefault:"30s"`
	ProviderBootstrapMergeConsumerHandlerTimeout      time.Duration `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_HANDLER_TIMEOUT" envDefault:"10s"`
	ProviderBootstrapMergeConsumerRetryInitialDelay   time.Duration `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_RETRY_INITIAL_DELAY" envDefault:"1s"`
	ProviderBootstrapMergeConsumerRetryMaxDelay       time.Duration `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_RETRY_MAX_DELAY" envDefault:"1m"`
	ProviderBootstrapMergeConsumerFailureMessageLimit int           `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	ProviderBootstrapMergeConsumerConcurrencyLimit    int           `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_CONCURRENCY_LIMIT" envDefault:"2"`
	ProviderBootstrapMergeConsumerMaxAttempts         int           `env:"KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_MAX_ATTEMPTS" envDefault:"5"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse project-catalog config from environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks configuration invariants that protect runtime boundaries.
func (cfg Config) Validate() error {
	if cfg.GRPCAuthRequired && strings.TrimSpace(cfg.GRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_PROJECT_CATALOG_GRPC_MAX_IN_FLIGHT", valid: cfg.GRPCMaxInFlight > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_MAX_CONCURRENT_STREAMS", valid: cfg.GRPCMaxConcurrentStreams > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_UNARY_TIMEOUT", valid: cfg.GRPCUnaryTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_TIME", valid: cfg.GRPCKeepaliveTime > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_TIMEOUT", valid: cfg.GRPCKeepaliveTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_MIN_TIME", valid: cfg.GRPCKeepaliveMinTime > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_MAX_RECV_MESSAGE_BYTES", valid: cfg.GRPCMaxRecvMessageBytes > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_MAX_SEND_MESSAGE_BYTES", valid: cfg.GRPCMaxSendMessageBytes > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_MAX_CONNS", valid: cfg.DatabaseMaxConns > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_MIN_CONNS", valid: cfg.DatabaseMinConns >= 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_MAX_CONN_LIFETIME", valid: cfg.DatabaseMaxConnLifetime > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_MAX_CONN_IDLE_TIME", valid: cfg.DatabaseMaxConnIdleTime > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_HEALTH_CHECK_PERIOD", valid: cfg.DatabaseHealthCheckPeriod > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_PING_TIMEOUT", valid: cfg.DatabasePingTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS", valid: cfg.DatabaseRetryMaxAttempts > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_INITIAL_DELAY", valid: cfg.DatabaseRetryInitialDelay >= 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_MAX_DELAY", valid: cfg.DatabaseRetryMaxDelay >= cfg.DatabaseRetryInitialDelay},
		{name: "KODEX_PROJECT_CATALOG_ACCESS_MANAGER_CHECK_TIMEOUT", valid: cfg.AccessManagerCheckTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_HUB_REQUEST_TIMEOUT", valid: cfg.ProviderHubRequestTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_EVENT_LOG_DATABASE_MAX_CONNS", valid: cfg.EventLogDatabaseMaxConns >= 0},
		{name: "KODEX_PROJECT_CATALOG_EVENT_LOG_DATABASE_MIN_CONNS", valid: cfg.EventLogDatabaseMinConns >= 0},
		{name: "KODEX_PROJECT_CATALOG_OUTBOX_BATCH_SIZE", valid: cfg.OutboxBatchSize > 0},
		{name: "KODEX_PROJECT_CATALOG_OUTBOX_POLL_INTERVAL", valid: cfg.OutboxPollInterval > 0},
		{name: "KODEX_PROJECT_CATALOG_OUTBOX_LOCK_TTL", valid: cfg.OutboxLockTTL > 0},
		{name: "KODEX_PROJECT_CATALOG_OUTBOX_PUBLISH_TIMEOUT", valid: cfg.OutboxPublishTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_OUTBOX_LEASE_SAFETY_MARGIN", valid: cfg.OutboxLeaseSafetyMargin >= 0},
		{name: "KODEX_PROJECT_CATALOG_OUTBOX_RETRY_INITIAL_DELAY", valid: cfg.OutboxRetryInitialDelay > 0},
		{name: "KODEX_PROJECT_CATALOG_OUTBOX_RETRY_MAX_DELAY", valid: cfg.OutboxRetryMaxDelay >= cfg.OutboxRetryInitialDelay},
		{name: "KODEX_PROJECT_CATALOG_OUTBOX_FAILURE_MESSAGE_LIMIT", valid: cfg.OutboxFailureMessageLimit > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_BATCH_SIZE", valid: cfg.ProviderBootstrapMergeConsumerBatchSize > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_POLL_INTERVAL", valid: cfg.ProviderBootstrapMergeConsumerPollInterval > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_LEASE_TTL", valid: cfg.ProviderBootstrapMergeConsumerLeaseTTL > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_HANDLER_TIMEOUT", valid: cfg.ProviderBootstrapMergeConsumerHandlerTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_RETRY_INITIAL_DELAY", valid: cfg.ProviderBootstrapMergeConsumerRetryInitialDelay > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_RETRY_MAX_DELAY", valid: cfg.ProviderBootstrapMergeConsumerRetryMaxDelay >= cfg.ProviderBootstrapMergeConsumerRetryInitialDelay},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_FAILURE_MESSAGE_LIMIT", valid: cfg.ProviderBootstrapMergeConsumerFailureMessageLimit > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_CONCURRENCY_LIMIT", valid: cfg.ProviderBootstrapMergeConsumerConcurrencyLimit > 0},
		{name: "KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_MAX_ATTEMPTS", valid: cfg.ProviderBootstrapMergeConsumerMaxAttempts > 0},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.DatabaseRetryJitterRatio < 0 || cfg.DatabaseRetryJitterRatio > 1 {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_JITTER_RATIO must be between 0 and 1")
	}
	if cfg.AccessCheckEnabled {
		if strings.TrimSpace(cfg.AccessManagerGRPCAddr) == "" {
			return fmt.Errorf("KODEX_PROJECT_CATALOG_ACCESS_MANAGER_GRPC_ADDR is required when access checks are enabled")
		}
		if strings.TrimSpace(cfg.AccessManagerGRPCAuthToken) == "" {
			return fmt.Errorf("KODEX_PROJECT_CATALOG_ACCESS_MANAGER_GRPC_AUTH_TOKEN is required when access checks are enabled")
		}
	}
	if cfg.ProviderHubBootstrapEnabled {
		if strings.TrimSpace(cfg.ProviderHubGRPCAddr) == "" {
			return fmt.Errorf("KODEX_PROJECT_CATALOG_PROVIDER_HUB_GRPC_ADDR is required when provider-hub bootstrap is enabled")
		}
		if strings.TrimSpace(cfg.ProviderHubGRPCAuthToken) == "" {
			return fmt.Errorf("KODEX_PROJECT_CATALOG_PROVIDER_HUB_GRPC_AUTH_TOKEN is required when provider-hub bootstrap is enabled")
		}
	}
	switch strings.TrimSpace(cfg.OutboxPublisherKind) {
	case outboxlib.PublisherKindDisabled, outboxlib.PublisherKindDiagnosticLogLossy, outboxlib.PublisherKindPostgresEventLog:
	default:
		return fmt.Errorf("KODEX_PROJECT_CATALOG_OUTBOX_PUBLISHER_KIND must be disabled, diagnostic-log-lossy or postgres-event-log")
	}
	if cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDisabled {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_OUTBOX_PUBLISHER_KIND must be configured when outbox dispatch is enabled")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDiagnosticLogLossy && !cfg.OutboxAllowLossyPublisher {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER must be true for diagnostic-log-lossy publisher")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog && strings.TrimSpace(cfg.OutboxEventLogSource) == "" {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_OUTBOX_EVENT_LOG_SOURCE must be configured for postgres-event-log publisher")
	}
	if cfg.ProviderBootstrapMergeConsumerEnabled && strings.TrimSpace(cfg.ProviderBootstrapMergeConsumerName) == "" {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_PROVIDER_BOOTSTRAP_MERGE_CONSUMER_NAME is required when bootstrap merge consumer is enabled")
	}
	if cfg.needsEventLogDatabase() && strings.TrimSpace(cfg.EventLogDatabaseDSN) == "" {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_EVENT_LOG_DATABASE_DSN is required for event-log publisher or consumer")
	}
	if cfg.needsEventLogDatabase() && cfg.EventLogDatabaseMaxConns < 1 {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_EVENT_LOG_DATABASE_MAX_CONNS must be greater than zero for event-log publisher or consumer")
	}
	if cfg.EventLogDatabaseMaxConns > 0 && cfg.EventLogDatabaseMinConns > cfg.EventLogDatabaseMaxConns {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_EVENT_LOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.OutboxPublishTimeout+cfg.OutboxLeaseSafetyMargin >= cfg.OutboxLockTTL {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_OUTBOX_PUBLISH_TIMEOUT plus safety margin must be less than KODEX_PROJECT_CATALOG_OUTBOX_LOCK_TTL")
	}
	return nil
}

func (cfg Config) needsEventLogDatabase() bool {
	return cfg.ProviderBootstrapMergeConsumerEnabled || (cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog)
}

// DatabasePoolSettings converts service config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.databaseRuntimeSettings(cfg.DatabaseDSN, cfg.DatabaseMaxConns, cfg.DatabaseMinConns))
}

// EventLogDatabasePoolSettings converts event-log env config to a separate pgxpool contract.
func (cfg Config) EventLogDatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.databaseRuntimeSettings(cfg.EventLogDatabaseDSN, cfg.EventLogDatabaseMaxConns, cfg.EventLogDatabaseMinConns))
}

func (cfg Config) databaseRuntimeSettings(dsn string, maxConns int32, minConns int32) postgreslib.PoolRuntimeSettings {
	runtime := postgreslib.PoolRuntimeSettingsFromValues(dsn, maxConns, minConns, cfg.DatabaseMaxConnLifetime, cfg.DatabaseMaxConnIdleTime, cfg.DatabaseHealthCheckPeriod, cfg.DatabasePingTimeout, cfg.DatabaseRetryMaxAttempts, cfg.DatabaseRetryInitialDelay, cfg.DatabaseRetryMaxDelay, cfg.DatabaseRetryJitterRatio)
	runtime.MinConns = minConns
	return runtime
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.ConfigFromRuntimeValues(cfg.GRPCMaxInFlight, cfg.GRPCMaxConcurrentStreams, cfg.GRPCUnaryTimeout, cfg.GRPCKeepaliveTime, cfg.GRPCKeepaliveTimeout, cfg.GRPCKeepaliveMinTime, cfg.GRPCPermitWithoutStream, cfg.GRPCMaxRecvMessageBytes, cfg.GRPCMaxSendMessageBytes, cfg.GRPCAuthRequired)
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxlib.Config {
	return outboxlib.ConfigFromRuntimeValues(cfg.OutboxBatchSize, cfg.OutboxPollInterval, cfg.OutboxLockTTL, cfg.OutboxPublishTimeout, cfg.OutboxRetryInitialDelay, cfg.OutboxRetryMaxDelay, cfg.OutboxFailureMessageLimit)
}

// ProviderBootstrapMergeConsumerConfig converts env fields to the shared event consumer runtime.
func (cfg Config) ProviderBootstrapMergeConsumerConfig() eventconsumer.Config {
	leaseOwner := strings.TrimSpace(cfg.ProviderBootstrapMergeConsumerLeaseOwner)
	if leaseOwner == "" {
		leaseOwner = eventconsumer.DefaultLeaseOwner("project-catalog-provider-bootstrap-merge")
	}
	return eventconsumer.ConfigFromRuntimeValues(
		cfg.ProviderBootstrapMergeConsumerName,
		leaseOwner,
		cfg.ProviderBootstrapMergeConsumerBatchSize,
		cfg.ProviderBootstrapMergeConsumerPollInterval,
		cfg.ProviderBootstrapMergeConsumerLeaseTTL,
		cfg.ProviderBootstrapMergeConsumerHandlerTimeout,
		cfg.ProviderBootstrapMergeConsumerRetryInitialDelay,
		cfg.ProviderBootstrapMergeConsumerRetryMaxDelay,
		cfg.ProviderBootstrapMergeConsumerFailureMessageLimit,
		cfg.ProviderBootstrapMergeConsumerConcurrencyLimit,
		cfg.ProviderBootstrapMergeConsumerMaxAttempts,
	)
}
