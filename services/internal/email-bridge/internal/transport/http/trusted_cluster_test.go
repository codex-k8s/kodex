package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

func TestTrustedAdmissionDoesNotReplaceExecutionGrant(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/mailbox-operations", nil)
	request.Header.Set("X-Kodex-Rpc-Profile", transportprofile.TrustedCluster)
	request.Header.Set("X-Kodex-Trusted-Caller", CallerSPIFFE)
	handler := Handler{RPCProfile: transportprofile.TrustedCluster}
	if !handler.admit(request) || (Handler{}).admit(request) {
		t.Fatal("profile boundary rejected")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatal("trusted caller bypassed execution grant")
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Add("X-Kodex-Rpc-Profile", transportprofile.TrustedCluster) },
		func(r *http.Request) { r.Header.Add("X-Kodex-Trusted-Caller", CallerSPIFFE) },
		func(r *http.Request) { r.Header.Set("X-Kodex-Trusted-Caller", "browser") },
		func(r *http.Request) { r.Header.Set("X-Kodex-Authorization", "legacy") },
	} {
		changed := request.Clone(request.Context())
		mutate(changed)
		if handler.admit(changed) {
			t.Fatal("ambiguous trusted admission accepted")
		}
	}
}
