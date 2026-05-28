package mcptransport

import (
	"time"

	ownerclients "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/owners"
)

const ToolDiagnosticsMCPStatusRead = "diagnostics.mcp_status.read"

// Config contains MCP transport settings that are independent from env parsing.
type Config struct {
	ServiceName       string
	RegistryVersion   string
	ToolsPageSize     int
	JSONResponse      bool
	SessionTimeout    time.Duration
	OwnerRoutes       ownerclients.Catalog
	AgentManager      AgentManagerClient
	ProviderHub       ProviderHubClient
	GovernanceManager GovernanceManagerClient
	InteractionHub    InteractionHubClient
	AuthRequired      bool
	AuthToken         string
	AuthScope         string
	AuthTokenTTL      time.Duration
}

// ToolDescriptor is a stable summary used by diagnostics and snapshot tests.
type ToolDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// StatusInput controls bounded MCP status diagnostics.
type StatusInput struct {
	IncludeDependencyRoutes bool `json:"include_dependency_routes,omitempty" jsonschema:"include configured service-owner routes without secrets"`
}

// StatusOutput is the safe response of diagnostics.mcp_status.read.
type StatusOutput struct {
	Service          string            `json:"service" jsonschema:"service name"`
	RegistryVersion  string            `json:"registry_version" jsonschema:"MCP registry version"`
	Ready            bool              `json:"ready" jsonschema:"whether MCP registry is ready"`
	ToolCount        int               `json:"tool_count" jsonschema:"registered tool count"`
	Tools            []ToolDescriptor  `json:"tools" jsonschema:"registered MCP tools"`
	DependencyRoutes []DependencyRoute `json:"dependency_routes,omitempty" jsonschema:"configured service-owner routes without secrets"`
}

// DependencyRoute is a value-safe owner route description.
type DependencyRoute struct {
	Service        string `json:"service" jsonschema:"service owner name"`
	Transport      string `json:"transport" jsonschema:"internal transport"`
	Target         string `json:"target" jsonschema:"configured target address without credentials"`
	Enabled        bool   `json:"enabled" jsonschema:"whether route is enabled"`
	AuthConfigured bool   `json:"auth_configured" jsonschema:"whether auth token is configured without exposing it"`
	TimeoutMS      int64  `json:"timeout_ms" jsonschema:"route timeout in milliseconds"`
}
