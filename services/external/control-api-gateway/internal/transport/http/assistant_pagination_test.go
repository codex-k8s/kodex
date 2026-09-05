package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func assistantCatalogHandler(client *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Assistant: cp.NewSystemAssistantServiceClient(client)}})
}

func TestAssistantHistoryPreservesPagination(t *testing.T) {
	client := &catalogRPCRecorder{response: &cp.ListAssistantConversationsResponse{Conversations: []*cp.AssistantConversation{{Ref: "conv_fixture01", ProjectRef: "prj_fixture01", State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE}}, Page: &cp.PageInfo{NextPageToken: "opaque-next"}}}
	w := httptest.NewRecorder()
	assistantCatalogHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/assistant-conversations?projectRef=prj_fixture01&pageSize=20&pageToken=opaque-first", nil))
	if w.Code != 200 || client.method != cp.SystemAssistantService_ListAssistantConversations_FullMethodName {
		t.Fatalf("unexpected history status: %d", w.Code)
	}
	r := client.request.(*cp.ListAssistantConversationsRequest)
	if r.ProjectRef != "prj_fixture01" || r.Page.PageSize != 20 || r.Page.PageToken != "opaque-first" {
		t.Fatal("history filter/cursor lost")
	}
	var result struct {
		Items         []struct{ Ref string }
		NextPageToken string
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.NextPageToken != "opaque-next" {
		t.Fatal("history page lost")
	}
}

func TestAssistantHistoryRejectsMalformedPages(t *testing.T) {
	for _, query := range []string{"pageSize=0", "pageSize=101", "pageToken=" + strings.Repeat("x", 513)} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		assistantCatalogHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/assistant-conversations?"+query, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatal("malformed history page reached RPC")
		}
	}
	for _, response := range []*cp.ListAssistantConversationsResponse{
		{Conversations: []*cp.AssistantConversation{{Ref: "conv_fixture01", ProjectRef: "prj_foreign01"}}},
		{Conversations: []*cp.AssistantConversation{{Ref: "bad", ProjectRef: "prj_fixture01"}}},
		{Page: &cp.PageInfo{NextPageToken: strings.Repeat("x", 513)}},
	} {
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		assistantCatalogHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/assistant-conversations?projectRef=prj_fixture01", nil))
		if w.Code != 502 || strings.Contains(w.Body.String(), "prj_foreign01") {
			t.Fatal("corrupt history returned")
		}
	}
}
