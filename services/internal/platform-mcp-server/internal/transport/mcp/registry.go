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

func (registry *Registry) addAgentTools(server *mcpsdk.Server, handler *AgentToolsHandler, version string) {
	tools := []struct {
		name        string
		description string
		register    func()
	}{
		{
			name:        ToolAgentSessionStart,
			description: agentSessionStartDescription,
			register: func() {
				mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolAgentSessionStart, Description: agentSessionStartDescription}, handler.StartSession)
			},
		},
		{
			name:        ToolAgentRunStart,
			description: agentRunStartDescription,
			register: func() {
				mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolAgentRunStart, Description: agentRunStartDescription}, handler.StartRun)
			},
		},
		{
			name:        ToolAgentRunRecordState,
			description: agentRunRecordStateDescription,
			register: func() {
				mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolAgentRunRecordState, Description: agentRunRecordStateDescription}, handler.RecordRunState)
			},
		},
		{
			name:        ToolAgentSessionRecordSnapshot,
			description: agentSessionRecordSnapshotDescription,
			register: func() {
				mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolAgentSessionRecordSnapshot, Description: agentSessionRecordSnapshotDescription}, handler.RecordSessionSnapshot)
			},
		},
		{
			name:        ToolDiagnosticsRunContextRead,
			description: diagnosticsRunContextReadDescription,
			register: func() {
				mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolDiagnosticsRunContextRead, Description: diagnosticsRunContextReadDescription}, handler.ReadRunContext)
			},
		},
	}
	for _, tool := range tools {
		tool.register()
		registry.tools = append(registry.tools, ToolDescriptor{
			Name:        tool.name,
			Description: tool.description,
			Version:     version,
		})
	}
}

func (registry *Registry) addProviderTools(server *mcpsdk.Server, handler *ProviderToolsHandler, version string) {
	tools := []struct {
		name     string
		register func(string)
	}{
		{name: ToolProviderProjectionGet, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderProjectionGet, Description: description}, handler.GetProjection)
		}},
		{name: ToolProviderProjectionFind, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderProjectionFind, Description: description}, handler.FindProjection)
		}},
		{name: ToolProviderProjectionsList, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderProjectionsList, Description: description}, handler.ListProjections)
		}},
		{name: ToolProviderCommentsList, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderCommentsList, Description: description}, handler.ListComments)
		}},
		{name: ToolProviderRelationshipsList, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderRelationshipsList, Description: description}, handler.ListRelationships)
		}},
		{name: ToolProviderArtifactSignalRegister, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderArtifactSignalRegister, Description: description}, handler.RegisterArtifactSignal)
		}},
		{name: ToolProviderIssueCreate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderIssueCreate, Description: description}, handler.CreateIssue)
		}},
		{name: ToolProviderIssueUpdate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderIssueUpdate, Description: description}, handler.UpdateIssue)
		}},
		{name: ToolProviderCommentCreate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderCommentCreate, Description: description}, handler.CreateComment)
		}},
		{name: ToolProviderCommentUpdate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderCommentUpdate, Description: description}, handler.UpdateComment)
		}},
		{name: ToolProviderPullRequestCreate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderPullRequestCreate, Description: description}, handler.CreatePullRequest)
		}},
		{name: ToolProviderPullRequestUpdate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderPullRequestUpdate, Description: description}, handler.UpdatePullRequest)
		}},
		{name: ToolProviderReviewSignalCreate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderReviewSignalCreate, Description: description}, handler.CreateReviewSignal)
		}},
		{name: ToolProviderRelationshipUpdate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderRelationshipUpdate, Description: description}, handler.UpdateRelationship)
		}},
		{name: ToolProviderRepositoryCreate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderRepositoryCreate, Description: description}, handler.CreateRepository)
		}},
		{name: ToolProviderRepositoryBootstrapPullRequestCreate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderRepositoryBootstrapPullRequestCreate, Description: description}, handler.CreateBootstrapPullRequest)
		}},
		{name: ToolProviderRepositoryAdoptionPullRequestCreate, register: func(description string) {
			mcpsdk.AddTool(server, &mcpsdk.Tool{Name: ToolProviderRepositoryAdoptionPullRequestCreate, Description: description}, handler.CreateAdoptionPullRequest)
		}},
	}
	for _, tool := range tools {
		description := providerToolDescriptions[tool.name]
		tool.register(description)
		registry.tools = append(registry.tools, ToolDescriptor{
			Name:        tool.name,
			Description: description,
			Version:     version,
		})
	}
}

// Tools returns a copy of registered tool descriptors.
func (registry *Registry) Tools() []ToolDescriptor {
	result := make([]ToolDescriptor, len(registry.tools))
	copy(result, registry.tools)
	return result
}
