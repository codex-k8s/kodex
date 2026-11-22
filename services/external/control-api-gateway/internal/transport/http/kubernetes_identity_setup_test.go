package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesSetupHTTPFixture contains only public metadata, never a credential or storage locator.
// kubernetesSetupHTTPFixture содержит лишь public metadata без credential либо storage locator.
func kubernetesSetupHTTPFixture() *cp.ManagedKubernetesIdentitySetupView {
	return &cp.ManagedKubernetesIdentitySetupView{SetupRef: "ksu_fixture01", ConnectionRef: "int_fixture01", ConnectionVersion: 7, BindingGeneration: 1, BindingVersion: 1,
		IdentityRef: "kbi_fixture01", IdentityRevision: 1, CanonicalContext: "operator-context", TargetRef: "target_fixture01", State: cp.ManagedKubernetesIdentitySetupState_MANAGED_KUBERNETES_IDENTITY_SETUP_STATE_PREPARED,
		ExpiresAt: timestamppb.New(time.Now().UTC().Add(time.Minute)), Version: 1, CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED}
}

// kubernetesSetupHTTPHandler exercises the actual generated router and typed protected-client adapter.
// Browser authentication and the CP owner transaction have separate qualification paths.
// kubernetesSetupHTTPHandler проверяет настоящий generated router и typed protected-client adapter.
// Browser authentication и CP owner transaction имеют отдельные qualification paths.
func kubernetesSetupHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{KubernetesIdentities: cp.NewManagedKubernetesIdentitySetupServiceClient(recorder)}})
}

// TestKubernetesSetupHTTPRejectsMaterialAndAmbiguousMetadata prevents setup from becoming a token path.
// Explicit absent-binding zero is required, while duplicate/case-alias/foreign owner fields fail locally.
// TestKubernetesSetupHTTPRejectsMaterialAndAmbiguousMetadata не даёт setup превратиться в token path.
// Нужен явный absent-binding zero; duplicate/case-alias/foreign owner fields отклоняются локально.
func TestKubernetesSetupHTTPRejectsMaterialAndAmbiguousMetadata(t *testing.T) {
	valid := `{"expectedVersion":0,"expectedConnectionVersion":7,"contentSHA256":"` + strings.Repeat("a", 64) + `","contentBytes":42}`
	for _, body := range []string{
		strings.Replace(valid, `"expectedVersion":0,`, "", 1),
		strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":null`, 1),
		strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":0,"expectedVersion":1`, 1),
		strings.Replace(valid, `"expectedVersion"`, `"ExpectedVersion"`, 1),
		strings.TrimSuffix(valid, "}") + `,"kubeconfig":"synthetic material"}`,
		strings.TrimSuffix(valid, "}") + `,"subjectRef":"usr_foreign"}`,
		valid + strings.Repeat(" ", 4096),
		strings.Replace(valid, `"contentSHA256":"`, `"contentSHA256":"`+string([]byte{0xff}), 1),
		"null", "[]",
	} {
		recorder := &catalogRPCRecorder{}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/me/integration-connections/int_fixture01/kubernetes-setups", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "setup-http-idempotency")
		request.Header.Set("X-CSRF-Token", "synthetic-csrf")
		writer := httptest.NewRecorder()
		kubernetesSetupHTTPHandler(recorder).ServeHTTP(writer, request)
		if writer.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatal("ambiguous metadata or private material reached owner RPC")
		}
	}
}

// TestKubernetesSetupHTTPRoutesPreserveOwnerCommands verifies exact selectors, OCC and safe replies.
// Completed/quarantined history remains readable without exposing the private rejected ACK witness.
// TestKubernetesSetupHTTPRoutesPreserveOwnerCommands проверяет exact selectors, OCC и safe replies.
// Completed/quarantined history читается без раскрытия private rejected ACK witness.
func TestKubernetesSetupHTTPRoutesPreserveOwnerCommands(t *testing.T) {
	view := kubernetesSetupHTTPFixture()
	view.State = cp.ManagedKubernetesIdentitySetupState_MANAGED_KUBERNETES_IDENTITY_SETUP_STATE_COMPLETED
	view.ReasonCode = "SETUP_ACK_CONFLICT"
	for _, item := range []struct{ method, suffix, body, operation string }{
		{http.MethodGet, "?setupRef=ksu_fixture01", "", cp.ManagedKubernetesIdentitySetupService_GetManagedKubernetesIdentitySetup_FullMethodName},
		{http.MethodGet, "?beginIdempotencyKey=setup-http-idempotency", "", cp.ManagedKubernetesIdentitySetupService_GetManagedKubernetesIdentitySetup_FullMethodName},
		{http.MethodPost, "/ksu_fixture01/cancel", "{}", cp.ManagedKubernetesIdentitySetupService_CancelManagedKubernetesIdentitySetup_FullMethodName},
	} {
		var response proto.Message = &cp.GetManagedKubernetesIdentitySetupResponse{Setup: view}
		if item.method == http.MethodPost {
			response = &cp.CancelManagedKubernetesIdentitySetupResponse{Setup: view}
		}
		recorder := &catalogRPCRecorder{response: response}
		request := httptest.NewRequest(item.method, "/api/v1/me/integration-connections/int_fixture01/kubernetes-setups"+item.suffix, strings.NewReader(item.body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "setup-cancel-idempotency")
		request.Header.Set("If-Match", `"1"`)
		request.Header.Set("X-CSRF-Token", "synthetic-csrf")
		writer := httptest.NewRecorder()
		kubernetesSetupHTTPHandler(recorder).ServeHTTP(writer, request)
		if writer.Code != http.StatusOK || recorder.method != item.operation || writer.Header().Get("ETag") != `"1"` || writer.Header().Get("Cache-Control") != "no-store" ||
			!strings.Contains(writer.Body.String(), `"state":"COMPLETED"`) || !strings.Contains(writer.Body.String(), `"reasonCode":"SETUP_ACK_CONFLICT"`) || strings.Contains(writer.Body.String(), "secret") {
			t.Fatal("setup route lost dedicated command, historical state or safe response contract")
		}
		if input, ok := recorder.request.(*cp.CancelManagedKubernetesIdentitySetupRequest); ok && (input.GetSetupRef() != view.GetSetupRef() || input.GetMutation().GetExpectedVersion() != 1) {
			t.Fatal("cancel lost exact setup OCC")
		}
	}
	for _, suffix := range []string{"", "?setupRef=ksu_fixture01&beginIdempotencyKey=setup-http-idempotency", "?setupRef="} {
		recorder := &catalogRPCRecorder{}
		writer := httptest.NewRecorder()
		kubernetesSetupHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/me/integration-connections/int_fixture01/kubernetes-setups"+suffix, nil))
		if writer.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatal("absent or ambiguous selector reached owner read")
		}
	}
}

// TestKubernetesSetupHTTPBeginSeparatesFirstPermitFromReplay covers the exact admission oneof.
// A partial upstream result cannot create a second upload authority or expose arbitrary fields.
// TestKubernetesSetupHTTPBeginSeparatesFirstPermitFromReplay проверяет exact admission oneof.
// Неполный upstream result не создаёт второй upload authority и не раскрывает arbitrary fields.
func TestKubernetesSetupHTTPBeginSeparatesFirstPermitFromReplay(t *testing.T) {
	ca := []byte("synthetic projection-only CA; full CLI certificate verification is separate")
	hash := sha256.Sum256(ca)
	first := &cp.BeginManagedKubernetesIdentitySetupResponse{Setup: kubernetesSetupHTTPFixture(), UploadPermit: make([]byte, 32), Endpoint: &cp.ManagedKubernetesIdentityUploadEndpoint{
		ServerOrigin: "https://broker.example.test:9443", TlsServerName: "broker.example.test", CertificateAuthorityPem: ca, CertificateAuthoritySha256: hex.EncodeToString(hash[:]), AddressIps: []string{"192.0.2.1"}}}
	for _, item := range []struct {
		response *cp.BeginManagedKubernetesIdentitySetupResponse
		status   int
	}{
		{first, http.StatusCreated},
		{&cp.BeginManagedKubernetesIdentitySetupResponse{Setup: kubernetesSetupHTTPFixture(), ReconcileOnly: true}, http.StatusCreated},
		{&cp.BeginManagedKubernetesIdentitySetupResponse{Setup: kubernetesSetupHTTPFixture()}, http.StatusBadGateway},
		{&cp.BeginManagedKubernetesIdentitySetupResponse{Setup: kubernetesSetupHTTPFixture(), ReconcileOnly: true, UploadPermit: make([]byte, 32)}, http.StatusBadGateway},
		{&cp.BeginManagedKubernetesIdentitySetupResponse{}, http.StatusBadGateway},
	} {
		recorder := &catalogRPCRecorder{response: proto.Clone(item.response)}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/me/integration-connections/int_fixture01/kubernetes-setups", strings.NewReader(`{"expectedVersion":0,"expectedConnectionVersion":7,"contentSHA256":"`+strings.Repeat("a", 64)+`","contentBytes":42}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "setup-http-idempotency")
		request.Header.Set("X-CSRF-Token", "synthetic-csrf")
		writer := httptest.NewRecorder()
		kubernetesSetupHTTPHandler(recorder).ServeHTTP(writer, request)
		input, ok := recorder.request.(*cp.BeginManagedKubernetesIdentitySetupRequest)
		if writer.Code != item.status || !ok || input.GetMutation().ExpectedVersion == nil || input.GetMutation().GetExpectedVersion() != 0 || input.GetExpectedConnectionVersion() != 7 ||
			recorder.method != cp.ManagedKubernetesIdentitySetupService_BeginManagedKubernetesIdentitySetup_FullMethodName || writer.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("Begin lost explicit zero precondition, exact RPC or no-store projection")
		}
		if item.response.GetReconcileOnly() && item.status == http.StatusCreated && (strings.Contains(writer.Body.String(), "uploadPermit") || strings.Contains(writer.Body.String(), "endpoint")) {
			t.Fatal("replay manufactured an upload permit or endpoint")
		}
	}
}

// TestKubernetesSetupHTTPRejectsUnknownResponseEnvelope keeps the new per-RPC wrapper closed;
// a valid nested view does not authorize unknown fields in the transport envelope.
// TestKubernetesSetupHTTPRejectsUnknownResponseEnvelope закрывает новый per-RPC wrapper;
// valid nested view не разрешает unknown fields transport envelope.
func TestKubernetesSetupHTTPRejectsUnknownResponseEnvelope(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		view := kubernetesSetupHTTPFixture()
		var response proto.Message = &cp.GetManagedKubernetesIdentitySetupResponse{Setup: view}
		suffix := "?setupRef=ksu_fixture01"
		if method == http.MethodPost {
			response = &cp.CancelManagedKubernetesIdentitySetupResponse{Setup: view}
			suffix = "/ksu_fixture01/cancel"
		}
		response.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		recorder := &catalogRPCRecorder{response: response}
		request := httptest.NewRequest(method, "/api/v1/me/integration-connections/int_fixture01/kubernetes-setups"+suffix, strings.NewReader("{}"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "setup-wrapper-negative")
		request.Header.Set("If-Match", `"1"`)
		request.Header.Set("X-CSRF-Token", "synthetic-csrf")
		writer := httptest.NewRecorder()
		kubernetesSetupHTTPHandler(recorder).ServeHTTP(writer, request)
		if writer.Code != http.StatusBadGateway || strings.Contains(writer.Body.String(), view.SetupRef) {
			t.Fatal("unknown setup envelope released its nested owner view")
		}
	}
}
