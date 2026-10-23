package httptransport

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/usertext"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// assistantSearchRecorder wraps an in-memory HTTP recorder with a deterministic default-title localizer for history-search query tests; it does not replace owner catalog filtering.
// assistantSearchRecorder дополняет in-memory HTTP recorder детерминированной локализацией исходного имени для query-тестов истории; owner catalog filtering не подменяет.
type assistantSearchRecorder struct {
	*httptest.ResponseRecorder
	defaultTitle string
}

// Localize substitutes only the new-conversation message ID and preserves every other ID, so tests observe whether default-title matching was requested.
// Localize подставляет только message ID нового диалога и сохраняет остальные ID, чтобы тест видел запрос сопоставления исходного имени.
func (recorder *assistantSearchRecorder) Localize(messageID string) string {
	if messageID == "NEW_ASSISTANT_CONVERSATION" {
		return recorder.defaultTitle
	}
	return messageID
}

// TestAssistantHistorySearchAndState checks real locale catalog labels, literal query/state mapping and localized default-title matches, then rejects invalid filters before owner RPC. It exercises a synthetic HTTP handler, not live conversation storage or browser search.
// TestAssistantHistorySearchAndState проверяет реальные locale labels каталога, literal query/state mapping и совпадения локализованного исходного имени, затем отклоняет неверные фильтры до owner RPC. Использует synthetic HTTP handler, не live хранилище диалогов и браузерный поиск.
func TestAssistantHistorySearchAndState(t *testing.T) {
	texts, err := usertext.New()
	if err != nil {
		t.Fatal(err)
	}
	for locale, expected := range map[string]string{"ru": "Новый диалог", "en": "New conversation"} {
		if actual := texts.Localize(locale, "NEW_ASSISTANT_CONVERSATION", nil); actual != expected {
			t.Fatalf("default assistant title for %s=%q", locale, actual)
		}
	}
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
	for _, test := range []struct {
		name, title, query string
	}{
		{name: "russian", title: "Новый диалог", query: "новый"},
		{name: "english", title: "New conversation", query: "CONVERS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &catalogRPCRecorder{response: &cp.ListAssistantConversationsResponse{}}
			writer := &assistantSearchRecorder{ResponseRecorder: httptest.NewRecorder(), defaultTitle: test.title}
			assistantCatalogHandler(client).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/assistant-conversations?query="+url.QueryEscape(test.query), nil))
			request := client.request.(*cp.ListAssistantConversationsRequest)
			if writer.Code != http.StatusOK || request.Query != test.query || !request.MatchLocalizedDefaultTitle {
				t.Fatal("localized default assistant title search was not preserved")
			}
		})
	}
	client := &catalogRPCRecorder{response: &cp.ListAssistantConversationsResponse{}}
	writer := &assistantSearchRecorder{ResponseRecorder: httptest.NewRecorder(), defaultTitle: "New conversation"}
	assistantCatalogHandler(client).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/assistant-conversations?query=unrelated", nil))
	if client.request.(*cp.ListAssistantConversationsRequest).MatchLocalizedDefaultTitle {
		t.Fatal("unrelated assistant search expanded to the default title")
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

// TestAssistantArchiveExactMutationAndErrors pins the archive RPC, OCC/idempotency and public ETag/no-store response; missing mutation headers, owner errors and inconsistent identity/version/state fail without exposing private diagnostics. It does not execute the owner transaction or claim archival completion in a live environment.
// TestAssistantArchiveExactMutationAndErrors закрепляет archive RPC, OCC/idempotency и публичный ответ ETag/no-store; отсутствие mutation headers, owner errors и несогласованные identity/version/state отклоняются без private diagnostics. Owner transaction не исполняется, live завершение архивации не утверждается.
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

// TestAssistantTrashRestoreAndPurgeExactMutations preserves distinct restore/purge RPCs and exact mutation pins, including restore ETag and a bodyless purge 204. In-memory replies prove transport mapping, not storage cleanup or recovery of a real archived conversation.
// TestAssistantTrashRestoreAndPurgeExactMutations сохраняет разные restore/purge RPC и точные mutation pins, включая restore ETag и purge 204 без тела. In-memory ответы доказывают transport mapping, не очистку хранилища и восстановление реального диалога.
func TestAssistantTrashRestoreAndPurgeExactMutations(t *testing.T) {
	mutationRequest := func(method, path, key string) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("If-Match", `"4"`)
		r.Header.Set("X-CSRF-Token", "fixture-csrf")
		return r
	}

	restored := &cp.AssistantConversation{Ref: "conv_fixture01", Version: 5, State: cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE}
	client := &catalogRPCRecorder{response: &cp.RestoreAssistantConversationResponse{Conversation: restored}}
	w := httptest.NewRecorder()
	assistantCatalogHandler(client).ServeHTTP(w, mutationRequest(http.MethodPost, "/api/v1/assistant-conversations/conv_fixture01/restore", "restore-fixture"))
	if w.Code != http.StatusOK || client.method != cp.SystemAssistantService_RestoreAssistantConversation_FullMethodName || w.Header().Get("ETag") != `"5"` {
		t.Fatalf("restore status=%d method=%s", w.Code, client.method)
	}
	restore := client.request.(*cp.RestoreAssistantConversationRequest)
	if restore.ConversationRef != "conv_fixture01" || restore.Mutation.IdempotencyKey != "restore-fixture" || restore.Mutation.GetExpectedVersion() != 4 {
		t.Fatal("restore OCC/idempotency lost")
	}

	client = &catalogRPCRecorder{response: &cp.PurgeAssistantConversationResponse{ConversationRef: "conv_fixture01"}}
	w = httptest.NewRecorder()
	assistantCatalogHandler(client).ServeHTTP(w, mutationRequest(http.MethodDelete, "/api/v1/assistant-conversations/conv_fixture01", "purge-fixture"))
	if w.Code != http.StatusNoContent || client.method != cp.SystemAssistantService_PurgeAssistantConversation_FullMethodName || w.Body.Len() != 0 {
		t.Fatalf("purge status=%d method=%s body=%q", w.Code, client.method, w.Body.String())
	}
	purge := client.request.(*cp.PurgeAssistantConversationRequest)
	if purge.ConversationRef != "conv_fixture01" || purge.Mutation.IdempotencyKey != "purge-fixture" || purge.Mutation.GetExpectedVersion() != 4 {
		t.Fatal("purge OCC/idempotency lost")
	}
}
