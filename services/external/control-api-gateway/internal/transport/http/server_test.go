package httptransport

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestUIOwnershipIsAssignedServerSide(t *testing.T) {
	ownership := uiOwnership()
	if ownership.GetManagedBy() != controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_UI ||
		ownership.GetSourceRef() != "" || ownership.GetSourceRevision() != 0 {
		t.Fatalf("unexpected UI ownership: %#v", ownership)
	}
}
