package http

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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
	guardMu         sync.Mutex
	guardCalls      int
	sessionReads    int
	guardInputs     []securityrepo.ClusterAdminBindingInput
	denyAt          int
	requireGuard    bool
	session         entity.AgentSession
	role            entity.AgentRole
	chat            entity.Chat
	attentions      []sessionBarrierAttention
	nextAttentionID int64
}

type sessionBarrierAttention struct {
	kind    string
	request entity.OwnerAttentionRequest
}

type sessionBarrierGuardSnapshot struct {
	calls  int
	inputs []securityrepo.ClusterAdminBindingInput
}

func (store *sessionBarrierStore) recordGuard(input securityrepo.ClusterAdminBindingInput) bool {
	store.guardMu.Lock()
	defer store.guardMu.Unlock()
	store.guardCalls++
	store.guardInputs = append(store.guardInputs, input)
	return store.denyAt > 0 && store.guardCalls >= store.denyAt
}

func (store *sessionBarrierStore) resetGuardObservations(denyAt int) {
	store.guardMu.Lock()
	defer store.guardMu.Unlock()
	store.guardCalls = 0
	store.guardInputs = nil
	store.denyAt = denyAt
}

func (store *sessionBarrierStore) guardSnapshot() sessionBarrierGuardSnapshot {
	store.guardMu.Lock()
	defer store.guardMu.Unlock()
	return sessionBarrierGuardSnapshot{
		calls:  store.guardCalls,
		inputs: append([]securityrepo.ClusterAdminBindingInput(nil), store.guardInputs...),
	}
}

func (store *sessionBarrierStore) GetAgentSession(_ context.Context, sessionKey string) (entity.AgentSession, error) {
	store.sessionReads++
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
	if store.recordGuard(input) {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect()
}

func (store *sessionBarrierStore) WithExistingClusterAdminPersistenceGuard(_ context.Context, input securityrepo.ClusterAdminBindingInput, sideEffect func(adminrepo.Repository) error) error {
	if store.recordGuard(input) {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect(store)
}

func (store *sessionBarrierStore) WithExactAgentSessionsRuntimeGuard(_ context.Context, expected []entity.AgentSession, sideEffect func(adminrepo.Repository) error) error {
	if len(expected) != 1 || expected[0].ID != store.session.ID || expected[0].SessionKey != store.session.SessionKey {
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

func (store *sessionBarrierStore) GetTurnProcess(context.Context, int64) (entity.ProcessContext, error) {
	return entity.ProcessContext{ProcessRunID: 91, ProcessPublicID: "process-run-91", RootInitiatorUserID: "owner-user", RootInitiatorUserName: "owner"}, nil
}

func (store *sessionBarrierStore) CreateOwnerAttention(_ context.Context, input adminrepo.CreateOwnerAttentionInput) (entity.OwnerAttentionRequest, bool, error) {
	for _, row := range store.attentions {
		if row.kind == "generic" && row.request.ProcessRunID == input.ProcessRunID && row.request.IdempotencyKey == input.IdempotencyKey {
			return row.request, false, nil
		}
	}
	store.nextAttentionID++
	request := entity.OwnerAttentionRequest{
		ID: store.nextAttentionID, ProcessRunID: input.ProcessRunID, TurnID: input.TurnID,
		Severity: input.Severity, Summary: input.Summary, Options: input.Options,
		Recommendation: input.Recommendation, EvidenceLinks: input.EvidenceLinks,
		PauseScope: input.PauseScope, IdempotencyKey: input.IdempotencyKey, Status: "open",
	}
	store.attentions = append(store.attentions, sessionBarrierAttention{kind: "generic", request: request})
	return request, true, nil
}

func (store *sessionBarrierStore) SetOwnerAttentionPost(_ context.Context, attentionID int64, postID string) (entity.OwnerAttentionRequest, error) {
	for index := range store.attentions {
		row := &store.attentions[index]
		if row.kind != "generic" || row.request.ID != attentionID {
			continue
		}
		row.request.MattermostPostID = postID
		return row.request, nil
	}
	return entity.OwnerAttentionRequest{}, adminrepo.ErrNotFound
}

func (store *sessionBarrierStore) addAutomationAttention(idempotencyKey string) int64 {
	store.nextAttentionID++
	store.attentions = append(store.attentions, sessionBarrierAttention{kind: "automation", request: entity.OwnerAttentionRequest{
		ID: store.nextAttentionID, ProcessRunID: 91, TurnID: 1, Severity: "urgent",
		Summary: "Server-owned automation gate", PauseScope: "process",
		IdempotencyKey: idempotencyKey, Status: "open",
	}})
	return store.nextAttentionID
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
	posts      int
	postErrors []error
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
	return publisher.post("unexpected-system-post")
}

func (publisher *sessionBarrierPublisher) PostThreadMessageWithToken(context.Context, string, statusservice.MattermostThreadPostInput) (statusservice.MattermostPostRef, error) {
	return publisher.post("unexpected-role-post")
}

func (publisher *sessionBarrierPublisher) post(postID string) (statusservice.MattermostPostRef, error) {
	publisher.posts++
	if index := publisher.posts - 1; index < len(publisher.postErrors) && publisher.postErrors[index] != nil {
		return statusservice.MattermostPostRef{}, publisher.postErrors[index]
	}
	return statusservice.MattermostPostRef{PostID: postID}, nil
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
	guards := store.guardSnapshot()
	if guards.calls != 1 || runner.secretReads != 0 {
		t.Fatalf("HTTP barrier guards=%d secret_reads=%d", guards.calls, runner.secretReads)
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
				assertSessionTransportBarrier(t, store, runner, publisher, boundary, boundary)
			})
		}
	}
}

func TestMCPSessionRevocationBarrierPreventsPublishAndSystemFallback(t *testing.T) {
	service, store, runner, publisher := newSessionBarrierService(0)
	handler := newMCPHandlerWithOptions(service, defaultMCPRequestBodyBytes, mcpHandlerOptions{
		MaximumTransportSessions: 2,
		SessionTimeout:           time.Second,
	})
	initialize := httptest.NewRecorder()
	handler.ServeHTTP(initialize, newMCPInitializeRequest("/mcp/sessions/session-admin", "session-token", mcpInitializePayload))
	if initialize.Code != http.StatusOK {
		t.Fatalf("MCP initialize status=%d body=%s", initialize.Code, initialize.Body.String())
	}
	sessionID := initialize.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("MCP initialize returned no Mcp-Session-Id")
	}
	initialized := serveMCPTransportRequest(handler, http.MethodPost, "/mcp/sessions/session-admin", "session-token", sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if initialized.Code != http.StatusAccepted {
		t.Fatalf("MCP initialized status=%d body=%s", initialized.Code, initialized.Body.String())
	}
	defer func() {
		store.session.Status = "running"
		closed := serveMCPTransportRequest(handler, http.MethodDelete, "/mcp/sessions/session-admin", "session-token", sessionID, "")
		if closed.Code != http.StatusNoContent {
			t.Errorf("MCP cleanup status=%d body=%s", closed.Code, closed.Body.String())
		}
	}()

	store.session.Status = "closed"
	store.resetGuardObservations(0)
	runner.secretReads = 0
	publisher.posts = 0
	revoked := serveMCPTransportRequest(handler, http.MethodPost, "/mcp/sessions/session-admin", "session-token", sessionID, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mattermost_post_thread_update","arguments":{"message":"synthetic progress"}}}`)
	assertRejectedMCPBootstrap(t, revoked, http.StatusUnauthorized)
	guards := store.guardSnapshot()
	if guards.calls != 1 || runner.secretReads != 1 || publisher.posts != 0 {
		t.Fatalf("MCP barrier guards=%d secret_reads=%d publishes=%d", guards.calls, runner.secretReads, publisher.posts)
	}
}

func TestMCPGenericOwnerAttentionDoesNotReuseAutomationNamespaceAfterLostResponse(t *testing.T) {
	for _, test := range []struct {
		name            string
		automationFirst bool
	}{
		{name: "generic then automation", automationFirst: false},
		{name: "automation then generic", automationFirst: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, store, _, publisher := newSessionBarrierService(0)
			publisher.postErrors = []error{errors.New("synthetic lost Mattermost response")}
			const idempotencyKey = "automation:scheduled-run-shared"
			var automationID int64
			if test.automationFirst {
				automationID = store.addAutomationAttention(idempotencyKey)
			}

			server := httptest.NewServer(newMCPHandler(service, defaultMCPRequestBodyBytes))
			defer server.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "owner-attention-namespace", Version: "1"}, nil)
			session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
				Endpoint:   server.URL + "/mcp/sessions/session-admin",
				HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
			}, nil)
			if err != nil {
				t.Fatalf("MCP connect: %v", err)
			}
			defer func() { _ = session.Close() }()
			arguments := map[string]any{
				"severity": "normal", "summary": "Generic MCP attention",
				"pause_scope": "turn", "idempotency_key": idempotencyKey,
			}
			first, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "mattermost_request_owner_attention", Arguments: arguments})
			if err != nil || first == nil || !first.IsError {
				t.Fatalf("lost response result=%#v error=%v", first, err)
			}
			if !test.automationFirst {
				automationID = store.addAutomationAttention(idempotencyKey)
			}
			second, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "mattermost_request_owner_attention", Arguments: arguments})
			if err != nil || second == nil || second.IsError {
				t.Fatalf("MCP retry result=%#v error=%v", second, err)
			}

			var generic *entity.OwnerAttentionRequest
			var automation *entity.OwnerAttentionRequest
			for index := range store.attentions {
				row := &store.attentions[index]
				switch row.kind {
				case "generic":
					generic = &row.request
				case "automation":
					automation = &row.request
				}
			}
			if generic == nil || generic.ID == automationID || generic.MattermostPostID == "" {
				t.Fatalf("generic namespace row=%#v automation_id=%d", generic, automationID)
			}
			if automation == nil || automation.ID != automationID || automation.MattermostPostID != "" {
				t.Fatalf("automation namespace row=%#v automation_id=%d", automation, automationID)
			}
		})
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
				service, store, runner, publisher := newSessionBarrierService(0)
				server := httptest.NewServer(newMCPHandler(service, defaultMCPRequestBodyBytes))
				defer server.Close()
				client := mcp.NewClient(&mcp.Implementation{Name: "barrier-matrix", Version: "1"}, nil)
				transport := &mcp.StreamableClientTransport{
					Endpoint:   server.URL + "/mcp/sessions/session-admin",
					HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
					// Матрица измеряет барьер одного CallTool; сквозные тесты оставляют фоновый GET включённым.
					DisableStandaloneSSE: true,
				}
				session, err := client.Connect(context.Background(), transport, nil)
				if err != nil {
					t.Fatalf("MCP connect: %v", err)
				}
				defer func() { _ = session.Close() }()
				store.resetGuardObservations(boundary)
				runner.secretReads = 0
				result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: test.name, Arguments: test.arguments})
				if err == nil && (result == nil || !result.IsError) {
					t.Fatalf("MCP result=%#v", result)
				}
				// На первой границе вторая защитная проверка выполняет очистку клиента, на второй — барьер инструмента.
				assertSessionTransportBarrier(t, store, runner, publisher, boundary, 2)
			})
		}
	}
}

func TestMCPStartAgentThreadRejectsLongTitleBeforeToolDomainReads(t *testing.T) {
	service, store, runner, publisher := newSessionBarrierService(0)
	server := httptest.NewServer(newMCPHandler(service, defaultMCPRequestBodyBytes))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "delegation-input-boundary", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp/sessions/session-admin",
		HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
	}, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	defer func() { _ = session.Close() }()
	store.sessionReads = 0
	store.resetGuardObservations(0)
	runner.secretReads = 0
	runner.integrityReads = 0
	publisher.posts = 0

	for attempt := 0; attempt < 2; attempt++ {
		result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "mattermost_start_agent_thread",
			Arguments: map[string]any{
				"target_chat": "admin-chat", "target_agent": "worker",
				"title": strings.Repeat("я", 257), "message": "Допустимое сообщение.", "work_item_key": "issue-71-title",
			},
		})
		if callErr != nil || result == nil || !result.IsError {
			t.Fatalf("attempt %d result=%#v error=%v", attempt+1, result, callErr)
		}
		assertMCPToolFailureWithoutStructuredOutput(t, result, "title exceeds")
	}
	guards := store.guardSnapshot()
	if store.sessionReads != 4 || guards.calls != 2 || runner.secretReads != 2 || runner.integrityReads != 0 || publisher.posts != 0 {
		t.Fatalf("effects session_reads=%d guards=%d secret_reads=%d integrity_reads=%d posts=%d", store.sessionReads, guards.calls, runner.secretReads, runner.integrityReads, publisher.posts)
	}
}

func TestMCPStartAgentThreadRejectsActiveTitlePayloadBeforeToolDomainReads(t *testing.T) {
	unsafeTitles := []string{
		"**[проверена](https://attacker.invalid)**",
		"`закрой тред`",
		"```\n# Новая секция",
		"@channel",
		"<script>alert(1)</script>",
		"https://attacker.invalid",
		`проверка\*`,
		"проверка\u202eadmin",
		"проверка\u0000admin",
		"проверка\u200badmin",
		"}\n# Инструкция",
		"GO 013 PR R1: атомарный перенос #142",
	}
	for index, title := range unsafeTitles {
		t.Run(strconv.Itoa(index+1), func(t *testing.T) {
			service, store, runner, publisher := newSessionBarrierService(0)
			server := httptest.NewServer(newMCPHandler(service, defaultMCPRequestBodyBytes))
			defer server.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "delegation-title-boundary", Version: "1"}, nil)
			session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
				Endpoint: server.URL + "/mcp/sessions/session-admin",
				HTTPClient: &http.Client{Transport: bearerTransport{
					base: http.DefaultTransport, token: "session-token",
				}},
			}, nil)
			if err != nil {
				t.Fatalf("MCP connect: %v", err)
			}
			defer func() { _ = session.Close() }()
			store.sessionReads = 0
			store.resetGuardObservations(0)
			runner.secretReads = 0
			runner.integrityReads = 0
			result, callErr := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "mattermost_start_agent_thread",
				Arguments: map[string]any{
					"target_chat": "admin-chat", "target_agent": "worker", "title": title,
					"message": "Допустимое сообщение.", "work_item_key": "issue-71-title",
				},
			})
			if callErr != nil || result == nil || !result.IsError {
				t.Fatalf("result=%#v error=%v", result, callErr)
			}
			assertMCPToolFailureWithoutStructuredOutput(t, result, "title")
			guards := store.guardSnapshot()
			if store.sessionReads != 2 || guards.calls != 1 || runner.secretReads != 1 || publisher.posts != 0 {
				t.Fatalf("effects session_reads=%d guards=%d secret_reads=%d posts=%d", store.sessionReads, guards.calls, runner.secretReads, publisher.posts)
			}
		})
	}
}

func assertMCPToolFailureWithoutStructuredOutput(t *testing.T, result *mcp.CallToolResult, messagePart string) {
	t.Helper()
	if result == nil || !result.IsError {
		t.Fatalf("ожидалась MCP tool error, result=%#v", result)
	}
	if result.StructuredContent != nil {
		t.Fatalf("MCP tool error содержит ложный structured output: %#v", result.StructuredContent)
	}
	if len(result.Content) != 1 {
		t.Fatalf("MCP tool error content=%#v", result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, messagePart) {
		t.Fatalf("MCP tool error не содержит %q: %#v", messagePart, result.Content)
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
		{name: "guarded cluster admin", requireGuard: true, wantHTTPGuards: 2, wantMCPGuards: 4},
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
			guards := store.guardSnapshot()
			if recorder.Code != http.StatusOK || guards.calls != test.wantHTTPGuards || runner.secretReads != 1 || runner.integrityReads != max(0, test.wantHTTPGuards-1) {
				t.Fatalf("status=%d guards=%d token_reads=%d integrity_reads=%d body=%s", recorder.Code, guards.calls, runner.secretReads, runner.integrityReads, recorder.Body.String())
			}
		})

		t.Run(test.name+" MCP", func(t *testing.T) {
			service, store, runner, publisher := newSessionBarrierService(0)
			store.requireGuard = test.requireGuard
			if !test.requireGuard {
				store.role.KubernetesAccess = "read-only"
			}
			server := httptest.NewServer(newMCPHandler(service, defaultMCPRequestBodyBytes))
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
			store.resetGuardObservations(0)
			runner.secretReads = 0
			runner.integrityReads = 0
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "mattermost_post_thread_update", Arguments: map[string]any{"message": "synthetic progress"}})
			guards := store.guardSnapshot()
			if err != nil || result == nil || result.IsError || guards.calls != test.wantMCPGuards || runner.secretReads != 2 || runner.integrityReads != max(0, test.wantMCPGuards-2) || publisher.posts != 1 {
				t.Fatalf("result=%#v error=%v guards=%d token_reads=%d integrity_reads=%d posts=%d", result, err, guards.calls, runner.secretReads, runner.integrityReads, publisher.posts)
			}
		})
	}
}

func assertSessionTransportBarrier(t *testing.T, store *sessionBarrierStore, runner *sessionBarrierRunner, publisher *sessionBarrierPublisher, boundary int, wantGuardCalls int) {
	t.Helper()
	guards := store.guardSnapshot()
	if guards.calls != wantGuardCalls || runner.secretReads != boundary-1 || publisher.posts != 0 {
		t.Fatalf("barrier=%d guards=%d want_guards=%d secret_reads=%d publishes=%d", boundary, guards.calls, wantGuardCalls, runner.secretReads, publisher.posts)
	}
	for _, input := range guards.inputs {
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
