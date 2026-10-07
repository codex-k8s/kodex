package httptransport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestAssistantTurnInputBoundaryAndUTF8(t *testing.T) {
	for _, test := range []struct {
		name, content string
		valid         bool
	}{
		{"russian-live-size", strings.Repeat("я", 24995), true},
		{"russian-boundary", strings.Repeat("я", 32768), true},
		{"astral-boundary", strings.Repeat("😀", 32768), true},
		{"russian-overflow", strings.Repeat("я", 32769), false},
		{"nul", "bad\x00", false},
		{"blank", " \n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"content": test.content})
			if err != nil {
				t.Fatal(err)
			}
			client := &catalogRPCRecorder{response: &cp.AddAssistantTurnResponse{Conversation: &cp.AssistantConversation{Ref: "cnv_fixture01"}}}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/assistant-conversations/cnv_fixture01/turns", bytes.NewReader(raw))
			r.Header.Set("Idempotency-Key", "turn-input-fixture")
			r.Header.Set("X-CSRF-Token", "fixture-csrf")
			assistantCatalogHandler(client).ServeHTTP(w, r)
			if test.valid {
				if w.Code != http.StatusAccepted || client.method != cp.SystemAssistantService_AddAssistantTurn_FullMethodName || client.request.(*cp.AddAssistantTurnRequest).Content != test.content {
					t.Fatalf("valid turn did not roundtrip: status=%d", w.Code)
				}
			} else if w.Code != http.StatusBadRequest || client.method != "" || strings.Contains(w.Body.String(), test.content) {
				t.Fatal("invalid input reached owner or escaped in error")
			}
		})
	}
	t.Run("raw-invalid-utf8", func(t *testing.T) {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/assistant-conversations/cnv_fixture01/turns", strings.NewReader("{\"content\":\"bad\xff\"}"))
		r.Header.Set("Idempotency-Key", "turn-input-fixture")
		r.Header.Set("X-CSRF-Token", "fixture-csrf")
		assistantCatalogHandler(client).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || client.method != "" {
			t.Fatal("invalid UTF-8 was normalized and sent to owner")
		}
	})
}
