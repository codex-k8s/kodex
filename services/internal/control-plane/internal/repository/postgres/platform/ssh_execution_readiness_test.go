package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestSSHExecutionReadinessRejectsMissingOwnerSource verifies absent configuration or catalog cannot reach SQL even when a caller requests mixed ordinary capacity; nil pool makes an unexpected write fail this test.
// TestSSHExecutionReadinessRejectsMissingOwnerSource проверяет отказ до SQL при absent configuration либо catalog, в том числе для mixed ordinary capacity; nil pool обнаруживает неожиданную запись.
func TestSSHExecutionReadinessRejectsMissingOwnerSource(t *testing.T) {
	for _, configured := range []bool{false, true} {
		repository := &Repository{}
		if configured {
			if err := repository.ConfigureSSHIdentitySetup(value.SSHIdentityUploadEndpoint{Origin: "https://ssh-upload.example.invalid", TLSServerName: "ssh-upload.example.invalid"}); err != nil {
				t.Fatal(err)
			}
		}
		ready, err := repository.sshExecutionDeliveryReady(t.Context())
		if err != nil || ready {
			t.Fatal("missing owner configuration or package became ready")
		}
		claims, err := repository.ClaimIntegrationInvocationsWithCapacity(t.Context(), value.Principal{CallerWorkload: "integration-gateway", Permission: "platform.runtime.integrations.claim"}, "ssh-source-gateway", 2, value.IntegrationInvocationDeliveryCapacity{Ordinary: 1, SSH: 1})
		if !errors.Is(err, errs.ErrUnavailable) || claims != nil {
			t.Fatal("unavailable SSH source admitted mixed claim writes")
		}
	}
}
