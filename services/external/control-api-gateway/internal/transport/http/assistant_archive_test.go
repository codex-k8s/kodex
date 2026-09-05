package httptransport

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAssistantHistorySearchAndState(t *testing.T) {
	for _, state := range []string{"ACTIVE", "CLOSED", "ARCHIVED"} {
		value := cp.AssistantConversationState(cp.AssistantConversationState_value["ASSISTANT_CONVERSATION_STATE_"+state])
		client := &catalogRPCRecorder{response: &cp.ListAssistantConversationsResponse{Conversations: []*cp.AssistantConversation{{Ref: "conv_fixture01", State: value}}}}
		w := httptest.NewRecorder()
		query := "  Поиск %_  "
		assistantCatalogHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/assistant-conversations?query="+url.QueryEscape(query)+"&state="+state, nil))
		if w.Code != 200 {
			t.Fatalf("history status=%d", w.Code)
		}
		r := client.request.(*cp.ListAssistantConversationsRequest)
		if r.Query != query || r.State != value || !strings.Contains(w.Body.String(), `"state":"`+state+`"`) {
			t.Fatal("history state/search mapping lost")
		}
	}
	for _, query := range []string{"state=UNKNOWN", "state=UNSPECIFIED", "state=", "query=" + url.QueryEscape(strings.Repeat("я", 201)), "query=a%00b", "query=%FF"} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		assistantCatalogHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/assistant-conversations?"+query, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatal("invalid history filter reached producer")
		}
	}
}

func TestAssistantArchiveExactMutationAndErrors(t *testing.T) {
	request := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/v1/assistant-conversations/conv_fixture01/archive", nil)
		r.Header.Set("Idempotency-Key", "archive-fixture")
		r.Header.Set("If-Match", `"3"`)
		r.Header.Set("X-CSRF-Token", "fixture-csrf")
		return r
	}
	response := &cp.ArchiveAssistantConversationResponse{Conversation: &cp.AssistantConversation{Ref: "conv_fixture01", Version: 4, State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ARCHIVED}}
	client := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	assistantCatalogHandler(client).ServeHTTP(w, request())
	if w.Code != 200 || client.method != cp.SystemAssistantService_ArchiveAssistantConversation_FullMethodName || w.Header().Get("ETag") != `"4"` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("archive status=%d", w.Code)
	}
	r := client.request.(*cp.ArchiveAssistantConversationRequest)
	if r.ConversationRef != "conv_fixture01" || r.Mutation.IdempotencyKey != "archive-fixture" || r.Mutation.GetExpectedVersion() != 3 {
		t.Fatal("archive OCC/idempotency lost")
	}
	for _, header := range []string{"Idempotency-Key", "If-Match", "X-CSRF-Token"} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		r := request()
		r.Header.Del(header)
		assistantCatalogHandler(client).ServeHTTP(w, r)
		if w.Code != 400 || client.method != "" {
			t.Fatal("missing mutation header reached CP")
		}
	}
	for code, expected := range map[codes.Code]int{codes.NotFound: 404, codes.PermissionDenied: 403, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.Unavailable: 503} {
		client := &catalogRPCRecorder{failure: status.Error(code, "private owner detail")}
		w := httptest.NewRecorder()
		assistantCatalogHandler(client).ServeHTTP(w, request())
		if w.Code != expected || strings.Contains(w.Body.String(), "private owner") {
			t.Fatalf("archive error status=%d want=%d", w.Code, expected)
		}
	}
	for _, value := range []*cp.AssistantConversation{nil, {Ref: "conv_foreign01", Version: 4, State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ARCHIVED}, {Ref: "conv_fixture01", Version: 4, State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE}, {Ref: "conv_fixture01", Version: maximumSafeJSONInteger + 1, State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ARCHIVED}} {
		client := &catalogRPCRecorder{response: &cp.ArchiveAssistantConversationResponse{Conversation: value}}
		w := httptest.NewRecorder()
		assistantCatalogHandler(client).ServeHTTP(w, request())
		if w.Code != 502 {
			t.Fatal("invalid archive readback accepted")
		}
	}
}
