// Package app contains fleet-manager process composition and lifecycle.
package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	fleetservice "github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/service"
)

// Config contains process-level fleet-manager server configuration.
type Config struct {
	HTTPAddr         string                  `env:"KODEX_FLEET_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr         string                  `env:"KODEX_FLEET_MANAGER_GRPC_ADDR" envDefault:":9090"`
	GRPC             FleetGRPCConfig         `envPrefix:"KODEX_FLEET_MANAGER_GRPC_"`
	Database         FleetDatabaseConfig     `envPrefix:"KODEX_FLEET_MANAGER_DATABASE_"`
	EventLogDatabase FleetEventLogDBConfig   `envPrefix:"KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_"`
	Outbox           FleetOutboxConfig       `envPrefix:"KODEX_FLEET_MANAGER_OUTBOX_"`
	Access           FleetAccessConfig       `envPrefix:"KODEX_FLEET_MANAGER_ACCESS_"`
	Bootstrap        FleetBootstrapConfig    `envPrefix:"KODEX_FLEET_MANAGER_BOOTSTRAP_"`
	SecretResolver   FleetSecretConfig       `envPrefix:"KODEX_FLEET_MANAGER_SECRET_RESOLVER_"`
	Connectivity     FleetConnectivityConfig `envPrefix:"KODEX_FLEET_MANAGER_CONNECTIVITY_"`
}

// FleetGRPCConfig contains gRPC boundary limits.
type FleetGRPCConfig struct {
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

// FleetDatabaseConfig contains owned fleet-manager database settings.
type FleetDatabaseConfig struct {
	DSN   string                   `env:"DSN,required,notEmpty"`
	Pool  FleetDatabasePoolConfig  `envPrefix:""`
	Retry FleetDatabaseRetryConfig `envPrefix:"CONNECT_RETRY_"`
}

// FleetDatabasePoolConfig contains bounded PostgreSQL connection pool settings.
type FleetDatabasePoolConfig struct {
	MaxConns          int32         `env:"MAX_CONNS" envDefault:"8"`
	MinConns          int32         `env:"MIN_CONNS" envDefault:"1"`
	MaxConnLifetime   time.Duration `env:"MAX_CONN_LIFETIME" envDefault:"1h"`
	MaxConnIdleTime   time.Duration `env:"MAX_CONN_IDLE_TIME" envDefault:"15m"`
	HealthCheckPeriod time.Duration `env:"HEALTH_CHECK_PERIOD" envDefault:"30s"`
	PingTimeout       time.Duration `env:"PING_TIMEOUT" envDefault:"5s"`
}

// FleetDatabaseRetryConfig contains startup database connection retry settings.
type FleetDatabaseRetryConfig struct {
	MaxAttempts int           `env:"MAX_ATTEMPTS" envDefault:"6"`
	Initial     time.Duration `env:"INITIAL_DELAY" envDefault:"500ms"`
	Max         time.Duration `env:"MAX_DELAY" envDefault:"5s"`
	JitterRatio float64       `env:"JITTER_RATIO" envDefault:"0.2"`
}

// FleetEventLogDBConfig contains shared event-log database settings.
type FleetEventLogDBConfig struct {
	DSN      string `env:"DSN"`
	MaxConns int32  `env:"MAX_CONNS" envDefault:"4"`
	MinConns int32  `env:"MIN_CONNS" envDefault:"0"`
}

// FleetOutboxConfig contains local outbox dispatcher settings.
type FleetOutboxConfig struct {
	DispatchEnabled     bool          `env:"DISPATCH_ENABLED" envDefault:"true"`
	AllowLossyPublisher bool          `env:"ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER" envDefault:"false"`
	EventLogSource      string        `env:"EVENT_LOG_SOURCE" envDefault:"fleet-manager"`
	PublisherKind       string        `env:"PUBLISHER_KIND" envDefault:"postgres-event-log"`
	BatchSize           int           `env:"BATCH_SIZE" envDefault:"100"`
	FailureMessageLimit int           `env:"FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	LeaseSafetyMargin   time.Duration `env:"LEASE_SAFETY_MARGIN" envDefault:"5s"`
	LockTTL             time.Duration `env:"LOCK_TTL" envDefault:"30s"`
	PollInterval        time.Duration `env:"POLL_INTERVAL" envDefault:"1s"`
	PublishTimeout      time.Duration `env:"PUBLISH_TIMEOUT" envDefault:"10s"`
	RetryInitialDelay   time.Duration `env:"RETRY_INITIAL_DELAY" envDefault:"1s"`
	RetryMaxDelay       time.Duration `env:"RETRY_MAX_DELAY" envDefault:"1m"`
}

// FleetAccessConfig contains access-manager authorization settings.
type FleetAccessConfig struct {
	CheckEnabled           bool          `env:"CHECK_ENABLED" envDefault:"true"`
	AccessManagerGRPCAddr  string        `env:"MANAGER_GRPC_ADDR" envDefault:"access-manager:9090"`
	AccessManagerAuthToken string        `env:"MANAGER_GRPC_AUTH_TOKEN"`
	CheckTimeout           time.Duration `env:"MANAGER_CHECK_TIMEOUT" envDefault:"3s"`
}

// FleetSecretConfig contains value-safe secret resolver backend settings.
type FleetSecretConfig struct {
	EnvEnabled                bool   `env:"ENV_ENABLED" envDefault:"true"`
	MountedKubernetesRoot     string `env:"MOUNTED_KUBERNETES_ROOT"`
	MountedKubernetesMaxBytes int64  `env:"MOUNTED_KUBERNETES_MAX_SECRET_BYTES" envDefault:"1048576"`
	VaultAddr                 string `env:"VAULT_ADDR"`
	VaultToken                string `env:"VAULT_TOKEN"`
	VaultNamespace            string `env:"VAULT_NAMESPACE"`
}

// FleetConnectivityConfig contains bounded Kubernetes API probe settings.
type FleetConnectivityConfig struct {
	CheckTimeout time.Duration `env:"CHECK_TIMEOUT" envDefault:"5s"`
}

// FleetBootstrapConfig contains bootstrap seed for the default local installation path.
type FleetBootstrapConfig struct {
	SeedEnabled       bool   `env:"SEED_ENABLED" envDefault:"true"`
	FleetScopeID      string `env:"FLEET_SCOPE_ID" envDefault:"00000000-0000-0000-0000-000000000001"`
	ClusterID         string `env:"CLUSTER_ID" envDefault:"00000000-0000-0000-0000-000000000002"`
	ScopeKey          string `env:"SCOPE_KEY" envDefault:"platform-default"`
	ScopeDisplayName  string `env:"SCOPE_DISPLAY_NAME" envDefault:"Platform default"`
	ClusterKey        string `env:"CLUSTER_KEY" envDefault:"platform-default"`
	APIEndpointRef    string `env:"API_ENDPOINT_REF"`
	SecretStoreType   string `env:"SECRET_STORE_TYPE"`
	SecretStoreRef    string `env:"SECRET_STORE_REF"`
	KubernetesVersion string `env:"KUBERNETES_VERSION"`
	Region            string `env:"REGION"`
	CapacityClass     string `env:"CAPACITY_CLASS"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load fleet-manager config: %w", err)
	}
	return cfg, nil
}

// Validate checks configuration invariants that protect fleet boundaries.
func (cfg Config) Validate() error {
	if cfg.GRPC.AuthRequired && strings.TrimSpace(cfg.GRPC.AuthToken) == "" {
		return fmt.Errorf("KODEX_FLEET_MANAGER_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	validators := []func() error{
		cfg.validateGRPC,
		cfg.validateDatabase,
		cfg.validateOutbox,
		cfg.validateAccess,
		cfg.validateSecrets,
		cfg.validateConnectivity,
		cfg.validateBootstrap,
	}
	for _, validate := range validators {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (cfg Config) validateGRPC() error {
	numericChecks := []struct {
		envName string
		valid   bool
	}{
		{envName: "KODEX_FLEET_MANAGER_GRPC_MAX_IN_FLIGHT", valid: cfg.GRPC.MaxInFlight > 0},
		{envName: "KODEX_FLEET_MANAGER_GRPC_MAX_CONCURRENT_STREAMS", valid: cfg.GRPC.MaxConcurrentStreams > 0},
		{envName: "KODEX_FLEET_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES", valid: cfg.GRPC.MaxRecvMessageBytes > 0},
		{envName: "KODEX_FLEET_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES", valid: cfg.GRPC.MaxSendMessageBytes > 0},
	}
	for _, check := range numericChecks {
		if !check.valid {
			return fmt.Errorf("%s is invalid", check.envName)
		}
	}
	return validatePositiveDurations(map[string]time.Duration{
		"KODEX_FLEET_MANAGER_GRPC_UNARY_TIMEOUT":      cfg.GRPC.UnaryTimeout,
		"KODEX_FLEET_MANAGER_GRPC_KEEPALIVE_TIME":     cfg.GRPC.KeepaliveTime,
		"KODEX_FLEET_MANAGER_GRPC_KEEPALIVE_TIMEOUT":  cfg.GRPC.KeepaliveTimeout,
		"KODEX_FLEET_MANAGER_GRPC_KEEPALIVE_MIN_TIME": cfg.GRPC.KeepaliveMinTime,
	})
}

func (cfg Config) validateDatabase() error {
	if cfg.Database.Pool.MaxConns <= 0 || cfg.Database.Pool.MinConns < 0 {
		return fmt.Errorf("KODEX_FLEET_MANAGER_DATABASE_MAX_CONNS and MIN_CONNS are invalid")
	}
	if cfg.Database.Pool.MinConns > cfg.Database.Pool.MaxConns {
		return fmt.Errorf("KODEX_FLEET_MANAGER_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.EventLogDatabase.MaxConns < 0 || cfg.EventLogDatabase.MinConns < 0 {
		return fmt.Errorf("KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS and MIN_CONNS are invalid")
	}
	if cfg.EventLogDatabase.MaxConns > 0 && cfg.EventLogDatabase.MinConns > cfg.EventLogDatabase.MaxConns {
		return fmt.Errorf("KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.Database.Retry.MaxAttempts <= 0 || cfg.Database.Retry.JitterRatio < 0 || cfg.Database.Retry.JitterRatio > 1 {
		return fmt.Errorf("KODEX_FLEET_MANAGER_DATABASE_CONNECT_RETRY_* settings are invalid")
	}
	if cfg.Database.Retry.Max < cfg.Database.Retry.Initial {
		return fmt.Errorf("KODEX_FLEET_MANAGER_DATABASE_CONNECT_RETRY_MAX_DELAY must be greater than or equal to initial delay")
	}
	return validatePositiveDurations(map[string]time.Duration{
		"KODEX_FLEET_MANAGER_DATABASE_MAX_CONN_LIFETIME":       cfg.Database.Pool.MaxConnLifetime,
		"KODEX_FLEET_MANAGER_DATABASE_MAX_CONN_IDLE_TIME":      cfg.Database.Pool.MaxConnIdleTime,
		"KODEX_FLEET_MANAGER_DATABASE_HEALTH_CHECK_PERIOD":     cfg.Database.Pool.HealthCheckPeriod,
		"KODEX_FLEET_MANAGER_DATABASE_PING_TIMEOUT":            cfg.Database.Pool.PingTimeout,
		"KODEX_FLEET_MANAGER_DATABASE_CONNECT_RETRY_MAX_DELAY": cfg.Database.Retry.Max,
	})
}

func (cfg Config) validateOutbox() error {
	if err := validatePositiveDurations(map[string]time.Duration{
		"KODEX_FLEET_MANAGER_OUTBOX_POLL_INTERVAL":       cfg.Outbox.PollInterval,
		"KODEX_FLEET_MANAGER_OUTBOX_LOCK_TTL":            cfg.Outbox.LockTTL,
		"KODEX_FLEET_MANAGER_OUTBOX_PUBLISH_TIMEOUT":     cfg.Outbox.PublishTimeout,
		"KODEX_FLEET_MANAGER_OUTBOX_RETRY_INITIAL_DELAY": cfg.Outbox.RetryInitialDelay,
		"KODEX_FLEET_MANAGER_OUTBOX_RETRY_MAX_DELAY":     cfg.Outbox.RetryMaxDelay,
	}); err != nil {
		return err
	}
	if cfg.Outbox.BatchSize <= 0 || cfg.Outbox.FailureMessageLimit <= 0 || cfg.Outbox.LeaseSafetyMargin < 0 {
		return fmt.Errorf("KODEX_FLEET_MANAGER_OUTBOX_* numeric settings are invalid")
	}
	if cfg.Outbox.RetryMaxDelay < cfg.Outbox.RetryInitialDelay {
		return fmt.Errorf("KODEX_FLEET_MANAGER_OUTBOX_RETRY_MAX_DELAY must be greater than or equal to initial delay")
	}
	if cfg.Outbox.PublishTimeout+cfg.Outbox.LeaseSafetyMargin >= cfg.Outbox.LockTTL {
		return fmt.Errorf("KODEX_FLEET_MANAGER_OUTBOX_PUBLISH_TIMEOUT plus safety margin must be less than lock ttl")
	}
	return cfg.validateOutboxPublisher()
}

func (cfg Config) validateOutboxPublisher() error {
	kind := strings.TrimSpace(cfg.Outbox.PublisherKind)
	switch kind {
	case outboxlib.PublisherKindDisabled, outboxlib.PublisherKindDiagnosticLogLossy, outboxlib.PublisherKindPostgresEventLog:
	default:
		return fmt.Errorf("KODEX_FLEET_MANAGER_OUTBOX_PUBLISHER_KIND must be disabled, diagnostic-log-lossy or postgres-event-log")
	}
	if cfg.Outbox.DispatchEnabled && kind == outboxlib.PublisherKindDisabled {
		return fmt.Errorf("KODEX_FLEET_MANAGER_OUTBOX_PUBLISHER_KIND must be configured when outbox dispatch is enabled")
	}
	if kind == outboxlib.PublisherKindDiagnosticLogLossy && !cfg.Outbox.AllowLossyPublisher {
		return fmt.Errorf("KODEX_FLEET_MANAGER_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER must be true for diagnostic-log-lossy publisher")
	}
	if kind == outboxlib.PublisherKindPostgresEventLog && strings.TrimSpace(cfg.Outbox.EventLogSource) == "" {
		return fmt.Errorf("KODEX_FLEET_MANAGER_OUTBOX_EVENT_LOG_SOURCE must be configured for postgres-event-log publisher")
	}
	if cfg.needsEventLogDatabase() && strings.TrimSpace(cfg.EventLogDatabase.DSN) == "" {
		return fmt.Errorf("KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_DSN is required for postgres-event-log publisher")
	}
	if cfg.needsEventLogDatabase() && cfg.EventLogDatabase.MaxConns < 1 {
		return fmt.Errorf("KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS must be greater than zero for postgres-event-log publisher")
	}
	return nil
}

func (cfg Config) validateAccess() error {
	if !cfg.Access.CheckEnabled {
		return nil
	}
	if strings.TrimSpace(cfg.Access.AccessManagerGRPCAddr) == "" {
		return fmt.Errorf("KODEX_FLEET_MANAGER_ACCESS_MANAGER_GRPC_ADDR is required when access checks are enabled")
	}
	if cfg.Access.CheckTimeout <= 0 {
		return fmt.Errorf("KODEX_FLEET_MANAGER_ACCESS_MANAGER_CHECK_TIMEOUT is invalid")
	}
	return nil
}

func (cfg Config) validateSecrets() error {
	if cfg.SecretResolver.MountedKubernetesMaxBytes <= 0 {
		return fmt.Errorf("KODEX_FLEET_MANAGER_SECRET_RESOLVER_MOUNTED_KUBERNETES_MAX_SECRET_BYTES is invalid")
	}
	if strings.TrimSpace(cfg.SecretResolver.VaultAddr) != "" && strings.TrimSpace(cfg.SecretResolver.VaultToken) == "" {
		return fmt.Errorf("KODEX_FLEET_MANAGER_SECRET_RESOLVER_VAULT_TOKEN is required when Vault address is configured")
	}
	return nil
}

func (cfg Config) validateConnectivity() error {
	return validatePositiveDurations(map[string]time.Duration{
		"KODEX_FLEET_MANAGER_CONNECTIVITY_CHECK_TIMEOUT": cfg.Connectivity.CheckTimeout,
	})
}

func (cfg Config) validateBootstrap() error {
	if !cfg.Bootstrap.SeedEnabled {
		return nil
	}
	if _, err := uuid.Parse(strings.TrimSpace(cfg.Bootstrap.FleetScopeID)); err != nil {
		return fmt.Errorf("KODEX_FLEET_MANAGER_BOOTSTRAP_FLEET_SCOPE_ID is invalid")
	}
	if _, err := uuid.Parse(strings.TrimSpace(cfg.Bootstrap.ClusterID)); err != nil {
		return fmt.Errorf("KODEX_FLEET_MANAGER_BOOTSTRAP_CLUSTER_ID is invalid")
	}
	if strings.TrimSpace(cfg.Bootstrap.ScopeKey) == "" || strings.TrimSpace(cfg.Bootstrap.ClusterKey) == "" {
		return fmt.Errorf("KODEX_FLEET_MANAGER_BOOTSTRAP_SCOPE_KEY and CLUSTER_KEY are required")
	}
	return nil
}

func validatePositiveDurations(values map[string]time.Duration) error {
	for name, value := range values {
		if value <= 0 {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	return nil
}

func (cfg Config) needsEventLogDatabase() bool {
	return cfg.Outbox.DispatchEnabled && strings.TrimSpace(cfg.Outbox.PublisherKind) == outboxlib.PublisherKindPostgresEventLog
}

// DatabasePoolSettings converts service config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.poolSettings(cfg.Database.DSN, cfg.Database.Pool.MaxConns, cfg.Database.Pool.MinConns))
}

// EventLogDatabasePoolSettings converts event-log env config to a separate pgxpool contract.
func (cfg Config) EventLogDatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(cfg.poolSettings(cfg.EventLogDatabase.DSN, cfg.EventLogDatabase.MaxConns, cfg.EventLogDatabase.MinConns))
}

func (cfg Config) poolSettings(dsn string, maxConns int32, minConns int32) postgreslib.PoolRuntimeSettings {
	return postgreslib.PoolRuntimeSettingsFromValues(
		dsn,
		maxConns,
		minConns,
		cfg.Database.Pool.MaxConnLifetime,
		cfg.Database.Pool.MaxConnIdleTime,
		cfg.Database.Pool.HealthCheckPeriod,
		cfg.Database.Pool.PingTimeout,
		cfg.Database.Retry.MaxAttempts,
		cfg.Database.Retry.Initial,
		cfg.Database.Retry.Max,
		cfg.Database.Retry.JitterRatio,
	)
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.ConfigFromRuntimeSettings(grpcserver.RuntimeSettings{
		MaxInFlight:          cfg.GRPC.MaxInFlight,
		MaxConcurrentStreams: cfg.GRPC.MaxConcurrentStreams,
		UnaryTimeout:         cfg.GRPC.UnaryTimeout,
		KeepaliveTime:        cfg.GRPC.KeepaliveTime,
		KeepaliveTimeout:     cfg.GRPC.KeepaliveTimeout,
		KeepaliveMinTime:     cfg.GRPC.KeepaliveMinTime,
		PermitWithoutStream:  cfg.GRPC.PermitWithoutStream,
		MaxRecvMessageBytes:  cfg.GRPC.MaxRecvMessageBytes,
		MaxSendMessageBytes:  cfg.GRPC.MaxSendMessageBytes,
		AuthRequired:         cfg.GRPC.AuthRequired,
	})
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxlib.Config {
	return outboxlib.Config{
		BatchSize:           cfg.Outbox.BatchSize,
		PollInterval:        cfg.Outbox.PollInterval,
		LockTTL:             cfg.Outbox.LockTTL,
		PublishTimeout:      cfg.Outbox.PublishTimeout,
		RetryInitialDelay:   cfg.Outbox.RetryInitialDelay,
		RetryMaxDelay:       cfg.Outbox.RetryMaxDelay,
		FailureMessageLimit: cfg.Outbox.FailureMessageLimit,
	}
}

// PlatformDefaultSeed converts bootstrap env config to the fleet domain seed.
func (cfg Config) PlatformDefaultSeed() (fleetservice.PlatformDefaultSeed, error) {
	scopeID, err := uuid.Parse(strings.TrimSpace(cfg.Bootstrap.FleetScopeID))
	if err != nil {
		return fleetservice.PlatformDefaultSeed{}, err
	}
	clusterID, err := uuid.Parse(strings.TrimSpace(cfg.Bootstrap.ClusterID))
	if err != nil {
		return fleetservice.PlatformDefaultSeed{}, err
	}
	return fleetservice.PlatformDefaultSeed{
		FleetScopeID:      scopeID,
		ClusterID:         clusterID,
		ScopeKey:          strings.TrimSpace(cfg.Bootstrap.ScopeKey),
		ScopeDisplayName:  strings.TrimSpace(cfg.Bootstrap.ScopeDisplayName),
		ClusterKey:        strings.TrimSpace(cfg.Bootstrap.ClusterKey),
		APIEndpointRef:    strings.TrimSpace(cfg.Bootstrap.APIEndpointRef),
		SecretStoreType:   strings.TrimSpace(cfg.Bootstrap.SecretStoreType),
		SecretStoreRef:    strings.TrimSpace(cfg.Bootstrap.SecretStoreRef),
		KubernetesVersion: strings.TrimSpace(cfg.Bootstrap.KubernetesVersion),
		Region:            strings.TrimSpace(cfg.Bootstrap.Region),
		CapacityClass:     strings.TrimSpace(cfg.Bootstrap.CapacityClass),
	}, nil
}
