package app

import (
	"testing"
	"time"
)

func TestLoadConfigAllowsMissingConditionalEnvWhenAuthDisabled(t *testing.T) {
	t.Setenv("KODEX_AGENT_MANAGER_DATABASE_DSN", "postgres://agent-manager")
	t.Setenv("KODEX_AGENT_MANAGER_OUTBOX_DISPATCH_ENABLED", "false")
	t.Setenv("KODEX_AGENT_MANAGER_OUTBOX_PUBLISHER_KIND", "disabled")
	t.Setenv("KODEX_AGENT_MANAGER_GRPC_AUTH_REQUIRED", "false")
	t.Setenv("KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN", "")
	t.Setenv("KODEX_AGENT_MANAGER_PACKAGE_HUB_ENABLED", "false")
	t.Setenv("KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_ENABLED", "false")
	t.Setenv("KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.GRPCAuthRequired {
		t.Fatal("GRPCAuthRequired = true, want false")
	}
}

func TestLoadConfigDefaultsRuntimePreparationDisabledUntilDeployWired(t *testing.T) {
	t.Setenv("KODEX_AGENT_MANAGER_DATABASE_DSN", "postgres://agent-manager")
	t.Setenv("KODEX_AGENT_MANAGER_OUTBOX_DISPATCH_ENABLED", "false")
	t.Setenv("KODEX_AGENT_MANAGER_OUTBOX_PUBLISHER_KIND", "disabled")
	t.Setenv("KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN", "agent-token")
	t.Setenv("KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_AUTH_TOKEN", "package-token")
	t.Setenv("KODEX_AGENT_MANAGER_INTERACTION_RESPONSE_CONSUMER_ENABLED", "false")
	t.Setenv("KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED", "false")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if cfg.RuntimePreparationEnabled {
		t.Fatal("RuntimePreparationEnabled = true, want default false")
	}
	if cfg.RuntimeJobDispatchEnabled {
		t.Fatal("RuntimeJobDispatchEnabled = true, want default false")
	}
	if cfg.SelfDeployBuildDispatchEnabled {
		t.Fatal("SelfDeployBuildDispatchEnabled = true, want default false")
	}
}

func TestValidateRequiresGRPCAuthTokenWhenAuthEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.GRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want gRPC auth token error")
	}
}

func TestValidateRequiresEventLogDSNWhenPostgresPublisherEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.OutboxDispatchEnabled = true
	cfg.OutboxPublisherKind = "postgres-event-log"
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want event-log database dsn error")
	}
}

func TestValidateRequiresEventLogDSNWhenInteractionResponseConsumerEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.InteractionResponseConsumerEnabled = true
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want event-log database dsn error")
	}
}

func TestValidateRequiresEventLogDSNWhenSelfDeploySignalConsumerEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.InteractionResponseConsumerEnabled = false
	cfg.SelfDeploySignalConsumerEnabled = true
	cfg.EventLogDatabaseDSN = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want event-log database dsn error")
	}
}

func TestValidateRequiresRuntimeClientTokensWhenPreparationEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.ProjectCatalogGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want project-catalog auth token error")
	}

	cfg = validConfig()
	cfg.RuntimeManagerGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want runtime-manager auth token error")
	}
}

func TestValidateRequiresProjectCatalogTokenWhenSelfDeploySignalConsumerEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.RuntimePreparationEnabled = false
	cfg.SelfDeploySignalConsumerEnabled = true
	cfg.ProjectCatalogGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want project-catalog auth token error")
	}
}

func TestValidateRequiresProjectIDWhenSelfDeploySignalConsumerEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.SelfDeploySignalConsumerEnabled = true
	cfg.SelfDeploySignalConsumerProjectID = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want self-deploy signal project id error")
	}
}

func TestValidateRequiresRuntimePreparationWhenJobDispatchEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.RuntimePreparationEnabled = false
	cfg.RuntimeJobDispatchEnabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want runtime preparation requirement")
	}
}

func TestValidateRequiresRuntimeJobRunnerImageWhenDispatchEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.RuntimePreparationEnabled = true
	cfg.RuntimeJobDispatchEnabled = true
	cfg.RuntimeJobRunnerImageRef = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want runtime job runner image ref requirement")
	}
}

func TestValidateRequiresCodexSessionConfigWhenDispatchEnabled(t *testing.T) {
	t.Parallel()

	valid := runtimeJobDispatchConfig()
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() err = %v, want nil", err)
	}

	tests := []struct {
		name string
		edit func(*Config)
	}{
		{
			name: "missing schema ref",
			edit: func(cfg *Config) {
				cfg.CodexSessionResultSchemaRef = ""
			},
		},
		{
			name: "unsafe schema ref",
			edit: func(cfg *Config) {
				cfg.CodexSessionResultSchemaRef = "object://schemas/{raw_provider_payload}"
			},
		},
		{
			name: "invalid schema digest",
			edit: func(cfg *Config) {
				cfg.CodexSessionResultSchemaDigest = "sha256:not-a-digest"
			},
		},
		{
			name: "missing hook ref",
			edit: func(cfg *Config) {
				cfg.CodexSessionHookEndpointRef = ""
			},
		},
		{
			name: "unsafe hook ref",
			edit: func(cfg *Config) {
				cfg.CodexSessionHookEndpointRef = "hook://codex-hook-ingress/bearer token"
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := runtimeJobDispatchConfig()
			tt.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() err = nil, want codex session config error")
			}
		})
	}
}

func TestValidateRequiresProviderHubWriteTokenWhenEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.ProviderHubWriteEnabled = true
	cfg.ProviderHubGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want provider-hub auth token error")
	}
}

func TestValidateRequiresInteractionHubTokenWhenRequestEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.InteractionHubRequestEnabled = true
	cfg.InteractionHubGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want interaction-hub auth token error")
	}
}

func TestValidateRequiresGovernanceManagerTokenWhenSelfDeployGateEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.SelfDeployGovernanceGateEnabled = true
	cfg.GovernanceManagerGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want governance-manager auth token error")
	}
}

func TestValidateRequiresApprovedGatePathWhenSelfDeployBuildDispatchEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.SelfDeployBuildDispatchEnabled = true
	cfg.SelfDeployGovernanceGateEnabled = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want self-deploy governance gate error")
	}

	cfg = validConfig()
	cfg.SelfDeployBuildDispatchEnabled = true
	cfg.SelfDeployGovernanceGateEnabled = true
	cfg.RuntimeManagerGRPCAuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() err = nil, want runtime-manager auth token error")
	}
}

func TestGRPCServerConfigMapsRuntimeLimits(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	runtime := cfg.GRPCServerConfig()
	if runtime.MaxInFlight != cfg.GRPCMaxInFlight {
		t.Fatalf("MaxInFlight = %d, want %d", runtime.MaxInFlight, cfg.GRPCMaxInFlight)
	}
	if runtime.MaxConcurrentStreams != cfg.GRPCMaxConcurrentStreams {
		t.Fatalf("MaxConcurrentStreams = %d, want %d", runtime.MaxConcurrentStreams, cfg.GRPCMaxConcurrentStreams)
	}
	if runtime.AuthRequired != cfg.GRPCAuthRequired {
		t.Fatalf("AuthRequired = %v, want %v", runtime.AuthRequired, cfg.GRPCAuthRequired)
	}
}

func runtimeJobDispatchConfig() Config {
	cfg := validConfig()
	cfg.RuntimePreparationEnabled = true
	cfg.RuntimeJobDispatchEnabled = true
	cfg.RuntimeJobRunnerImageRef = "image://codex-agent@sha256:runner"
	cfg.CodexSessionResultSchemaRef = "object://schemas/codex-result-v1"
	cfg.CodexSessionResultSchemaDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cfg.CodexSessionHookEndpointRef = "hook://codex-hook-ingress/agent-runner"
	return cfg
}

func validConfig() Config {
	return Config{
		HTTPAddr:                                       ":8080",
		DatabaseDSN:                                    "postgres://agent-manager",
		DatabaseMaxConns:                               8,
		DatabaseMinConns:                               1,
		DatabaseMaxConnLifetime:                        time.Hour,
		DatabaseMaxConnIdleTime:                        15 * time.Minute,
		DatabaseHealthPeriod:                           30 * time.Second,
		DatabasePingTimeout:                            5 * time.Second,
		DatabaseRetryAttempts:                          6,
		DatabaseRetryInitial:                           500 * time.Millisecond,
		DatabaseRetryMax:                               5 * time.Second,
		DatabaseRetryJitterRatio:                       0.2,
		EventLogDatabaseDSN:                            "postgres://platform-event-log",
		EventLogDatabaseMaxConns:                       4,
		GRPCAddr:                                       ":9090",
		GRPCAuthRequired:                               true,
		GRPCAuthToken:                                  "test-token",
		GRPCMaxInFlight:                                128,
		GRPCMaxConcurrentStreams:                       128,
		GRPCUnaryTimeout:                               30 * time.Second,
		GRPCKeepaliveTime:                              2 * time.Minute,
		GRPCKeepaliveTimeout:                           20 * time.Second,
		GRPCKeepaliveMinTime:                           30 * time.Second,
		GRPCMaxRecvMessageBytes:                        4 * 1024 * 1024,
		GRPCMaxSendMessageBytes:                        4 * 1024 * 1024,
		PackageHubEnabled:                              true,
		PackageHubGRPCAddr:                             "package-hub:9090",
		PackageHubGRPCAuthToken:                        "package-token",
		PackageHubReadTimeout:                          3 * time.Second,
		RuntimePreparationEnabled:                      true,
		ProjectCatalogGRPCAddr:                         "project-catalog:9090",
		ProjectCatalogGRPCAuthToken:                    "project-token",
		ProjectCatalogReadTimeout:                      3 * time.Second,
		RuntimeManagerGRPCAddr:                         "runtime-manager:9090",
		RuntimeManagerGRPCAuthToken:                    "runtime-token",
		RuntimeManagerPrepareTimeout:                   10 * time.Second,
		CodexSessionTimeout:                            30 * time.Minute,
		ProviderHubGRPCAddr:                            "provider-hub:9090",
		ProviderHubGRPCAuthToken:                       "provider-token",
		ProviderHubWriteTimeout:                        10 * time.Second,
		InteractionHubRequestEnabled:                   false,
		InteractionHubGRPCAddr:                         "interaction-hub:9090",
		InteractionHubGRPCAuthToken:                    "interaction-token",
		InteractionHubRequestTimeout:                   10 * time.Second,
		GovernanceManagerGRPCAddr:                      "governance-manager:9090",
		GovernanceManagerGRPCAuthToken:                 "governance-token",
		GovernanceManagerRequestTimeout:                10 * time.Second,
		OutboxDispatchEnabled:                          false,
		OutboxPublisherKind:                            "disabled",
		OutboxBatchSize:                                100,
		OutboxPollInterval:                             time.Second,
		OutboxLockTTL:                                  30 * time.Second,
		OutboxPublishTimeout:                           10 * time.Second,
		OutboxLeaseSafetyMargin:                        5 * time.Second,
		OutboxRetryInitialDelay:                        time.Second,
		OutboxRetryMaxDelay:                            time.Minute,
		OutboxFailureLimit:                             512,
		InteractionResponseConsumerEnabled:             true,
		InteractionResponseConsumerName:                "agent-manager.human-gate-response",
		InteractionResponseConsumerBatchSize:           50,
		InteractionResponseConsumerPollInterval:        time.Second,
		InteractionResponseConsumerLeaseTTL:            30 * time.Second,
		InteractionResponseConsumerHandlerTimeout:      10 * time.Second,
		InteractionResponseConsumerRetryInitialDelay:   time.Second,
		InteractionResponseConsumerRetryMaxDelay:       time.Minute,
		InteractionResponseConsumerFailureMessageLimit: 512,
		InteractionResponseConsumerConcurrencyLimit:    2,
		InteractionResponseConsumerMaxAttempts:         5,
		SelfDeploySignalConsumerEnabled:                true,
		SelfDeploySignalConsumerName:                   "agent-manager.self-deploy-signal",
		SelfDeploySignalConsumerProjectID:              "11111111-2222-4333-8444-555555555555",
		SelfDeploySignalConsumerTargetBranch:           "main",
		SelfDeploySignalConsumerBatchSize:              20,
		SelfDeploySignalConsumerPollInterval:           time.Second,
		SelfDeploySignalConsumerLeaseTTL:               30 * time.Second,
		SelfDeploySignalConsumerHandlerTimeout:         10 * time.Second,
		SelfDeploySignalConsumerRetryInitialDelay:      5 * time.Second,
		SelfDeploySignalConsumerRetryMaxDelay:          5 * time.Minute,
		SelfDeploySignalConsumerFailureMessageLimit:    512,
		SelfDeploySignalConsumerConcurrencyLimit:       1,
		SelfDeploySignalConsumerMaxAttempts:            24,
	}
}
