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

type mcpListChatsInput struct {
	TargetAgent string `json:"target_agent,omitempty" jsonschema:"optional role name or Mattermost @username; when set only chats available to both agents are returned"`
}

type mcpGetChatInput struct {
	Chat string `json:"chat" jsonschema:"project-local chat slug or name"`
}

type mcpStartAgentThreadInput struct {
	TargetChat  string `json:"target_chat" jsonschema:"project-local destination chat slug or name"`
	TargetAgent string `json:"target_agent" jsonschema:"role name or Mattermost @username of the target agent"`
	Title       string `json:"title" jsonschema:"concise title for the new Mattermost thread"`
	Message     string `json:"message" jsonschema:"self-contained task message for the target agent"`
	WorkItemKey string `json:"work_item_key" jsonschema:"stable idempotency key unique within the current source session, for example issue-59-architecture"`
}

type mcpListDelegationsInput struct {
	Limit int `json:"limit" jsonschema:"maximum number of child delegations to return, max 50"`
}

type mcpReturnToRequesterInput struct {
	Message string `json:"message" jsonschema:"self-contained result to return to the immediate requesting agent"`
}

func newMCPHandler(sessionService *statusservice.AgentSessionService) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "matter-codex",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: "Use these tools only for the current Mattermost project. Keep reads small: prefer mattermost_get_thread before mattermost_search_chat. Use mattermost_list_chats and mattermost_get_chat before choosing a cross-chat destination. Use mattermost_start_agent_thread for a new child work thread and mattermost_return_to_requester to return its result to the immediate requesting agent. Use mattermost_list_delegations for concise child-work status. Use mattermost_update_turn_status for concise progress updates; matter-codex keeps the system start/limits/stop-button status message separate. Progress updates are posted as non-triggering thread messages. Use mattermost_post_thread_update only when you intentionally need an additional non-triggering thread message. Use mattermost_request_agent only for another role in the current thread. Delegate only when the user or role prompt allows it.",
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
		Description: "Post a concise non-triggering progress update for the active agent turn in the current Mattermost thread. It does not edit the system start/limits/stop-button status message.",
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
		Name:        "mattermost_list_chats",
		Description: "List project chats available to the current agent. Optionally return only chats also available to a target agent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpListChatsInput) (*mcp.CallToolResult, statusservice.AgentSessionChatCatalog, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionChatCatalog{}, nil
		}
		output, err := sessionService.ListAvailableChats(ctx, sessionKey, token, input.TargetAgent)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionChatCatalog{}, nil
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_get_chat",
		Description: "Get purpose, work policy, repositories, and available agents for one accessible project chat.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpGetChatInput) (*mcp.CallToolResult, statusservice.AgentSessionChatDetails, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionChatDetails{}, nil
		}
		output, err := sessionService.ChatDetails(ctx, sessionKey, token, input.Chat)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionChatDetails{}, nil
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_start_agent_thread",
		Description: "Idempotently create a child thread in another accessible project chat and queue a target agent there.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpStartAgentThreadInput) (*mcp.CallToolResult, statusservice.AgentSessionDelegationResult, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionDelegationResult{}, nil
		}
		output, err := sessionService.StartAgentThread(ctx, sessionKey, token, statusservice.StartAgentThreadCommand{
			TargetChat:  input.TargetChat,
			TargetAgent: input.TargetAgent,
			Title:       input.Title,
			Message:     input.Message,
			WorkItemKey: input.WorkItemKey,
		})
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionDelegationResult{}, nil
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_list_delegations",
		Description: "List child threads and their current turn or callback status for the current agent session.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpListDelegationsInput) (*mcp.CallToolResult, statusservice.AgentSessionDelegationList, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionDelegationList{}, nil
		}
		output, err := sessionService.ListDelegations(ctx, sessionKey, token, input.Limit)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionDelegationList{}, nil
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "mattermost_return_to_requester",
		Description: "Return a child thread result to the immediate requesting agent session. The callback is queued durably and is idempotent.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpReturnToRequesterInput) (*mcp.CallToolResult, statusservice.AgentSessionDelegationResult, error) {
		sessionKey, token, ok := mcpSessionAuth(ctx)
		if !ok {
			return mcpToolError("session authorization is missing"), statusservice.AgentSessionDelegationResult{}, nil
		}
		output, err := sessionService.ReturnToRequester(ctx, sessionKey, token, input.Message)
		if err != nil {
			return mcpToolError(err.Error()), statusservice.AgentSessionDelegationResult{}, nil
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
