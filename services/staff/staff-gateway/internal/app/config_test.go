package app

import (
	"strings"
	"testing"
	"time"
)

func TestConfigRequiresProjectCatalogAuthToken(t *testing.T) {
	cfg := validConfig()
	cfg.ProjectCatalog.AuthToken = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want missing project-catalog token")
	}
	if !strings.Contains(err.Error(), "KODEX_STAFF_GATEWAY_PROJECT_CATALOG_GRPC_AUTH_TOKEN is required") {
		t.Fatalf("Validate() error = %v, want project-catalog auth token error", err)
	}
}

func TestConfigProjectCatalogClientUsesOwnAuthToken(t *testing.T) {
	cfg := validConfig()
	cfg.ProjectCatalog.AuthToken = " project-catalog-token "
	cfg.InteractionHub.AuthToken = "interaction-hub-token"

	clientCfg := cfg.ProjectCatalogClientConfig()
	if clientCfg.AuthToken != "project-catalog-token" {
		t.Fatalf("ProjectCatalogClientConfig().AuthToken = %q, want project-catalog-token", clientCfg.AuthToken)
	}
}

func TestConfigAgentManagerKeepsInteractionHubFallback(t *testing.T) {
	cfg := validConfig()
	cfg.AgentManager.AuthToken = ""
	cfg.InteractionHub.AuthToken = "interaction-hub-token"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	clientCfg := cfg.AgentManagerClientConfig()
	if clientCfg.AuthToken != "interaction-hub-token" {
		t.Fatalf("AgentManagerClientConfig().AuthToken = %q, want interaction-hub-token", clientCfg.AuthToken)
	}
}

func validConfig() Config {
	return Config{
		HTTPAddr:        ":8080",
		OpenAPISpecPath: "specs/openapi/staff-gateway.v1.yaml",
		HTTP: HTTPConfig{
			ReadHeaderTimeout: time.Second,
			RequestTimeout:    time.Second,
			ShutdownTimeout:   time.Second,
			ReadinessTimeout:  time.Second,
			MaxBodyBytes:      1024,
		},
		InteractionHub: InteractionHubConfig{
			GRPCAddr:  "interaction-hub:9090",
			AuthToken: "interaction-hub-token",
			Timeout:   time.Second,
		},
		AgentManager: AgentManagerConfig{
			GRPCAddr:  "agent-manager:9090",
			AuthToken: "agent-manager-token",
			Timeout:   time.Second,
		},
		Governance: GovernanceConfig{
			GRPCAddr:  "governance-manager:9090",
			AuthToken: "governance-token",
			Timeout:   time.Second,
		},
		ProjectCatalog: ProjectCatalogConfig{
			GRPCAddr:  "project-catalog:9090",
			AuthToken: "project-catalog-token",
			Timeout:   time.Second,
		},
	}
}
