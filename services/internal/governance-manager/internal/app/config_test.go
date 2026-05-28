package app

import (
	"strings"
	"testing"
	"time"
)

func TestConfigValidateRequiresGRPCTokenWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCAuthToken = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN") {
		t.Fatalf("Validate() error = %v, want missing grpc token", err)
	}
}

func TestConfigValidateAllowsDisabledGRPCAuth(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCAuthRequired = false
	cfg.GRPCAuthToken = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestConfigValidateRejectsInvalidGRPCLimit(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCMaxInFlight = 0
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "grpc max in-flight") {
		t.Fatalf("Validate() error = %v, want grpc max in-flight error", err)
	}
}

func TestConfigValidateRequiresAccessTokenWhenChecksEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.AccessCheckEnabled = true
	cfg.AccessManagerGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN") {
		t.Fatalf("Validate() error = %v, want missing access-manager token", err)
	}
}

func TestConfigValidateRequiresEventLogDSNWhenProviderReviewSignalConsumerEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.ProviderReviewSignalConsumerEnabled = true
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN") {
		t.Fatalf("Validate() error = %v, want missing event-log dsn", err)
	}
}

func TestConfigValidateRequiresEventLogDSNWhenInteractionGateDecisionConsumerEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.InteractionGateDecisionConsumerEnabled = true
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN") {
		t.Fatalf("Validate() error = %v, want missing event-log dsn", err)
	}
}

func TestConfigValidateRequiresEventLogDSNWhenAgentAcceptanceEvidenceConsumerEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.AgentAcceptanceEvidenceConsumerEnabled = true
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN") {
		t.Fatalf("Validate() error = %v, want missing event-log dsn", err)
	}
}

func TestProviderReviewSignalConsumerConfigUsesConfiguredLeaseOwner(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.ProviderReviewSignalConsumerEnabled = true
	cfg.ProviderReviewSignalConsumerLeaseOwner = "test-lease-owner"
	consumerCfg := cfg.ProviderReviewSignalConsumerConfig()
	if consumerCfg.ConsumerName != "governance-manager.provider-review-signal" || consumerCfg.LeaseOwner != "test-lease-owner" {
		t.Fatalf("ProviderReviewSignalConsumerConfig() = %+v, want configured name and lease owner", consumerCfg)
	}
}

func TestInteractionGateDecisionConsumerConfigUsesConfiguredLeaseOwner(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.InteractionGateDecisionConsumerEnabled = true
	cfg.InteractionGateDecisionConsumerLeaseOwner = "test-interaction-lease-owner"
	consumerCfg := cfg.InteractionGateDecisionConsumerConfig()
	if consumerCfg.ConsumerName != "governance-manager.interaction-gate-decision" || consumerCfg.LeaseOwner != "test-interaction-lease-owner" {
		t.Fatalf("InteractionGateDecisionConsumerConfig() = %+v, want configured name and lease owner", consumerCfg)
	}
}

func TestAgentAcceptanceEvidenceConsumerConfigUsesConfiguredLeaseOwner(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.AgentAcceptanceEvidenceConsumerEnabled = true
	cfg.AgentAcceptanceEvidenceConsumerLeaseOwner = "test-agent-acceptance-lease-owner"
	consumerCfg := cfg.AgentAcceptanceEvidenceConsumerConfig()
	if consumerCfg.ConsumerName != "governance-manager.agent-acceptance-evidence" || consumerCfg.LeaseOwner != "test-agent-acceptance-lease-owner" {
		t.Fatalf("AgentAcceptanceEvidenceConsumerConfig() = %+v, want configured name and lease owner", consumerCfg)
	}
}

func validConfig() Config {
	return Config{
		DatabaseDSN:                                     "postgres://kodex:kodex@localhost:5432/kodex?sslmode=disable",
		DatabaseMaxConns:                                8,
		DatabaseMinConns:                                1,
		DatabaseMaxConnLifetime:                         time.Hour,
		DatabaseMaxConnIdleTime:                         15 * time.Minute,
		DatabaseHealthPeriod:                            30 * time.Second,
		DatabasePingTimeout:                             5 * time.Second,
		DatabaseRetryAttempts:                           6,
		DatabaseRetryInitial:                            500 * time.Millisecond,
		DatabaseRetryMax:                                5 * time.Second,
		DatabaseRetryJitterRatio:                        0.2,
		EventLogDatabaseDSN:                             "postgres://kodex:kodex@localhost:5432/platform_event_log?sslmode=disable",
		EventLogDatabaseMaxConns:                        4,
		EventLogDatabaseMinConns:                        0,
		HTTPAddr:                                        ":8080",
		GRPCAddr:                                        ":9090",
		GRPCAuthRequired:                                true,
		GRPCAuthToken:                                   "test-token",
		GRPCMaxConcurrentStreams:                        128,
		GRPCMaxInFlight:                                 128,
		GRPCMaxRecvMessageBytes:                         4 << 20,
		GRPCMaxSendMessageBytes:                         4 << 20,
		GRPCKeepaliveMinTime:                            30 * time.Second,
		GRPCKeepaliveTime:                               2 * time.Minute,
		GRPCKeepaliveTimeout:                            20 * time.Second,
		GRPCUnaryTimeout:                                30 * time.Second,
		AccessCheckEnabled:                              false,
		AccessManagerGRPCAddr:                           "access-manager:9090",
		AccessManagerGRPCAuthToken:                      "access-token",
		AccessManagerCheckTimeout:                       3 * time.Second,
		OutboxDispatchEnabled:                           false,
		OutboxPublisherKind:                             "disabled",
		OutboxEventLogSource:                            "governance-manager",
		OutboxBatchSize:                                 100,
		OutboxPollInterval:                              time.Second,
		OutboxLockTTL:                                   30 * time.Second,
		OutboxPublishTimeout:                            10 * time.Second,
		OutboxLeaseSafetyMargin:                         5 * time.Second,
		OutboxRetryInitialDelay:                         time.Second,
		OutboxRetryMaxDelay:                             time.Minute,
		OutboxFailureLimit:                              512,
		ProviderReviewSignalConsumerEnabled:             false,
		ProviderReviewSignalConsumerName:                "governance-manager.provider-review-signal",
		ProviderReviewSignalConsumerBatchSize:           50,
		ProviderReviewSignalConsumerPollInterval:        time.Second,
		ProviderReviewSignalConsumerLeaseTTL:            30 * time.Second,
		ProviderReviewSignalConsumerHandlerTimeout:      10 * time.Second,
		ProviderReviewSignalConsumerRetryInitialDelay:   time.Second,
		ProviderReviewSignalConsumerRetryMaxDelay:       time.Minute,
		ProviderReviewSignalConsumerFailureMessageLimit: 512,
		ProviderReviewSignalConsumerConcurrencyLimit:    2,
		ProviderReviewSignalConsumerMaxAttempts:         5,
		InteractionGateDecisionConsumerEnabled:          false,
		InteractionGateDecisionConsumerName:             "governance-manager.interaction-gate-decision",
		InteractionGateDecisionConsumerBatchSize:        50,
		InteractionGateDecisionConsumerPollInterval:     time.Second,
		InteractionGateDecisionConsumerLeaseTTL:         30 * time.Second,
		InteractionGateDecisionConsumerHandlerTimeout:   10 * time.Second,
		InteractionGateDecisionConsumerRetryInitial:     time.Second,
		InteractionGateDecisionConsumerRetryMax:         time.Minute,
		InteractionGateDecisionConsumerFailureLimit:     512,
		InteractionGateDecisionConsumerConcurrencyLimit: 2,
		InteractionGateDecisionConsumerMaxAttempts:      5,
		AgentAcceptanceEvidenceConsumerEnabled:          false,
		AgentAcceptanceEvidenceConsumerName:             "governance-manager.agent-acceptance-evidence",
		AgentAcceptanceEvidenceConsumerBatchSize:        50,
		AgentAcceptanceEvidenceConsumerPollInterval:     time.Second,
		AgentAcceptanceEvidenceConsumerLeaseTTL:         30 * time.Second,
		AgentAcceptanceEvidenceConsumerHandlerTimeout:   10 * time.Second,
		AgentAcceptanceEvidenceConsumerRetryInitial:     time.Second,
		AgentAcceptanceEvidenceConsumerRetryMax:         time.Minute,
		AgentAcceptanceEvidenceConsumerFailureLimit:     512,
		AgentAcceptanceEvidenceConsumerConcurrencyLimit: 2,
		AgentAcceptanceEvidenceConsumerMaxAttempts:      5,
	}
}
