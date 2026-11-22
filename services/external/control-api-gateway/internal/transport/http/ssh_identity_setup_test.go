package httptransport

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// sshHTTPFixtureConnection is an inert owner reference used only by the local generated-router fixture.
	// sshHTTPFixtureConnection является inert owner reference локальной generated-router fixture.
	sshHTTPFixtureConnection = "int_12345678901234567890123456789012"
	// sshHTTPFixtureSetup fixes a valid server-assigned setup URL, without providing any upload or ownership authority.
	// sshHTTPFixtureSetup задаёт valid server-assigned setup URL без upload либо ownership authority.
	sshHTTPFixtureSetup = "ssu_12345678901234567890123456789012"
)

// sshHTTPView builds safe PREPARED metadata independently of the request; no Secret coordinates, private key or claimed actor is present.
// sshHTTPView создаёт safe PREPARED metadata независимо от request; Secret coordinates, private key либо claimed actor отсутствуют.
func sshHTTPView() *cp.ManagedSSHIdentitySetupView {
	return &cp.ManagedSSHIdentitySetupView{Ref: sshHTTPFixtureSetup, ConnectionRef: sshHTTPFixtureConnection, IdentityRef: "bki_12345678901234567890123456789012", TargetRef: "target_fixture_123", ReasonCode: "PREPARED", Version: 1, ConnectionVersion: 7, BindingVersion: 1, BindingGeneration: 1, IdentityRevision: 1, Policy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED, State: cp.ManagedSSHIdentitySetupState_MANAGED_SSH_IDENTITY_SETUP_STATE_PREPARED, ExpiresAt: timestamppb.New(time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC))}
}

// sshHTTPHandler exercises the actual generated router and generated dedicated RPC client; authentication and PostgreSQL ownership are qualified separately, not simulated by this recorder.
// sshHTTPHandler использует actual generated router и generated dedicated RPC client; authentication и PostgreSQL ownership проверяются отдельно, не имитируются recorder.
func sshHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{SSHIdentities: cp.NewManagedSSHIdentitySetupServiceClient(recorder)}})
}

// sshHTTPMutationRequest prepares only synthetic CSRF/idempotency and an explicit ETag; it does not claim a logged-in browser or bypass owner authorization.
// sshHTTPMutationRequest подготавливает лишь synthetic CSRF/idempotency и explicit ETag; logged-in browser либо обход owner authorization не заявляется.
func sshHTTPMutationRequest(path, body, etag string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "synthetic-csrf")
	request.Header.Set("Idempotency-Key", "ssh-setup-synthetic-key")
	request.Header.Set("If-Match", etag)
	return request
}

// TestSSHSetupHTTPBeginPinsMetadataAndOneTimeCapability proves independent zero binding OCC, exact generated RPC and response-only URL-safe upload capability; a replay cannot reissue a permit.
// TestSSHSetupHTTPBeginPinsMetadataAndOneTimeCapability проверяет independent zero binding OCC, exact generated RPC и response-only URL-safe upload capability; replay не перевыдаёт permit.
func TestSSHSetupHTTPBeginPinsMetadataAndOneTimeCapability(t *testing.T) {
	for _, replay := range []bool{false, true} {
		response := &cp.BeginSSHIdentitySetupResponse{Setup: sshHTTPView(), ReconcileOnly: replay}
		if !replay {
			response.UploadPermit = []byte(strings.Repeat("p", 32))
			response.UploadOrigin = "https://ssh-upload.example.invalid"
			response.UploadTlsServerName = "ssh-upload.example.invalid"
		}
		recorder := &catalogRPCRecorder{response: response}
		writer := httptest.NewRecorder()
		body := `{"expectedConnectionVersion":7,"contentSHA256":"` + strings.Repeat("a", 64) + `","contentBytes":123}`
		sshHTTPHandler(recorder).ServeHTTP(writer, sshHTTPMutationRequest("/api/v1/integration-connections/"+sshHTTPFixtureConnection+"/ssh-identity-setups", body, `"0"`))
		if writer.Code != http.StatusOK || writer.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("admission: %d %s", writer.Code, writer.Body.String())
		}
		request, ok := recorder.request.(*cp.BeginSSHIdentitySetupRequest)
		if !ok || recorder.method != cp.ManagedSSHIdentitySetupService_BeginSSHIdentitySetup_FullMethodName || request.GetConnectionRef() != sshHTTPFixtureConnection || request.GetMutation().ExpectedVersion == nil || request.GetMutation().GetExpectedVersion() != 0 || request.GetExpectedConnectionVersion() != 7 || request.GetContentBytes() != 123 {
			t.Fatal("owner setup mapping lost independent OCC")
		}
		var output map[string]json.RawMessage
		if json.Unmarshal(writer.Body.Bytes(), &output) != nil || len(output) < 2 {
			t.Fatal("missing safe admission")
		}
		upload, exists := output["upload"]
		if exists == replay {
			t.Fatal("replay upload union mismatch")
		}
		if !replay {
			var capability struct {
				Permit string `json:"permit"`
			}
			if json.Unmarshal(upload, &capability) != nil || capability.Permit != base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("p", 32))) {
				t.Fatal("upload permit representation changed")
			}
		}
		if strings.Contains(writer.Body.String(), "private_key") || strings.Contains(writer.Body.String(), "contentSHA256") || strings.Contains(writer.Body.String(), "secretUID") {
			t.Fatal("private owner state escaped")
		}
	}
}

// TestSSHSetupHTTPRejectsAmbiguousAuthorityAndOCC demonstrates that malformed metadata, key/actor injection and noncanonical ETags never reach an owner RPC.
// TestSSHSetupHTTPRejectsAmbiguousAuthorityAndOCC доказывает отказ malformed metadata, key/actor injection и noncanonical ETags до owner RPC.
func TestSSHSetupHTTPRejectsAmbiguousAuthorityAndOCC(t *testing.T) {
	valid := `{"expectedConnectionVersion":7,"contentSHA256":"` + strings.Repeat("a", 64) + `","contentBytes":123}`
	for _, body := range []string{"null", "[]", `{}`, strings.Replace(valid, `"contentBytes":123`, `"contentBytes":null`, 1), strings.Replace(valid, `"contentBytes":123`, `"contentBytes":123,"contentBytes":124`, 1), strings.Replace(valid, `"contentBytes":123`, `"ContentBytes":123`, 1), strings.Replace(valid, `"contentBytes":123`, `"contentBytes":123,"privateKey":"synthetic-forbidden"`, 1), strings.Replace(valid, `"contentBytes":123`, `"contentBytes":123,"actorRef":"usr_other"`, 1), strings.Replace(valid, `"contentBytes":123`, `"contentBytes":16385`, 1), valid + `{}`} {
		recorder := &catalogRPCRecorder{}
		writer := httptest.NewRecorder()
		sshHTTPHandler(recorder).ServeHTTP(writer, sshHTTPMutationRequest("/api/v1/integration-connections/"+sshHTTPFixtureConnection+"/ssh-identity-setups", body, `"0"`))
		if writer.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatalf("invalid body reached owner: %d", writer.Code)
		}
	}
	for _, etag := range []string{`0`, `"00"`, `"01"`, `"-1"`, `"+1"`, `"9007199254740992"`, `W/"1"`} {
		recorder := &catalogRPCRecorder{}
		writer := httptest.NewRecorder()
		sshHTTPHandler(recorder).ServeHTTP(writer, sshHTTPMutationRequest("/api/v1/integration-connections/"+sshHTTPFixtureConnection+"/ssh-identity-setups", valid, etag))
		if writer.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatalf("invalid OCC reached owner: %d", writer.Code)
		}
	}
}

// TestSSHSetupHTTPRejectsForeignOrMalformedOwnerAdmission covers the response boundary independently of input validation, including replay capability leakage and a valid-looking foreign Connection.
// TestSSHSetupHTTPRejectsForeignOrMalformedOwnerAdmission проверяет response boundary отдельно от input validation, включая replay capability leak и valid-looking foreign Connection.
func TestSSHSetupHTTPRejectsForeignOrMalformedOwnerAdmission(t *testing.T) {
	for _, change := range []func(*cp.BeginSSHIdentitySetupResponse){
		func(r *cp.BeginSSHIdentitySetupResponse) { r.Setup.ConnectionRef = "int_foreign_123" },
		func(r *cp.BeginSSHIdentitySetupResponse) { r.Setup.Ref = "ssu_noncanonical_123" },
		func(r *cp.BeginSSHIdentitySetupResponse) { r.UploadPermit = nil },
		func(r *cp.BeginSSHIdentitySetupResponse) { r.ReconcileOnly = true },
		func(r *cp.BeginSSHIdentitySetupResponse) { r.UploadOrigin = "https://ssh-upload.example.invalid:" },
		func(r *cp.BeginSSHIdentitySetupResponse) { r.Setup.Version = 9007199254740992 },
		func(r *cp.BeginSSHIdentitySetupResponse) { r.Setup.State = cp.ManagedSSHIdentitySetupState(999) },
		func(r *cp.BeginSSHIdentitySetupResponse) { r.Setup.BindingVersion = 0 },
		func(r *cp.BeginSSHIdentitySetupResponse) { r.Setup = nil },
	} {
		response := &cp.BeginSSHIdentitySetupResponse{Setup: sshHTTPView(), UploadPermit: []byte(strings.Repeat("p", 32)), UploadOrigin: "https://ssh-upload.example.invalid", UploadTlsServerName: "ssh-upload.example.invalid"}
		change(response)
		recorder := &catalogRPCRecorder{response: response}
		writer := httptest.NewRecorder()
		body := `{"expectedConnectionVersion":7,"contentSHA256":"` + strings.Repeat("a", 64) + `","contentBytes":123}`
		sshHTTPHandler(recorder).ServeHTTP(writer, sshHTTPMutationRequest("/api/v1/integration-connections/"+sshHTTPFixtureConnection+"/ssh-identity-setups", body, `"0"`))
		if writer.Code != http.StatusBadGateway || strings.Contains(writer.Body.String(), "upload") {
			t.Fatalf("invalid owner admission returned success: %d", writer.Code)
		}
	}
}

// TestSSHSetupHTTPReadRecoveryAndCancelKeepOwnerSelectors checks exact Begin-key recovery, closed query selection, safe addressed cancellation and the absence of a generic credential-material route.
// TestSSHSetupHTTPReadRecoveryAndCancelKeepOwnerSelectors проверяет exact Begin-key recovery, closed query selection, safe addressed cancellation и отсутствие generic credential-material route.
func TestSSHSetupHTTPReadRecoveryAndCancelKeepOwnerSelectors(t *testing.T) {
	path := "/api/v1/integration-connections/" + sshHTTPFixtureConnection + "/ssh-identity-setups"
	recorder := &catalogRPCRecorder{response: &cp.GetSSHIdentitySetupResponse{Setup: sshHTTPView()}}
	writer := httptest.NewRecorder()
	sshHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path+"?beginIdempotencyKey=ssh-setup-synthetic-key", nil))
	request, ok := recorder.request.(*cp.GetSSHIdentitySetupRequest)
	if writer.Code != 200 || !ok || request.GetSetupRef() != "" || request.GetBeginIdempotencyKey() != "ssh-setup-synthetic-key" {
		t.Fatal("receipt selector not preserved")
	}
	for _, query := range []string{"?beginIdempotencyKey=ssh-setup-synthetic-key&beginIdempotencyKey=other-key", "?beginIdempotencyKey=ssh-setup-synthetic-key&actorRef=usr_other", "?beginIdempotencyKey=ssh-setup-synthetic-key&bad%"} {
		recorder = &catalogRPCRecorder{}
		writer = httptest.NewRecorder()
		sshHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path+query, nil))
		if writer.Code != 400 || recorder.request != nil {
			t.Fatal("ambiguous query reached owner")
		}
	}
	cancelled := proto.Clone(sshHTTPView()).(*cp.ManagedSSHIdentitySetupView)
	cancelled.State = cp.ManagedSSHIdentitySetupState_MANAGED_SSH_IDENTITY_SETUP_STATE_CANCELLED
	cancelled.ReasonCode = "CANCELLED"
	cancelled.Version = 2
	recorder = &catalogRPCRecorder{response: &cp.CancelSSHIdentitySetupResponse{Setup: cancelled}}
	writer = httptest.NewRecorder()
	sshHTTPHandler(recorder).ServeHTTP(writer, sshHTTPMutationRequest(path+"/"+sshHTTPFixtureSetup+"/cancel", `{}`, `"1"`))
	cancel, ok := recorder.request.(*cp.CancelSSHIdentitySetupRequest)
	if writer.Code != 200 || !ok || cancel.GetSetupRef() != sshHTTPFixtureSetup || cancel.GetMutation().GetExpectedVersion() != 1 {
		t.Fatal("cancel owner coordinates changed")
	}
}
