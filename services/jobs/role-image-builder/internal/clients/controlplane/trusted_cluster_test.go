package controlplane

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

func TestTrustedBuilderDoesNotRequireIssuerOrTLSMaterial(t *testing.T) {
	config := Config{RPCProfile: transportprofile.TrustedCluster, Target: "control-plane.kodex-system.svc:8443",
		DialTimeout: time.Second, RPCDeadline: time.Second}
	client, err := Dial(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.CheckLocalAuthority(t.Context()); err != nil {
		t.Fatal("trusted builder still requires local issuer")
	}
	for _, target := range []string{"external.invalid:8443", "secret-broker.kodex-system.svc:8443"} {
		config.Target = target
		if client, err := Dial(t.Context(), config); err == nil {
			client.Close()
			t.Fatal("unregistered plaintext destination accepted")
		}
	}
}
