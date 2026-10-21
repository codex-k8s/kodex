package imageowner

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

// TestTrustedImageOwnerDoesNotRequireIssuerOrTLSMaterial constructs both trusted
// operation profiles without local issuer/TLS files and rejects an unknown profile.
// Check uses local identity readiness only; no admission or promotion RPC is issued.
// TestTrustedImageOwnerDoesNotRequireIssuerOrTLSMaterial создаёт оба trusted-профиля
// операций без файлов локального issuer/TLS и отклоняет неизвестный профиль. Check
// проверяет только локальную identity readiness, без admission либо promotion RPC.
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
