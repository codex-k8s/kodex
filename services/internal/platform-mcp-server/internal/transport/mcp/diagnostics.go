package mcptransport

import (
	"context"

	ownerclients "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/owners"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DiagnosticsHandler serves MCP-owned diagnostics without calling business owners.
type DiagnosticsHandler struct {
	serviceName     string
	registryVersion string
	routes          ownerclients.Catalog
	registry        *Registry
}

func NewDiagnosticsHandler(serviceName string, registryVersion string, routes ownerclients.Catalog, registry *Registry) *DiagnosticsHandler {
	return &DiagnosticsHandler{
		serviceName:     serviceName,
		registryVersion: registryVersion,
		routes:          routes,
		registry:        registry,
	}
}

// Status returns a bounded status snapshot for MCP clients.
func (handler *DiagnosticsHandler) Status(_ context.Context, _ *mcpsdk.CallToolRequest, input StatusInput) (*mcpsdk.CallToolResult, StatusOutput, error) {
	tools := handler.registry.Tools()
	output := StatusOutput{
		Service:         handler.serviceName,
		RegistryVersion: handler.registryVersion,
		Ready:           true,
		ToolCount:       len(tools),
		Tools:           tools,
	}
	if input.IncludeDependencyRoutes {
		output.DependencyRoutes = dependencyRoutes(handler.routes.Routes())
	}
	return nil, output, nil
}

func dependencyRoutes(routes []ownerclients.Route) []DependencyRoute {
	result := make([]DependencyRoute, 0, len(routes))
	for _, route := range routes {
		result = append(result, DependencyRoute{
			Service:        route.Service,
			Transport:      route.Transport,
			Target:         route.Target,
			Enabled:        route.Enabled,
			AuthConfigured: route.AuthConfigured,
			TimeoutMS:      route.Timeout.Milliseconds(),
		})
	}
	return result
}
