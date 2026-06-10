package app

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
)

var codexSessionSchemaDigestPattern = regexp.MustCompile(`(?i)^sha256:[0-9a-f]{64}$`)

const codexSessionConfigForbiddenMarkers = "raw_provider_payload|provider_payload|prompt_body|transcript|workspace_path|kubeconfig|secret_value|token=|authorization|-----begin|bearer "

// Config contains process-level agent-manager server configuration.
type Config struct {
	DatabaseDSN                                    string        `env:"KODEX_AGENT_MANAGER_DATABASE_DSN,required,notEmpty"`
	DatabaseMaxConns                               int32         `env:"KODEX_AGENT_MANAGER_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns                               int32         `env:"KODEX_AGENT_MANAGER_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime                        time.Duration `env:"KODEX_AGENT_MANAGER_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime                        time.Duration `env:"KODEX_AGENT_MANAGER_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthPeriod                           time.Duration `env:"KODEX_AGENT_MANAGER_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout                            time.Duration `env:"KODEX_AGENT_MANAGER_DATABASE_PING_TIMEOUT" envDefault:"5s"`
	DatabaseRetryAttempts                          int           `env:"KODEX_AGENT_MANAGER_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS" envDefault:"6"`
	DatabaseRetryInitial                           time.Duration `env:"KODEX_AGENT_MANAGER_DATABASE_CONNECT_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	DatabaseRetryMax                               time.Duration `env:"KODEX_AGENT_MANAGER_DATABASE_CONNECT_RETRY_MAX_DELAY" envDefault:"5s"`
	DatabaseRetryJitterRatio                       float64       `env:"KODEX_AGENT_MANAGER_DATABASE_CONNECT_RETRY_JITTER_RATIO" envDefault:"0.2"`
	EventLogDatabaseDSN                            string        `env:"KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_DSN"`
	EventLogDatabaseMaxConns                       int32         `env:"KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS" envDefault:"4"`
	EventLogDatabaseMinConns                       int32         `env:"KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS" envDefault:"0"`
	HTTPAddr                                       string        `env:"KODEX_AGENT_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                                       string        `env:"KODEX_AGENT_MANAGER_GRPC_ADDR" envDefault:":9090"`
	GRPCAuthRequired                               bool          `env:"KODEX_AGENT_MANAGER_GRPC_AUTH_REQUIRED" envDefault:"true"`
	GRPCAuthToken                                  string        `env:"KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN"`
	GRPCMaxConcurrentStreams                       uint32        `env:"KODEX_AGENT_MANAGER_GRPC_MAX_CONCURRENT_STREAMS" envDefault:"128"`
	GRPCMaxInFlight                                int           `env:"KODEX_AGENT_MANAGER_GRPC_MAX_IN_FLIGHT" envDefault:"128"`
	GRPCMaxRecvMessageBytes                        int           `env:"KODEX_AGENT_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCMaxSendMessageBytes                        int           `env:"KODEX_AGENT_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCKeepaliveMinTime                           time.Duration `env:"KODEX_AGENT_MANAGER_GRPC_KEEPALIVE_MIN_TIME" envDefault:"30s"`
	GRPCKeepaliveTime                              time.Duration `env:"KODEX_AGENT_MANAGER_GRPC_KEEPALIVE_TIME" envDefault:"2m"`
	GRPCKeepaliveTimeout                           time.Duration `env:"KODEX_AGENT_MANAGER_GRPC_KEEPALIVE_TIMEOUT" envDefault:"20s"`
	GRPCPermitWithoutStream                        bool          `env:"KODEX_AGENT_MANAGER_GRPC_PERMIT_WITHOUT_STREAM" envDefault:"false"`
	GRPCUnaryTimeout                               time.Duration `env:"KODEX_AGENT_MANAGER_GRPC_UNARY_TIMEOUT" envDefault:"30s"`
	PackageHubEnabled                              bool          `env:"KODEX_AGENT_MANAGER_PACKAGE_HUB_ENABLED" envDefault:"true"`
	PackageHubGRPCAddr                             string        `env:"KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_ADDR" envDefault:"package-hub:9090"`
	PackageHubGRPCAuthToken                        string        `env:"KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_AUTH_TOKEN"`
	PackageHubReadTimeout                          time.Duration `env:"KODEX_AGENT_MANAGER_PACKAGE_HUB_READ_TIMEOUT" envDefault:"3s"`
	RuntimePreparationEnabled                      bool          `env:"KODEX_AGENT_MANAGER_RUNTIME_PREPARATION_ENABLED" envDefault:"false"`
	ProjectCatalogGRPCAddr                         string        `env:"KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_ADDR" envDefault:"project-catalog:9090"`
	ProjectCatalogGRPCAuthToken                    string        `env:"KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN"`
	ProjectCatalogReadTimeout                      time.Duration `env:"KODEX_AGENT_MANAGER_PROJECT_CATALOG_READ_TIMEOUT" envDefault:"3s"`
	RuntimeManagerGRPCAddr                         string        `env:"KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_ADDR" envDefault:"runtime-manager:9090"`
	RuntimeManagerGRPCAuthToken                    string        `env:"KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN"`
	RuntimeManagerPrepareTimeout                   time.Duration `env:"KODEX_AGENT_MANAGER_RUNTIME_MANAGER_PREPARE_TIMEOUT" envDefault:"10s"`
	RuntimeJobDispatchEnabled                      bool          `env:"KODEX_AGENT_MANAGER_RUNTIME_JOB_DISPATCH_ENABLED" envDefault:"false"`
	SelfDeployBuildDispatchEnabled                 bool          `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED" envDefault:"false"`
	RuntimeJobRunnerImageRef                       string        `env:"KODEX_AGENT_MANAGER_RUNTIME_JOB_RUNNER_IMAGE_REF"`
	CodexSessionResultSchemaRef                    string        `env:"KODEX_AGENT_MANAGER_CODEX_SESSION_RESULT_SCHEMA_REF"`
	CodexSessionResultSchemaDigest                 string        `env:"KODEX_AGENT_MANAGER_CODEX_SESSION_RESULT_SCHEMA_DIGEST"`
	CodexSessionHookEndpointRef                    string        `env:"KODEX_AGENT_MANAGER_CODEX_SESSION_HOOK_ENDPOINT_REF" envDefault:"hook://codex-hook-ingress/agent-runner"`
	CodexSessionTimeout                            time.Duration `env:"KODEX_AGENT_MANAGER_CODEX_SESSION_TIMEOUT" envDefault:"30m"`
	ProviderHubWriteEnabled                        bool          `env:"KODEX_AGENT_MANAGER_PROVIDER_HUB_WRITE_ENABLED" envDefault:"false"`
	ProviderHubGRPCAddr                            string        `env:"KODEX_AGENT_MANAGER_PROVIDER_HUB_GRPC_ADDR" envDefault:"provider-hub:9090"`
	ProviderHubGRPCAuthToken                       string        `env:"KODEX_AGENT_MANAGER_PROVIDER_HUB_GRPC_AUTH_TOKEN"`
	ProviderHubWriteTimeout                        time.Duration `env:"KODEX_AGENT_MANAGER_PROVIDER_HUB_WRITE_TIMEOUT" envDefault:"10s"`
	InteractionHubRequestEnabled                   bool          `env:"KODEX_AGENT_MANAGER_INTERACTION_HUB_REQUEST_ENABLED" envDefault:"false"`
	InteractionHubGRPCAddr                         string        `env:"KODEX_AGENT_MANAGER_INTERACTION_HUB_GRPC_ADDR" envDefault:"interaction-hub:9090"`
	InteractionHubGRPCAuthToken                    string        `env:"KODEX_AGENT_MANAGER_INTERACTION_HUB_GRPC_AUTH_TOKEN"`
	InteractionHubRequestTimeout                   time.Duration `env:"KODEX_AGENT_MANAGER_INTERACTION_HUB_REQUEST_TIMEOUT" envDefault:"10s"`
	SelfDeployGovernanceGateEnabled                bool          `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GOVERNANCE_GATE_ENABLED" envDefault:"false"`
	GovernanceManagerGRPCAddr                      string        `env:"KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_GRPC_ADDR" envDefault:"governance-manager:9090"`
	GovernanceManagerGRPCAuthToken                 string        `env:"KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN"`
	GovernanceManagerRequestTimeout                time.Duration `env:"KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_REQUEST_TIMEOUT" envDefault:"10s"`
	OutboxDispatchEnabled                          bool          `env:"KODEX_AGENT_MANAGER_OUTBOX_DISPATCH_ENABLED" envDefault:"true"`
	OutboxPublisherKind                            string        `env:"KODEX_AGENT_MANAGER_OUTBOX_PUBLISHER_KIND" envDefault:"postgres-event-log"`
	OutboxEventLogSource                           string        `env:"KODEX_AGENT_MANAGER_OUTBOX_EVENT_LOG_SOURCE" envDefault:"agent-manager"`
	OutboxAllowLossy                               bool          `env:"KODEX_AGENT_MANAGER_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER" envDefault:"false"`
	OutboxBatchSize                                int           `env:"KODEX_AGENT_MANAGER_OUTBOX_BATCH_SIZE" envDefault:"100"`
	OutboxPollInterval                             time.Duration `env:"KODEX_AGENT_MANAGER_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	OutboxLockTTL                                  time.Duration `env:"KODEX_AGENT_MANAGER_OUTBOX_LOCK_TTL" envDefault:"30s"`
	OutboxPublishTimeout                           time.Duration `env:"KODEX_AGENT_MANAGER_OUTBOX_PUBLISH_TIMEOUT" envDefault:"10s"`
	OutboxLeaseSafetyMargin                        time.Duration `env:"KODEX_AGENT_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN" envDefault:"5s"`
	OutboxRetryInitialDelay                        time.Duration `env:"KODEX_AGENT_MANAGER_OUTBOX_RETRY_INITIAL_DELAY" envDefault:"1s"`
	OutboxRetryMaxDelay                            time.Duration `env:"KODEX_AGENT_MANAGER_OUTBOX_RETRY_MAX_DELAY" envDefault:"1m"`
	OutboxFailureLimit                             int           `env:"KODEX_AGENT_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	InteractionResponseConsumerEnabled             bool          `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_ENABLED" envDefault:"true"`
	InteractionResponseConsumerName                string        `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_NAME" envDefault:"agent-manager.human-gate-response"`
	InteractionResponseConsumerLeaseOwner          string        `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_LEASE_OWNER"`
	InteractionResponseConsumerBatchSize           int           `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_BATCH_SIZE" envDefault:"50"`
	InteractionResponseConsumerPollInterval        time.Duration `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_POLL_INTERVAL" envDefault:"1s"`
	InteractionResponseConsumerLeaseTTL            time.Duration `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_LEASE_TTL" envDefault:"30s"`
	InteractionResponseConsumerHandlerTimeout      time.Duration `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_HANDLER_TIMEOUT" envDefault:"10s"`
	InteractionResponseConsumerRetryInitialDelay   time.Duration `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_RETRY_INITIAL_DELAY" envDefault:"1s"`
	InteractionResponseConsumerRetryMaxDelay       time.Duration `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_RETRY_MAX_DELAY" envDefault:"1m"`
	InteractionResponseConsumerFailureMessageLimit int           `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	InteractionResponseConsumerConcurrencyLimit    int           `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_CONCURRENCY_LIMIT" envDefault:"2"`
	InteractionResponseConsumerMaxAttempts         int           `env:"KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_MAX_ATTEMPTS" envDefault:"5"`
	SelfDeploySignalConsumerEnabled                bool          `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED" envDefault:"true"`
	SelfDeploySignalConsumerName                   string        `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_NAME" envDefault:"agent-manager.self-deploy-signal"`
	SelfDeploySignalConsumerLeaseOwner             string        `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_LEASE_OWNER"`
	SelfDeploySignalConsumerProjectID              string        `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID"`
	SelfDeploySignalConsumerRepositoryID           string        `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_REPOSITORY_ID"`
	SelfDeploySignalConsumerTargetBranch           string        `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_TARGET_BRANCH" envDefault:"main"`
	SelfDeploySignalConsumerBatchSize              int           `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_BATCH_SIZE" envDefault:"20"`
	SelfDeploySignalConsumerPollInterval           time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_POLL_INTERVAL" envDefault:"1s"`
	SelfDeploySignalConsumerLeaseTTL               time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_LEASE_TTL" envDefault:"30s"`
	SelfDeploySignalConsumerHandlerTimeout         time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_HANDLER_TIMEOUT" envDefault:"10s"`
	SelfDeploySignalConsumerRetryInitialDelay      time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_RETRY_INITIAL_DELAY" envDefault:"5s"`
	SelfDeploySignalConsumerRetryMaxDelay          time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_RETRY_MAX_DELAY" envDefault:"5m"`
	SelfDeploySignalConsumerFailureMessageLimit    int           `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	SelfDeploySignalConsumerConcurrencyLimit       int           `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_CONCURRENCY_LIMIT" envDefault:"1"`
	SelfDeploySignalConsumerMaxAttempts            int           `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_MAX_ATTEMPTS" envDefault:"24"`
	SelfDeployGateDecisionConsumerEnabled          bool          `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_ENABLED" envDefault:"false"`
	SelfDeployGateDecisionConsumerName             string        `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_NAME" envDefault:"agent-manager.self-deploy-gate-decision"`
	SelfDeployGateDecisionConsumerLeaseOwner       string        `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_LEASE_OWNER"`
	SelfDeployGateDecisionConsumerBatchSize        int           `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_BATCH_SIZE" envDefault:"20"`
	SelfDeployGateDecisionConsumerPollInterval     time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_POLL_INTERVAL" envDefault:"1s"`
	SelfDeployGateDecisionConsumerLeaseTTL         time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_LEASE_TTL" envDefault:"30s"`
	SelfDeployGateDecisionConsumerHandlerTimeout   time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_HANDLER_TIMEOUT" envDefault:"10s"`
	SelfDeployGateDecisionConsumerRetryInitial     time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_RETRY_INITIAL_DELAY" envDefault:"5s"`
	SelfDeployGateDecisionConsumerRetryMax         time.Duration `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_RETRY_MAX_DELAY" envDefault:"5m"`
	SelfDeployGateDecisionConsumerFailureLimit     int           `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	SelfDeployGateDecisionConsumerConcurrencyLimit int           `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_CONCURRENCY_LIMIT" envDefault:"1"`
	SelfDeployGateDecisionConsumerMaxAttempts      int           `env:"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_MAX_ATTEMPTS" envDefault:"24"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := parseEnvironmentConfig()
	if err != nil {
		return Config{}, err
	}
	return cfg, cfg.Validate()
}

func parseEnvironmentConfig() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse agent-manager config from environment: %w", err)
	}
	return cfg, nil
}

// Validate checks process settings before runtime construction.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_HTTP_ADDR is required")
	}
	if strings.TrimSpace(cfg.GRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_GRPC_ADDR is required")
	}
	if cfg.GRPCAuthRequired && strings.TrimSpace(cfg.GRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if cfg.PackageHubEnabled && strings.TrimSpace(cfg.PackageHubGRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_ADDR is required when package-hub integration is enabled")
	}
	if cfg.PackageHubEnabled && strings.TrimSpace(cfg.PackageHubGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_AUTH_TOKEN is required when package-hub integration is enabled")
	}
	if cfg.RuntimePreparationEnabled && strings.TrimSpace(cfg.ProjectCatalogGRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_ADDR is required when runtime preparation is enabled")
	}
	if cfg.RuntimePreparationEnabled && strings.TrimSpace(cfg.ProjectCatalogGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN is required when runtime preparation is enabled")
	}
	if cfg.needsProjectCatalogClient() && strings.TrimSpace(cfg.ProjectCatalogGRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_ADDR is required when project-catalog integration is enabled")
	}
	if cfg.needsProjectCatalogClient() && strings.TrimSpace(cfg.ProjectCatalogGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN is required when project-catalog integration is enabled")
	}
	if cfg.RuntimePreparationEnabled && strings.TrimSpace(cfg.RuntimeManagerGRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_ADDR is required when runtime preparation is enabled")
	}
	if cfg.RuntimePreparationEnabled && strings.TrimSpace(cfg.RuntimeManagerGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN is required when runtime preparation is enabled")
	}
	if cfg.RuntimeJobDispatchEnabled && !cfg.RuntimePreparationEnabled {
		return fmt.Errorf("KODEX_AGENT_MANAGER_RUNTIME_PREPARATION_ENABLED is required when runtime job dispatch is enabled")
	}
	if cfg.RuntimeJobDispatchEnabled && strings.TrimSpace(cfg.RuntimeJobRunnerImageRef) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_RUNTIME_JOB_RUNNER_IMAGE_REF is required when runtime job dispatch is enabled")
	}
	if cfg.SelfDeployBuildDispatchEnabled && strings.TrimSpace(cfg.RuntimeManagerGRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_ADDR is required when self-deploy build dispatch is enabled")
	}
	if cfg.SelfDeployBuildDispatchEnabled && strings.TrimSpace(cfg.RuntimeManagerGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN is required when self-deploy build dispatch is enabled")
	}
	if cfg.SelfDeployBuildDispatchEnabled && !cfg.SelfDeployGovernanceGateEnabled {
		return fmt.Errorf("KODEX_AGENT_MANAGER_SELF_DEPLOY_GOVERNANCE_GATE_ENABLED is required when self-deploy build dispatch is enabled")
	}
	if err := cfg.validateCodexSessionExecutionConfig(); err != nil {
		return err
	}
	if cfg.ProviderHubWriteEnabled && strings.TrimSpace(cfg.ProviderHubGRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_PROVIDER_HUB_GRPC_ADDR is required when provider-hub write integration is enabled")
	}
	if cfg.ProviderHubWriteEnabled && strings.TrimSpace(cfg.ProviderHubGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_PROVIDER_HUB_GRPC_AUTH_TOKEN is required when provider-hub write integration is enabled")
	}
	if cfg.InteractionHubRequestEnabled && strings.TrimSpace(cfg.InteractionHubGRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_INTERACTION_HUB_GRPC_ADDR is required when interaction-hub request integration is enabled")
	}
	if cfg.InteractionHubRequestEnabled && strings.TrimSpace(cfg.InteractionHubGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_INTERACTION_HUB_GRPC_AUTH_TOKEN is required when interaction-hub request integration is enabled")
	}
	if cfg.SelfDeployGovernanceGateEnabled && strings.TrimSpace(cfg.GovernanceManagerGRPCAddr) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_GRPC_ADDR is required when self-deploy governance gate integration is enabled")
	}
	if cfg.SelfDeployGovernanceGateEnabled && strings.TrimSpace(cfg.GovernanceManagerGRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN is required when self-deploy governance gate integration is enabled")
	}
	if err := cfg.GRPCServerConfig().Validate(); err != nil {
		return err
	}
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_AGENT_MANAGER_DATABASE_MAX_CONNS", valid: cfg.DatabaseMaxConns > 0},
		{name: "KODEX_AGENT_MANAGER_DATABASE_MIN_CONNS", valid: cfg.DatabaseMinConns >= 0},
		{name: "KODEX_AGENT_MANAGER_DATABASE_MAX_CONN_LIFETIME", valid: cfg.DatabaseMaxConnLifetime > 0},
		{name: "KODEX_AGENT_MANAGER_DATABASE_MAX_CONN_IDLE_TIME", valid: cfg.DatabaseMaxConnIdleTime > 0},
		{name: "KODEX_AGENT_MANAGER_DATABASE_HEALTH_CHECK_PERIOD", valid: cfg.DatabaseHealthPeriod > 0},
		{name: "KODEX_AGENT_MANAGER_DATABASE_PING_TIMEOUT", valid: cfg.DatabasePingTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS", valid: cfg.DatabaseRetryAttempts > 0},
		{name: "KODEX_AGENT_MANAGER_DATABASE_CONNECT_RETRY_INITIAL_DELAY", valid: cfg.DatabaseRetryInitial >= 0},
		{name: "KODEX_AGENT_MANAGER_DATABASE_CONNECT_RETRY_MAX_DELAY", valid: cfg.DatabaseRetryMax >= cfg.DatabaseRetryInitial},
		{name: "KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS", valid: cfg.EventLogDatabaseMaxConns >= 0},
		{name: "KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS", valid: cfg.EventLogDatabaseMinConns >= 0},
		{name: "KODEX_AGENT_MANAGER_PACKAGE_HUB_READ_TIMEOUT", valid: cfg.PackageHubReadTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_PROJECT_CATALOG_READ_TIMEOUT", valid: cfg.ProjectCatalogReadTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_RUNTIME_MANAGER_PREPARE_TIMEOUT", valid: cfg.RuntimeManagerPrepareTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_CODEX_SESSION_TIMEOUT", valid: cfg.CodexSessionTimeout > 0 && cfg.CodexSessionTimeout <= 24*time.Hour},
		{name: "KODEX_AGENT_MANAGER_PROVIDER_HUB_WRITE_TIMEOUT", valid: cfg.ProviderHubWriteTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_HUB_REQUEST_TIMEOUT", valid: cfg.InteractionHubRequestTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_REQUEST_TIMEOUT", valid: cfg.GovernanceManagerRequestTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_OUTBOX_BATCH_SIZE", valid: cfg.OutboxBatchSize > 0},
		{name: "KODEX_AGENT_MANAGER_OUTBOX_POLL_INTERVAL", valid: cfg.OutboxPollInterval > 0},
		{name: "KODEX_AGENT_MANAGER_OUTBOX_LOCK_TTL", valid: cfg.OutboxLockTTL > 0},
		{name: "KODEX_AGENT_MANAGER_OUTBOX_PUBLISH_TIMEOUT", valid: cfg.OutboxPublishTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN", valid: cfg.OutboxLeaseSafetyMargin >= 0},
		{name: "KODEX_AGENT_MANAGER_OUTBOX_RETRY_INITIAL_DELAY", valid: cfg.OutboxRetryInitialDelay > 0},
		{name: "KODEX_AGENT_MANAGER_OUTBOX_RETRY_MAX_DELAY", valid: cfg.OutboxRetryMaxDelay >= cfg.OutboxRetryInitialDelay},
		{name: "KODEX_AGENT_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT", valid: cfg.OutboxFailureLimit > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_BATCH_SIZE", valid: cfg.InteractionResponseConsumerBatchSize > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_POLL_INTERVAL", valid: cfg.InteractionResponseConsumerPollInterval > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_LEASE_TTL", valid: cfg.InteractionResponseConsumerLeaseTTL > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_HANDLER_TIMEOUT", valid: cfg.InteractionResponseConsumerHandlerTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_RETRY_INITIAL_DELAY", valid: cfg.InteractionResponseConsumerRetryInitialDelay > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_RETRY_MAX_DELAY", valid: cfg.InteractionResponseConsumerRetryMaxDelay >= cfg.InteractionResponseConsumerRetryInitialDelay},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_FAILURE_MESSAGE_LIMIT", valid: cfg.InteractionResponseConsumerFailureMessageLimit > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_CONCURRENCY_LIMIT", valid: cfg.InteractionResponseConsumerConcurrencyLimit > 0},
		{name: "KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_MAX_ATTEMPTS", valid: cfg.InteractionResponseConsumerMaxAttempts > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_BATCH_SIZE", valid: cfg.SelfDeploySignalConsumerBatchSize > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_POLL_INTERVAL", valid: cfg.SelfDeploySignalConsumerPollInterval > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_LEASE_TTL", valid: cfg.SelfDeploySignalConsumerLeaseTTL > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_HANDLER_TIMEOUT", valid: cfg.SelfDeploySignalConsumerHandlerTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_RETRY_INITIAL_DELAY", valid: cfg.SelfDeploySignalConsumerRetryInitialDelay > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_RETRY_MAX_DELAY", valid: cfg.SelfDeploySignalConsumerRetryMaxDelay >= cfg.SelfDeploySignalConsumerRetryInitialDelay},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_FAILURE_MESSAGE_LIMIT", valid: cfg.SelfDeploySignalConsumerFailureMessageLimit > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_CONCURRENCY_LIMIT", valid: cfg.SelfDeploySignalConsumerConcurrencyLimit > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_MAX_ATTEMPTS", valid: cfg.SelfDeploySignalConsumerMaxAttempts > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_BATCH_SIZE", valid: cfg.SelfDeployGateDecisionConsumerBatchSize > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_POLL_INTERVAL", valid: cfg.SelfDeployGateDecisionConsumerPollInterval > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_LEASE_TTL", valid: cfg.SelfDeployGateDecisionConsumerLeaseTTL > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_HANDLER_TIMEOUT", valid: cfg.SelfDeployGateDecisionConsumerHandlerTimeout > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_RETRY_INITIAL_DELAY", valid: cfg.SelfDeployGateDecisionConsumerRetryInitial > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_RETRY_MAX_DELAY", valid: cfg.SelfDeployGateDecisionConsumerRetryMax >= cfg.SelfDeployGateDecisionConsumerRetryInitial},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_FAILURE_MESSAGE_LIMIT", valid: cfg.SelfDeployGateDecisionConsumerFailureLimit > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_CONCURRENCY_LIMIT", valid: cfg.SelfDeployGateDecisionConsumerConcurrencyLimit > 0},
		{name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_MAX_ATTEMPTS", valid: cfg.SelfDeployGateDecisionConsumerMaxAttempts > 0},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return fmt.Errorf("KODEX_AGENT_MANAGER_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.DatabaseRetryJitterRatio < 0 || cfg.DatabaseRetryJitterRatio > 1 {
		return fmt.Errorf("KODEX_AGENT_MANAGER_DATABASE_CONNECT_RETRY_JITTER_RATIO must be between 0 and 1")
	}
	if cfg.EventLogDatabaseMaxConns > 0 && cfg.EventLogDatabaseMinConns > cfg.EventLogDatabaseMaxConns {
		return fmt.Errorf("KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	switch strings.TrimSpace(cfg.OutboxPublisherKind) {
	case outboxlib.PublisherKindDisabled, outboxlib.PublisherKindDiagnosticLogLossy, outboxlib.PublisherKindPostgresEventLog:
	default:
		return fmt.Errorf("KODEX_AGENT_MANAGER_OUTBOX_PUBLISHER_KIND must be disabled, diagnostic-log-lossy or postgres-event-log")
	}
	if cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDisabled {
		return fmt.Errorf("KODEX_AGENT_MANAGER_OUTBOX_PUBLISHER_KIND must be configured when outbox dispatch is enabled")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDiagnosticLogLossy && !cfg.OutboxAllowLossy {
		return fmt.Errorf("KODEX_AGENT_MANAGER_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER must be true for diagnostic-log-lossy publisher")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog && strings.TrimSpace(cfg.OutboxEventLogSource) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_OUTBOX_EVENT_LOG_SOURCE must be configured for postgres-event-log publisher")
	}
	if cfg.InteractionResponseConsumerEnabled && strings.TrimSpace(cfg.InteractionResponseConsumerName) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_NAME is required when interaction response consumer is enabled")
	}
	if cfg.SelfDeploySignalConsumerEnabled && strings.TrimSpace(cfg.SelfDeploySignalConsumerName) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_NAME is required when self-deploy signal consumer is enabled")
	}
	if cfg.SelfDeployGateDecisionConsumerEnabled && strings.TrimSpace(cfg.SelfDeployGateDecisionConsumerName) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_NAME is required when self-deploy gate decision consumer is enabled")
	}
	if cfg.SelfDeploySignalConsumerEnabled && strings.TrimSpace(cfg.SelfDeploySignalConsumerTargetBranch) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_TARGET_BRANCH is required when self-deploy signal consumer is enabled")
	}
	if cfg.SelfDeploySignalConsumerEnabled {
		projectID, err := uuid.Parse(strings.TrimSpace(cfg.SelfDeploySignalConsumerProjectID))
		if err != nil || projectID == uuid.Nil {
			return fmt.Errorf("KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID must be a non-empty uuid when self-deploy signal consumer is enabled")
		}
	}
	if cfg.needsEventLogDatabase() && strings.TrimSpace(cfg.EventLogDatabaseDSN) == "" {
		return fmt.Errorf("KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_DSN is required for event-log publisher or consumer")
	}
	if cfg.needsEventLogDatabase() && cfg.EventLogDatabaseMaxConns < 1 {
		return fmt.Errorf("KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS must be greater than zero for event-log publisher or consumer")
	}
	if cfg.OutboxPublishTimeout+cfg.OutboxLeaseSafetyMargin >= cfg.OutboxLockTTL {
		return fmt.Errorf("KODEX_AGENT_MANAGER_OUTBOX_PUBLISH_TIMEOUT plus safety margin must be less than KODEX_AGENT_MANAGER_OUTBOX_LOCK_TTL")
	}
	return nil
}

func (cfg Config) validateCodexSessionExecutionConfig() error {
	if !cfg.RuntimeJobDispatchEnabled {
		return nil
	}
	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "KODEX_AGENT_MANAGER_CODEX_SESSION_RESULT_SCHEMA_REF", value: cfg.CodexSessionResultSchemaRef},
		{name: "KODEX_AGENT_MANAGER_CODEX_SESSION_HOOK_ENDPOINT_REF", value: cfg.CodexSessionHookEndpointRef},
	} {
		if !safeCodexSessionConfigRef(item.value) {
			return fmt.Errorf("%s is invalid when runtime job dispatch is enabled", item.name)
		}
	}
	if !codexSessionSchemaDigestPattern.MatchString(strings.TrimSpace(cfg.CodexSessionResultSchemaDigest)) {
		return fmt.Errorf("KODEX_AGENT_MANAGER_CODEX_SESSION_RESULT_SCHEMA_DIGEST must be sha256:<64 hex> when runtime job dispatch is enabled")
	}
	return nil
}

func safeCodexSessionConfigRef(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 512 || !utf8.ValidString(trimmed) || strings.ContainsAny(trimmed, "\r\n\t{}") {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range strings.Split(codexSessionConfigForbiddenMarkers, "|") {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func (cfg Config) needsEventLogDatabase() bool {
	return cfg.InteractionResponseConsumerEnabled ||
		cfg.SelfDeploySignalConsumerEnabled ||
		cfg.SelfDeployGateDecisionConsumerEnabled ||
		(cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog)
}

func (cfg Config) needsProjectCatalogClient() bool {
	return cfg.RuntimePreparationEnabled || cfg.SelfDeploySignalConsumerEnabled || cfg.SelfDeployBuildDispatchEnabled
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
	settings := cfg.databaseTimingSettings()
	settings.DSN = dsn
	settings.MaxConns = maxConns
	settings.MinConns = minConns
	return settings
}

func (cfg Config) databaseTimingSettings() postgreslib.PoolRuntimeSettings {
	return postgreslib.PoolRuntimeSettings{
		MaxConnLifetime:          cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime:          cfg.DatabaseMaxConnIdleTime,
		HealthCheckPeriod:        cfg.DatabaseHealthPeriod,
		PingTimeout:              cfg.DatabasePingTimeout,
		ConnectRetryMaxAttempts:  cfg.DatabaseRetryAttempts,
		ConnectRetryInitialDelay: cfg.DatabaseRetryInitial,
		ConnectRetryMaxDelay:     cfg.DatabaseRetryMax,
		ConnectRetryJitterRatio:  cfg.DatabaseRetryJitterRatio,
	}
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.ConfigFromRuntimeValues(cfg.GRPCMaxInFlight, cfg.GRPCMaxConcurrentStreams, cfg.GRPCUnaryTimeout, cfg.GRPCKeepaliveTime, cfg.GRPCKeepaliveTimeout, cfg.GRPCKeepaliveMinTime, cfg.GRPCPermitWithoutStream, cfg.GRPCMaxRecvMessageBytes, cfg.GRPCMaxSendMessageBytes, cfg.GRPCAuthRequired)
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxlib.Config {
	return outboxlib.ConfigFromRuntimeValues(cfg.OutboxBatchSize, cfg.OutboxPollInterval, cfg.OutboxLockTTL, cfg.OutboxPublishTimeout, cfg.OutboxRetryInitialDelay, cfg.OutboxRetryMaxDelay, cfg.OutboxFailureLimit)
}

// InteractionResponseConsumerConfig converts env fields to the shared event consumer runtime.
func (cfg Config) InteractionResponseConsumerConfig() eventconsumer.Config {
	return eventConsumerConfig(cfg.interactionResponseConsumerRuntime())
}

// SelfDeploySignalConsumerConfig converts env fields to the shared event consumer runtime.
func (cfg Config) SelfDeploySignalConsumerConfig() eventconsumer.Config {
	return eventConsumerConfig(cfg.selfDeploySignalConsumerRuntime())
}

// SelfDeployGateDecisionConsumerConfig converts env fields to the shared event consumer runtime.
func (cfg Config) SelfDeployGateDecisionConsumerConfig() eventconsumer.Config {
	return eventConsumerConfig(cfg.selfDeployGateDecisionConsumerRuntime())
}

type eventConsumerRuntime struct {
	name                string
	leaseOwner          string
	batchSize           int
	pollInterval        time.Duration
	leaseTTL            time.Duration
	handlerTimeout      time.Duration
	retryInitialDelay   time.Duration
	retryMaxDelay       time.Duration
	failureMessageLimit int
	concurrencyLimit    int
	maxAttempts         int
}

func eventConsumerConfig(runtime eventConsumerRuntime) eventconsumer.Config {
	return eventconsumer.ConfigFromRuntimeValues(
		runtime.name,
		runtime.leaseOwner,
		runtime.batchSize,
		runtime.pollInterval,
		runtime.leaseTTL,
		runtime.handlerTimeout,
		runtime.retryInitialDelay,
		runtime.retryMaxDelay,
		runtime.failureMessageLimit,
		runtime.concurrencyLimit,
		runtime.maxAttempts,
	)
}

func normalizeEventConsumerRuntime(runtime eventConsumerRuntime, defaultLeaseOwner string) eventConsumerRuntime {
	trimmedLeaseOwner := strings.TrimSpace(runtime.leaseOwner)
	if trimmedLeaseOwner == "" {
		trimmedLeaseOwner = eventconsumer.DefaultLeaseOwner(defaultLeaseOwner)
	}
	runtime.leaseOwner = trimmedLeaseOwner
	return runtime
}

func (cfg Config) interactionResponseConsumerRuntime() eventConsumerRuntime {
	return eventConsumerRuntimeFromValues(
		cfg.InteractionResponseConsumerName,
		cfg.InteractionResponseConsumerLeaseOwner,
		cfg.InteractionResponseConsumerBatchSize,
		cfg.InteractionResponseConsumerPollInterval,
		cfg.InteractionResponseConsumerLeaseTTL,
		cfg.InteractionResponseConsumerHandlerTimeout,
		cfg.InteractionResponseConsumerRetryInitialDelay,
		cfg.InteractionResponseConsumerRetryMaxDelay,
		cfg.InteractionResponseConsumerFailureMessageLimit,
		cfg.InteractionResponseConsumerConcurrencyLimit,
		cfg.InteractionResponseConsumerMaxAttempts,
		"agent-manager-human-gate-response",
	)
}

func (cfg Config) selfDeploySignalConsumerRuntime() eventConsumerRuntime {
	return eventConsumerRuntimeFromValues(
		cfg.SelfDeploySignalConsumerName,
		cfg.SelfDeploySignalConsumerLeaseOwner,
		cfg.SelfDeploySignalConsumerBatchSize,
		cfg.SelfDeploySignalConsumerPollInterval,
		cfg.SelfDeploySignalConsumerLeaseTTL,
		cfg.SelfDeploySignalConsumerHandlerTimeout,
		cfg.SelfDeploySignalConsumerRetryInitialDelay,
		cfg.SelfDeploySignalConsumerRetryMaxDelay,
		cfg.SelfDeploySignalConsumerFailureMessageLimit,
		cfg.SelfDeploySignalConsumerConcurrencyLimit,
		cfg.SelfDeploySignalConsumerMaxAttempts,
		"agent-manager-self-deploy-signal",
	)
}

func (cfg Config) selfDeployGateDecisionConsumerRuntime() eventConsumerRuntime {
	return eventConsumerRuntimeFromValues(
		cfg.SelfDeployGateDecisionConsumerName,
		cfg.SelfDeployGateDecisionConsumerLeaseOwner,
		cfg.SelfDeployGateDecisionConsumerBatchSize,
		cfg.SelfDeployGateDecisionConsumerPollInterval,
		cfg.SelfDeployGateDecisionConsumerLeaseTTL,
		cfg.SelfDeployGateDecisionConsumerHandlerTimeout,
		cfg.SelfDeployGateDecisionConsumerRetryInitial,
		cfg.SelfDeployGateDecisionConsumerRetryMax,
		cfg.SelfDeployGateDecisionConsumerFailureLimit,
		cfg.SelfDeployGateDecisionConsumerConcurrencyLimit,
		cfg.SelfDeployGateDecisionConsumerMaxAttempts,
		"agent-manager-self-deploy-gate-decision",
	)
}

func eventConsumerRuntimeFromValues(name string, leaseOwner string, batchSize int, pollInterval time.Duration, leaseTTL time.Duration, handlerTimeout time.Duration, retryInitialDelay time.Duration, retryMaxDelay time.Duration, failureMessageLimit int, concurrencyLimit int, maxAttempts int, defaultLeaseOwner string) eventConsumerRuntime {
	return normalizeEventConsumerRuntime(eventConsumerRuntime{
		name:                name,
		leaseOwner:          leaseOwner,
		batchSize:           batchSize,
		pollInterval:        pollInterval,
		leaseTTL:            leaseTTL,
		handlerTimeout:      handlerTimeout,
		retryInitialDelay:   retryInitialDelay,
		retryMaxDelay:       retryMaxDelay,
		failureMessageLimit: failureMessageLimit,
		concurrencyLimit:    concurrencyLimit,
		maxAttempts:         maxAttempts,
	}, defaultLeaseOwner)
}
