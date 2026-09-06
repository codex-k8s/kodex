package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func emailCredentialRequest(client *catalogRPCRecorder, body, etag, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("PUT", "/api/v1/integration-connections/conn_fixture01/email-mailbox/credential", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("If-Match", etag)
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("X-CSRF-Token", strings.Repeat("c", 43))
	w := httptest.NewRecorder()
	generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}}).ServeHTTP(w, r)
	return w
}

func emailCredentialFixture() *cp.ConfigureEmailMailboxCredentialResponse {
	return &cp.ConfigureEmailMailboxCredentialResponse{Credential: &cp.EmailMailboxCredential{Name: "email-fixture01", Generation: 3, ConnectionVersion: 3, ConnectionRef: "conn_fixture01", Kind: cp.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_AUTH_SECRET}}
}

func TestEmailCredentialTypedWriteOnly(t *testing.T) {
	for _, kind := range []string{"CA_CERTIFICATE", "USERNAME", "AUTH_SECRET"} {
		r := emailCredentialFixture()
		r.Credential.Kind = cp.EmailMailboxCredentialKind(cp.EmailMailboxCredentialKind_value["EMAIL_MAILBOX_CREDENTIAL_KIND_"+kind])
		client := &catalogRPCRecorder{response: r}
		body, _ := json.Marshal(map[string]string{"kind": kind, "value": " fixture-value "})
		w := emailCredentialRequest(client, string(body), `"2"`, "fixture-email-credential-01")
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", kind, w.Code, w.Body.String())
		}
		q := client.request.(*cp.ConfigureEmailMailboxCredentialRequest)
		if string(q.CredentialValue) != " fixture-value " || q.Kind != r.Credential.Kind || q.ConnectionRef != "conn_fixture01" || q.Mutation.GetExpectedVersion() != 2 || q.Mutation.IdempotencyKey != "fixture-email-credential-01" {
			t.Fatal("typed input or significant whitespace lost")
		}
		if client.method != cp.PlatformCommandService_ConfigureEmailMailboxCredential_FullMethodName || w.Header().Get("ETag") != `"3"` || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("method/OCC/cache mismatch")
		}
		var view map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if len(view) != 5 || view["name"] != "email-fixture01" || view["kind"] != kind || strings.Contains(w.Body.String(), "fixture-value") {
			t.Fatal("unsafe or incomplete view")
		}
	}
}

func TestEmailCredentialRejectsInputBeforeRPC(t *testing.T) {
	for _, tc := range []struct{ kind, value string }{
		{"UNKNOWN", "x"}, {"AUTH_SECRET", ""}, {"AUTH_SECRET", "a\nb"}, {"USERNAME", "a\x00b"},
		{"USERNAME", strings.Repeat("я", 161)}, {"AUTH_SECRET", strings.Repeat("x", 16385)}, {"CA_CERTIFICATE", strings.Repeat("x", 65537)},
	} {
		body, _ := json.Marshal(map[string]string{"kind": tc.kind, "value": tc.value})
		client := &catalogRPCRecorder{}
		w := emailCredentialRequest(client, string(body), `"2"`, "fixture-email-credential-01")
		if w.Code != 400 || client.method != "" {
			t.Fatalf("%s: %d", tc.kind, w.Code)
		}
	}
	for _, tc := range []struct{ body, etag, key string }{
		{`{"kind":"AUTH_SECRET","value":"x","actorRef":"forged"}`, `"2"`, "fixture-email-credential-01"},
		{`{"kind":"AUTH_SECRET","value":"x"}`, "", "fixture-email-credential-01"},
		{`{"kind":"AUTH_SECRET","value":"x"}`, `"2"`, ""},
		{`{"kind":"AUTH_SECRET","value":"x"}`, `"9007199254740991"`, "fixture-email-credential-01"},
	} {
		client := &catalogRPCRecorder{}
		w := emailCredentialRequest(client, tc.body, tc.etag, tc.key)
		if w.Code < 400 || client.method != "" {
			t.Fatalf("invalid mutation accepted: %d", w.Code)
		}
	}
}

func TestEmailCredentialRejectsReadback(t *testing.T) {
	for _, mutate := range []func(*cp.ConfigureEmailMailboxCredentialResponse){
		func(r *cp.ConfigureEmailMailboxCredentialResponse) { r.Credential = nil },
		func(r *cp.ConfigureEmailMailboxCredentialResponse) { r.Credential.ConnectionRef = "conn_other01" },
		func(r *cp.ConfigureEmailMailboxCredentialResponse) { r.Credential.Kind = 1 },
		func(r *cp.ConfigureEmailMailboxCredentialResponse) { r.Credential.ConnectionVersion = 4 },
		func(r *cp.ConfigureEmailMailboxCredentialResponse) { r.Credential.Generation = 4 },
		func(r *cp.ConfigureEmailMailboxCredentialResponse) { r.Credential.Name = "invalid!" },
	} {
		r := emailCredentialFixture()
		mutate(r)
		w := emailCredentialRequest(&catalogRPCRecorder{response: r}, `{"kind":"AUTH_SECRET","value":"fixture-value"}`, `"2"`, "fixture-email-credential-01")
		if w.Code != 502 {
			t.Fatalf("readback accepted: %d", w.Code)
		}
	}
	for code, expected := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.Unavailable: 503} {
		w := emailCredentialRequest(&catalogRPCRecorder{failure: status.Error(code, "fixture-value")}, `{"kind":"AUTH_SECRET","value":"fixture-value"}`, `"2"`, "fixture-email-credential-01")
		if w.Code != expected || strings.Contains(w.Body.String(), "fixture-value") || w.Header().Get("ETag") != "" {
			t.Fatalf("unsafe error: %d", w.Code)
		}
	}
}
