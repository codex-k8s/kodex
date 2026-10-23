package credentialprojection

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

// TestTrustedProjectionUsesExplicitInternalTargetWithoutIssuer constructs a trusted client without an issuer
// and rejects alternate targets and an unknown profile. It performs no broker RPC and proves configuration selection, not live readiness.
// TestTrustedProjectionUsesExplicitInternalTargetWithoutIssuer создаёт доверенный клиент без издателя
// и отклоняет иные адреса и неизвестный профиль. RPC брокера не выполняется; доказан выбор конфигурации, а не живая готовность.
func TestTrustedProjectionUsesExplicitInternalTargetWithoutIssuer(t *testing.T) {
	config := Config{RPCProfile: transportprofile.TrustedCluster,
		Target: "dns:///secret-broker.kodex-system.svc:8443", DialTimeout: time.Second}
	client, err := Dial(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.issuer != nil || client.api == nil {
		t.Fatal("trusted projection acquired an issuer dependency")
	}
	for _, target := range []string{"dns:///example.invalid:8443", "127.0.0.1:8443", "dns:///secret-broker.other.svc:8443"} {
		config.Target = target
		if rejected, err := Dial(t.Context(), config); err == nil {
			rejected.Close()
			t.Fatal("unapproved plaintext destination accepted")
		}
	}
	config.Target = "dns:///secret-broker.kodex-system.svc:8443"
	config.RPCProfile = "implicit"
	if rejected, err := Dial(t.Context(), config); err == nil {
		rejected.Close()
		t.Fatal("unknown RPC profile accepted")
	}
}
