// Package app contains governance-manager process composition and lifecycle.
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

// Config contains process-level governance-manager server configuration.
type Config struct {
	DatabaseDSN                                     string        `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_DSN,required,notEmpty"`
	DatabaseMaxConns                                int32         `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns                                int32         `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime                         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime                         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthPeriod                            time.Duration `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout                             time.Duration `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_PING_TIMEOUT" envDefault:"5s"`
	DatabaseRetryAttempts                           int           `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS" envDefault:"6"`
	DatabaseRetryInitial                            time.Duration `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_CONNECT_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	DatabaseRetryMax                                time.Duration `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_CONNECT_RETRY_MAX_DELAY" envDefault:"5s"`
	DatabaseRetryJitterRatio                        float64       `env:"KODEX_GOVERNANCE_MANAGER_DATABASE_CONNECT_RETRY_JITTER_RATIO" envDefault:"0.2"`
	EventLogDatabaseDSN                             string        `env:"KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN"`
	EventLogDatabaseMaxConns                        int32         `env:"KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS" envDefault:"4"`
	EventLogDatabaseMinConns                        int32         `env:"KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS" envDefault:"0"`
	HTTPAddr                                        string        `env:"KODEX_GOVERNANCE_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                                        string        `env:"KODEX_GOVERNANCE_MANAGER_GRPC_ADDR" envDefault:":9090"`
	GRPCAuthRequired                                bool          `env:"KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_REQUIRED" envDefault:"true"`
	GRPCAuthToken                                   string        `env:"KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN"`
	GRPCMaxConcurrentStreams                        uint32        `env:"KODEX_GOVERNANCE_MANAGER_GRPC_MAX_CONCURRENT_STREAMS" envDefault:"128"`
	GRPCMaxInFlight                                 int           `env:"KODEX_GOVERNANCE_MANAGER_GRPC_MAX_IN_FLIGHT" envDefault:"128"`
	GRPCMaxRecvMessageBytes                         int           `env:"KODEX_GOVERNANCE_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCMaxSendMessageBytes                         int           `env:"KODEX_GOVERNANCE_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCKeepaliveMinTime                            time.Duration `env:"KODEX_GOVERNANCE_MANAGER_GRPC_KEEPALIVE_MIN_TIME" envDefault:"30s"`
	GRPCKeepaliveTime                               time.Duration `env:"KODEX_GOVERNANCE_MANAGER_GRPC_KEEPALIVE_TIME" envDefault:"2m"`
	GRPCKeepaliveTimeout                            time.Duration `env:"KODEX_GOVERNANCE_MANAGER_GRPC_KEEPALIVE_TIMEOUT" envDefault:"20s"`
	GRPCPermitWithoutStream                         bool          `env:"KODEX_GOVERNANCE_MANAGER_GRPC_PERMIT_WITHOUT_STREAM" envDefault:"false"`
	GRPCUnaryTimeout                                time.Duration `env:"KODEX_GOVERNANCE_MANAGER_GRPC_UNARY_TIMEOUT" envDefault:"30s"`
	AccessCheckEnabled                              bool          `env:"KODEX_GOVERNANCE_MANAGER_ACCESS_CHECK_ENABLED" envDefault:"true"`
	AccessManagerGRPCAddr                           string        `env:"KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_ADDR" envDefault:"access-manager:9090"`
	AccessManagerGRPCAuthToken                      string        `env:"KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN"`
	AccessManagerCheckTimeout                       time.Duration `env:"KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_CHECK_TIMEOUT" envDefault:"3s"`
	OutboxDispatchEnabled                           bool          `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_DISPATCH_ENABLED" envDefault:"true"`
	OutboxPublisherKind                             string        `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_PUBLISHER_KIND" envDefault:"postgres-event-log"`
	OutboxEventLogSource                            string        `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_EVENT_LOG_SOURCE" envDefault:"governance-manager"`
	OutboxAllowLossy                                bool          `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER" envDefault:"false"`
	OutboxBatchSize                                 int           `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_BATCH_SIZE" envDefault:"100"`
	OutboxPollInterval                              time.Duration `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_POLL_INTERVAL" envDefault:"1s"`
	OutboxLockTTL                                   time.Duration `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_LOCK_TTL" envDefault:"30s"`
	OutboxPublishTimeout                            time.Duration `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_PUBLISH_TIMEOUT" envDefault:"10s"`
	OutboxLeaseSafetyMargin                         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN" envDefault:"5s"`
	OutboxRetryInitialDelay                         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_RETRY_INITIAL_DELAY" envDefault:"1s"`
	OutboxRetryMaxDelay                             time.Duration `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_RETRY_MAX_DELAY" envDefault:"1m"`
	OutboxFailureLimit                              int           `env:"KODEX_GOVERNANCE_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	ProviderReviewSignalConsumerEnabled             bool          `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_ENABLED" envDefault:"false"`
	ProviderReviewSignalConsumerName                string        `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_NAME" envDefault:"governance-manager.provider-review-signal"`
	ProviderReviewSignalConsumerLeaseOwner          string        `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_LEASE_OWNER"`
	ProviderReviewSignalConsumerBatchSize           int           `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_BATCH_SIZE" envDefault:"50"`
	ProviderReviewSignalConsumerPollInterval        time.Duration `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_POLL_INTERVAL" envDefault:"1s"`
	ProviderReviewSignalConsumerLeaseTTL            time.Duration `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_LEASE_TTL" envDefault:"30s"`
	ProviderReviewSignalConsumerHandlerTimeout      time.Duration `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_HANDLER_TIMEOUT" envDefault:"10s"`
	ProviderReviewSignalConsumerRetryInitialDelay   time.Duration `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_RETRY_INITIAL_DELAY" envDefault:"1s"`
	ProviderReviewSignalConsumerRetryMaxDelay       time.Duration `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_RETRY_MAX_DELAY" envDefault:"1m"`
	ProviderReviewSignalConsumerFailureMessageLimit int           `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	ProviderReviewSignalConsumerConcurrencyLimit    int           `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_CONCURRENCY_LIMIT" envDefault:"2"`
	ProviderReviewSignalConsumerMaxAttempts         int           `env:"KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_MAX_ATTEMPTS" envDefault:"5"`
	InteractionGateDecisionConsumerEnabled          bool          `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_ENABLED" envDefault:"false"`
	InteractionGateDecisionConsumerName             string        `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_NAME" envDefault:"governance-manager.interaction-gate-decision"`
	InteractionGateDecisionConsumerLeaseOwner       string        `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_LEASE_OWNER"`
	InteractionGateDecisionConsumerBatchSize        int           `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_BATCH_SIZE" envDefault:"50"`
	InteractionGateDecisionConsumerPollInterval     time.Duration `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_POLL_INTERVAL" envDefault:"1s"`
	InteractionGateDecisionConsumerLeaseTTL         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_LEASE_TTL" envDefault:"30s"`
	InteractionGateDecisionConsumerHandlerTimeout   time.Duration `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_HANDLER_TIMEOUT" envDefault:"10s"`
	InteractionGateDecisionConsumerRetryInitial     time.Duration `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_RETRY_INITIAL_DELAY" envDefault:"1s"`
	InteractionGateDecisionConsumerRetryMax         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_RETRY_MAX_DELAY" envDefault:"1m"`
	InteractionGateDecisionConsumerFailureLimit     int           `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	InteractionGateDecisionConsumerConcurrencyLimit int           `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_CONCURRENCY_LIMIT" envDefault:"2"`
	InteractionGateDecisionConsumerMaxAttempts      int           `env:"KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_MAX_ATTEMPTS" envDefault:"5"`
	AgentAcceptanceEvidenceConsumerEnabled          bool          `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_ENABLED" envDefault:"false"`
	AgentAcceptanceEvidenceConsumerName             string        `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_NAME" envDefault:"governance-manager.agent-acceptance-evidence"`
	AgentAcceptanceEvidenceConsumerLeaseOwner       string        `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_LEASE_OWNER"`
	AgentAcceptanceEvidenceConsumerBatchSize        int           `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_BATCH_SIZE" envDefault:"50"`
	AgentAcceptanceEvidenceConsumerPollInterval     time.Duration `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_POLL_INTERVAL" envDefault:"1s"`
	AgentAcceptanceEvidenceConsumerLeaseTTL         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_LEASE_TTL" envDefault:"30s"`
	AgentAcceptanceEvidenceConsumerHandlerTimeout   time.Duration `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_HANDLER_TIMEOUT" envDefault:"10s"`
	AgentAcceptanceEvidenceConsumerRetryInitial     time.Duration `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_RETRY_INITIAL_DELAY" envDefault:"1s"`
	AgentAcceptanceEvidenceConsumerRetryMax         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_RETRY_MAX_DELAY" envDefault:"1m"`
	AgentAcceptanceEvidenceConsumerFailureLimit     int           `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_FAILURE_MESSAGE_LIMIT" envDefault:"512"`
	AgentAcceptanceEvidenceConsumerConcurrencyLimit int           `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_CONCURRENCY_LIMIT" envDefault:"2"`
	AgentAcceptanceEvidenceConsumerMaxAttempts      int           `env:"KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_MAX_ATTEMPTS" envDefault:"5"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load governance-manager config: %w", err)
	}
	return cfg, nil
}

// Validate checks process settings before runtime construction.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_HTTP_ADDR is required")
	}
	if strings.TrimSpace(cfg.GRPCAddr) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_GRPC_ADDR is required")
	}
	if cfg.GRPCAuthRequired && strings.TrimSpace(cfg.GRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if err := cfg.GRPCServerConfig().Validate(); err != nil {
		return err
	}
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_MAX_CONNS", valid: cfg.DatabaseMaxConns > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_MIN_CONNS", valid: cfg.DatabaseMinConns >= 0},
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_MAX_CONN_LIFETIME", valid: cfg.DatabaseMaxConnLifetime > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_MAX_CONN_IDLE_TIME", valid: cfg.DatabaseMaxConnIdleTime > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_HEALTH_CHECK_PERIOD", valid: cfg.DatabaseHealthPeriod > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_PING_TIMEOUT", valid: cfg.DatabasePingTimeout > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS", valid: cfg.DatabaseRetryAttempts > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_CONNECT_RETRY_INITIAL_DELAY", valid: cfg.DatabaseRetryInitial >= 0},
		{name: "KODEX_GOVERNANCE_MANAGER_DATABASE_CONNECT_RETRY_MAX_DELAY", valid: cfg.DatabaseRetryMax >= cfg.DatabaseRetryInitial},
		{name: "KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_CHECK_TIMEOUT", valid: cfg.AccessManagerCheckTimeout > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS", valid: cfg.EventLogDatabaseMaxConns >= 0},
		{name: "KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS", valid: cfg.EventLogDatabaseMinConns >= 0},
		{name: "KODEX_GOVERNANCE_MANAGER_OUTBOX_BATCH_SIZE", valid: cfg.OutboxBatchSize > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_OUTBOX_POLL_INTERVAL", valid: cfg.OutboxPollInterval > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_OUTBOX_LOCK_TTL", valid: cfg.OutboxLockTTL > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_OUTBOX_PUBLISH_TIMEOUT", valid: cfg.OutboxPublishTimeout > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_OUTBOX_LEASE_SAFETY_MARGIN", valid: cfg.OutboxLeaseSafetyMargin >= 0},
		{name: "KODEX_GOVERNANCE_MANAGER_OUTBOX_RETRY_INITIAL_DELAY", valid: cfg.OutboxRetryInitialDelay > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_OUTBOX_RETRY_MAX_DELAY", valid: cfg.OutboxRetryMaxDelay >= cfg.OutboxRetryInitialDelay},
		{name: "KODEX_GOVERNANCE_MANAGER_OUTBOX_FAILURE_MESSAGE_LIMIT", valid: cfg.OutboxFailureLimit > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_BATCH_SIZE", valid: cfg.ProviderReviewSignalConsumerBatchSize > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_POLL_INTERVAL", valid: cfg.ProviderReviewSignalConsumerPollInterval > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_LEASE_TTL", valid: cfg.ProviderReviewSignalConsumerLeaseTTL > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_HANDLER_TIMEOUT", valid: cfg.ProviderReviewSignalConsumerHandlerTimeout > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_RETRY_INITIAL_DELAY", valid: cfg.ProviderReviewSignalConsumerRetryInitialDelay > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_RETRY_MAX_DELAY", valid: cfg.ProviderReviewSignalConsumerRetryMaxDelay >= cfg.ProviderReviewSignalConsumerRetryInitialDelay},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_FAILURE_MESSAGE_LIMIT", valid: cfg.ProviderReviewSignalConsumerFailureMessageLimit > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_CONCURRENCY_LIMIT", valid: cfg.ProviderReviewSignalConsumerConcurrencyLimit > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_MAX_ATTEMPTS", valid: cfg.ProviderReviewSignalConsumerMaxAttempts > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_BATCH_SIZE", valid: cfg.InteractionGateDecisionConsumerBatchSize > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_POLL_INTERVAL", valid: cfg.InteractionGateDecisionConsumerPollInterval > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_LEASE_TTL", valid: cfg.InteractionGateDecisionConsumerLeaseTTL > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_HANDLER_TIMEOUT", valid: cfg.InteractionGateDecisionConsumerHandlerTimeout > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_RETRY_INITIAL_DELAY", valid: cfg.InteractionGateDecisionConsumerRetryInitial > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_RETRY_MAX_DELAY", valid: cfg.InteractionGateDecisionConsumerRetryMax >= cfg.InteractionGateDecisionConsumerRetryInitial},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_FAILURE_MESSAGE_LIMIT", valid: cfg.InteractionGateDecisionConsumerFailureLimit > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_CONCURRENCY_LIMIT", valid: cfg.InteractionGateDecisionConsumerConcurrencyLimit > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_MAX_ATTEMPTS", valid: cfg.InteractionGateDecisionConsumerMaxAttempts > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_BATCH_SIZE", valid: cfg.AgentAcceptanceEvidenceConsumerBatchSize > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_POLL_INTERVAL", valid: cfg.AgentAcceptanceEvidenceConsumerPollInterval > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_LEASE_TTL", valid: cfg.AgentAcceptanceEvidenceConsumerLeaseTTL > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_HANDLER_TIMEOUT", valid: cfg.AgentAcceptanceEvidenceConsumerHandlerTimeout > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_RETRY_INITIAL_DELAY", valid: cfg.AgentAcceptanceEvidenceConsumerRetryInitial > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_RETRY_MAX_DELAY", valid: cfg.AgentAcceptanceEvidenceConsumerRetryMax >= cfg.AgentAcceptanceEvidenceConsumerRetryInitial},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_FAILURE_MESSAGE_LIMIT", valid: cfg.AgentAcceptanceEvidenceConsumerFailureLimit > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_CONCURRENCY_LIMIT", valid: cfg.AgentAcceptanceEvidenceConsumerConcurrencyLimit > 0},
		{name: "KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_MAX_ATTEMPTS", valid: cfg.AgentAcceptanceEvidenceConsumerMaxAttempts > 0},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.DatabaseRetryJitterRatio < 0 || cfg.DatabaseRetryJitterRatio > 1 {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_DATABASE_CONNECT_RETRY_JITTER_RATIO must be between 0 and 1")
	}
	if cfg.AccessCheckEnabled {
		if strings.TrimSpace(cfg.AccessManagerGRPCAddr) == "" {
			return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_ADDR is required when access checks are enabled")
		}
		if strings.TrimSpace(cfg.AccessManagerGRPCAuthToken) == "" {
			return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN is required when access checks are enabled")
		}
	}
	if cfg.EventLogDatabaseMaxConns > 0 && cfg.EventLogDatabaseMinConns > cfg.EventLogDatabaseMaxConns {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	switch strings.TrimSpace(cfg.OutboxPublisherKind) {
	case outboxlib.PublisherKindDisabled, outboxlib.PublisherKindDiagnosticLogLossy, outboxlib.PublisherKindPostgresEventLog:
	default:
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_OUTBOX_PUBLISHER_KIND must be disabled, diagnostic-log-lossy or postgres-event-log")
	}
	if cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDisabled {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_OUTBOX_PUBLISHER_KIND must be configured when outbox dispatch is enabled")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindDiagnosticLogLossy && !cfg.OutboxAllowLossy {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_OUTBOX_ALLOW_LOSSY_DIAGNOSTIC_PUBLISHER must be true for diagnostic-log-lossy publisher")
	}
	if strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog && strings.TrimSpace(cfg.OutboxEventLogSource) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_OUTBOX_EVENT_LOG_SOURCE must be configured for postgres-event-log publisher")
	}
	if cfg.ProviderReviewSignalConsumerEnabled && strings.TrimSpace(cfg.ProviderReviewSignalConsumerName) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_PROVIDER_REVIEW_SIGNAL_CONSUMER_NAME is required when provider review signal consumer is enabled")
	}
	if cfg.InteractionGateDecisionConsumerEnabled && strings.TrimSpace(cfg.InteractionGateDecisionConsumerName) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_INTERACTION_GATE_DECISION_CONSUMER_NAME is required when interaction gate decision consumer is enabled")
	}
	if cfg.AgentAcceptanceEvidenceConsumerEnabled && strings.TrimSpace(cfg.AgentAcceptanceEvidenceConsumerName) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_AGENT_ACCEPTANCE_EVIDENCE_CONSUMER_NAME is required when agent acceptance evidence consumer is enabled")
	}
	if cfg.needsEventLogDatabase() && strings.TrimSpace(cfg.EventLogDatabaseDSN) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN is required for event-log publisher or consumer")
	}
	if cfg.needsEventLogDatabase() && cfg.EventLogDatabaseMaxConns < 1 {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_MAX_CONNS must be greater than zero for event-log publisher or consumer")
	}
	if cfg.OutboxPublishTimeout+cfg.OutboxLeaseSafetyMargin >= cfg.OutboxLockTTL {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_OUTBOX_PUBLISH_TIMEOUT plus safety margin must be less than KODEX_GOVERNANCE_MANAGER_OUTBOX_LOCK_TTL")
	}
	return nil
}

func (cfg Config) needsEventLogDatabase() bool {
	return cfg.ProviderReviewSignalConsumerEnabled ||
		cfg.InteractionGateDecisionConsumerEnabled ||
		cfg.AgentAcceptanceEvidenceConsumerEnabled ||
		(cfg.OutboxDispatchEnabled && strings.TrimSpace(cfg.OutboxPublisherKind) == outboxlib.PublisherKindPostgresEventLog)
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
	settings := postgreslib.PoolRuntimeSettingsFromValues(dsn, maxConns, minConns, cfg.DatabaseMaxConnLifetime, cfg.DatabaseMaxConnIdleTime, cfg.DatabaseHealthPeriod, cfg.DatabasePingTimeout, cfg.DatabaseRetryAttempts, cfg.DatabaseRetryInitial, cfg.DatabaseRetryMax, cfg.DatabaseRetryJitterRatio)
	settings.PingTimeout = cfg.DatabasePingTimeout
	return settings
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.ConfigFromRuntimeValues(cfg.GRPCMaxInFlight, cfg.GRPCMaxConcurrentStreams, cfg.GRPCUnaryTimeout, cfg.GRPCKeepaliveTime, cfg.GRPCKeepaliveTimeout, cfg.GRPCKeepaliveMinTime, cfg.GRPCPermitWithoutStream, cfg.GRPCMaxRecvMessageBytes, cfg.GRPCMaxSendMessageBytes, cfg.GRPCAuthRequired)
}

// OutboxDispatcherConfig converts service env config to the outbox delivery worker contract.
func (cfg Config) OutboxDispatcherConfig() outboxlib.Config {
	return outboxlib.ConfigFromRuntimeValues(cfg.OutboxBatchSize, cfg.OutboxPollInterval, cfg.OutboxLockTTL, cfg.OutboxPublishTimeout, cfg.OutboxRetryInitialDelay, cfg.OutboxRetryMaxDelay, cfg.OutboxFailureLimit)
}

// ProviderReviewSignalConsumerConfig converts env fields to the shared event consumer runtime.
func (cfg Config) ProviderReviewSignalConsumerConfig() eventconsumer.Config {
	return cfg.eventConsumerConfig(consumerKindProviderReviewSignal)
}

func (cfg Config) providerReviewSignalConsumerLeaseOwner() string {
	leaseOwner := strings.TrimSpace(cfg.ProviderReviewSignalConsumerLeaseOwner)
	if leaseOwner == "" {
		return eventconsumer.DefaultLeaseOwner("governance-provider-review-signal")
	}
	return leaseOwner
}

// InteractionGateDecisionConsumerConfig converts env fields to the shared event consumer runtime.
func (cfg Config) InteractionGateDecisionConsumerConfig() eventconsumer.Config {
	return cfg.eventConsumerConfig(consumerKindInteractionGateDecision)
}

func (cfg Config) interactionGateDecisionConsumerLeaseOwner() string {
	leaseOwner := strings.TrimSpace(cfg.InteractionGateDecisionConsumerLeaseOwner)
	if leaseOwner == "" {
		return eventconsumer.DefaultLeaseOwner("governance-interaction-gate-decision")
	}
	return leaseOwner
}

// AgentAcceptanceEvidenceConsumerConfig собирает runtime-настройки shared event consumer.
func (cfg Config) AgentAcceptanceEvidenceConsumerConfig() eventconsumer.Config {
	return cfg.eventConsumerConfig(consumerKindAgentAcceptanceEvidence)
}

func (cfg Config) agentAcceptanceEvidenceConsumerLeaseOwner() string {
	leaseOwner := strings.TrimSpace(cfg.AgentAcceptanceEvidenceConsumerLeaseOwner)
	if leaseOwner == "" {
		return eventconsumer.DefaultLeaseOwner("governance-agent-acceptance-evidence")
	}
	return leaseOwner
}

type governanceConsumerKind string

const (
	consumerKindProviderReviewSignal    governanceConsumerKind = "provider_review_signal"
	consumerKindInteractionGateDecision governanceConsumerKind = "interaction_gate_decision"
	consumerKindAgentAcceptanceEvidence governanceConsumerKind = "agent_acceptance_evidence"
)

func (cfg Config) eventConsumerConfig(kind governanceConsumerKind) eventconsumer.Config {
	runtime := governanceConsumerRuntime{}
	switch kind {
	case consumerKindProviderReviewSignal:
		runtime.Name = strings.TrimSpace(cfg.ProviderReviewSignalConsumerName)
		runtime.LeaseOwner = cfg.providerReviewSignalConsumerLeaseOwner()
		runtime.BatchSize = cfg.ProviderReviewSignalConsumerBatchSize
		runtime.PollInterval = cfg.ProviderReviewSignalConsumerPollInterval
		runtime.LeaseTTL = cfg.ProviderReviewSignalConsumerLeaseTTL
		runtime.HandlerTimeout = cfg.ProviderReviewSignalConsumerHandlerTimeout
		runtime.RetryInitial = cfg.ProviderReviewSignalConsumerRetryInitialDelay
		runtime.RetryMax = cfg.ProviderReviewSignalConsumerRetryMaxDelay
		runtime.FailureLimit = cfg.ProviderReviewSignalConsumerFailureMessageLimit
		runtime.ConcurrencyLimit = cfg.ProviderReviewSignalConsumerConcurrencyLimit
		runtime.MaxAttempts = cfg.ProviderReviewSignalConsumerMaxAttempts
	case consumerKindInteractionGateDecision:
		runtime.MaxAttempts = cfg.InteractionGateDecisionConsumerMaxAttempts
		runtime.ConcurrencyLimit = cfg.InteractionGateDecisionConsumerConcurrencyLimit
		runtime.FailureLimit = cfg.InteractionGateDecisionConsumerFailureLimit
		runtime.RetryMax = cfg.InteractionGateDecisionConsumerRetryMax
		runtime.RetryInitial = cfg.InteractionGateDecisionConsumerRetryInitial
		runtime.HandlerTimeout = cfg.InteractionGateDecisionConsumerHandlerTimeout
		runtime.LeaseTTL = cfg.InteractionGateDecisionConsumerLeaseTTL
		runtime.PollInterval = cfg.InteractionGateDecisionConsumerPollInterval
		runtime.BatchSize = cfg.InteractionGateDecisionConsumerBatchSize
		runtime.LeaseOwner = cfg.interactionGateDecisionConsumerLeaseOwner()
		runtime.Name = strings.TrimSpace(cfg.InteractionGateDecisionConsumerName)
	case consumerKindAgentAcceptanceEvidence:
		runtime.Name = strings.TrimSpace(cfg.AgentAcceptanceEvidenceConsumerName)
		runtime.MaxAttempts = cfg.AgentAcceptanceEvidenceConsumerMaxAttempts
		runtime.LeaseOwner = cfg.agentAcceptanceEvidenceConsumerLeaseOwner()
		runtime.ConcurrencyLimit = cfg.AgentAcceptanceEvidenceConsumerConcurrencyLimit
		runtime.BatchSize = cfg.AgentAcceptanceEvidenceConsumerBatchSize
		runtime.FailureLimit = cfg.AgentAcceptanceEvidenceConsumerFailureLimit
		runtime.PollInterval = cfg.AgentAcceptanceEvidenceConsumerPollInterval
		runtime.RetryMax = cfg.AgentAcceptanceEvidenceConsumerRetryMax
		runtime.LeaseTTL = cfg.AgentAcceptanceEvidenceConsumerLeaseTTL
		runtime.RetryInitial = cfg.AgentAcceptanceEvidenceConsumerRetryInitial
		runtime.HandlerTimeout = cfg.AgentAcceptanceEvidenceConsumerHandlerTimeout
	}
	return governanceEventConsumerConfig(runtime.Name, runtime.LeaseOwner, runtime.BatchSize, runtime.PollInterval, runtime.LeaseTTL, runtime.HandlerTimeout, runtime.RetryInitial, runtime.RetryMax, runtime.FailureLimit, runtime.ConcurrencyLimit, runtime.MaxAttempts)
}
