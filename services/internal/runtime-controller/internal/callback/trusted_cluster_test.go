package callback

import (
	"net/http/httptest"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestTrustedCallbackTLSRequiresServerConfiguration checks trusted-cluster is an
// explicit configured plaintext profile rather than a fallback for unknown profiles,
// and a profile header without a valid execution ticket never authorizes a callback.
// The historical test name does not imply trusted-cluster requires certificate files.
// TestTrustedCallbackTLSRequiresServerConfiguration проверяет trusted-cluster как явно
// настроенный plaintext profile,не fallback unknown profiles; profile header без valid
// execution ticket не авторизует callback. Историческое имя не означает,что trusted-
// cluster требует certificate files.
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
