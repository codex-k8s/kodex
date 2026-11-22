package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// materialHTTPFixture contains safe, actor-owned artifact metadata rather than a target Secret result.
// materialHTTPFixture содержит safe actor-owned artifact metadata, не результат изменения Secret.
func materialHTTPFixture() *cp.ManagedKubernetesResourceMaterialView {
	return &cp.ManagedKubernetesResourceMaterialView{MaterialRef: "krs_fixture01", ConnectionRef: "int_fixture01", ConnectionVersion: 7, IdentityRef: "kbi_fixture01", IdentityRevision: 2, TargetRef: "target_fixture01", Namespace: "team", Name: "application", Operation: "kubernetes.create", SecretType: "Opaque", State: cp.ManagedKubernetesResourceMaterialState_MANAGED_KUBERNETES_RESOURCE_MATERIAL_STATE_PREPARED, ExpiresAt: timestamppb.New(time.Now().UTC().Add(time.Minute)), Version: 1, CredentialPolicy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED, BindingVersion: 3}
}

// materialHTTPHandler uses the actual generated HTTP router and protected RPC client mapping.
// materialHTTPHandler использует настоящий generated HTTP router и protected RPC client mapping.
func materialHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{KubernetesMaterials: cp.NewManagedKubernetesResourceMaterialServiceClient(recorder)}})
}

// materialHTTPRequest supplies public mutation headers; no private source bytes are sent to the API.
// materialHTTPRequest передаёт public mutation headers; private source bytes в API не идут.
func materialHTTPRequest(method, suffix, body string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/me/integration-connections/int_fixture01/kubernetes-resource-materials"+suffix, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "material-http-idempotency")
	r.Header.Set("X-CSRF-Token", "synthetic-csrf")
	r.Header.Set("If-Match", `"1"`)
	return r
}

// TestMaterialHTTPRejectsPrivateAndAmbiguousBegin proves malformed metadata never reaches owner RPC.
// Required empty fields remain explicit; null, UTF-8 corruption and target/credential payloads are rejected.
// TestMaterialHTTPRejectsPrivateAndAmbiguousBegin доказывает отказ malformed metadata до owner RPC.
// Required empty fields явны; null, UTF-8 corruption и target/credential payloads запрещены.
func TestMaterialHTTPRejectsPrivateAndAmbiguousBegin(t *testing.T) {
	valid := `{"expectedVersion":3,"expectedConnectionVersion":7,"operation":"kubernetes.create","namespace":"team","name":"application","generateName":"","secretType":"Opaque","patchType":"","contentSHA256":"` + strings.Repeat("a", 64) + `","contentBytes":42}`
	cases := []string{
		strings.Replace(valid, `"generateName":"",`, "", 1), strings.Replace(valid, `"generateName":""`, `"generateName":null`, 1), strings.Replace(valid, `"expectedVersion":3`, `"expectedVersion":3,"expectedVersion":4`, 1), strings.Replace(valid, `"namespace"`, `"Namespace"`, 1), strings.TrimSuffix(valid, "}") + `,"data":{"key":"private"}}`, strings.TrimSuffix(valid, "}") + `,"identityRef":"foreign"}`, strings.Replace(valid, `"name":"application"`, `"name":"`+string([]byte{0xff})+`"`, 1), strings.Replace(valid, `"secretType":"Opaque"`, `"secretType":"kubernetes.io/service-account-token"`, 1), strings.Replace(valid, `"generateName":""`, `"generateName":"prefix-"`, 1), valid + strings.Repeat(" ", 4096), "null", "[]"}
	for _, body := range cases {
		recorder := &catalogRPCRecorder{failure: errors.New("synthetic unexpected RPC")}
		w := httptest.NewRecorder()
		materialHTTPHandler(recorder).ServeHTTP(w, materialHTTPRequest(http.MethodPost, "", body))
		if w.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatal("private or ambiguous Begin reached owner RPC")
		}
	}
}

// TestMaterialHTTPBeginAdmissionSeparatesFirstPermitFromReconcile verifies exact forwarding and one-shot admission.
// Successful replay never reconstructs an upload authority, and malformed success fails without raw response data.
// TestMaterialHTTPBeginAdmissionSeparatesFirstPermitFromReconcile проверяет exact forwarding/one-shot admission.
// Replay не восстанавливает upload authority; malformed success закрывается без raw response data.
func TestMaterialHTTPBeginAdmissionSeparatesFirstPermitFromReconcile(t *testing.T) {
	ca := []byte("synthetic projection CA; native CLI TLS has separate tests")
	digest := sha256.Sum256(ca)
	first := &cp.ManagedKubernetesResourceMaterialAdmission{Material: materialHTTPFixture(), UploadPermit: make([]byte, 32), Endpoint: &cp.ManagedKubernetesIdentityUploadEndpoint{ServerOrigin: "https://broker.example.test:9443", TlsServerName: "broker.example.test", CertificateAuthorityPem: ca, CertificateAuthoritySha256: hex.EncodeToString(digest[:]), AddressIps: []string{"192.0.2.1"}}}
	foreign := proto.Clone(first).(*cp.ManagedKubernetesResourceMaterialAdmission)
	foreign.Material.Namespace = "foreign"
	for _, item := range []struct {
		response *cp.ManagedKubernetesResourceMaterialAdmission
		status   int
	}{
		{first, http.StatusCreated}, {foreign, http.StatusBadGateway}, {&cp.ManagedKubernetesResourceMaterialAdmission{Material: materialHTTPFixture(), ReconcileOnly: true}, http.StatusCreated}, {&cp.ManagedKubernetesResourceMaterialAdmission{Material: materialHTTPFixture()}, http.StatusBadGateway}, {&cp.ManagedKubernetesResourceMaterialAdmission{Material: materialHTTPFixture(), ReconcileOnly: true, UploadPermit: make([]byte, 32)}, http.StatusBadGateway},
	} {
		recorder := &catalogRPCRecorder{response: &cp.BeginManagedKubernetesResourceMaterialResponse{Admission: proto.Clone(item.response).(*cp.ManagedKubernetesResourceMaterialAdmission)}}
		w := httptest.NewRecorder()
		body := `{"expectedVersion":3,"expectedConnectionVersion":7,"operation":"kubernetes.create","namespace":"team","name":"application","generateName":"","secretType":"Opaque","patchType":"","contentSHA256":"` + strings.Repeat("a", 64) + `","contentBytes":42}`
		materialHTTPHandler(recorder).ServeHTTP(w, materialHTTPRequest(http.MethodPost, "", body))
		input, ok := recorder.request.(*cp.BeginManagedKubernetesResourceMaterialRequest)
		if w.Code != item.status || !ok || input.GetMutation().GetExpectedVersion() != 3 || input.GetExpectedConnectionVersion() != 7 || input.GetNamespace() != "team" || input.GetName() != "application" || recorder.method != cp.ManagedKubernetesResourceMaterialService_BeginManagedKubernetesResourceMaterial_FullMethodName || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("Begin lost exact safe metadata/OCC/RPC")
		}
		if item.response.GetReconcileOnly() && item.status == http.StatusCreated && (strings.Contains(w.Body.String(), "uploadPermit") || strings.Contains(w.Body.String(), "endpoint")) {
			t.Fatal("reconcile exposed upload authority")
		}
	}
}

// TestMaterialHTTPGetCancelPreserveArtifactIdentity checks exact selectors, ETag and closed history projection.
// AVAILABLE is an artifact state, not proof that a target Secret was created or a connection became ready.
// TestMaterialHTTPGetCancelPreserveArtifactIdentity проверяет exact selectors, ETag и closed history projection.
// AVAILABLE — состояние artifact, не доказательство target Secret либо готовности подключения.
func TestMaterialHTTPGetCancelPreserveArtifactIdentity(t *testing.T) {
	view := materialHTTPFixture()
	view.State = cp.ManagedKubernetesResourceMaterialState_MANAGED_KUBERNETES_RESOURCE_MATERIAL_STATE_AVAILABLE
	for _, item := range []struct{ method, suffix, body, rpc string }{{http.MethodGet, "?materialRef=krs_fixture01", "", cp.ManagedKubernetesResourceMaterialService_GetManagedKubernetesResourceMaterial_FullMethodName}, {http.MethodGet, "?beginIdempotencyKey=material-http-idempotency", "", cp.ManagedKubernetesResourceMaterialService_GetManagedKubernetesResourceMaterial_FullMethodName}, {http.MethodPost, "/krs_fixture01/cancel", "{}", cp.ManagedKubernetesResourceMaterialService_CancelManagedKubernetesResourceMaterial_FullMethodName}} {
		reply := proto.Message(&cp.GetManagedKubernetesResourceMaterialResponse{Material: view})
		if item.method == http.MethodPost {
			reply = &cp.CancelManagedKubernetesResourceMaterialResponse{Material: view}
		}
		recorder := &catalogRPCRecorder{response: reply}
		w := httptest.NewRecorder()
		materialHTTPHandler(recorder).ServeHTTP(w, materialHTTPRequest(item.method, item.suffix, item.body))
		if w.Code != http.StatusOK || recorder.method != item.rpc || w.Header().Get("ETag") != `"1"` || !strings.Contains(w.Body.String(), `"state":"AVAILABLE"`) || strings.Contains(w.Body.String(), "uploadPermit") {
			t.Fatal("safe history or exact owner selector changed")
		}
		if input, ok := recorder.request.(*cp.CancelManagedKubernetesResourceMaterialRequest); ok && (input.GetMaterialRef() != view.GetMaterialRef() || input.GetMutation().GetExpectedVersion() != 1) {
			t.Fatal("Cancel lost exact artifact OCC")
		}
	}
	for _, suffix := range []string{"", "?materialRef=krs_fixture01&beginIdempotencyKey=material-http-idempotency", "?materialRef=", "?materialRef=ksu_foreign01"} {
		recorder := &catalogRPCRecorder{failure: errors.New("synthetic unexpected RPC")}
		w := httptest.NewRecorder()
		materialHTTPHandler(recorder).ServeHTTP(w, materialHTTPRequest(http.MethodGet, suffix, ""))
		if w.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatal("ambiguous artifact selector reached owner")
		}
	}
	for _, mutate := range []func(*cp.ManagedKubernetesResourceMaterialView){func(v *cp.ManagedKubernetesResourceMaterialView) { v.ReasonCode = "MATERIAL_raw_private" }, func(v *cp.ManagedKubernetesResourceMaterialView) { v.ConnectionRef = "int_foreign01" },
		func(v *cp.ManagedKubernetesResourceMaterialView) { v.MaterialRef = "krs_foreign01" }, func(v *cp.ManagedKubernetesResourceMaterialView) { v.State = 999 }, func(v *cp.ManagedKubernetesResourceMaterialView) { v.BindingVersion = 0 }} {
		invalid := proto.Clone(view).(*cp.ManagedKubernetesResourceMaterialView)
		mutate(invalid)
		recorder := &catalogRPCRecorder{response: &cp.GetManagedKubernetesResourceMaterialResponse{Material: invalid}}
		w := httptest.NewRecorder()
		materialHTTPHandler(recorder).ServeHTTP(w, materialHTTPRequest(http.MethodGet, "?materialRef=krs_fixture01", ""))
		if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "raw_private") {
			t.Fatal("invalid private/history response escaped boundary")
		}
	}
}
