package mcptransport

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server owns the MCP SDK server and the HTTP transport handler.
type Server struct {
	mcpServer *mcpsdk.Server
	handler   http.Handler
	registry  *Registry
}

// NewServer creates an MCP transport boundary with registered skeleton tools.
func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	registry := &Registry{}
	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    strings.TrimSpace(cfg.ServiceName),
		Version: strings.TrimSpace(cfg.RegistryVersion),
	}, &mcpsdk.ServerOptions{
		Logger:   logger,
		PageSize: cfg.ToolsPageSize,
	})
	diagnostics := NewDiagnosticsHandler(cfg.ServiceName, cfg.RegistryVersion, cfg.OwnerRoutes, registry)
	registry.addDiagnosticsTools(mcpServer, diagnostics, cfg.RegistryVersion)
	agentTools := NewAgentToolsHandler(cfg.AgentManager)
	registry.addAgentTools(mcpServer, agentTools, cfg.RegistryVersion)
	providerTools := NewProviderToolsHandler(cfg.ProviderHub)
	registry.addProviderTools(mcpServer, providerTools, cfg.RegistryVersion)
	streamable := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return mcpServer
	}, &mcpsdk.StreamableHTTPOptions{
		JSONResponse:   cfg.JSONResponse,
		Logger:         logger,
		SessionTimeout: cfg.SessionTimeout,
	})
	return &Server{
		mcpServer: mcpServer,
		handler:   bearerTokenMiddleware(cfg)(streamable),
		registry:  registry,
	}, nil
}

// Validate protects the MCP boundary from incomplete runtime configuration.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.ServiceName) == "" {
		return fmt.Errorf("mcp service name is required")
	}
	if strings.TrimSpace(cfg.RegistryVersion) == "" {
		return fmt.Errorf("mcp registry version is required")
	}
	if cfg.ToolsPageSize <= 0 {
		return fmt.Errorf("mcp tools page size is invalid")
	}
	if cfg.SessionTimeout <= 0 {
		return fmt.Errorf("mcp session timeout is invalid")
	}
	if cfg.AuthRequired {
		if strings.TrimSpace(cfg.AuthToken) == "" {
			return fmt.Errorf("mcp auth token is required")
		}
		if strings.TrimSpace(cfg.AuthScope) == "" {
			return fmt.Errorf("mcp auth scope is required")
		}
		if cfg.AuthTokenTTL <= 0 {
			return fmt.Errorf("mcp auth token ttl is invalid")
		}
	}
	if !cfg.OwnerRoutes.Ready() {
		return fmt.Errorf("mcp owner route catalog is not ready")
	}
	if cfg.AgentManager == nil {
		return fmt.Errorf("mcp agent-manager client is required")
	}
	if cfg.ProviderHub == nil {
		return fmt.Errorf("mcp provider-hub client is required")
	}
	return nil
}

// HTTPHandler returns the streamable MCP HTTP boundary.
func (server *Server) HTTPHandler() http.Handler {
	return server.handler
}

// Ready reports whether the MCP SDK server and registry are composed.
func (server *Server) Ready() bool {
	return server != nil && server.mcpServer != nil && server.registry != nil
}

// RegisteredTools returns a stable copy of registered MCP tools.
func (server *Server) RegisteredTools() []ToolDescriptor {
	if server == nil || server.registry == nil {
		return nil
	}
	return server.registry.Tools()
}
