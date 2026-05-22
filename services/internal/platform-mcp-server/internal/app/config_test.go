package app

import (
	"testing"
	"time"

	ownerclients "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/owners"
)

func TestConfigValidateRequiresAuthTokenWhenMCPAuthEnabled(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.MCP.AuthToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error is nil, want auth token error")
	}
}

func TestConfigValidateRejectsRootMCPPath(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.MCP.Path = "/"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error is nil, want path error")
	}
}

func TestOwnerRouteCatalogUsesDefaultOwnerTargets(t *testing.T) {
	t.Parallel()

	catalog, err := validConfig().OwnerRouteCatalog()
	if err != nil {
		t.Fatalf("OwnerRouteCatalog(): %v", err)
	}
	routes := catalog.Routes()
	if len(routes) != 8 {
		t.Fatalf("routes len = %d, want 8", len(routes))
	}
	if routes[0].Service != ownerclients.ServiceAccessManager || routes[0].Target != "access-manager:9090" {
		t.Fatalf("first route = %+v", routes[0])
	}
}

func validConfig() Config {
	return Config{
		HTTPAddr: ":8080",
		MCP: MCPConfig{
			Path:            "/mcp",
			RegistryVersion: "mcp-2",
			ToolsPageSize:   100,
			JSONResponse:    true,
			SessionTimeout:  30 * time.Minute,
			AuthRequired:    true,
			AuthToken:       "test-token",
			AuthScope:       "kodex.mcp",
			AuthTokenTTL:    24 * time.Hour,
		},
	}
}
