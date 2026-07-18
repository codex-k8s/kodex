package http

import (
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
	guardCalls int
	denyAt     int
	session    entity.AgentSession
	role       entity.AgentRole
	chat       entity.Chat
}

func (store *sessionBarrierStore) GetAgentSession(context.Context, string) (entity.AgentSession, error) {
	return store.session, nil
}

func (store *sessionBarrierStore) GetAgentRole(context.Context, int64) (entity.AgentRole, error) {
	return store.role, nil
}

func (store *sessionBarrierStore) GetChat(context.Context, int64) (entity.Chat, error) {
	return store.chat, nil
}

func (store *sessionBarrierStore) RequiresClusterAdminSessionGuard(context.Context, int64, string) (bool, error) {
	return true, nil
}

func (store *sessionBarrierStore) WithExistingClusterAdminRuntimeGuard(_ context.Context, _ securityrepo.ClusterAdminBindingInput, sideEffect func() error) error {
	store.guardCalls++
	if store.guardCalls == store.denyAt {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect()
}

func (store *sessionBarrierStore) WithExistingClusterAdminPersistenceGuard(_ context.Context, _ securityrepo.ClusterAdminBindingInput, sideEffect func(adminrepo.Repository) error) error {
	store.guardCalls++
	if store.guardCalls == store.denyAt {
		return adminrepo.ErrClusterAdminAdmissionDenied
	}
	return sideEffect(store)
}

func (store *sessionBarrierStore) ListClusterAdminSecretIntegrity(context.Context, int64, string) ([]securityrepo.SecretIntegrityBinding, error) {
	return []securityrepo.SecretIntegrityBinding{{
		Kind: "session", SecretRef: "session-secret", SecretKey: "token",
		ContentSHA256: "synthetic-sha256", ResourceUID: "synthetic-uid", ResourceVersion: "1",
	}}, nil
}

type sessionBarrierRunner struct {
	runtimerepo.Runner
	secretReads int
}

func (runner *sessionBarrierRunner) InspectSecretIntegrity(context.Context, runtimerepo.SecretIntegrityInput) (runtimerepo.SecretIntegrity, error) {
	return runtimerepo.SecretIntegrity{ContentSHA256: "synthetic-sha256", UID: "synthetic-uid", ResourceVersion: "1"}, nil
}

func (runner *sessionBarrierRunner) GetMattermostBotTokenSecret(context.Context, string) (runtimerepo.MattermostBotTokenSecret, error) {
	runner.secretReads++
	return runtimerepo.MattermostBotTokenSecret{Token: "session-token"}, nil
}

type sessionBarrierPublisher struct {
	statusservice.MattermostThreadPublisher
	posts int
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
		denyAt: denyAt,
		session: entity.AgentSession{
			ID: 1, SessionKey: "session-admin", ProjectID: 1, ChatID: 1, RoleID: 1,
			MattermostChannelID: "channel-admin", MattermostRootPostID: "root-admin",
			Status: "running", TokenSecretRef: "session-secret", ExpiresAt: now.Add(time.Hour),
		},
		role: entity.AgentRole{ID: 1, ProjectID: 1, Name: "mattercodex-admin", KubernetesAccess: "cluster-admin", Enabled: true},
		chat: entity.Chat{ID: 1, ProjectID: 1, Slug: "admin-chat", MattermostChannelID: "channel-admin"},
	}
	runner := &sessionBarrierRunner{}
	publisher := &sessionBarrierPublisher{}
	service := statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
		Store: store, RuntimeRunner: runner, ThreadPublisher: publisher, StorageReady: true, RuntimeReady: true,
	})
	return service, store, runner, publisher
}
