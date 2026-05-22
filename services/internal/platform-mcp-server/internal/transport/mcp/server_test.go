package mcptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ownerclients "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/owners"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsListSnapshot(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools(): %v", err)
	}
	snapshot := make([]toolSnapshot, 0, len(result.Tools))
	for _, tool := range result.Tools {
		snapshot = append(snapshot, toolSnapshot{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			OutputSchema: tool.OutputSchema,
		})
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(): %v", err)
	}
	const expected = `[
  {
    "name": "diagnostics.mcp_status.read",
    "description": "Ограниченная диагностика MCP-регистра и маршрутов без секретов и бизнес-данных.",
    "input_schema": {
      "additionalProperties": false,
      "properties": {
        "include_dependency_routes": {
          "description": "include configured service-owner routes without secrets",
          "type": "boolean"
        }
      },
      "type": "object"
    },
    "output_schema": {
      "additionalProperties": false,
      "properties": {
        "dependency_routes": {
          "description": "configured service-owner routes without secrets",
          "items": {
            "additionalProperties": false,
            "properties": {
              "auth_configured": {
                "description": "whether auth token is configured without exposing it",
                "type": "boolean"
              },
              "enabled": {
                "description": "whether route is enabled",
                "type": "boolean"
              },
              "service": {
                "description": "service owner name",
                "type": "string"
              },
              "target": {
                "description": "configured target address without credentials",
                "type": "string"
              },
              "timeout_ms": {
                "description": "route timeout in milliseconds",
                "type": "integer"
              },
              "transport": {
                "description": "internal transport",
                "type": "string"
              }
            },
            "required": [
              "service",
              "transport",
              "target",
              "enabled",
              "auth_configured",
              "timeout_ms"
            ],
            "type": "object"
          },
          "type": [
            "null",
            "array"
          ]
        },
        "ready": {
          "description": "whether MCP registry is ready",
          "type": "boolean"
        },
        "registry_version": {
          "description": "MCP registry version",
          "type": "string"
        },
        "service": {
          "description": "service name",
          "type": "string"
        },
        "tool_count": {
          "description": "registered tool count",
          "type": "integer"
        },
        "tools": {
          "description": "registered MCP tools",
          "items": {
            "additionalProperties": false,
            "properties": {
              "description": {
                "type": "string"
              },
              "name": {
                "type": "string"
              },
              "version": {
                "type": "string"
              }
            },
            "required": [
              "name",
              "description",
              "version"
            ],
            "type": "object"
          },
          "type": [
            "null",
            "array"
          ]
        }
      },
      "required": [
        "service",
        "registry_version",
        "ready",
        "tool_count",
        "tools"
      ],
      "type": "object"
    }
  }
]`
	if string(data) != expected {
		t.Fatalf("tools/list snapshot mismatch:\n%s", data)
	}
}

func TestDiagnosticsStatusDoesNotExposeSecrets(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      ToolDiagnosticsMCPStatusRead,
		Arguments: map[string]any{"include_dependency_routes": true},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if strings.Contains(string(data), "secret-token") {
		t.Fatalf("structured content exposes secret: %s", data)
	}
	if !strings.Contains(string(data), "project-catalog:9090") {
		t.Fatalf("structured content does not include safe route target: %s", data)
	}
}

func TestHTTPHandlerRequiresBearerToken(t *testing.T) {
	t.Parallel()

	server := newTestServerWithAuth(t)

	for _, tt := range []struct {
		name   string
		header string
	}{
		{name: "missing token"},
		{name: "wrong token", header: "Bearer wrong-token"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rr := httptest.NewRecorder()
			server.HTTPHandler().ServeHTTP(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()
	server.HTTPHandler().ServeHTTP(rr, req)
	if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
		t.Fatalf("status = %d, want auth middleware to pass request to MCP handler", rr.Code)
	}
}

type toolSnapshot struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	InputSchema  any    `json:"input_schema"`
	OutputSchema any    `json:"output_schema,omitempty"`
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	routes, err := ownerclients.NewCatalog([]ownerclients.RouteConfig{{
		Service:   ownerclients.ServiceProjectCatalog,
		GRPCAddr:  "project-catalog:9090",
		AuthToken: "secret-token",
		Timeout:   3 * time.Second,
		Enabled:   true,
	}})
	if err != nil {
		t.Fatalf("NewCatalog(): %v", err)
	}
	server, err := NewServer(Config{
		ServiceName:     "platform-mcp-server",
		RegistryVersion: "mcp-2",
		ToolsPageSize:   100,
		JSONResponse:    true,
		SessionTimeout:  time.Minute,
		OwnerRoutes:     routes,
		AuthRequired:    false,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	return server
}

func newTestServerWithAuth(t *testing.T) *Server {
	t.Helper()

	routes, err := ownerclients.NewCatalog([]ownerclients.RouteConfig{{
		Service:  ownerclients.ServiceProjectCatalog,
		GRPCAddr: "project-catalog:9090",
		Timeout:  3 * time.Second,
		Enabled:  true,
	}})
	if err != nil {
		t.Fatalf("NewCatalog(): %v", err)
	}
	server, err := NewServer(Config{
		ServiceName:     "platform-mcp-server",
		RegistryVersion: "mcp-2",
		ToolsPageSize:   100,
		JSONResponse:    true,
		SessionTimeout:  time.Minute,
		OwnerRoutes:     routes,
		AuthRequired:    true,
		AuthToken:       "test-token",
		AuthScope:       "kodex.mcp",
		AuthTokenTTL:    24 * time.Hour,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	return server
}

func connectClient(t *testing.T, server *Server) (*mcpsdk.ClientSession, func()) {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect(): %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect(): %v", err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Wait()
	}
}
