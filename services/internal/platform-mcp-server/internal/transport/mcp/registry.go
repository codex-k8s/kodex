package mcptransport

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const diagnosticsMCPStatusDescription = "Ограниченная диагностика MCP-регистра и маршрутов без секретов и бизнес-данных."

// Registry wraps SDK tool registration and keeps a stable tool summary.
type Registry struct {
	tools []ToolDescriptor
}

func (registry *Registry) addDiagnosticsTools(server *mcpsdk.Server, handler *DiagnosticsHandler, version string) {
	tool := &mcpsdk.Tool{
		Name:        ToolDiagnosticsMCPStatusRead,
		Description: diagnosticsMCPStatusDescription,
	}
	mcpsdk.AddTool(server, tool, func(ctx context.Context, request *mcpsdk.CallToolRequest, input StatusInput) (*mcpsdk.CallToolResult, StatusOutput, error) {
		return handler.Status(ctx, request, input)
	})
	registry.tools = append(registry.tools, ToolDescriptor{
		Name:        ToolDiagnosticsMCPStatusRead,
		Description: diagnosticsMCPStatusDescription,
		Version:     version,
	})
}

// Tools returns a copy of registered tool descriptors.
func (registry *Registry) Tools() []ToolDescriptor {
	result := make([]ToolDescriptor, len(registry.tools))
	copy(result, registry.tools)
	return result
}
