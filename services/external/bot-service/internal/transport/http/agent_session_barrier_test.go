package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type sessionBarrierStore struct {
	adminrepo.Repository
	adminrepo.CoordinationRepository
	guardCalls   int
	guardInputs  []securityrepo.ClusterAdminBindingInput
	denyAt       int
	requireGuard bool
	session      entity.AgentSession
	role         entity.AgentRole
	chat         entity.Chat
}

func (store *sessionBarrierStore) GetAgentSession(_ context.Context, sessionKey string) (entity.AgentSession, error) {
	if sessionKey != store.session.SessionKey {
		return entity.AgentSession{}, adminrepo.ErrNotFound
	}
	return store.session, nil
}

func (store *sessionBarrierStore) GetAgentSessionByID(context.Context, int64) (entity.AgentSession, error) {
	return store.session, nil
}

func (store *sessionBarrierStore) GetAgentRole(context.Context, int64) (entity.AgentRole, error) {
	return store.role, nil
}

func (store *sessionBarrierStore) GetChat(context.Context, int64) (entity.Chat, error) {
	return store.chat, nil
}

func (store *sessionBarrierStore) RequiresClusterAdminSessionGuard(context.Context, int64, string) (bool, error) {
	return store.requireGuard, nil
}

func (store *sessionBarrierStore) WithExistingClusterAdminRuntimeGuard(_ context.Context, input securityrepo.ClusterAdminBindingInput, sideEffect func() error) error {
	store.guardCalls++
	store.guardInputs = append(store.guardInputs, input)
	if store.guardCalls == store.denyAt {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect()
}

func (store *sessionBarrierStore) WithExistingClusterAdminPersistenceGuard(_ context.Context, input securityrepo.ClusterAdminBindingInput, sideEffect func(adminrepo.Repository) error) error {
	store.guardCalls++
	store.guardInputs = append(store.guardInputs, input)
	if store.guardCalls == store.denyAt {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect(store)
}

func (store *sessionBarrierStore) GetProject(context.Context, int64) (entity.Project, error) {
	return entity.Project{ID: 1, Slug: "project"}, nil
}

func (store *sessionBarrierStore) ListAgentRoles(context.Context, int64) ([]entity.AgentRole, error) {
	return []entity.AgentRole{
		store.role,
		{ID: 2, ProjectID: store.role.ProjectID, Name: "worker", KubernetesAccess: "read-only", Enabled: true},
	}, nil
}

func (store *sessionBarrierStore) ListMattermostBotIdentitiesByProject(context.Context, int64) ([]entity.MattermostBotIdentity, error) {
	return nil, nil
}

func (store *sessionBarrierStore) GetMattermostBotIdentityByRoleID(context.Context, int64) (entity.MattermostBotIdentity, error) {
	return entity.MattermostBotIdentity{RoleID: store.role.ID, Username: store.role.Name}, nil
}

func (store *sessionBarrierStore) ListChats(context.Context, int64) ([]entity.Chat, error) {
	return []entity.Chat{store.chat}, nil
}

func (store *sessionBarrierStore) ListChatParticipants(context.Context, int64) ([]entity.ChatParticipant, error) {
	return []entity.ChatParticipant{
		{ChatID: store.chat.ID, RoleID: store.role.ID, RoleName: store.role.Name, Enabled: true},
		{ChatID: store.chat.ID, RoleID: 2, RoleName: "worker", Enabled: true},
	}, nil
}

func (store *sessionBarrierStore) ListChatRepositories(context.Context, int64) ([]entity.ChatRepositoryBinding, error) {
	return nil, nil
}

func (store *sessionBarrierStore) ListProjectRepositories(context.Context, int64) ([]entity.ProjectRepository, error) {
	return nil, nil
}

func (store *sessionBarrierStore) GetAgentSessionTurn(context.Context, int64) (entity.AgentSessionTurn, error) {
	return entity.AgentSessionTurn{
		ID: 1, SessionID: store.session.ID, RunID: "run-1", Status: "running",
		MattermostChannelID: store.session.MattermostChannelID, MattermostRootPostID: store.session.MattermostRootPostID,
	}, nil
}

func (store *sessionBarrierStore) ListAgentDelegationsBySource(context.Context, int64, int) ([]entity.AgentDelegation, error) {
	return nil, nil
}

func (store *sessionBarrierStore) IsRoleCapabilityAllowed(context.Context, int64, int64, int64, string) (bool, error) {
	return true, nil
}

func (store *sessionBarrierStore) IsRoleRelationshipAllowed(context.Context, int64, int64, int64, string, int64) (bool, error) {
	return true, nil
}

func (store *sessionBarrierStore) ListClusterAdminSecretIntegrity(context.Context, int64, string) ([]securityrepo.SecretIntegrityBinding, error) {
	return []securityrepo.SecretIntegrityBinding{{
		Kind: "session", SecretRef: "session-secret", SecretKey: "token",
		ContentSHA256: "synthetic-sha256", ResourceUID: "synthetic-uid", ResourceVersion: "1",
	}}, nil
}

type sessionBarrierRunner struct {
	runtimerepo.Runner
	secretReads    int
	integrityReads int
}

func (runner *sessionBarrierRunner) InspectSecretIntegrity(context.Context, runtimerepo.SecretIntegrityInput) (runtimerepo.SecretIntegrity, error) {
	runner.integrityReads++
	return runtimerepo.SecretIntegrity{ContentSHA256: "synthetic-sha256", UID: "synthetic-uid", ResourceVersion: "1"}, nil
}

func (runner *sessionBarrierRunner) GetMattermostBotTokenSecret(context.Context, string) (runtimerepo.MattermostBotTokenSecret, error) {
	runner.secretReads++
	return runtimerepo.MattermostBotTokenSecret{
		Token: "session-token",
		Integrity: runtimerepo.SecretIntegrity{
			SecretName: "session-secret", SecretKey: "token", ContentSHA256: "synthetic-sha256", UID: "synthetic-uid", ResourceVersion: "1",
		},
	}, nil
}

type sessionBarrierPublisher struct {
	statusservice.MattermostThreadPublisher
	posts int
}

type sessionBarrierConversationReader struct{}

func (sessionBarrierConversationReader) GetThreadPosts(context.Context, string, int) ([]statusservice.MattermostPostMessage, error) {
	return nil, nil
}

func (sessionBarrierConversationReader) SearchChannelPosts(context.Context, string, string, int) ([]statusservice.MattermostPostMessage, error) {
	return nil, nil
}

type sessionBarrierDispatcher struct {
	calls int
}

func (dispatcher *sessionBarrierDispatcher) EnqueueAgentTurn(context.Context, statusservice.AgentTurnRequest) (statusservice.AgentTurnQueued, error) {
	dispatcher.calls++
	return statusservice.AgentTurnQueued{}, nil
}

func (dispatcher *sessionBarrierDispatcher) RetryAgentTurn(context.Context, statusservice.AgentTurnRetryRequest) (statusservice.AgentTurnQueued, error) {
	dispatcher.calls++
	return statusservice.AgentTurnQueued{}, nil
}

func (publisher *sessionBarrierPublisher) PostThreadMessage(context.Context, statusservice.MattermostThreadPostInput) (statusservice.MattermostPostRef, error) {
	publisher.posts++
	return statusservice.MattermostPostRef{PostID: "unexpected-system-post"}, nil
}

func (publisher *sessionBarrierPublisher) PostThreadMessageWithToken(context.Context, string, statusservice.MattermostThreadPostInput) (statusservice.MattermostPostRef, error) {
	publisher.posts++
	return statusservice.MattermostPostRef{PostID: "unexpected-role-post"}, nil
}

func TestInternalAgentSessionHTTPRevocationBarrierPreventsTokenRead(t *testing.T) {
	service, store, runner, _ := newSessionBarrierService(1)
	router := NewRouter(RouterConfig{SessionService: service})
	request := httptest.NewRequest(http.MethodGet, "/internal/agent-sessions/session-admin/snapshot", nil)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.guardCalls != 1 || runner.secretReads != 0 {
		t.Fatalf("HTTP barrier guards=%d secret_reads=%d", store.guardCalls, runner.secretReads)
	}
}

func TestInternalAgentSessionProductionTransportBarrierMatrix(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "snapshot", method: http.MethodGet, path: "snapshot"},
		{name: "claim", method: http.MethodPost, path: "turns/claim"},
		{name: "complete", method: http.MethodPost, path: "turns/complete", body: `{"turn_id":1,"status":"succeeded"}`},
		{name: "status", method: http.MethodPost, path: "turns/status", body: `{"run_id":"run-1","phase":"running"}`},
	}
	for _, boundary := range []int{1, 2} {
		for _, test := range tests {
			t.Run(test.name+" boundary "+string(rune('0'+boundary)), func(t *testing.T) {
				service, store, runner, publisher := newSessionBarrierService(boundary)
				router := NewRouter(RouterConfig{SessionService: service})
				request := httptest.NewRequest(test.method, "/internal/agent-sessions/session-admin/"+test.path, bytes.NewBufferString(test.body))
				request.Header.Set("Authorization", "Bearer session-token")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if recorder.Code < http.StatusBadRequest {
					t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
				}
				assertSessionTransportBarrier(t, store, runner, publisher, boundary)
			})
		}
	}
}

func TestMCPSessionRevocationBarrierPreventsPublishAndSystemFallback(t *testing.T) {
	service, store, runner, publisher := newSessionBarrierService(2)
	server := httptest.NewServer(newMCPHandler(service))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "barrier-test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp/sessions/session-admin",
		HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "mattermost_post_thread_update", Arguments: map[string]any{"message": "synthetic progress"},
	})
	if err != nil {
		t.Fatalf("MCP call: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("MCP result=%#v", result)
	}
	if store.guardCalls != 2 || runner.secretReads != 1 || publisher.posts != 0 {
		t.Fatalf("MCP barrier guards=%d secret_reads=%d publishes=%d", store.guardCalls, runner.secretReads, publisher.posts)
	}
}

func TestMCPSessionProductionTransportBarrierMatrix(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
	}{
		{name: "mattermost_get_thread", arguments: map[string]any{"limit": 10}},
		{name: "mattermost_search_chat", arguments: map[string]any{"query": "synthetic", "limit": 10}},
		{name: "mattermost_post_thread_update", arguments: map[string]any{"message": "synthetic progress"}},
		{name: "mattermost_update_turn_status", arguments: map[string]any{"message": "synthetic status"}},
		{name: "mattermost_list_chats", arguments: map[string]any{}},
		{name: "mattermost_get_chat", arguments: map[string]any{"chat": "admin-chat"}},
		{name: "mattermost_start_agent_thread", arguments: map[string]any{"target_chat": "admin-chat", "target_agent": "worker", "title": "Synthetic", "message": "Synthetic task", "work_item_key": "synthetic-work"}},
		{name: "mattermost_list_delegations", arguments: map[string]any{"limit": 10}},
		{name: "mattermost_return_to_requester", arguments: map[string]any{"message": "synthetic result"}},
		{name: "mattermost_request_agent", arguments: map[string]any{"target_agent": "worker", "message": "synthetic task"}},
		{name: "mattermost_request_sync", arguments: map[string]any{"target_agent": "worker", "message": "synthetic sync"}},
		{name: "mattermost_memory_search", arguments: map[string]any{"query": "synthetic", "limit": 10}},
		{name: "mattermost_memory_remember", arguments: map[string]any{"scope": "role", "title": "Synthetic", "content": "Durable synthetic fact"}},
		{name: "mattermost_list_active_work", arguments: map[string]any{"limit": 10}},
		{name: "mattermost_update_work_context", arguments: map[string]any{"summary": "Synthetic work"}},
		{name: "mattermost_request_owner_attention", arguments: map[string]any{"severity": "normal", "summary": "Synthetic question", "idempotency_key": "synthetic-attention"}},
	}
	for _, boundary := range []int{1, 2} {
		for _, test := range tests {
			t.Run(test.name+" boundary "+string(rune('0'+boundary)), func(t *testing.T) {
				service, store, runner, publisher := newSessionBarrierService(boundary)
				server := httptest.NewServer(newMCPHandler(service))
				defer server.Close()
				client := mcp.NewClient(&mcp.Implementation{Name: "barrier-matrix", Version: "1"}, nil)
				transport := &mcp.StreamableClientTransport{
					Endpoint:   server.URL + "/mcp/sessions/session-admin",
					HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
				}
				session, err := client.Connect(context.Background(), transport, nil)
				if err != nil {
					t.Fatalf("MCP connect: %v", err)
				}
				defer func() { _ = session.Close() }()
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: test.name, Arguments: test.arguments})
				if err != nil {
					t.Fatalf("MCP call: %v", err)
				}
				if result == nil || !result.IsError {
					t.Fatalf("MCP result=%#v", result)
				}
				assertSessionTransportBarrier(t, store, runner, publisher, boundary)
			})
		}
	}
}

func TestSessionProductionTransportAllowedControls(t *testing.T) {
	for _, test := range []struct {
		name           string
		requireGuard   bool
		wantHTTPGuards int
		wantMCPGuards  int
	}{
		{name: "ordinary"},
		{name: "guarded cluster admin", requireGuard: true, wantHTTPGuards: 2, wantMCPGuards: 3},
	} {
		t.Run(test.name+" HTTP", func(t *testing.T) {
			service, store, runner, _ := newSessionBarrierService(0)
			store.requireGuard = test.requireGuard
			if !test.requireGuard {
				store.role.KubernetesAccess = "read-only"
			}
			router := NewRouter(RouterConfig{SessionService: service})
			request := httptest.NewRequest(http.MethodGet, "/internal/agent-sessions/session-admin/snapshot", nil)
			request.Header.Set("Authorization", "Bearer session-token")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK || store.guardCalls != test.wantHTTPGuards || runner.secretReads != 1 || runner.integrityReads != max(0, test.wantHTTPGuards-1) {
				t.Fatalf("status=%d guards=%d token_reads=%d integrity_reads=%d body=%s", recorder.Code, store.guardCalls, runner.secretReads, runner.integrityReads, recorder.Body.String())
			}
		})

		t.Run(test.name+" MCP", func(t *testing.T) {
			service, store, runner, publisher := newSessionBarrierService(0)
			store.requireGuard = test.requireGuard
			if !test.requireGuard {
				store.role.KubernetesAccess = "read-only"
			}
			server := httptest.NewServer(newMCPHandler(service))
			defer server.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "allowed-control", Version: "1"}, nil)
			session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
				Endpoint:   server.URL + "/mcp/sessions/session-admin",
				HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
			}, nil)
			if err != nil {
				t.Fatalf("MCP connect: %v", err)
			}
			defer func() { _ = session.Close() }()
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "mattermost_post_thread_update", Arguments: map[string]any{"message": "synthetic progress"}})
			if err != nil || result == nil || result.IsError || store.guardCalls != test.wantMCPGuards || runner.secretReads != 1 || runner.integrityReads != max(0, test.wantMCPGuards-1) || publisher.posts != 1 {
				t.Fatalf("result=%#v error=%v guards=%d token_reads=%d integrity_reads=%d posts=%d", result, err, store.guardCalls, runner.secretReads, runner.integrityReads, publisher.posts)
			}
		})
	}
}

func assertSessionTransportBarrier(t *testing.T, store *sessionBarrierStore, runner *sessionBarrierRunner, publisher *sessionBarrierPublisher, boundary int) {
	t.Helper()
	if store.guardCalls != boundary || runner.secretReads != boundary-1 || publisher.posts != 0 {
		t.Fatalf("barrier=%d guards=%d secret_reads=%d publishes=%d", boundary, store.guardCalls, runner.secretReads, publisher.posts)
	}
	for _, input := range store.guardInputs {
		if input.SessionKey != "session-admin" || input.RoleID != 1 || input.ProjectID != 1 || input.ChatID != 1 || input.MattermostChannelID != "channel-admin" {
			t.Fatalf("guard subject=%#v", input)
		}
	}
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (transport bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+transport.token)
	return transport.base.RoundTrip(clone)
}

func newSessionBarrierService(denyAt int) (*statusservice.AgentSessionService, *sessionBarrierStore, *sessionBarrierRunner, *sessionBarrierPublisher) {
	now := time.Now().UTC()
	store := &sessionBarrierStore{
		denyAt: denyAt, requireGuard: true,
		session: entity.AgentSession{
			ID: 1, SessionKey: "session-admin", ProjectID: 1, ChatID: 1, RoleID: 1,
			MattermostChannelID: "channel-admin", MattermostRootPostID: "root-admin",
			Status: "running", ActiveTurnID: 1, ActiveRunID: "run-1", TokenSecretRef: "session-secret", ExpiresAt: now.Add(time.Hour),
		},
		role: entity.AgentRole{ID: 1, ProjectID: 1, Name: "mattercodex-admin", KubernetesAccess: "cluster-admin", Enabled: true},
		chat: entity.Chat{ID: 1, ProjectID: 1, Slug: "admin-chat", MattermostChannelID: "channel-admin"},
	}
	runner := &sessionBarrierRunner{}
	publisher := &sessionBarrierPublisher{}
	dispatcher := &sessionBarrierDispatcher{}
	service := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
		Store: store, RuntimeRunner: runner, ThreadPublisher: publisher, ConversationReader: sessionBarrierConversationReader{},
		TurnDispatcher: dispatcher, StorageReady: true, RuntimeReady: true,
	})
	return service, store, runner, publisher
}
