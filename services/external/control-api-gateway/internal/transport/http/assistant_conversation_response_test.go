package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestWriteAssistantConversationStatePreservesProjection checks both specialized reply types with typed nil, absent or foreign conversations, unsafe versions and wrong states. Successful output matches the existing public projector exactly and inputs stay unchanged; failures expose no partial conversation.
// TestWriteAssistantConversationStatePreservesProjection проверяет оба отдельных reply types с типизированным nil, отсутствующим и чужим диалогом, unsafe versions и неверными states. Успех точно совпадает с прежним публичным projector, входы не меняются; отказ не выдаёт частичный диалог.
func TestWriteAssistantConversationStatePreservesProjection(t *testing.T) {
	t.Parallel()
	for _, archive := range []bool{false, true} {
		for _, test := range []struct {
			name                                  string
			change                                func(*cp.AssistantConversation)
			nilReply, missingConversation, wantOK bool
		}{
			{name: "valid", wantOK: true},
			{name: "typed nil", nilReply: true},
			{name: "missing conversation", missingConversation: true},
			{name: "foreign reference", change: func(c *cp.AssistantConversation) { c.Ref = "conv_foreign01" }},
			{name: "empty reference", change: func(c *cp.AssistantConversation) { c.Ref = "" }},
			{name: "zero version", change: func(c *cp.AssistantConversation) { c.Version = 0 }},
			{name: "negative version", change: func(c *cp.AssistantConversation) { c.Version = -1 }},
			{name: "unsafe version", change: func(c *cp.AssistantConversation) { c.Version = maximumSafeJSONInteger + 1 }},
			{name: "smallest version", change: func(c *cp.AssistantConversation) { c.Version = 1 }, wantOK: true},
			{name: "largest safe version", change: func(c *cp.AssistantConversation) { c.Version = maximumSafeJSONInteger }, wantOK: true},
			{name: "wrong target", change: func(c *cp.AssistantConversation) {
				if c.State == cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE {
					c.State = cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ARCHIVED
				} else {
					c.State = cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE
				}
			}},
			{name: "unknown state", change: func(c *cp.AssistantConversation) { c.State = cp.AssistantConversationState(99) }},
		} {
			name := "restore/" + test.name
			if archive {
				name = "archive/" + test.name
			}
			t.Run(name, func(t *testing.T) {
				target := cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE
				if archive {
					target = cp.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ARCHIVED
				}
				conversation := &cp.AssistantConversation{Ref: "conv_fixture01", Version: 4, State: target, Title: "literal title"}
				if test.change != nil {
					test.change(conversation)
				}
				if test.missingConversation {
					conversation = nil
				}
				before := proto.Clone(conversation)
				actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
				if archive {
					var reply *cp.ArchiveAssistantConversationResponse
					if !test.nilReply {
						reply = &cp.ArchiveAssistantConversationResponse{Conversation: conversation}
					}
					if test.wantOK {
						writeMessage(expected, http.StatusOK, reply, "conversation", "")
					}
					writeAssistantConversationState(actual, reply, "conv_fixture01", target)
				} else {
					var reply *cp.RestoreAssistantConversationResponse
					if !test.nilReply {
						reply = &cp.RestoreAssistantConversationResponse{Conversation: conversation}
					}
					if test.wantOK {
						writeMessage(expected, http.StatusOK, reply, "conversation", "")
					}
					writeAssistantConversationState(actual, reply, "conv_fixture01", target)
				}
				if !test.wantOK {
					writeLocalProblem(expected, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
				}
				assertReadRPCProjection(t, actual, expected)
				if !proto.Equal(before, conversation) {
					t.Fatal("conversation input changed")
				}
				if test.wantOK && (actual.Code != 200 || actual.Header().Get("Cache-Control") != "no-store" || actual.Header().Get("ETag") == "") {
					t.Fatal("successful reply lost public projection")
				}
				if !test.wantOK && actual.Code != 502 {
					t.Fatal("invalid state reply did not fail closed")
				}
			})
		}
	}
}

// TestAssistantArchiveRestoreKeepSpecializedRPCs verifies both endpoint error paths still use their exact owner methods and suppress private diagnostics for every non-OK gRPC code. Missing mutation headers fail before any RPC; these synthetic route checks never execute a live archive or restore.
// TestAssistantArchiveRestoreKeepSpecializedRPCs проверяет сохранение точных owner methods обоих endpoint и скрытие private diagnostics для каждого non-OK gRPC-кода. Отсутствие mutation headers отклоняется до RPC; synthetic route checks не выполняют live archive и restore.
func TestAssistantArchiveRestoreKeepSpecializedRPCs(t *testing.T) {
	t.Parallel()
	for _, route := range []struct{ action, method string }{
		{action: "archive", method: cp.SystemAssistantService_ArchiveAssistantConversation_FullMethodName},
		{action: "restore", method: cp.SystemAssistantService_RestoreAssistantConversation_FullMethodName},
	} {
		request := func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/assistant-conversations/conv_fixture01/"+route.action, nil)
			r.Header.Set("Idempotency-Key", "conversation-state-fixture")
			r.Header.Set("If-Match", `"3"`)
			r.Header.Set("X-CSRF-Token", "fixture-csrf")
			return r
		}
		for code := codes.Canceled; code <= codes.Unauthenticated; code++ {
			t.Run(route.action+"/"+code.String(), func(t *testing.T) {
				err := status.Error(code, "private owner detail")
				client := &catalogRPCRecorder{failure: err}
				actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
				writeRPCProblem(expected, err)
				assistantCatalogHandler(client).ServeHTTP(actual, request())
				if client.method != route.method {
					t.Fatal("state command used a different owner RPC")
				}
				assertReadRPCProjection(t, actual, expected)
			})
		}
		for _, header := range []string{"Idempotency-Key", "If-Match", "X-CSRF-Token"} {
			t.Run(route.action+"/missing "+header, func(t *testing.T) {
				client := &catalogRPCRecorder{}
				writer := httptest.NewRecorder()
				r := request()
				r.Header.Del(header)
				assistantCatalogHandler(client).ServeHTTP(writer, r)
				if writer.Code != 400 || client.method != "" {
					t.Fatal("missing mutation header reached owner")
				}
			})
		}
	}
}
