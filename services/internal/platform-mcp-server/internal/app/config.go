// Package app contains platform-mcp-server process composition and lifecycle.
package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	agentmanagerclient "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/agentmanager"
	governanceclient "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/governance"
	ownerclients "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/owners"
	providerhubclient "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/providerhub"
	mcptransport "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/transport/mcp"
)

const minMCPTokenTTL = 24 * time.Hour

// Config contains process-level platform-mcp-server configuration.
type Config struct {
	HTTPAddr          string             `env:"KODEX_PLATFORM_MCP_SERVER_HTTP_ADDR" envDefault:":8080"`
	MCP               MCPConfig          `envPrefix:"KODEX_PLATFORM_MCP_SERVER_MCP_"`
	AccessManager     OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_ACCESS_MANAGER_"`
	AgentManager      OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_AGENT_MANAGER_"`
	ProjectCatalog    OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_PROJECT_CATALOG_"`
	ProviderHub       OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_PROVIDER_HUB_"`
	GovernanceManager OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_GOVERNANCE_MANAGER_"`
	RuntimeManager    OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_RUNTIME_MANAGER_"`
	FleetManager      OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_FLEET_MANAGER_"`
	PackageHub        OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_PACKAGE_HUB_"`
	InteractionHub    OwnerServiceConfig `envPrefix:"KODEX_PLATFORM_MCP_SERVER_INTERACTION_HUB_"`
}

// MCPConfig contains MCP HTTP transport and registry settings.
type MCPConfig struct {
	Path            string        `env:"PATH" envDefault:"/mcp"`
	RegistryVersion string        `env:"REGISTRY_VERSION" envDefault:"mcp-4"`
	ToolsPageSize   int           `env:"TOOLS_PAGE_SIZE" envDefault:"100"`
	JSONResponse    bool          `env:"JSON_RESPONSE" envDefault:"true"`
	SessionTimeout  time.Duration `env:"SESSION_TIMEOUT" envDefault:"30m"`
	AuthRequired    bool          `env:"AUTH_REQUIRED" envDefault:"true"`
	AuthToken       string        `env:"AUTH_TOKEN"`
	AuthScope       string        `env:"AUTH_SCOPE" envDefault:"kodex.mcp"`
	AuthTokenTTL    time.Duration `env:"AUTH_TOKEN_TTL" envDefault:"24h"`
}

// OwnerServiceConfig contains one service-owner route configuration.
type OwnerServiceConfig struct {
	Enabled   bool          `env:"ENABLED" envDefault:"true"`
	GRPCAddr  string        `env:"GRPC_ADDR"`
	AuthToken string        `env:"GRPC_AUTH_TOKEN"`
	Timeout   time.Duration `env:"TIMEOUT" envDefault:"3s"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load platform-mcp-server config: %w", err)
	}
	return cfg, nil
}

// Validate checks process settings before runtime construction.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_HTTP_ADDR is required")
	}
	mcpPath := strings.TrimSpace(cfg.MCP.Path)
	if mcpPath == "" || !strings.HasPrefix(mcpPath, "/") || mcpPath == "/" {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_MCP_PATH must be an absolute non-root path")
	}
	if conflictsWithServicePath(mcpPath) {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_MCP_PATH conflicts with service HTTP endpoints")
	}
	if strings.TrimSpace(cfg.MCP.RegistryVersion) == "" {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_MCP_REGISTRY_VERSION is required")
	}
	if cfg.MCP.ToolsPageSize <= 0 {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_MCP_TOOLS_PAGE_SIZE is invalid")
	}
	if cfg.MCP.SessionTimeout <= 0 {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_MCP_SESSION_TIMEOUT is invalid")
	}
	if cfg.MCP.AuthRequired && strings.TrimSpace(cfg.MCP.AuthToken) == "" {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_MCP_AUTH_TOKEN is required when MCP auth is enabled")
	}
	if cfg.MCP.AuthRequired && strings.TrimSpace(cfg.MCP.AuthScope) == "" {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_MCP_AUTH_SCOPE is required when MCP auth is enabled")
	}
	if cfg.MCP.AuthRequired && cfg.MCP.AuthTokenTTL < minMCPTokenTTL {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_MCP_AUTH_TOKEN_TTL must be at least 24h")
	}
	if !cfg.AgentManager.Enabled {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_AGENT_MANAGER_ENABLED must stay enabled for agent tools")
	}
	if strings.TrimSpace(cfg.AgentManager.AuthToken) == "" {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_AGENT_MANAGER_GRPC_AUTH_TOKEN is required")
	}
	if !cfg.ProviderHub.Enabled {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_PROVIDER_HUB_ENABLED must stay enabled for provider tools")
	}
	if strings.TrimSpace(cfg.ProviderHub.AuthToken) == "" {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_PROVIDER_HUB_GRPC_AUTH_TOKEN is required")
	}
	if !cfg.GovernanceManager.Enabled {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_GOVERNANCE_MANAGER_ENABLED must stay enabled for governance tools")
	}
	if strings.TrimSpace(cfg.GovernanceManager.AuthToken) == "" {
		return fmt.Errorf("KODEX_PLATFORM_MCP_SERVER_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN is required")
	}
	_, err := cfg.OwnerRouteCatalog()
	return err
}

// OwnerRouteCatalog converts env settings to value-safe owner route catalog.
func (cfg Config) OwnerRouteCatalog() (ownerclients.Catalog, error) {
	return ownerclients.NewCatalog([]ownerclients.RouteConfig{
		cfg.AccessManager.route(ownerclients.ServiceAccessManager, "access-manager:9090"),
		cfg.AgentManager.route(ownerclients.ServiceAgentManager, "agent-manager:9090"),
		cfg.ProjectCatalog.route(ownerclients.ServiceProjectCatalog, "project-catalog:9090"),
		cfg.ProviderHub.route(ownerclients.ServiceProviderHub, "provider-hub:9090"),
		cfg.GovernanceManager.route(ownerclients.ServiceGovernanceManager, "governance-manager:9090"),
		cfg.RuntimeManager.route(ownerclients.ServiceRuntimeManager, "runtime-manager:9090"),
		cfg.FleetManager.route(ownerclients.ServiceFleetManager, "fleet-manager:9090"),
		cfg.PackageHub.route(ownerclients.ServicePackageHub, "package-hub:9090"),
		cfg.InteractionHub.route(ownerclients.ServiceInteractionHub, "interaction-hub:9090"),
	})
}

// MCPTransportConfig converts process config to the MCP transport runtime contract.
func (cfg Config) MCPTransportConfig(
	routes ownerclients.Catalog,
	agentManager mcptransport.AgentManagerClient,
	providerHub mcptransport.ProviderHubClient,
	governanceManager mcptransport.GovernanceManagerClient,
) mcptransport.Config {
	return mcptransport.Config{
		ServiceName:       serviceName,
		RegistryVersion:   strings.TrimSpace(cfg.MCP.RegistryVersion),
		ToolsPageSize:     cfg.MCP.ToolsPageSize,
		JSONResponse:      cfg.MCP.JSONResponse,
		SessionTimeout:    cfg.MCP.SessionTimeout,
		OwnerRoutes:       routes,
		AgentManager:      agentManager,
		ProviderHub:       providerHub,
		GovernanceManager: governanceManager,
		AuthRequired:      cfg.MCP.AuthRequired,
		AuthToken:         strings.TrimSpace(cfg.MCP.AuthToken),
		AuthScope:         strings.TrimSpace(cfg.MCP.AuthScope),
		AuthTokenTTL:      cfg.MCP.AuthTokenTTL,
	}
}

// AgentManagerClientConfig returns the owner client settings for agent-manager.
func (cfg Config) AgentManagerClientConfig() agentmanagerclient.Config {
	route := cfg.AgentManager.route(ownerclients.ServiceAgentManager, "agent-manager:9090")
	return agentmanagerclient.Config{
		Addr:      route.GRPCAddr,
		AuthToken: route.AuthToken,
		Timeout:   route.Timeout,
	}
}

// ProviderHubClientConfig returns the owner client settings for provider-hub.
func (cfg Config) ProviderHubClientConfig() providerhubclient.Config {
	route := cfg.ProviderHub.route(ownerclients.ServiceProviderHub, "provider-hub:9090")
	return providerhubclient.Config{
		Addr:      route.GRPCAddr,
		AuthToken: route.AuthToken,
		Timeout:   route.Timeout,
	}
}

// GovernanceManagerClientConfig returns the owner client settings for governance-manager.
func (cfg Config) GovernanceManagerClientConfig() governanceclient.Config {
	route := cfg.GovernanceManager.route(ownerclients.ServiceGovernanceManager, "governance-manager:9090")
	return governanceclient.Config{
		Addr:      route.GRPCAddr,
		AuthToken: route.AuthToken,
		Timeout:   route.Timeout,
	}
}

func conflictsWithServicePath(path string) bool {
	return path == "/health" ||
		strings.HasPrefix(path, "/health/") ||
		path == "/metrics" ||
		strings.HasPrefix(path, "/metrics/")
}

func (cfg OwnerServiceConfig) route(service string, defaultAddr string) ownerclients.RouteConfig {
	addr := strings.TrimSpace(cfg.GRPCAddr)
	if addr == "" {
		addr = defaultAddr
	}
	return ownerclients.RouteConfig{
		Service:   service,
		GRPCAddr:  addr,
		AuthToken: cfg.AuthToken,
		Timeout:   cfg.Timeout,
		Enabled:   cfg.Enabled,
	}
}
