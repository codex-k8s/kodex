package imageowner

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

func TestTrustedImageOwnerDoesNotRequireIssuerOrTLSMaterial(t *testing.T) {
	for _, promotion := range []bool{false, true} {
		config := Config{RPCProfile: transportprofile.TrustedCluster, Target: "control-plane.kodex-system.svc:8443",
			DialTimeout: time.Second, RPCDeadline: time.Second, Promotion: promotion}
		client, err := Dial(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Check(t.Context()); err != nil {
			t.Fatal("trusted image owner still requires local issuer")
		}
		client.Close()
		config.RPCProfile = "insecure"
		if client, err := Dial(t.Context(), config); err == nil {
			client.Close()
			t.Fatal("unknown image owner profile accepted")
		}
	}
}
