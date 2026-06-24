package http

import (
	"context"
	"net/http"
	"strings"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpContextKey string

const (
	mcpSessionKeyContext mcpContextKey = "matter-codex-session-key"
	mcpTokenContext      mcpContextKey = "matter-codex-session-token"
)

type mcpLimitInput struct {
	Limit int `json:"limit" jsonschema:"maximum number of Mattermost posts to return, max 50"`
}

type mcpSearchInput struct {
	Query string `json:"query" jsonschema:"search text"`
	Limit int    `json:"limit" jsonschema:"maximum number of Mattermost posts to return, max 50"`
}

type mcpPostInput struct {
	Message string `json:"message" jsonschema:"message to post into the current Mattermost thread"`
}

type mcpStatusInput struct {
	Message string `json:"message" jsonschema:"current concise status for the active agent turn"`
}

type mcpRequestAgentInput struct {
	TargetAgent string `json:"target_agent" jsonschema:"role name or Mattermost @username of the target agent"`
	Message     string `json:"message" jsonschema:"task message for the target agent"`
}

func newMCPHandler(sessionService *statusservice.AgentSessionService) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "matter-codex",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: "Use these tools only for the current Mattermost project chat/thread. Keep reads small: prefer mattermost_get_thread before mattermost_search_chat. Use mattermost_update_turn_status for progress because it edits one status message for the active turn. Use mattermost_post_thread_update only when you intentionally need an additional thread message. Use mattermost_request_agent only when the user or role prompt allows asking another agent to work.",
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_get_thread",
		Description: "Read recent messages from the current Mattermost thread. Returns at most 50 posts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpLimitInput) (*mcp.CallToolResult, statusservice.AgentSessionThreadHistory, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionThreadHistory{}, nil
		}
		output, err := sessionService.ThreadHistory(ctx, sessionKey, token, input.Limit)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionThreadHistory{}, nil
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_search_chat",
		Description: "Search recent messages in the current Mattermost channel. Returns at most 50 posts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpSearchInput) (*mcp.CallToolResult, statusservice.AgentSessionChatSearch, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionChatSearch{}, nil
		}
		output, err := sessionService.SearchChat(ctx, sessionKey, token, input.Query, input.Limit)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionChatSearch{}, nil
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_post_thread_update",
		Description: "Post an additional concise progress update to the current Mattermost thread. Prefer mattermost_update_turn_status for routine progress.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpPostInput) (*mcp.CallToolResult, statusservice.AgentSessionPostResult, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionPostResult{}, nil
		}
		output, err := sessionService.PostThreadUpdate(ctx, sessionKey, token, input.Message)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionPostResult{}, nil
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_update_turn_status",
		Description: "Create or update the single status message for the active agent turn in the current Mattermost thread.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpStatusInput) (*mcp.CallToolResult, statusservice.AgentSessionPostResult, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionPostResult{}, nil
		}
		output, err := sessionService.UpdateTurnStatus(ctx, sessionKey, token, input.Message)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionPostResult{}, nil
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_request_agent",
		Description: "Queue another agent role in the current Mattermost thread. Use only when allowed by the role prompt or user request.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpRequestAgentInput) (*mcp.CallToolResult, statusservice.AgentSessionAgentRequest, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionAgentRequest{}, nil
		}
		output, err := sessionService.RequestAgent(ctx, sessionKey, token, input.TargetAgent, input.Message)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionAgentRequest{}, nil
		}
		return nil, output, nil
	})
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionKey := strings.Trim(strings.TrimPrefix(r.URL.Path, pathMCPSessions), "/")
		token := bearerToken(r.Header.Get("Authorization"))
		ctx := context.WithValue(r.Context(), mcpSessionKeyContext, sessionKey)
		ctx = context.WithValue(ctx, mcpTokenContext, token)
		streamable.ServeHTTP(w, r.WithContext(ctx))
	})
}

func mcpSessionAuth(ctx context.Context) (string, string, bool) {
	sessionKey, _ := ctx.Value(mcpSessionKeyContext).(string)
	token, _ := ctx.Value(mcpTokenContext).(string)
	sessionKey = strings.TrimSpace(sessionKey)
	token = strings.TrimSpace(token)
	return sessionKey, token, sessionKey != "" && token != ""
}

func mcpToolError(message string) *mcp.CallToolResult {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "tool failed"
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
		IsError: true,
	}
}
