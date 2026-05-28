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

// Config contains process-level provider-hub server configuration.
type Config struct {
	HTTPAddr                   string        `env:"KODEX_PROVIDER_HUB_HTTP_ADDR" envDefault:":8080"`
	DatabaseDSN                string        `env:"KODEX_PROVIDER_HUB_DATABASE_DSN,required,notEmpty"`
	DatabaseMaxConns           int32         `env:"KODEX_PROVIDER_HUB_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns           int32         `env:"KODEX_PROVIDER_HUB_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime    time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime    time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthCheckPeriod  time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout        time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_PING_TIMEOUT" envDefault:"5s"`
	DatabaseRetryMaxAttempts   int           `env:"KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS" envDefault:"6"`
	DatabaseRetryInitialDelay  time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	DatabaseRetryMaxDelay      time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_MAX_DELAY" envDefault:"5s"`
	DatabaseRetryJitterRatio   float64       `env:"KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_JITTER_RATIO" envDefault:"0.2"`
	GRPCAddr                   string        `env:"KODEX_PROVIDER_HUB_GRPC_ADDR" envDefault:":9090"`
	GRPCAuthRequired           bool          `env:"KODEX_PROVIDER_HUB_GRPC_AUTH_REQUIRED" envDefault:"true"`
	GRPCAuthToken              string        `env:"KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN"`
	GRPCMaxInFlight            int           `env:"KODEX_PROVIDER_HUB_GRPC_MAX_IN_FLIGHT" envDefault:"128"`
	GRPCMaxConcurrentStreams   uint32        `env:"KODEX_PROVIDER_HUB_GRPC_MAX_CONCURRENT_STREAMS" envDefault:"128"`
	GRPCUnaryTimeout           time.Duration `env:"KODEX_PROVIDER_HUB_GRPC_UNARY_TIMEOUT" envDefault:"30s"`
	GRPCKeepaliveTime          time.Duration `env:"KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_TIME" envDefault:"2m"`
	GRPCKeepaliveTimeout       time.Duration `env:"KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_TIMEOUT" envDefault:"20s"`
	GRPCKeepaliveMinTime       time.Duration `env:"KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_MIN_TIME" envDefault:"30s"`
	GRPCPermitWithoutStream    bool          `env:"KODEX_PROVIDER_HUB_GRPC_PERMIT_WITHOUT_STREAM" envDefault:"false"`
	GRPCMaxRecvMessageBytes    int           `env:"KODEX_PROVIDER_HUB_GRPC_MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCMaxSendMessageBytes    int           `env:"KODEX_PROVIDER_HUB_GRPC_MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
	AccessManagerGRPCAddr      string        `env:"KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_ADDR" envDefault:"access-manager:9090"`
	AccessManagerGRPCAuthToken string        `env:"KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN"`
	AccessManagerGRPCTimeout   time.Duration `env:"KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_TIMEOUT" envDefault:"3s"`
	GitHubBaseURL              string        `env:"KODEX_PROVIDER_HUB_GITHUB_BASE_URL" envDefault:"https://api.github.com"`
	GitHubUserAgent            string        `env:"KODEX_PROVIDER_HUB_GITHUB_USER_AGENT" envDefault:"kodex-provider-hub"`
	WebhookPayloadRetention    time.Duration `env:"KODEX_PROVIDER_HUB_WEBHOOK_PAYLOAD_RETENTION" envDefault:"168h"`
	WebhookPayloadCleanupLimit int32         `env:"KODEX_PROVIDER_HUB_WEBHOOK_PAYLOAD_CLEANUP_LIMIT" envDefault:"100"`
	SecretMountedRoot          string        `env:"KODEX_PROVIDER_HUB_SECRET_MOUNTED_ROOT" envDefault:"/var/run/kodex/secrets"`
	SecretMaxBytes             int64         `env:"KODEX_PROVIDER_HUB_SECRET_MAX_BYTES" envDefault:"1048576"`
	VaultAddr                  string        `env:"KODEX_PROVIDER_HUB_VAULT_ADDR"`
	VaultToken                 string        `env:"KODEX_PROVIDER_HUB_VAULT_TOKEN"`
	VaultNamespace             string        `env:"KODEX_PROVIDER_HUB_VAULT_NAMESPACE"`
	EventLogDatabaseDSN        string        `env:"KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_DSN"`
	EventLogDatabaseMaxConns   int32         `env:"KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_MAX_CONNS" envDefault:"4"`
	EventLogDatabaseMinConns   int32         `env:"KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_MIN_CONNS" envDefault:"0"`
	OutboxDispatchEnabled      bool          `env:"KODEX_PROVIDER_HUB_OUTBOX_DISPATCH_ENABLED" envDefault:"true"`
	OutboxPublisherKind        string        `env:"KODEX_PROVIDER_HUB_OUTBOX_PUBLISHER_KIND" envDefault:"postgres-event-log"`
	OutboxEventLogSource       string        `env:"KODEX_PROVIDER_HUB_OUTBOX_EVENT_LOG_SOURCE" envDefault:"provider-hub"`
	OutboxAllowLossyPublisher  bool          `env:"KODEX_PROVIDER_HUB_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER" envDefault:"false"`
	OutboxBatchSize            int           `env:"KODEX_PROVIDER_HUB_OUTBOX_BATCH_SIZE" envDefault:"100"`
	OutboxPollInterval         time.Duration `env:"KODEX_PROVIDER_HUB_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	OutboxLockTTL              time.Duration `env:"KODEX_PROVIDER_HUB_OUTBOX_LOCK_TTL" envDefault:"30s"`
	OutboxPublishTimeout       time.Duration `env:"KODEX_PROVIDER_HUB_OUTBOX_PUBLISH_TIMEOUT" envDefault:"10s"`
	OutboxLeaseSafetyMargin    time.Duration `env:"KODEX_PROVIDER_HUB_OUTBOX_LEASE_SAFETY_MARGIN" envDefault:"5s"`
	OutboxRetryInitialDelay    time.Duration `env:"KODEX_PROVIDER_HUB_OUTBOX_RETRY_INITIAL_DELAY" envDefault:"1s"`
	OutboxRetryMaxDelay        time.Duration `env:"KODEX_PROVIDER_HUB_OUTBOX_RETRY_MAX_DELAY" envDefault:"1m"`
	OutboxFailureMessageLimit  int           `env:"KODEX_PROVIDER_HUB_OUTBOX_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load provider-hub config: %w", err)
	}
	return cfg, nil
}

// Validate checks configuration invariants that protect runtime boundaries.
func (cfg Config) Validate() error {
	if cfg.GRPCAuthRequired && strings.TrimSpace(cfg.GRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if err := cfg.validateGRPCSettings(); err != nil {
		return err
	}
	if err := cfg.validateDatabaseSettings(); err != nil {
		return err
	}
	if err := cfg.validateProviderIntegrationSettings(); err != nil {
		return err
	}
	return cfg.validateOutboxSettings()
}

func (cfg Config) validateGRPCSettings() error {
	if err := requirePositive("KODEX_PROVIDER_HUB_GRPC_MAX_IN_FLIGHT", cfg.GRPCMaxInFlight); err != nil {
		return err
	}
	if cfg.GRPCMaxConcurrentStreams == 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_GRPC_MAX_CONCURRENT_STREAMS is invalid")
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_GRPC_UNARY_TIMEOUT", cfg.GRPCUnaryTimeout); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_TIME", cfg.GRPCKeepaliveTime); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_TIMEOUT", cfg.GRPCKeepaliveTimeout); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_MIN_TIME", cfg.GRPCKeepaliveMinTime); err != nil {
		return err
	}
	if err := requirePositive("KODEX_PROVIDER_HUB_GRPC_MAX_RECV_MESSAGE_BYTES", cfg.GRPCMaxRecvMessageBytes); err != nil {
		return err
	}
	return requirePositive("KODEX_PROVIDER_HUB_GRPC_MAX_SEND_MESSAGE_BYTES", cfg.GRPCMaxSendMessageBytes)
}

func (cfg Config) validateDatabaseSettings() error {
	if cfg.DatabaseMaxConns <= 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_MAX_CONNS is invalid")
	}
	if cfg.DatabaseMinConns < 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_MIN_CONNS is invalid")
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_DATABASE_MAX_CONN_LIFETIME", cfg.DatabaseMaxConnLifetime); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_DATABASE_MAX_CONN_IDLE_TIME", cfg.DatabaseMaxConnIdleTime); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_DATABASE_HEALTH_CHECK_PERIOD", cfg.DatabaseHealthCheckPeriod); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_DATABASE_PING_TIMEOUT", cfg.DatabasePingTimeout); err != nil {
		return err
	}
	if err := requirePositive("KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS", cfg.DatabaseRetryMaxAttempts); err != nil {
		return err
	}
	if cfg.DatabaseRetryInitialDelay < 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_INITIAL_DELAY is invalid")
	}
	if cfg.DatabaseRetryMaxDelay < cfg.DatabaseRetryInitialDelay {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_MAX_DELAY is invalid")
	}
	if cfg.DatabaseRetryJitterRatio < 0 || cfg.DatabaseRetryJitterRatio > 1 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_JITTER_RATIO must be between 0 and 1")
	}
	return nil
}

func (cfg Config) validateProviderIntegrationSettings() error {
	if strings.TrimSpace(cfg.AccessManagerGRPCAddr) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_ADDR is required")
	}
	if strings.TrimSpace(cfg.AccessManagerGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN is required")
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_TIMEOUT", cfg.AccessManagerGRPCTimeout); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.GitHubBaseURL) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_GITHUB_BASE_URL is required")
	}
	if strings.TrimSpace(cfg.GitHubUserAgent) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_GITHUB_USER_AGENT is required")
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_WEBHOOK_PAYLOAD_RETENTION", cfg.WebhookPayloadRetention); err != nil {
		return err
	}
	if cfg.WebhookPayloadCleanupLimit <= 0 || cfg.WebhookPayloadCleanupLimit > 500 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_WEBHOOK_PAYLOAD_CLEANUP_LIMIT must be between 1 and 500")
	}
	if strings.TrimSpace(cfg.SecretMountedRoot) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_SECRET_MOUNTED_ROOT is required")
	}
	if cfg.SecretMaxBytes <= 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_SECRET_MAX_BYTES is invalid")
	}
	if strings.TrimSpace(cfg.VaultAddr) != "" && strings.TrimSpace(cfg.VaultToken) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_VAULT_TOKEN is required when Vault address is configured")
	}
	return nil
}

func (cfg Config) validateOutboxSettings() error {
	switch strings.TrimSpace(cfg.OutboxPublisherKind) {
	case outboxlib.PublisherKindDisabled, outboxlib.PublisherKindDiagnosticLogLossy, outboxlib.PublisherKindPostgresEventLog:
	default:
		return fmt.Errorf("KODEX_PROVIDER_HUB_OUTBOX_PUBLISHER_KIND must be disabled, diagnostic-log-lossy or postgres-event-log")
	}
	if cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDisabled {
		return fmt.Errorf("KODEX_PROVIDER_HUB_OUTBOX_PUBLISHER_KIND must be configured when outbox dispatch is enabled")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDiagnosticLogLossy && !cfg.OutboxAllowLossyPublisher {
		return fmt.Errorf("KODEX_PROVIDER_HUB_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER must be true for diagnostic-log-lossy publisher")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog && strings.TrimSpace(cfg.OutboxEventLogSource) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_OUTBOX_EVENT_LOG_SOURCE must be configured for postgres-event-log publisher")
	}
	if cfg.needsEventLogDatabase() && strings.TrimSpace(cfg.EventLogDatabaseDSN) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_DSN is required for postgres-event-log publisher")
	}
	if cfg.EventLogDatabaseMaxConns < 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_MAX_CONNS must not be negative")
	}
	if cfg.EventLogDatabaseMinConns < 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_MIN_CONNS must not be negative")
	}
	if cfg.needsEventLogDatabase() && cfg.EventLogDatabaseMaxConns < 1 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_MAX_CONNS must be greater than zero for postgres-event-log publisher")
	}
	if cfg.EventLogDatabaseMaxConns > 0 && cfg.EventLogDatabaseMinConns > cfg.EventLogDatabaseMaxConns {
		return fmt.Errorf("KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if err := requirePositive("KODEX_PROVIDER_HUB_OUTBOX_BATCH_SIZE", cfg.OutboxBatchSize); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_OUTBOX_POLL_INTERVAL", cfg.OutboxPollInterval); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_OUTBOX_LOCK_TTL", cfg.OutboxLockTTL); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_OUTBOX_PUBLISH_TIMEOUT", cfg.OutboxPublishTimeout); err != nil {
		return err
	}
	if cfg.OutboxLeaseSafetyMargin < 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_OUTBOX_LEASE_SAFETY_MARGIN must not be negative")
	}
	if cfg.OutboxPublishTimeout+cfg.OutboxLeaseSafetyMargin >= cfg.OutboxLockTTL {
		return fmt.Errorf("KODEX_PROVIDER_HUB_OUTBOX_PUBLISH_TIMEOUT plus safety margin must be less than KODEX_PROVIDER_HUB_OUTBOX_LOCK_TTL")
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_OUTBOX_RETRY_INITIAL_DELAY", cfg.OutboxRetryInitialDelay); err != nil {
		return err
	}
	if cfg.OutboxRetryMaxDelay < cfg.OutboxRetryInitialDelay {
		return fmt.Errorf("KODEX_PROVIDER_HUB_OUTBOX_RETRY_MAX_DELAY must be greater than or equal to initial delay")
	}
	return requirePositive("KODEX_PROVIDER_HUB_OUTBOX_FAILURE_MESSAGE_LIMIT", cfg.OutboxFailureMessageLimit)
}

func (cfg Config) needsEventLogDatabase() bool {
	return cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog
}

func requirePositive(name string, value int) error {
	if value <= 0 {
		return fmt.Errorf("%s is invalid", name)
	}
	return nil
}

func requireDuration(name string, value time.Duration) error {
	if value <= 0 {
		return fmt.Errorf("%s is invalid", name)
	}
	return nil
}

// DatabasePoolSettings converts service config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.databaseRuntimeSettings(cfg.DatabaseDSN, cfg.DatabaseMaxConns, cfg.DatabaseMinConns))
}

// EventLogDatabasePoolSettings converts event-log env config to a separate pgxpool contract.
func (cfg Config) EventLogDatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.databaseRuntimeSettings(cfg.EventLogDatabaseDSN, cfg.EventLogDatabaseMaxConns, cfg.EventLogDatabaseMinConns))
}

func (cfg Config) optionalEventLogDatabasePoolSettings() (postgreslib.PoolSettings, bool) {
	if !cfg.needsEventLogDatabase() {
		return postgreslib.PoolSettings{}, false
	}
	return cfg.EventLogDatabasePoolSettings(), true
}

func (cfg Config) databaseRuntimeSettings(dsn string, maxConns int32, minConns int32) postgreslib.PoolRuntimeSettings {
	return postgreslib.PoolRuntimeSettingsFromValues(dsn, maxConns, minConns, cfg.DatabaseMaxConnLifetime, cfg.DatabaseMaxConnIdleTime, cfg.DatabaseHealthCheckPeriod, cfg.DatabasePingTimeout, cfg.DatabaseRetryMaxAttempts, cfg.DatabaseRetryInitialDelay, cfg.DatabaseRetryMaxDelay, cfg.DatabaseRetryJitterRatio)
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.ConfigFromRuntimeValues(cfg.GRPCMaxInFlight, cfg.GRPCMaxConcurrentStreams, cfg.GRPCUnaryTimeout, cfg.GRPCKeepaliveTime, cfg.GRPCKeepaliveTimeout, cfg.GRPCKeepaliveMinTime, cfg.GRPCPermitWithoutStream, cfg.GRPCMaxRecvMessageBytes, cfg.GRPCMaxSendMessageBytes, cfg.GRPCAuthRequired)
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxlib.Config {
	return outboxlib.ConfigFromRuntimeValues(cfg.OutboxBatchSize, cfg.OutboxPollInterval, cfg.OutboxLockTTL, cfg.OutboxPublishTimeout, cfg.OutboxRetryInitialDelay, cfg.OutboxRetryMaxDelay, cfg.OutboxFailureMessageLimit)
}
