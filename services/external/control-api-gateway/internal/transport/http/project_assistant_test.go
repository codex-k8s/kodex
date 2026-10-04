package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func projectAssistantFixture() *cp.ProjectAssistantProfile {
	return &cp.ProjectAssistantProfile{Ref: "asstp_fixture01", ProjectRef: "prj_fixture01", AgentRef: "agt_fixture01",
		Name: "Помощник проекта", State: "ACTIVE", Version: 1,
		CreatedAt: timestamppb.New(time.Unix(1791056209, 0)), UpdatedAt: timestamppb.New(time.Unix(1791056209, 0))}
}

func TestProjectAssistantEndpointsPreserveExactScope(t *testing.T) {
	get := &catalogRPCRecorder{response: &cp.GetProjectAssistantResponse{Profile: projectAssistantFixture()}}
	w := httptest.NewRecorder()
	assistantCatalogHandler(get).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/projects/prj_fixture01/assistant", nil))
	if w.Code != http.StatusOK || get.method != cp.SystemAssistantService_GetProjectAssistant_FullMethodName ||
		get.request.(*cp.GetProjectAssistantRequest).ProjectRef != "prj_fixture01" || !strings.Contains(w.Body.String(), `"agentRef":"agt_fixture01"`) {
		t.Fatalf("project assistant read mapping rejected: status=%d", w.Code)
	}
	create := &catalogRPCRecorder{response: &cp.CreateProjectAssistantResponse{Profile: projectAssistantFixture()}}
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/prj_fixture01/assistant", strings.NewReader(`{"name":"Помощник проекта","purpose":"Координация проекта","instructions":"Работай в пределах проекта."}`))
	r.Header.Set("Idempotency-Key", "create-project-assistant-fixture")
	r.Header.Set("X-CSRF-Token", "fixture-csrf")
	assistantCatalogHandler(create).ServeHTTP(w, r)
	if w.Code != http.StatusCreated || create.method != cp.SystemAssistantService_CreateProjectAssistant_FullMethodName {
		t.Fatalf("project assistant create mapping rejected: status=%d", w.Code)
	}
	payload := create.request.(*cp.CreateProjectAssistantRequest)
	if payload.ProjectRef != "prj_fixture01" || payload.Mutation.GetIdempotencyKey() != "create-project-assistant-fixture" ||
		payload.Name != "Помощник проекта" || payload.Instructions != "Работай в пределах проекта." {
		t.Fatal("project assistant create lost its exact input")
	}
}

func TestProjectAssistantEndpointsRejectForeignAndMalformedProfiles(t *testing.T) {
	for name, mutate := range map[string]func(*cp.ProjectAssistantProfile){
		"foreign-project": func(p *cp.ProjectAssistantProfile) { p.ProjectRef = "prj_foreign01" },
		"wrong-kind":      func(p *cp.ProjectAssistantProfile) { p.Ref = "agt_fixture01" },
		"missing-agent":   func(p *cp.ProjectAssistantProfile) { p.AgentRef = "" },
		"unknown-state":   func(p *cp.ProjectAssistantProfile) { p.State = "UNKNOWN" },
		"agent-state":     func(p *cp.ProjectAssistantProfile) { p.State = "DRAFT" },
		"missing-time":    func(p *cp.ProjectAssistantProfile) { p.CreatedAt = nil },
		"unsafe-version":  func(p *cp.ProjectAssistantProfile) { p.Version = maximumSafeJSONInteger + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			profile := projectAssistantFixture()
			mutate(profile)
			client := &catalogRPCRecorder{response: &cp.GetProjectAssistantResponse{Profile: profile}}
			w := httptest.NewRecorder()
			assistantCatalogHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/projects/prj_fixture01/assistant", nil))
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "prj_foreign01") {
				t.Fatal("foreign or malformed profile escaped the boundary")
			}
		})
	}
}

func TestProjectAssistantProfileStatesAreBindingStates(t *testing.T) {
	for _, state := range []string{"ACTIVE", "DISABLED", "ARCHIVED"} {
		profile := projectAssistantFixture()
		profile.State = state
		if !validProjectAssistantProfile(profile, profile.ProjectRef) {
			t.Fatalf("canonical profile state rejected: %s", state)
		}
	}
}

func TestAssistantConversationRequiresExplicitScope(t *testing.T) {
	for _, body := range []string{`{}`, `{"assistantScope":"NONE"}`, `{"assistantScope":"UNKNOWN"}`, `{"assistantScope":"PROJECT"}`} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/assistant-conversations", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", "conversation-scope-fixture")
		r.Header.Set("X-CSRF-Token", "fixture-csrf")
		assistantCatalogHandler(client).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || client.method != "" {
			t.Fatal("ambiguous conversation scope reached the owner")
		}
	}
	for _, scope := range []cp.AssistantScope{cp.AssistantScope_ASSISTANT_SCOPE_SYSTEM, cp.AssistantScope_ASSISTANT_SCOPE_PROJECT} {
		client := &catalogRPCRecorder{response: &cp.CreateAssistantConversationResponse{Conversation: &cp.AssistantConversation{
			Ref: "conv_fixture01", AssistantScope: scope, ProjectRef: "prj_fixture01", AssistantRef: "agt_fixture01", State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE,
		}}}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/assistant-conversations", strings.NewReader(`{"assistantScope":"`+strings.TrimPrefix(scope.String(), "ASSISTANT_SCOPE_")+`","projectRef":"prj_fixture01"}`))
		r.Header.Set("Idempotency-Key", "conversation-scope-fixture")
		r.Header.Set("X-CSRF-Token", "fixture-csrf")
		assistantCatalogHandler(client).ServeHTTP(w, r)
		if w.Code != http.StatusCreated || client.request.(*cp.CreateAssistantConversationRequest).AssistantScope != scope ||
			client.request.(*cp.CreateAssistantConversationRequest).ProjectRef != "prj_fixture01" {
			t.Fatalf("conversation scope mapping rejected: scope=%s status=%d method=%s body=%s", scope, w.Code, client.method, w.Body.String())
		}
	}
}

func TestAssistantHistoryFiltersPinnedProfile(t *testing.T) {
	conversation := &cp.AssistantConversation{Ref: "conv_fixture01", AssistantScope: cp.AssistantScope_ASSISTANT_SCOPE_PROJECT,
		AssistantRef: "agt_fixture01", ProjectRef: "prj_fixture01", State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE}
	client := &catalogRPCRecorder{response: &cp.ListAssistantConversationsResponse{Conversations: []*cp.AssistantConversation{conversation}}}
	w := httptest.NewRecorder()
	assistantCatalogHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/assistant-conversations?assistantScope=PROJECT&assistantRef=agt_fixture01&projectRef=prj_fixture01", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("profile history rejected: status=%d", w.Code)
	}
	payload := client.request.(*cp.ListAssistantConversationsRequest)
	if payload.AssistantScope != cp.AssistantScope_ASSISTANT_SCOPE_PROJECT || payload.AssistantRef != "agt_fixture01" {
		t.Fatal("history profile filter was dropped")
	}
	conversation.AssistantRef = "agt_foreign01"
	w = httptest.NewRecorder()
	assistantCatalogHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/assistant-conversations?assistantScope=PROJECT&assistantRef=agt_fixture01", nil))
	if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "agt_foreign01") {
		t.Fatal("history returned another assistant profile")
	}
}
