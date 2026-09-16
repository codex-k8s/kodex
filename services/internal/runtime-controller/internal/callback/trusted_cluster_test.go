package callback

import (
	"net/http/httptest"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestTrustedCallbackTLSRequiresServerConfiguration(t *testing.T) {
	tlsConfig, err := serverTLS(Config{RPCProfile: runtimecontract.CallbackProfileTrustedCluster})
	if err != nil || tlsConfig != nil {
		t.Fatal("trusted callback requires certificate files")
	}
	for _, profile := range []string{"", "insecure"} {
		if _, err := serverTLS(Config{RPCProfile: profile}); err == nil {
			t.Fatal("unconfigured callback silently disabled TLS")
		}
	}
	server := &Server{config: Config{RPCProfile: runtimecontract.CallbackProfileTrustedCluster}}
	request := httptest.NewRequest("POST", "/v1/executions/lease-fixture/complete", nil)
	request.Header.Set("X-Kodex-Rpc-Profile", runtimecontract.CallbackProfileTrustedCluster)
	if _, allowed := server.authorize(request, "lease-fixture"); allowed {
		t.Fatal("trusted callback bypassed execution ticket")
	}
}
