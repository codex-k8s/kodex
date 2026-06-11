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
	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
)

// Config contains process-level runtime-manager server configuration.
type Config struct {
	HTTPAddr         string                        `env:"KODEX_RUNTIME_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr         string                        `env:"KODEX_RUNTIME_MANAGER_GRPC_ADDR" envDefault:":9090"`
	GRPC             RuntimeGRPCConfig             `envPrefix:"KODEX_RUNTIME_MANAGER_GRPC_"`
	Database         RuntimeDatabaseConfig         `envPrefix:"KODEX_RUNTIME_MANAGER_DATABASE_"`
	EventLogDatabase RuntimeEventLogDBConfig       `envPrefix:"KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_"`
	Outbox           RuntimeOutboxConfig           `envPrefix:"KODEX_RUNTIME_MANAGER_OUTBOX_"`
	Slot             RuntimeSlotConfig             `envPrefix:"KODEX_RUNTIME_MANAGER_SLOT_"`
	Access           RuntimeAccessConfig           `envPrefix:"KODEX_RUNTIME_MANAGER_ACCESS_"`
	Fleet            RuntimeFleetConfig            `envPrefix:"KODEX_RUNTIME_MANAGER_FLEET_"`
	SecretResolver   RuntimeSecretConfig           `envPrefix:"KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_"`
	KubernetesWorker RuntimeKubernetesWorkerConfig `envPrefix:"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_"`
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

// RuntimeSlotConfig contains slot lifecycle defaults.
type RuntimeSlotConfig struct {
	DefaultFleetScopeID string        `env:"DEFAULT_FLEET_SCOPE_ID" envDefault:"00000000-0000-0000-0000-000000000001"`
	DefaultClusterID    string        `env:"DEFAULT_CLUSTER_ID" envDefault:"00000000-0000-0000-0000-000000000002"`
	NamespacePrefix     string        `env:"NAMESPACE_PREFIX" envDefault:"kodex-rt"`
	DefaultLeaseTTL     time.Duration `env:"DEFAULT_LEASE_TTL" envDefault:"30m"`
}

// RuntimeAccessConfig contains access-manager authorization settings.
type RuntimeAccessConfig struct {
	CheckEnabled           bool          `env:"CHECK_ENABLED" envDefault:"true"`
	AccessManagerGRPCAddr  string        `env:"MANAGER_GRPC_ADDR" envDefault:"access-manager:9090"`
	AccessManagerAuthToken string        `env:"MANAGER_GRPC_AUTH_TOKEN"`
	CheckTimeout           time.Duration `env:"MANAGER_CHECK_TIMEOUT" envDefault:"3s"`
}

// RuntimeFleetConfig contains fleet-manager placement settings.
type RuntimeFleetConfig struct {
	FleetManagerGRPCAddr  string        `env:"MANAGER_GRPC_ADDR" envDefault:"fleet-manager:9090"`
	FleetManagerAuthToken string        `env:"MANAGER_GRPC_AUTH_TOKEN"`
	ResolveTimeout        time.Duration `env:"MANAGER_RESOLVE_TIMEOUT" envDefault:"5s"`
}

// RuntimeSecretConfig contains secretresolver settings for the Kubernetes executor.
type RuntimeSecretConfig struct {
	EnvEnabled                bool   `env:"ENV_ENABLED" envDefault:"true"`
	MountedKubernetesRoot     string `env:"MOUNTED_KUBERNETES_ROOT"`
	MountedKubernetesMaxBytes int64  `env:"MOUNTED_KUBERNETES_MAX_SECRET_BYTES" envDefault:"1048576"`
	VaultAddr                 string `env:"VAULT_ADDR"`
	VaultToken                string `env:"VAULT_TOKEN"`
	VaultNamespace            string `env:"VAULT_NAMESPACE"`
}

// RuntimeKubernetesWorkerConfig controls the explicitly enabled Kubernetes job executor.
type RuntimeKubernetesWorkerConfig struct {
	Enabled                  bool          `env:"ENABLED" envDefault:"false"`
	WorkerID                 string        `env:"WORKER_ID" envDefault:"runtime-manager-kubernetes-executor"`
	DefaultNamespace         string        `env:"DEFAULT_NAMESPACE"`
	DefaultServiceAccount    string        `env:"DEFAULT_SERVICE_ACCOUNT"`
	DeployServiceAccount     string        `env:"DEPLOY_SERVICE_ACCOUNT" envDefault:"runtime-deployer"`
	DefaultImage             string        `env:"DEFAULT_IMAGE"`
	ImagePullPolicy          string        `env:"IMAGE_PULL_POLICY" envDefault:"IfNotPresent"`
	PollInterval             time.Duration `env:"POLL_INTERVAL" envDefault:"5s"`
	ClaimLeaseTTL            time.Duration `env:"CLAIM_LEASE_TTL" envDefault:"5m"`
	JobTimeout               time.Duration `env:"JOB_TIMEOUT" envDefault:"2m"`
	KubernetesPollInterval   time.Duration `env:"KUBERNETES_POLL_INTERVAL" envDefault:"2s"`
	BackoffLimit             int32         `env:"BACKOFF_LIMIT" envDefault:"0"`
	TTLSecondsAfterFinished  int32         `env:"TTL_SECONDS_AFTER_FINISHED" envDefault:"300"`
	LogTailBytes             int64         `env:"LOG_TAIL_BYTES" envDefault:"16384"`
	AgentManagerGRPCAddr     string        `env:"AGENT_MANAGER_GRPC_ADDR" envDefault:"agent-manager:9090"`
	AgentManagerSecretName   string        `env:"AGENT_MANAGER_GRPC_AUTH_SECRET_NAME" envDefault:"kodex-platform-runtime"`
	AgentManagerSecretKey    string        `env:"AGENT_MANAGER_GRPC_AUTH_SECRET_KEY" envDefault:"KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN"`
	AgentManagerTimeout      time.Duration `env:"AGENT_MANAGER_REPORT_TIMEOUT" envDefault:"3s"`
	SourceAuthSecretName     string        `env:"SOURCE_AUTH_SECRET_NAME" envDefault:""`
	SourceAuthSecretKey      string        `env:"SOURCE_AUTH_SECRET_KEY" envDefault:""`
	BuildContextStorageSize  string        `env:"BUILD_CONTEXT_STORAGE_SIZE" envDefault:"2Gi"`
	BuildContextStorageClass string        `env:"BUILD_CONTEXT_STORAGE_CLASS" envDefault:""`
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
	if err := cfg.validateOutboxSettings(); err != nil {
		return err
	}
	if err := cfg.validateSlotSettings(); err != nil {
		return err
	}
	if err := cfg.validateAccessSettings(); err != nil {
		return err
	}
	if err := cfg.validateFleetSettings(); err != nil {
		return err
	}
	if err := cfg.validateSecretResolverSettings(); err != nil {
		return err
	}
	return cfg.validateKubernetesWorkerSettings()
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

func (cfg Config) validateSlotSettings() error {
	if _, err := parseRequiredUUID("KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_FLEET_SCOPE_ID", cfg.Slot.DefaultFleetScopeID); err != nil {
		return err
	}
	if _, err := parseRequiredUUID("KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_CLUSTER_ID", cfg.Slot.DefaultClusterID); err != nil {
		return err
	}
	if strings.Trim(strings.ToLower(cfg.Slot.NamespacePrefix), "-") == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_SLOT_NAMESPACE_PREFIX is invalid")
	}
	if cfg.Slot.DefaultLeaseTTL <= 0 {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_LEASE_TTL is invalid")
	}
	return nil
}

func (cfg Config) validateAccessSettings() error {
	if cfg.Access.CheckTimeout <= 0 {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_CHECK_TIMEOUT is invalid")
	}
	if cfg.Access.CheckEnabled && strings.TrimSpace(cfg.Access.AccessManagerGRPCAddr) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_ADDR is required when access checks are enabled")
	}
	if cfg.Access.CheckEnabled && strings.TrimSpace(cfg.Access.AccessManagerAuthToken) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN is required when access checks are enabled")
	}
	return nil
}

func (cfg Config) validateFleetSettings() error {
	if cfg.Fleet.ResolveTimeout <= 0 {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_FLEET_MANAGER_RESOLVE_TIMEOUT is invalid")
	}
	if strings.TrimSpace(cfg.Fleet.FleetManagerGRPCAddr) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_ADDR is required")
	}
	if strings.TrimSpace(cfg.Fleet.FleetManagerAuthToken) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_AUTH_TOKEN is required")
	}
	return nil
}

func (cfg Config) validateSecretResolverSettings() error {
	secrets := cfg.SecretResolver
	switch {
	case secrets.MountedKubernetesMaxBytes <= 0:
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_MOUNTED_KUBERNETES_MAX_SECRET_BYTES is invalid")
	case strings.TrimSpace(secrets.VaultAddr) == "":
		return nil
	case strings.TrimSpace(secrets.VaultToken) == "":
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_SECRET_RESOLVER_VAULT_TOKEN is required when Vault address is configured")
	default:
		return nil
	}
}

func (cfg Config) validateKubernetesWorkerSettings() error {
	worker := cfg.KubernetesWorker
	if worker.PollInterval <= 0 || worker.ClaimLeaseTTL <= 0 || worker.JobTimeout <= 0 || worker.KubernetesPollInterval <= 0 {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_* durations are invalid")
	}
	if worker.BackoffLimit < 0 || worker.TTLSecondsAfterFinished < 0 || worker.LogTailBytes <= 0 {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_* numeric settings are invalid")
	}
	if worker.ClaimLeaseTTL <= worker.JobTimeout {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_CLAIM_LEASE_TTL must be greater than JOB_TIMEOUT")
	}
	if !worker.Enabled {
		return nil
	}
	if worker.AgentManagerTimeout <= 0 {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_REPORT_TIMEOUT is invalid")
	}
	if strings.TrimSpace(worker.WorkerID) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_WORKER_ID is required when executor is enabled")
	}
	if strings.TrimSpace(worker.DefaultNamespace) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_NAMESPACE is required when executor is enabled")
	}
	if strings.TrimSpace(worker.DefaultImage) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_IMAGE is required when executor is enabled")
	}
	if strings.TrimSpace(worker.DeployServiceAccount) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEPLOY_SERVICE_ACCOUNT is required when executor is enabled")
	}
	if strings.TrimSpace(worker.AgentManagerGRPCAddr) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_ADDR is required when executor is enabled")
	}
	if strings.TrimSpace(worker.AgentManagerSecretName) == "" || strings.TrimSpace(worker.AgentManagerSecretKey) == "" {
		return fmt.Errorf("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_* is required when executor is enabled")
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

// SlotServiceConfig converts process config to the runtime domain service config.
func (cfg Config) SlotServiceConfig() (runtimeservice.Config, error) {
	fleetScopeID, err := parseRequiredUUID("KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_FLEET_SCOPE_ID", cfg.Slot.DefaultFleetScopeID)
	if err != nil {
		return runtimeservice.Config{}, err
	}
	clusterID, err := parseRequiredUUID("KODEX_RUNTIME_MANAGER_SLOT_DEFAULT_CLUSTER_ID", cfg.Slot.DefaultClusterID)
	if err != nil {
		return runtimeservice.Config{}, err
	}
	return runtimeservice.Config{
		DefaultFleetScopeID: fleetScopeID,
		DefaultClusterID:    clusterID,
		NamespacePrefix:     strings.Trim(strings.ToLower(cfg.Slot.NamespacePrefix), "-"),
		DefaultLeaseTTL:     cfg.Slot.DefaultLeaseTTL,
	}, nil
}

func newSecretResolver(cfg RuntimeSecretConfig) (secretresolver.Resolver, error) {
	backends := make(map[string]secretresolver.Backend)
	if cfg.EnvEnabled {
		backends[secretresolver.StoreTypeEnv] = secretresolver.NewEnvBackend()
	}
	if err := addRuntimeMountedKubernetesSecrets(backends, cfg); err != nil {
		return nil, err
	}
	if err := addRuntimeVaultSecrets(backends, cfg); err != nil {
		return nil, err
	}
	return secretresolver.NewMux(backends)
}

func addRuntimeMountedKubernetesSecrets(backends map[string]secretresolver.Backend, cfg RuntimeSecretConfig) error {
	root := strings.TrimSpace(cfg.MountedKubernetesRoot)
	if root == "" {
		return nil
	}
	backend, err := secretresolver.NewMountedKubernetesBackend(secretresolver.MountedKubernetesBackendConfig{
		Root:           root,
		MaxSecretBytes: cfg.MountedKubernetesMaxBytes,
	})
	if err != nil {
		return err
	}
	backends[secretresolver.StoreTypeKubernetesMountedSecret] = backend
	return nil
}

func addRuntimeVaultSecrets(backends map[string]secretresolver.Backend, cfg RuntimeSecretConfig) error {
	addr := strings.TrimSpace(cfg.VaultAddr)
	if addr == "" {
		return nil
	}
	backend, err := secretresolver.NewVaultBackendFromClientConfig(secretresolver.VaultClientConfig{
		Addr:      addr,
		Token:     strings.TrimSpace(cfg.VaultToken),
		Namespace: strings.TrimSpace(cfg.VaultNamespace),
	})
	if err != nil {
		return err
	}
	backends[secretresolver.StoreTypeVault] = backend
	return nil
}

func parseRequiredUUID(name string, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%s is invalid", name)
	}
	return id, nil
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.ConfigFromRuntimeValues(cfg.GRPC.MaxInFlight, cfg.GRPC.MaxConcurrentStreams, cfg.GRPC.UnaryTimeout, cfg.GRPC.KeepaliveTime, cfg.GRPC.KeepaliveTimeout, cfg.GRPC.KeepaliveMinTime, cfg.GRPC.PermitWithoutStream, cfg.GRPC.MaxRecvMessageBytes, cfg.GRPC.MaxSendMessageBytes, cfg.GRPC.AuthRequired)
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxlib.Config {
	return outboxlib.ConfigFromRuntimeValues(cfg.Outbox.BatchSize, cfg.Outbox.PollInterval, cfg.Outbox.LockTTL, cfg.Outbox.PublishTimeout, cfg.Outbox.RetryInitialDelay, cfg.Outbox.RetryMaxDelay, cfg.Outbox.FailureMessageLimit)
}
