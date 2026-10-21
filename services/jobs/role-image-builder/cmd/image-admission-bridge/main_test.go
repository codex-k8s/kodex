package main

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

// TestImageOwnerProfileIsExplicit checks trusted-cluster configuration in both
// modes without TLS credentials, and rejects unknown profiles or an unconfigured
// protected default. It only builds config values and never dials the target.
// TestImageOwnerProfileIsExplicit проверяет настройки trusted-cluster в обоих
// режимах без TLS-реквизитов и отказ для неизвестного профиля либо незаполненного
// защищённого варианта. Тест лишь собирает конфигурацию и не соединяется с target.
func TestImageOwnerProfileIsExplicit(t *testing.T) {
	t.Setenv("KODEX_RPC_PROFILE", transportprofile.TrustedCluster)
	t.Setenv("IMAGE_OWNER_CONTROL_PLANE_TARGET", "control-plane.kodex-system.svc:8443")
	for _, name := range []string{"IMAGE_OWNER_CONTROL_PLANE_TLS_SERVER_NAME", "IMAGE_OWNER_CONTROL_PLANE_CA_FILE",
		"IMAGE_OWNER_CONTROL_PLANE_CERTIFICATE_FILE", "IMAGE_OWNER_CONTROL_PLANE_PRIVATE_KEY_FILE", "IMAGE_OWNER_APPLICATION_GRANT_FILE"} {
		t.Setenv(name, "")
	}
	for _, promotion := range []bool{false, true} {
		config, err := clientConfig(promotion)
		if err != nil || config.RPCProfile != transportprofile.TrustedCluster || config.Promotion != promotion || config.ClientPrivateKeyFile != "" {
			t.Fatal("trusted image owner configuration is incomplete")
		}
	}
	for _, profile := range []string{"", "insecure"} {
		t.Setenv("KODEX_RPC_PROFILE", profile)
		if _, err := clientConfig(false); err == nil {
			t.Fatal("implicit trusted profile or missing protected credentials accepted")
		}
	}
}
