package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestManagedConsumerSelectionHTTP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, selection string
		valid, absent   bool
	}{
		{"absent", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":true}`, true, true},
		{"match-omitted", `{"kind":"STT_SERVICE","ref":"stt-tts-service","revisionRef":"mrev_previous","version":7}`, true, false},
		{"match-false", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":false,"revisionRef":"mrev_previous","version":7}`, true, false},
		{"missing-pins", `{"kind":"STT_SERVICE","ref":"stt-tts-service"}`, false, false},
		{"false-missing-pins", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":false}`, false, false},
		{"absent-revision", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":true,"revisionRef":"mrev_previous"}`, false, false},
		{"absent-empty-revision", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":true,"revisionRef":""}`, false, false},
		{"absent-null-revision", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":true,"revisionRef":null}`, false, false},
		{"absent-zero-version", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":true,"version":0}`, false, false},
		{"absent-null-version", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":true,"version":null}`, false, false},
		{"absent-target-pins", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":true,"revisionRef":"mrev_fixture01","version":1}`, false, false},
		{"null-flag", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":null,"revisionRef":"mrev_previous","version":7}`, false, false},
		{"string-flag", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":"true"}`, false, false},
		{"missing-revision", `{"kind":"STT_SERVICE","ref":"stt-tts-service","version":7}`, false, false},
		{"missing-version", `{"kind":"STT_SERVICE","ref":"stt-tts-service","revisionRef":"mrev_previous"}`, false, false},
		{"zero-version", `{"kind":"STT_SERVICE","ref":"stt-tts-service","revisionRef":"mrev_previous","version":0}`, false, false},
		{"unsafe-version", `{"kind":"STT_SERVICE","ref":"stt-tts-service","revisionRef":"mrev_previous","version":9007199254740992}`, false, false},
		{"invalid-kind", `{"kind":"UNKNOWN","ref":"stt-tts-service","expectedAbsent":true}`, false, false},
		{"missing-identity", `{"kind":"STT_SERVICE","expectedAbsent":true}`, false, false},
		{"unknown-field", `{"kind":"STT_SERVICE","ref":"stt-tts-service","expectedAbsent":true,"actor":"owner"}`, false, false},
	}
	for _, route := range []string{"prompt-template-configurations", "integration-definition-configurations", "system-stt-configurations"} {
		for _, test := range cases {
			t.Run(route+"/"+test.name, func(t *testing.T) {
				client := &managedRPCRecorder{}
				body := `{"impactDigest":"` + strings.Repeat("b", 64) + `","consumers":[` + test.selection + `]}`
				response := httptest.NewRecorder()
				managedTestHandler(client).ServeHTTP(response, managedTestRequest(http.MethodPost, "/api/v1/"+route+"/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings", body))
				if !test.valid {
					if response.Code != http.StatusBadRequest || client.request != nil {
						t.Fatalf("invalid selection reached RPC: status=%d", response.Code)
					}
					return
				}
				if response.Code != http.StatusOK {
					t.Fatalf("valid selection status=%d", response.Code)
				}
				request := client.request.(interface {
					GetConsumers() []*cp.ManagedConfigurationConsumer
					GetMutation() *cp.MutationContext
					GetImpactDigest() string
				})
				if request.GetMutation().GetExpectedVersion() != 3 || request.GetImpactDigest() != strings.Repeat("b", 64) || len(request.GetConsumers()) != 1 {
					t.Fatal("configuration OCC or impact pin changed")
				}
				consumer := request.GetConsumers()[0]
				if consumer.GetExpectedAbsent() != test.absent || consumer.GetKind() != "STT_SERVICE" || consumer.GetRef() != "stt-tts-service" {
					t.Fatal("consumer identity or expectation changed")
				}
				if test.absent && (consumer.GetRevisionRef() != "" || consumer.GetVersion() != 0) {
					t.Fatal("absence acquired synthetic pins")
				}
				if !test.absent && (consumer.GetRevisionRef() != "mrev_previous" || consumer.GetVersion() != 7) {
					t.Fatal("previous binding pins changed")
				}
			})
		}
		t.Run(route+"/owner-conflict", func(t *testing.T) {
			client := &managedRPCRecorder{failure: status.Error(codes.Aborted, "conflict")}
			response := httptest.NewRecorder()
			body := `{"impactDigest":"` + strings.Repeat("b", 64) + `","consumers":[` + cases[0].selection + `]}`
			managedTestHandler(client).ServeHTTP(response, managedTestRequest(http.MethodPost, "/api/v1/"+route+"/mcfg_fixture01/revisions/mrev_fixture01/consumer-bindings", body))
			if response.Code != http.StatusPreconditionFailed || client.request == nil {
				t.Fatal("owner CAS conflict was not propagated as 412")
			}
		})
	}
}

func TestManagedConsumerReadDTOUnchanged(t *testing.T) {
	t.Parallel()
	value, err := managedConsumerView(&cp.ManagedConfigurationConsumer{Kind: "STT_SERVICE", Ref: "stt-tts-service", RevisionRef: "mrev_previous", Version: 7})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil || strings.Contains(string(raw), "expectedAbsent") {
		t.Fatal("write expectation leaked into read DTO")
	}
}
