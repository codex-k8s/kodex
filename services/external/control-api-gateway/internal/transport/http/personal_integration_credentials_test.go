package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

// personalCredentialHTTPFixture creates a public view with deliberately empty capability lists
// and no configured value, proving required defaults survive the standard ProtoJSON projection.
// personalCredentialHTTPFixture создаёт public view с намеренно пустыми capability lists и
// отсутствующим значением, проверяя required defaults штатной ProtoJSON projection.
func personalCredentialHTTPFixture() *cp.PersonalIntegrationCredentialView {
	return &cp.PersonalIntegrationCredentialView{ConnectionRef: "int_fixture01", BindingRef: "ipc_fixture01", Version: 4, ConnectionVersion: 7,
		Policy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED,
		State:  cp.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_NOT_CONFIGURED}
}

// personalCredentialHTTPHandler uses the actual generated router and dedicated generated RPC
// client with the existing request recorder; it does not simulate browser/JWT authentication.
// personalCredentialHTTPHandler использует настоящий generated router и отдельный generated RPC
// client с существующим request recorder; browser/JWT authentication здесь не имитируется.
func personalCredentialHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{PersonalCredentials: cp.NewPersonalIntegrationCredentialServiceClient(recorder)}})
}

// TestCredentialPolicyHTTPUsesDedicatedOwnerCommand checks closed policy input, exact RPC/OCC and
// public readback; user/Secret selectors and an absent owner response cannot become successful writes.
// TestCredentialPolicyHTTPUsesDedicatedOwnerCommand проверяет закрытый ввод политики, exact RPC/OCC
// и public readback; user/Secret selectors и пустой owner response не становятся успешной записью.
func TestCredentialPolicyHTTPUsesDedicatedOwnerCommand(t *testing.T) {
	t.Parallel()
	const path = "/api/v1/integration-connections/int_fixture01/credential-policy"
	for _, test := range []struct {
		name, body string
		response   *cp.SetIntegrationCredentialPolicyResponse
		status     int
	}{
		{name: "valid", body: `{"policy":"INITIATOR_REQUIRED"}`, status: http.StatusOK,
			response: &cp.SetIntegrationCredentialPolicyResponse{Connection: &cp.IntegrationConnection{
				Ref: "int_fixture01", Version: 8, CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED}}},
		{name: "unknown", body: `{"policy":"AUTO"}`, status: http.StatusBadRequest},
		{name: "owner selector", body: `{"policy":"INITIATOR_REQUIRED","userRef":"usr_other01"}`, status: http.StatusBadRequest},
		{name: "secret selector", body: `{"policy":"PROJECT_SHARED","secretRef":"private"}`, status: http.StatusBadRequest},
		{name: "nil response", body: `{"policy":"INITIATOR_REQUIRED"}`, response: &cp.SetIntegrationCredentialPolicyResponse{}, status: http.StatusBadGateway},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := &catalogRPCRecorder{}
			if test.response != nil {
				recorder.response = test.response
			}
			request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "credential-policy-http-operation")
			request.Header.Set("If-Match", `"7"`)
			request.Header.Set("X-CSRF-Token", "synthetic-csrf")
			writer := httptest.NewRecorder()
			personalCredentialHTTPHandler(recorder).ServeHTTP(writer, request)
			if writer.Code != test.status {
				t.Fatalf("policy HTTP status=%d, want=%d", writer.Code, test.status)
			}
			if test.status == http.StatusBadRequest {
				if recorder.request != nil {
					t.Fatal("invalid policy input reached owner RPC")
				}
				return
			}
			input, ok := recorder.request.(*cp.SetIntegrationCredentialPolicyRequest)
			if !ok || recorder.method != cp.PersonalIntegrationCredentialService_SetIntegrationCredentialPolicy_FullMethodName ||
				input.ConnectionRef != "int_fixture01" || input.GetMutation().GetExpectedVersion() != 7 ||
				input.Policy != cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED {
				t.Fatal("policy write lost dedicated operation or OCC")
			}
			if test.status == http.StatusOK && (writer.Header().Get("ETag") != `"8"` || writer.Header().Get("Cache-Control") != "no-store" ||
				!strings.Contains(writer.Body.String(), `"credentialPolicy":"INITIATOR_REQUIRED"`)) {
				t.Fatal("policy readback lost its exact public metadata")
			}
		})
	}
}

// TestPersonalCredentialHTTPRoutesAndSafeProjection verifies exact RPC dispatch, separate OCC,
// write-only input, no-store/ETag and complete empty/default metadata for all five endpoints.
// TestPersonalCredentialHTTPRoutesAndSafeProjection проверяет exact RPC dispatch, раздельный OCC,
// write-only input, no-store/ETag и полные empty/default metadata пяти endpoints.
func TestPersonalCredentialHTTPRoutesAndSafeProjection(t *testing.T) {
	t.Parallel()
	const path = "/api/v1/me/integration-connections/int_fixture01/credential"
	for _, test := range []struct {
		name, method, suffix, body string
		status                     int
		rpc                        string
	}{
		{"get", "GET", "", "", 200, cp.PersonalIntegrationCredentialService_GetPersonalIntegrationCredential_FullMethodName},
		{"create", "POST", "", `{"expectedConnectionVersion":7}`, 201, cp.PersonalIntegrationCredentialService_CreatePersonalIntegrationCredential_FullMethodName},
		{"configure", "PUT", "", `{"expectedConnectionVersion":7,"value":"synthetic-private-value"}`, 200, cp.PersonalIntegrationCredentialService_ConfigurePersonalIntegrationCredential_FullMethodName},
		{"revoke", "POST", "/revoke", `{}`, 200, cp.PersonalIntegrationCredentialService_RevokePersonalIntegrationCredential_FullMethodName},
		{"test", "POST", "/test", `{"expectedConnectionVersion":7}`, 200, cp.PersonalIntegrationCredentialService_TestPersonalIntegrationCredential_FullMethodName},
	} {
		t.Run(test.name, func(t *testing.T) {
			view := personalCredentialHTTPFixture()
			var reply proto.Message
			switch test.name {
			case "get":
				reply = &cp.GetPersonalIntegrationCredentialResponse{Credential: view}
			case "create":
				reply = &cp.CreatePersonalIntegrationCredentialResponse{Credential: view}
			case "configure":
				view.CredentialConfigured = true
				view.State = cp.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_CHECK_REQUIRED
				reply = &cp.ConfigurePersonalIntegrationCredentialResponse{Credential: view}
			case "test":
				view.State = cp.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_CHECKING
				view.CredentialConfigured = true
				reply = &cp.TestPersonalIntegrationCredentialResponse{Credential: view}
			case "revoke":
				view.State = cp.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_REVOKED
				reply = &cp.RevokePersonalIntegrationCredentialResponse{Credential: view}
			}
			recorder := &catalogRPCRecorder{response: reply}
			request := httptest.NewRequest(test.method, path+test.suffix, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "personal-http-operation")
			request.Header.Set("X-CSRF-Token", "synthetic-csrf")
			if test.name == "configure" || test.name == "revoke" || test.name == "test" {
				request.Header.Set("If-Match", `"3"`)
			}
			writer := httptest.NewRecorder()
			personalCredentialHTTPHandler(recorder).ServeHTTP(writer, request)
			if writer.Code != test.status || recorder.method != test.rpc || recorder.request == nil {
				t.Fatalf("personal HTTP dispatch rejected: status=%d method=%s", writer.Code, recorder.method)
			}
			if writer.Header().Get("Cache-Control") != "no-store" || writer.Header().Get("ETag") != `"4"` || strings.Contains(writer.Body.String(), "synthetic-private-value") {
				t.Fatal("personal response cache/ETag or value boundary failed")
			}
			var body map[string]any
			if json.Unmarshal(writer.Body.Bytes(), &body) != nil || body["connectionRef"] != "int_fixture01" || body["policy"] != "INITIATOR_REQUIRED" || body["credentialConfigured"] != view.CredentialConfigured {
				t.Fatal("personal response lost required public fields")
			}
			for _, key := range []string{"verifiedCapabilityKeys", "deniedCapabilityKeys"} {
				items, ok := body[key].([]any)
				if !ok || len(items) != 0 {
					t.Fatal("empty verified capability defaults were lost")
				}
			}
			if test.name == "test" {
				input := recorder.request.(*cp.TestPersonalIntegrationCredentialRequest)
				if input.ExpectedConnectionVersion != 7 || input.GetMutation().GetExpectedVersion() != 3 {
					t.Fatal("personal test lost binding or configuration OCC")
				}
			}
			if test.name == "configure" {
				input := recorder.request.(*cp.ConfigurePersonalIntegrationCredentialRequest)
				if string(input.CredentialValue) != "synthetic-private-value" || input.ExpectedConnectionVersion != 7 || input.GetMutation().GetExpectedVersion() != 3 {
					t.Fatal("personal input mixed credential/configuration/binding pins")
				}
			}
		})
	}
}

// TestPersonalCredentialHTTPRejectsOwnerSelectorsAndIncompleteMetadata checks extra authority
// fields fail before RPC and malformed/empty owner projections never become a successful response.
// TestPersonalCredentialHTTPRejectsOwnerSelectorsAndIncompleteMetadata проверяет отказ лишним
// authority fields до RPC и запрет успешного ответа для неверной либо пустой owner projection.
func TestPersonalCredentialHTTPRejectsOwnerSelectorsAndIncompleteMetadata(t *testing.T) {
	t.Parallel()
	const path = "/api/v1/me/integration-connections/int_fixture01/credential"
	for _, input := range []struct{ method, suffix, body string }{
		{"PUT", "", `{"expectedConnectionVersion":7,"value":"synthetic","subjectId":"other"}`},
		{"PUT", "", `{"expectedConnectionVersion":7,"value":"synthetic","secretRef":"foreign"}`},
		{"PUT", "", `{"expectedConnectionVersion":7}`},
		{"PUT", "", `{"expectedConnectionVersion":0,"value":"synthetic"}`},
		{"POST", "/revoke", `{"subjectId":"other"}`},
		{"POST", "/test", `{"expectedConnectionVersion":7,"subjectId":"other"}`},
		{"POST", "/test", `{"expectedConnectionVersion":7,"probeUrl":"https://other.example.invalid"}`},
		{"POST", "/test", `{"expectedConnectionVersion":7,"value":"synthetic"}`},
		{"POST", "/test", `{"expectedConnectionVersion":0}`},
		{"POST", "", `{"expectedConnectionVersion":0}`},
	} {
		recorder := &catalogRPCRecorder{}
		request := httptest.NewRequest(input.method, path+input.suffix, strings.NewReader(input.body))
		request.Header.Set("Idempotency-Key", "personal-invalid-operation")
		request.Header.Set("If-Match", `"3"`)
		request.Header.Set("X-CSRF-Token", "synthetic-csrf")
		writer := httptest.NewRecorder()
		personalCredentialHTTPHandler(recorder).ServeHTTP(writer, request)
		if writer.Code != 400 || recorder.request != nil {
			t.Fatal("invalid personal authority/value input reached RPC")
		}
	}
	for _, mutate := range []func(*cp.PersonalIntegrationCredentialView){
		func(view *cp.PersonalIntegrationCredentialView) { view.State = 999 },
		func(view *cp.PersonalIntegrationCredentialView) { view.Policy = 0 },
		func(view *cp.PersonalIntegrationCredentialView) { view.Version = 0 },
		func(view *cp.PersonalIntegrationCredentialView) {
			view.State = cp.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_READY
		},
	} {
		view := personalCredentialHTTPFixture()
		mutate(view)
		writer := httptest.NewRecorder()
		writeMessage(writer, 200, &cp.GetPersonalIntegrationCredentialResponse{Credential: view}, "credential", "")
		if writer.Code != 502 {
			t.Fatal("malformed personal metadata became HTTP success")
		}
	}
	writer := httptest.NewRecorder()
	writeMessage(writer, 200, &cp.GetPersonalIntegrationCredentialResponse{}, "credential", "")
	if writer.Code != 502 {
		t.Fatal("missing personal metadata became HTTP success")
	}
}

// TestPersonalGitHubDiscoveryPublicDefaults preserves empty pages and explicit false permission flags through the real ProtoJSON boundary without exposing credential descriptors.
// TestPersonalGitHubDiscoveryPublicDefaults сохраняет пустые страницы и явные false permission flags через настоящую ProtoJSON boundary без credential descriptors.
func TestPersonalGitHubDiscoveryPublicDefaults(t *testing.T) {
	view := personalCredentialHTTPFixture()
	view.State = cp.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_READY
	view.CredentialConfigured = true
	view.VerifiedCapabilityKeys = []string{"github.identity.read"}
	view.GithubDiscovery = &cp.GitHubRepositoryDiscoveryPage{IdentityId: 7, Login: "synthetic", Page: 1, Items: []*cp.GitHubDiscoveredRepository{{Id: 17, FullName: "team/repo", OwnerKind: "Organization", CanRead: true}}}
	result, err := messageMap(&cp.GetPersonalIntegrationCredentialResponse{Credential: view})
	if err != nil {
		t.Fatal(err)
	}
	credential := result["credential"].(map[string]any)
	page := credential["githubDiscovery"].(map[string]any)
	if page["nextPage"] != float64(0) || page["identityId"] != float64(7) {
		t.Fatal("safe numeric discovery fields were lost")
	}
	repo := page["items"].([]any)[0].(map[string]any)
	for _, key := range []string{"private", "archived", "canPush"} {
		if repo[key] != false {
			t.Fatalf("false %s observation disappeared", key)
		}
	}
	view.GithubDiscovery.Items = nil
	result, err = messageMap(&cp.GetPersonalIntegrationCredentialResponse{Credential: view})
	if err != nil {
		t.Fatal(err)
	}
	page = result["credential"].(map[string]any)["githubDiscovery"].(map[string]any)
	if items, ok := page["items"].([]any); !ok || len(items) != 0 {
		t.Fatal("empty authoritative discovery page disappeared")
	}
}
